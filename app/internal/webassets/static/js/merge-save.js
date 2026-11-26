// Merge at save (the edit model, docs/catalogue-edit-review.md §4.6;
// httpapi/merge_at_save.go). A chart saved over a version someone else
// moved on from is merged by the server: different parts silently (the
// answer says who saved meanwhile, X-Merged-With: the page reloads with
// both), the same part asked once here (409 with "conflicts": theirs or
// yours, each), then sent again with the choice (X-Merge-Resolve).
//
// It wraps window.fetch for this song's chart saves only (PUT
// /api/songs/{id}, /raw, /api/variants/{id}), so song.js's save code stays
// as it is; anything else goes through untouched.
(function () {
  const editor = document.getElementById('editor');
  const songId = Number(editor?.dataset.variantId) || 0;
  const dialog = document.getElementById('merge-dialog');
  if (!songId || !dialog) return;
  const paths = new Set([`/api/songs/${songId}`, `/api/songs/${songId}/raw`, `/api/variants/${songId}`]);
  const list = document.getElementById('merge-conflicts');
  const text = document.getElementById('merge-text');
  const merged = document.getElementById('merged-dialog');
  dialog.addEventListener('keydown', (e) => e.stopPropagation());
  merged?.addEventListener('keydown', (e) => e.stopPropagation());

  // Asks theirs or yours for each part; resolves to "key=mine;…" or null.
  function ask(data) {
    text.textContent = data.error || '';
    list.replaceChildren(...data.conflicts.map((c, i) => {
      const fs = document.createElement('fieldset');
      fs.className = 'merge-conflict';
      const lg = document.createElement('legend');
      lg.textContent = c.name;
      fs.append(lg);
      [['theirs', t('Their edit'), c.theirs], ['mine', t('Your edit'), c.mine]].forEach(([v, label, body], j) => {
        const l = document.createElement('label');
        const r = document.createElement('input');
        r.type = 'radio'; r.name = `merge-${i}`; r.value = v; r.dataset.key = c.key;
        if (j === 1) r.checked = true;
        const s = document.createElement('strong'); s.textContent = label;
        const pre = document.createElement('pre'); pre.textContent = body || t('(removed)');
        l.append(r, ' ', s, pre);
        fs.append(l);
      });
      return fs;
    }));
    return new Promise((resolve) => {
      let answer = null;
      let settled = false;
      const settle = () => { if (!settled) { settled = true; resolve(answer); } };
      dialog.querySelector('button[value=cancel]').onclick = (e) => { e.preventDefault(); dialog.close(); settle(); };
      dialog.addEventListener('cancel', settle, { once: true });
      dialog.addEventListener('close', settle, { once: true });
      document.getElementById('merge-go').onclick = () => {
        answer = [...list.querySelectorAll('input[type=radio]:checked')].map((r) => `${r.dataset.key}=${r.value}`).join(';');
        dialog.close();
        settle();
      };
      dialog.showModal();
    });
  }

  const prev = window.fetch;
  window.fetch = async function (input, init) {
    const url = typeof input === 'string' ? input : input?.url;
    const method = String(init?.method || 'GET').toUpperCase();
    let path = '';
    try { path = url ? new URL(url, location.href).pathname : ''; } catch (e) { path = ''; }
    if (method !== 'PUT' || !paths.has(path)) return prev.call(window, input, init);
    let res = await prev.call(window, input, init);
    if (res.status === 409) {
      const data = await res.clone().json().catch(() => ({}));
      if (data && Array.isArray(data.conflicts) && data.conflicts.length) {
        const answer = await ask(data);
        if (answer == null) return new Response(JSON.stringify({ error: t('Not saved.') }), { status: 400, headers: { 'Content-Type': 'application/json' } });
        const headers = new Headers(init?.headers || {});
        headers.set('X-Merge-Resolve', encodeURIComponent(answer));
        res = await prev.call(window, input, { ...init, headers });
      }
    }
    const who = res.headers.get('X-Merged-With');
    if (res.ok && who) {
      // Saved with someone else's changes: the editor's text hasn't them,
      // so the page reloads with both (the editor open again).
      window.songbooksEditorClean?.();
      const names = who === '1' ? '' : decodeURIComponent(who.replace(/\+/g, ' '));
      document.getElementById('merged-text').textContent = names
        ? t('%s saved while you were editing: both kept.', names)
        : t('Someone saved while you were editing: both kept.');
      const go = () => { const u = new URL(location.href); u.searchParams.set('edit', '1'); location.href = u.toString(); };
      if (merged) {
        merged.addEventListener('close', go, { once: true });
        merged.showModal();
      } else go();
    }
    return res;
  };
})();

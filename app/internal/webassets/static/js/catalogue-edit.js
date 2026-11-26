// The catalogue: open edits, on The Big Songbook's song page
// (catalogue_edit.html, httpapi/catalogue_edits.go). Anyone signed in
// edits the song; the server weighs each save against its recording:
// applied at once (200, as for any song), waiting for review (202: the
// song stays as it is), another recording's new song (201), or a
// transpose refused (422). A new reference, title or artist is asked
// about first (428): a fix of this song, or another recording.
//
// The editor's saves (song.js: the Chart, Source and the reference link)
// go through window.fetch; this wraps it for this song's chart saves
// only, so song.js's own save code stays as it is: the one-line summary
// the editor asks for goes along (X-Edit-Summary), and a 202 or a 201 is
// said in #catalogue-saved-dialog, then the page shows the song as it is
// (202) or opens the new version (201). A plain 200 goes back to song.js
// untouched.
(function () {
  const editor = document.getElementById('editor');
  const songId = Number(editor?.dataset.variantId) || 0;
  if (!songId) return;
  const summaryInput = document.getElementById('catalogue-edit-summary');
  const dialog = document.getElementById('catalogue-saved-dialog');
  const chartPaths = new Set([`/api/songs/${songId}`, `/api/songs/${songId}/raw`, `/api/songs/${songId}/meta`, `/api/variants/${songId}`]);

  function say(title, text, reasons, link, then) {
    if (!dialog) { then?.(); return; }
    document.getElementById('catalogue-saved-title').textContent = title;
    document.getElementById('catalogue-saved-text').textContent = text || '';
    const why = document.getElementById('catalogue-saved-why');
    why.replaceChildren(...(reasons || []).map((r) => { const li = document.createElement('li'); li.textContent = r; return li; }));
    why.hidden = !(reasons && reasons.length);
    const a = document.getElementById('catalogue-saved-link');
    a.hidden = !link;
    if (link) { a.href = link.href; a.textContent = link.label; }
    // 'close' doesn't always fire where the page runs hidden: OK goes on
    // by itself too, once.
    let done = false;
    const go = () => { if (!done) { done = true; then?.(); } };
    dialog.addEventListener('close', go, { once: true });
    dialog.querySelector('button[value=ok]').onclick = () => { dialog.close(); go(); };
    if (!dialog.open) dialog.showModal();
  }

  // The page as it is, the editor closed.
  function showSong() {
    const u = new URL(location.href);
    u.searchParams.delete('edit');
    location.href = u.toString();
  }

  dialog?.addEventListener('keydown', (e) => e.stopPropagation());

  const nativeFetch = window.fetch.bind(window);

  // A new reference, title or artist (the server answers 428): a fix of
  // this song, or another recording of it — another song of The Big
  // Songbook. Resolves to the headers saying which, or null (cancelled).
  const choice = document.getElementById('catalogue-choice-dialog');
  choice?.addEventListener('keydown', (e) => e.stopPropagation());
  function choose(data) {
    if (!choice) return Promise.resolve({ 'X-Edit-Kind': 'fix' });
    const form = document.getElementById('catalogue-choice-form');
    const rec = document.getElementById('catalogue-choice-recording');
    const refField = document.getElementById('catalogue-choice-ref-field');
    const ref = document.getElementById('catalogue-choice-ref');
    const label = document.getElementById('catalogue-choice-label');
    const status = document.getElementById('catalogue-choice-status');
    document.getElementById('catalogue-choice-text').textContent = data.referenceChanged
      ? t('You changed the reference video. Is it the same recording, or another one?')
      : t('You changed the title or the artist. Is it a fix of this song, or another recording of it?');
    refField.hidden = !!data.referenceChanged;
    ref.required = !data.referenceChanged;
    label.value = '';
    // The new link's title, cleaned of its wrapping and of what repeats
    // the song (the server's reftitle.Label): the label's suggestion,
    // when it comes.
    const suggest = (link) => {
      if (!link) return;
      const q = new URLSearchParams({ url: link, title: data.title || '', artist: data.artist || '' });
      nativeFetch('/api/reference-title?' + q).then((r) => r.ok ? r.json() : {}).then((d) => {
        const v = d && ('label' in d ? d.label : d.title);
        if (v && !label.value.trim()) label.value = v;
      }).catch(() => {});
    };
    suggest(data.newReference);
    ref.value = '';
    ref.onchange = () => suggest(ref.value.trim());
    form.elements.kind.value = 'fix';
    rec.hidden = true;
    Forms.status(status, '');
    const sync = () => { rec.hidden = form.elements.kind.value !== 'recording'; };
    form.querySelectorAll('input[name=kind]').forEach((r) => { r.onchange = sync; });
    return new Promise((resolve) => {
      let answer = null;
      let settled = false;
      // 'close' doesn't always fire where the page runs hidden: the
      // buttons settle it themselves too, once.
      const settle = () => { if (!settled) { settled = true; resolve(answer); } };
      form.querySelector('button[value=cancel]').onclick = (e) => { e.preventDefault(); choice.close(); settle(); };
      choice.addEventListener('cancel', settle, { once: true });
      document.getElementById('catalogue-choice-go').onclick = () => {
        if (form.elements.kind.value === 'recording' && !data.referenceChanged && !ref.value.trim()) {
          Forms.status(status, t('Give the link to the other recording’s video.'), 'error');
          ref.focus();
          return;
        }
        answer = { 'X-Edit-Kind': form.elements.kind.value };
        if (form.elements.kind.value === 'recording') {
          answer['X-Reference-Label'] = encodeURIComponent(label.value.trim());
          if (!data.referenceChanged) answer['X-New-Reference'] = encodeURIComponent(ref.value.trim());
        }
        choice.close();
        settle();
      };
      choice.addEventListener('close', settle, { once: true });
      choice.showModal();
    });
  }

  // A band member changing the sections (428 structureHint): the song
  // changes for everyone, the band included. Save for everyone, or the
  // band's arrangement (its dialog, edit_model.html), or not at all.
  const structure = document.getElementById('catalogue-structure-dialog');
  structure?.addEventListener('keydown', (e) => e.stopPropagation());
  function confirmStructure(data) {
    if (!structure) return Promise.resolve({ 'X-Structure-Confirmed': '1' });
    document.getElementById('catalogue-structure-text').textContent = data.message || '';
    const arrange = document.getElementById('catalogue-structure-arrange');
    const bandOffer = document.querySelector('.arrangement-dialog [data-arrangement-go][data-action="band"]')?.closest('dialog');
    arrange.hidden = !bandOffer;
    return new Promise((resolve) => {
      let answer = null;
      let settled = false;
      const settle = () => { if (!settled) { settled = true; resolve(answer); } };
      structure.querySelector('button[value=cancel]').onclick = (e) => { e.preventDefault(); structure.close(); settle(); };
      structure.addEventListener('cancel', settle, { once: true });
      structure.addEventListener('close', settle, { once: true });
      document.getElementById('catalogue-structure-go').onclick = () => {
        answer = { 'X-Structure-Confirmed': '1' };
        structure.close();
        settle();
      };
      arrange.onclick = () => {
        structure.close();
        settle();
        if (bandOffer && !bandOffer.open) bandOffer.showModal();
      };
      structure.showModal();
    });
  }

  // Saved (200): for everyone who plays it, at once — Undo (its author
  // reverts their edit while it's the latest) and History, in the note.
  const savedLineEl = document.getElementById('catalogue-saved-line');
  function savedLine(data) {
    if (!savedLineEl || !data || !data.revisionId) return;
    savedLineEl.querySelector('[data-saved-text]').textContent = data.saved || t('Saved.');
    const undo = savedLineEl.querySelector('[data-saved-undo]');
    undo.hidden = false;
    undo.onclick = async () => {
      Forms.busy(undo, true);
      const res = await nativeFetch(`/api/revisions/${data.revisionId}/revert`, { method: 'POST' }).catch(() => null);
      Forms.busy(undo, false);
      if (!res || !res.ok) {
        savedLineEl.querySelector('[data-saved-text]').textContent = t('Couldn’t undo it: someone edited the song since. Open History.');
        undo.hidden = true;
        return;
      }
      window.songbooksEditorClean?.();
      location.reload();
    };
    savedLineEl.hidden = false;
  }

  window.fetch = async function (input, init) {
    const url = typeof input === 'string' ? input : input?.url;
    const method = String(init?.method || 'GET').toUpperCase();
    let path = '';
    try { path = url ? new URL(url, location.href).pathname : ''; } catch (e) { path = ''; }
    if (method !== 'PUT' || !chartPaths.has(path)) return nativeFetch(input, init);
    const headers = new Headers(init?.headers || {});
    const summary = summaryInput?.value.trim();
    if (summary) headers.set('X-Edit-Summary', encodeURIComponent(summary));
    let res = await nativeFetch(input, { ...init, headers });
    // Asked first (428), each question once: a new reference, title or
    // artist (a fix, or another recording?); a band member changing the
    // sections (for everyone, or the band's arrangement?).
    for (let asked = 0; res.status === 428 && asked < 3; asked++) {
      const data = await res.clone().json().catch(() => ({}));
      const answer = data.structureHint ? await confirmStructure(data) : await choose(data);
      if (!answer) return new Response(JSON.stringify({ error: t('Not saved.') }), { status: 400, headers: { 'Content-Type': 'application/json' } });
      Object.entries(answer).forEach(([k, v]) => headers.set(k, v));
      res = await nativeFetch(input, { ...init, headers });
    }
    if (res.status !== 201 && res.status !== 202 && res.status !== 422) {
      if (res.ok) {
        if (summaryInput) summaryInput.value = '';
        res.clone().json().then(savedLine).catch(() => {});
      }
      return res;
    }
    const data = await res.clone().json().catch(() => ({}));
    if (res.status === 422) {
      say(t('Not saved'), data.error || '', null, null, null);
      return res;
    }
    // Saved, as a pending edit or a new song: nothing unsaved here now.
    window.songbooksEditorClean?.();
    if (summaryInput) summaryInput.value = '';
    if (res.status === 202) {
      say(data.suggested ? t('Suggested') : t('Sent for review'), data.message, data.reasons, data.url ? { href: data.url, label: t('See your edit') } : null, showSong);
    } else {
      say(t('A new song for this recording'), data.message, null, null, () => { location.href = data.url; });
    }
    // The caller waits: the page moves on once the dialog is closed.
    return new Promise(() => {});
  };

  // The viewer's suggestion, shown in place (catalogue_suggestions.go):
  // the sections it changes are marked "suggested".
  const sugg = document.getElementById('catalogue-suggestion');
  const marked = (sugg?.dataset.suggestedSections || '').split(',').filter(Boolean);
  marked.forEach((i) => {
    const sec = document.getElementById(`section-${i}`);
    if (!sec) return;
    sec.classList.add('is-suggested');
    sec.dataset.suggestedLabel = t('suggested');
  });

  // The reference label (the Reference view): shown after the artist.
  const labelForm = document.getElementById('catalogue-label-form');
  labelForm?.addEventListener('submit', (e) => {
    e.preventDefault();
    const btn = labelForm.querySelector('button[type=submit]');
    Forms.run(labelForm, btn, async () => {
      const res = await nativeFetch(`/api/variants/${songId}/reference-label`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label: document.getElementById('catalogue-label-input').value }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || t('Couldn’t save the label. Try again.'));
      const head = document.querySelector('.topbar-artist');
      if (head) {
        head.querySelector('.topbar-label')?.remove();
        if (data.label) {
          const span = document.createElement('span');
          span.className = 'topbar-label';
          span.textContent = ' · ' + data.label;
          head.appendChild(span);
        }
      }
      Forms.status(labelForm, t('Saved.'), 'ok');
    });
  });

  // Report… (the ⋯ menu, the phone's Song sheet).
  const reportDialog = document.getElementById('catalogue-report-dialog');
  if (reportDialog) {
    const reason = document.getElementById('catalogue-report-reason');
    const status = document.getElementById('catalogue-report-status');
    const send = document.getElementById('catalogue-report-send');
    document.querySelectorAll('[data-catalogue-report]').forEach((btn) => btn.addEventListener('click', () => {
      Forms.status(status, '');
      send.disabled = false;
      if (!reportDialog.open) reportDialog.showModal();
      reason.focus();
    }));
    // Its keys stay in it (song.js plays on Space, steps sections on ↑/↓).
    reportDialog.addEventListener('keydown', (e) => e.stopPropagation());
    send.addEventListener('click', async () => {
      if (!reason.value.trim()) { Forms.status(status, t('Say what’s wrong with the song.'), 'error'); reason.focus(); return; }
      Forms.busy(send, true);
      const res = await nativeFetch(`/api/big-songbook/songs/${songId}/report`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason: reason.value }),
      }).catch(() => null);
      const data = res ? await res.json().catch(() => ({})) : {};
      Forms.busy(send, false);
      if (!res || !res.ok) { Forms.status(status, data.error || t("Couldn't send the report. Try again."), 'error'); return; }
      reason.value = '';
      send.disabled = true;
      Forms.status(status, data.message || t('Reported.'), 'ok');
      setTimeout(() => reportDialog.close(), 1500);
    });
  }
})();

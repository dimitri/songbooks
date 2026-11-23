// The catalogue: open edits, on The Big Songbook's song page
// (catalogue_edit.html, httpapi/catalogue_edits.go). Anyone signed in
// edits the song; the server weighs each save against its recording:
// applied at once (200, as for any song), waiting for review (202: the
// song stays as it is), another recording's new version (201), or a
// transpose refused (422).
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
    dialog.addEventListener('close', () => then?.(), { once: true });
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
  window.fetch = async function (input, init) {
    const url = typeof input === 'string' ? input : input?.url;
    const method = String(init?.method || 'GET').toUpperCase();
    let path = '';
    try { path = url ? new URL(url, location.href).pathname : ''; } catch (e) { path = ''; }
    if (method !== 'PUT' || !chartPaths.has(path)) return nativeFetch(input, init);
    const headers = new Headers(init?.headers || {});
    const summary = summaryInput?.value.trim();
    if (summary) headers.set('X-Edit-Summary', encodeURIComponent(summary));
    const res = await nativeFetch(input, { ...init, headers });
    if (res.status !== 201 && res.status !== 202 && res.status !== 422) {
      if (res.ok && summaryInput) summaryInput.value = '';
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
      say(t('Sent for review'), data.message, data.reasons, data.url ? { href: data.url, label: t('See your edit') } : null, showSong);
    } else {
      say(t('A new version for this recording'), data.message, null, null, () => { location.href = data.url; });
    }
    // The caller waits: the page moves on once the dialog is closed.
    return new Promise(() => {});
  };

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

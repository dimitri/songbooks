// Lyrics/chord import controls in prep — fetches land in the raw textarea
// as a draft the user edits/merges by hand; nothing here saves on its own.
(function () {
  const textarea = document.getElementById('raw-text');
  if (!textarea) return;

  function titleArtist() {
    const title = (textarea.value.match(/^title:\s*(.+)$/m) || [, ''])[1].trim();
    const artist = (textarea.value.match(/^artist:\s*(.+)$/m) || [, ''])[1].trim();
    return { title, artist };
  }
  const googleLink = document.getElementById('google-lyrics-link');
  function refreshGoogleLink() {
    const { title, artist } = titleArtist();
    const q = encodeURIComponent(`${artist} ${title} lyrics`.trim());
    googleLink.href = `https://www.google.com/search?q=${q}`;
  }
  refreshGoogleLink();
  textarea.addEventListener('blur', refreshGoogleLink);

  function insertAtCursor(text) {
    const start = textarea.selectionStart ?? textarea.value.length;
    const end = textarea.selectionEnd ?? textarea.value.length;
    textarea.value = textarea.value.slice(0, start) + text + textarea.value.slice(end);
    textarea.dispatchEvent(new Event('input'));
  }

  async function doImport(btn, statusEl, endpoint, urlInput, format) {
    const url = urlInput.value.trim();
    if (!url) return;
    btn.disabled = true;
    statusEl.textContent = 'fetching…';
    statusEl.className = '';
    try {
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'fetch failed');
      insertAtCursor('\n\n# imported from ' + url + '\n' + format(data) + '\n');
      statusEl.textContent = 'inserted below cursor — review and fold into sections';
      statusEl.className = 'ok';
    } catch (e) {
      statusEl.textContent = e.message;
      statusEl.className = 'err';
    } finally {
      btn.disabled = false;
    }
  }

  document.getElementById('import-lyrics-btn').addEventListener('click', () => doImport(
    document.getElementById('import-lyrics-btn'),
    document.getElementById('import-lyrics-status'),
    '/api/import/lyrics',
    document.getElementById('import-lyrics-url'),
    data => data.text,
  ));

  document.getElementById('import-ug-btn').addEventListener('click', () => doImport(
    document.getElementById('import-ug-btn'),
    document.getElementById('import-ug-status'),
    '/api/import/chords',
    document.getElementById('import-ug-url'),
    data => data.text,
  ));
})();

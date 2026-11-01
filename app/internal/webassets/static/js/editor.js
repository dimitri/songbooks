(function () {
  const el = document.getElementById('editor');
  if (!el) return; // not on a page with the raw editor panel
  const book = el.dataset.book;
  const stem = el.dataset.stem;
  let version = el.dataset.version;

  document.getElementById('save-btn').addEventListener('click', async () => {
    const status = document.getElementById('save-status');
    status.className = '';
    status.textContent = 'saving…';
    const text = document.getElementById('raw-text').value;
    const res = await fetch(`/api/songs/${book}/${stem}/raw`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version, text }),
    });
    if (res.status === 409) {
      status.className = 'err';
      status.textContent = 'conflict: this song changed on disk — reload before saving';
      return;
    }
    if (!res.ok) {
      status.className = 'err';
      status.textContent = 'save failed';
      return;
    }
    const data = await res.json();
    version = data.version;
    status.className = 'ok';
    status.textContent = 'saved';
  });
})();

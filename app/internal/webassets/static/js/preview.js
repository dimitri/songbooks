// Live chart preview in prep: debounced re-render of the raw textarea
// through the server's own sheet-body renderer, so the preview can never
// drift from what play mode will actually show.
(function () {
  const textarea = document.getElementById('raw-text');
  const preview = document.getElementById('preview-sheet');
  if (!textarea || !preview) return;

  let timer = null;
  async function renderPreview() {
    try {
      const res = await fetch('/api/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: textarea.value }),
      });
      preview.innerHTML = await res.text();
    } catch (e) {
      preview.innerHTML = '<p class="hint">preview unavailable</p>';
    }
  }

  textarea.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(renderPreview, 400);
  });
  renderPreview();
})();

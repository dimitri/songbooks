// The "Look & feel" form ("brand-editor" partial, icons.html): loads a
// community's or band's brand (internal/brand) from its endpoint, lets
// colors be picked or typed, and saves it as a whole. Saving reloads the
// page, which the server draws in the new look.
(function () {
  const root = document.getElementById('brand-editor');
  if (!root) return;
  const endpoint = root.dataset.endpoint;
  const status = document.getElementById('brand-status');
  const fields = () => root.querySelectorAll('[name]');

  function fill(b) {
    for (const el of fields()) el.value = b[el.name] || '';
    for (const picker of root.querySelectorAll('[data-color-for]')) {
      const text = root.querySelector(`[name="${picker.dataset.colorFor}"]`);
      picker.value = text.value || '#888888';
      picker.classList.toggle('unset', !text.value);
    }
  }

  root.addEventListener('input', (e) => {
    const picker = e.target.closest('[data-color-for]');
    if (picker) {
      root.querySelector(`[name="${picker.dataset.colorFor}"]`).value = picker.value;
      picker.classList.remove('unset');
    }
  });

  async function save(b) {
    status.textContent = 'saving…';
    const res = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b) });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      status.textContent = data.error || 'could not save';
      return;
    }
    location.reload();
  }

  document.getElementById('brand-save').addEventListener('click', () => {
    const b = {};
    for (const el of fields()) if (el.value.trim()) b[el.name] = el.value.trim();
    save(b);
  });
  document.getElementById('brand-reset').addEventListener('click', () => {
    if (confirm('Go back to the app’s own look?')) save({});
  });

  fetch(endpoint).then((r) => (r.ok ? r.json() : {})).then(fill).catch(() => {});
})();

// The "Look & feel" form ("brand-editor" partial, icons.html): loads a
// community's or band's brand (internal/brand) from its endpoint, lets
// colors be picked or typed, and saves it as a whole. Saving reloads the
// page, which the server draws in the new look.
(function () {
  const root = document.getElementById('brand-editor');
  if (!root) return;
  const endpoint = root.dataset.endpoint;
  const form = document.getElementById('brand-form');
  const fields = () => form.querySelectorAll('[name]');

  function fill(b) {
    for (const el of fields()) el.value = b[el.name] || '';
    for (const picker of form.querySelectorAll('[data-color-for]')) {
      const text = form.querySelector(`[name="${picker.dataset.colorFor}"]`);
      picker.value = text.value || '#888888';
      picker.classList.toggle('unset', !text.value);
    }
  }

  form.addEventListener('input', (e) => {
    const picker = e.target.closest('[data-color-for]');
    if (picker) {
      const text = form.querySelector(`[name="${picker.dataset.colorFor}"]`);
      text.value = picker.value;
      Forms.fieldError(text, '');
      picker.classList.remove('unset');
    }
  });

  async function save(b, btn) {
    await Forms.run(form, btn, async () => {
      const res = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b) });
      if (!res.ok) {
        throw new Error(res.status === 400
          ? 'Some of these settings aren’t valid. Check the colors and links, then try again.'
          : 'Couldn’t save the look. Try again.');
      }
      location.reload();
    });
  }

  // Submit runs only once the browser's checks pass (forms.js shows a bad
  // hex code or link under its field).
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    const b = {};
    for (const el of fields()) if (el.value.trim()) b[el.name] = el.value.trim();
    save(b, document.getElementById('brand-save'));
  });
  document.getElementById('brand-reset').addEventListener('click', (e) => {
    if (confirm('Reset to the app’s own look? This clears the preset, colors, fonts, logo, tagline and website for everyone here.')) save({}, e.currentTarget);
  });

  fetch(endpoint).then((r) => (r.ok ? r.json() : {})).then(fill).catch(() => {});
})();

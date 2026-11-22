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
          ? t('Some of these settings aren’t valid. Check the colors and links, then try again.')
          : t('Couldn’t save the look. Try again.'));
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
  document.getElementById('brand-reset').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    if (!confirm(t('Reset to the app’s own look? This clears the preset, colors, fonts, logo, tagline and website for everyone here.'))) return;
    if (logoImage) await fetch(endpoint + '/logo', { method: 'DELETE' }).catch(() => {});
    save({}, btn);
  });

  // The logo: an image uploaded to the app (kept as soon as it's
  // picked, PUT {endpoint}/logo), or else an https address saved with the
  // form. The preview shows the one the pages show.
  const logoFile = document.getElementById('brand-logo-file');
  const logoPreview = document.getElementById('brand-logo-preview');
  const logoUpload = document.getElementById('brand-logo-upload');
  const logoRemove = document.getElementById('brand-logo-remove');
  const logoStatus = document.getElementById('brand-logo-status');
  const logoURL = form.querySelector('[name="logoURL"]');
  let logoImage = '';

  function showLogo() {
    const url = logoURL.value.trim();
    const src = logoImage || (url.startsWith('https://') ? url : '');
    logoPreview.hidden = !src;
    if (src) logoPreview.src = src;
    else logoPreview.removeAttribute('src');
    logoRemove.hidden = !logoImage;
    logoUpload.textContent = logoImage ? t('Replace the image') : t('Upload an image');
  }
  function setLogo(src) {
    logoImage = src || '';
    showLogo();
  }
  // Its own status line, not the form's (Forms.run writes to the first
  // .form-status in the form).
  function logoMsg(msg, kind) {
    logoStatus.textContent = msg || '';
    logoStatus.classList.toggle('brand-logo-error', kind === 'error');
    logoStatus.classList.toggle('brand-logo-ok', kind === 'ok');
  }
  logoPreview.addEventListener('error', () => { logoPreview.hidden = true; });
  logoURL.addEventListener('input', showLogo);
  logoUpload.addEventListener('click', () => logoFile.click());

  logoFile.addEventListener('change', async () => {
    const f = logoFile.files[0];
    if (!f) return;
    logoFile.value = '';
    logoMsg('');
    if (/svg/i.test(f.type) || /\.svg$/i.test(f.name)) {
      logoMsg(t('SVG images can’t be used: export the logo as a PNG, then upload that.'), 'error');
      return;
    }
    // Too big, or a format the server doesn't take (HEIC, GIF…): made to
    // fit here (image-shrink.js), not sent back to the user to resize.
    let up = f;
    if (window.ImageShrink) {
      try { up = await ImageShrink.fit(f); } catch (e) { up = f; }
    }
    const body = new FormData();
    body.append('file', up);
    Forms.busy(logoUpload, true);
    const res = await fetch(endpoint + '/logo', { method: 'PUT', body }).catch(() => null);
    Forms.busy(logoUpload, false);
    if (!res || !res.ok) {
      const why = !res ? '' : res.status === 413
        ? t('This image is larger than 2 MB. Make it smaller, then try again.')
        : res.status === 415 ? t('This file isn’t an image the app can use. Upload a PNG, JPEG or WebP image.') : '';
      logoMsg(why || t('Couldn’t upload the image. Try again.'), 'error');
      return;
    }
    const b = await res.json();
    setLogo(b.logoImage);
    logoMsg(t('Logo saved: it shows on this space’s pages now.'), 'ok');
  });

  logoRemove.addEventListener('click', async () => {
    if (!confirm(t('Remove the uploaded logo? Everyone here stops seeing it.'))) return;
    logoMsg('');
    Forms.busy(logoRemove, true);
    const res = await fetch(endpoint + '/logo', { method: 'DELETE' }).catch(() => null);
    Forms.busy(logoRemove, false);
    if (!res || !res.ok) {
      logoMsg(t('Couldn’t remove the image. Try again.'), 'error');
      return;
    }
    setLogo('');
    logoMsg(t('Logo removed.'), 'ok');
  });

  // A look made from the songs' covers: it fills the colors, fonts and
  // buttons, keeps the logo, tagline and website; nothing is saved yet.
  document.getElementById('brand-from-songs')?.addEventListener('click', async (e) => {
    const out = document.getElementById('brand-from-songs-status');
    const btn = e.currentTarget;
    Forms.busy(btn, true);
    const res = await fetch(endpoint + '/from-songs').catch(() => null);
    Forms.busy(btn, false);
    const d = res && res.ok ? await res.json() : null;
    if (!d || !d.covers) { out.textContent = t('No covers found for the songs yet.'); return; }
    const keep = {};
    for (const k of ['logoURL', 'tagline', 'website', 'mark']) keep[k] = form.querySelector(`[name="${k}"]`)?.value;
    fill({...d.brand, ...keep, preset: ''});
    showLogo();
    out.textContent = t('Made from %d covers of %d songs (%s). Save to use it.', d.covers, d.songs, t(d.mood) + (d.genres && d.genres.length ? ', ' + d.genres.slice(0, 3).join(', ') : ''));
  });

  fetch(endpoint).then((r) => (r.ok ? r.json() : {})).then((b) => { fill(b); setLogo(b.logoImage); }).catch(() => {});
})();

// A band's Look (band_settings.html, the Look section): one logo, what
// the band's card shows on the home page, its style (a preset's swatch,
// or one made from its songs' covers), the fine-tuning folded away — and
// beside them, the card and the band's header as they will look. One
// Save for all of it: the logo (PUT/DELETE {endpoint}/brand/logo), the
// look ({endpoint}/brand, which keeps the tagline and website: The
// band section's), the card's picture ({endpoint}/card-image); a new
// look reloads the page, drawn in it. The card follows the home page's
// rules (home.go choosePicture, store.CardMosaic).
(function () {
  const form = document.getElementById('look-form');
  if (!form) return;
  const t = window.t || ((m) => m);
  const $ = (id) => document.getElementById(id);
  const endpoint = form.dataset.endpoint;
  const saveBtn = $('look-save');
  const changesEl = form.querySelector('[data-changes]');
  const pic = $('card-image-pic');
  const picker = $('card-image-picker');
  const coverList = $('card-image-covers');
  const none = $('card-image-none');
  const logoImg = $('look-logo-img');
  const logoFile = $('look-logo-file');
  const logoUpload = $('look-logo-upload');
  const logoRemove = $('look-logo-remove');
  const logoStatus = $('look-logo-status');
  const head = $('look-preview-head');
  const markEl = $('look-preview-mark');
  let fonts = {};
  try { fonts = JSON.parse($('look-fonts').textContent) || {}; } catch (e) { fonts = {}; }

  // The look's fields: every [name] of the form but the card's mode.
  const brandFields = () => [...form.querySelectorAll('[name]')].filter((el) => el.name !== 'mode');
  let brand = {}; // as saved
  let card = null; // the card-image state, as saved
  let picked = [];
  let logo = ''; // the uploaded logo's address, as saved
  let pendingLogo = null; // {file, url} to upload, or 'remove'
  let savedLook = '';
  let savedCard = '';

  function collect() {
    const b = {};
    for (const el of brandFields()) {
      if (el.type === 'radio' && !el.checked) continue;
      const v = el.value.trim();
      if (v) b[el.name] = v;
    }
    return b;
  }
  function fill(b) {
    for (const el of brandFields()) {
      if (el.type === 'radio') el.checked = el.value === (b[el.name] || '');
      else el.value = b[el.name] || '';
    }
    for (const p of form.querySelectorAll('[data-color-for]')) {
      const text = form.querySelector(`[name="${p.dataset.colorFor}"]`);
      p.value = text.value || '#888888';
      p.classList.toggle('unset', !text.value);
    }
  }
  const mode = () => form.querySelector('input[name="mode"]:checked')?.value ?? '';
  const cardValue = () => JSON.stringify({ mode: mode(), works: mode() === 'picked' ? picked : [] });

  // What changed, counted as the person sees it: the logo, the card's
  // picture, the style (a swatch, its fine-tuning), the mark.
  function changes() {
    const now = collect();
    const was = JSON.parse(savedLook || '{}');
    const differs = (k) => (now[k] || '') !== (was[k] || '');
    const keys = new Set([...Object.keys(now), ...Object.keys(was)]);
    let n = 0;
    if (pendingLogo || differs('logoURL')) n++;
    if (card && cardValue() !== savedCard) n++;
    if ([...keys].some((k) => k !== 'logoURL' && k !== 'mark' && differs(k))) n++;
    if (differs('mark')) n++;
    return n;
  }
  function mark() {
    const n = changes();
    saveBtn.disabled = n === 0;
    changesEl.textContent = n ? tn(n, '%d change', '%d changes') : t('No changes');
  }

  // ---- The preview ----
  const preset = () => form.querySelector('input[name="preset"]:checked');
  // A field's value, else its style's (the swatch picked), else ''.
  function eff(name, dataKey) {
    const v = form.querySelector(`[name="${name}"]`)?.value.trim();
    if (v) return v;
    return preset()?.dataset[dataKey || name] || '';
  }
  const effLogo = () => {
    if (pendingLogo && pendingLogo.url) return pendingLogo.url;
    const url = form.querySelector('[name="logoURL"]').value.trim();
    const typed = url.startsWith('https://') ? url : '';
    if (pendingLogo === 'remove') return typed;
    return logo || typed;
  };
  // Black or white, whichever reads on hex (brand.ContrastOn's rule).
  function contrastOn(hex) {
    const m = /^#([0-9a-f]{6})$/i.exec(hex || '');
    if (!m) return '';
    const n = parseInt(m[1], 16);
    const lin = (c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4); };
    const L = 0.2126 * lin((n >> 16) & 255) + 0.7152 * lin((n >> 8) & 255) + 0.0722 * lin(n & 255);
    return (1.05 / (L + 0.05)) >= ((L + 0.05) / 0.05) ? '#ffffff' : '#000000';
  }
  const loaded = new Set();
  function fontStack(key) {
    const f = fonts[key];
    if (!f) return '';
    if (f.google && !loaded.has(key)) {
      loaded.add(key);
      const l = document.createElement('link');
      l.rel = 'stylesheet';
      l.href = 'https://fonts.googleapis.com/css2?family=' + f.google + '&display=swap';
      document.head.append(l);
    }
    return f.stack;
  }
  const square = (have, max) => [9, 4, 1].find((n) => n <= max && have >= n) || 0;
  function mosaic(m) {
    if (m === 'picked' && picked.length) {
      const byWork = new Map(card.choices.map((c) => [c.workId, c]));
      const list = picked.map((w) => byWork.get(w)).filter(Boolean);
      return list.slice(0, square(list.length, card.maxPicked));
    }
    return card.choices.slice(0, square(card.choices.length, card.cardMosaic));
  }
  function cardPicture() {
    const m = mode();
    const lg = effLogo();
    if (m === 'logo') return { logo: lg };
    if (m === 'mosaic' || m === 'picked') {
      const covers = mosaic(m);
      return covers.length ? { covers } : { logo: lg };
    }
    if (lg) return { logo: lg };
    return { covers: mosaic('') };
  }
  function drawCard() {
    if (!card) return;
    const p = cardPicture();
    pic.replaceChildren();
    pic.className = 'home-pic group-pic';
    pic.style.cssText = '';
    const tileBg = eff('header') || eff('accent') || card.tileBg;
    if (tileBg) { pic.style.setProperty('--tile-bg', tileBg); pic.style.setProperty('--tile-fg', contrastOn(tileBg) || card.tileFg); }
    if (p.logo) {
      pic.classList.add('has-logo');
      const img = document.createElement('img');
      img.src = p.logo;
      img.alt = '';
      pic.append(img);
    } else if (p.covers && p.covers.length) {
      pic.classList.add('m' + p.covers.length);
      for (const c of p.covers) {
        const img = document.createElement('img');
        img.src = c.image;
        img.alt = '';
        img.referrerPolicy = 'no-referrer';
        pic.append(img);
      }
    } else {
      pic.classList.add('has-mark');
      const span = document.createElement('span');
      span.className = 'home-pic-mark';
      span.textContent = eff('mark') || card.mark;
      pic.append(span);
    }
    const m = mode();
    picker.hidden = m !== 'picked' || !card.choices.length;
    none.hidden = card.choices.length > 0 || (m !== 'mosaic' && m !== 'picked');
    for (const li of coverList.children) {
      const n = picked.indexOf(Number(li.dataset.work));
      li.querySelector('button').setAttribute('aria-pressed', n >= 0 ? 'true' : 'false');
      li.querySelector('.card-image-order').textContent = n >= 0 ? String(n + 1) : '';
    }
  }
  function drawHead() {
    const s = head.style;
    const header = eff('header');
    const accent = eff('accent');
    s.setProperty('--pv-bg', header || '');
    s.setProperty('--pv-fg', contrastOn(header) || '');
    s.setProperty('--pv-accent', accent || '');
    s.setProperty('--pv-accent-fg', contrastOn(accent) || '');
    s.setProperty('--pv-font', fontStack(eff('fontDisplay', 'fontDisplay')) || '');
    s.setProperty('--pv-font-ui', fontStack(eff('fontUI', 'fontUi')) || '');
    head.dataset.shape = eff('shape') || '';
    const lg = effLogo();
    markEl.replaceChildren();
    if (lg) {
      const img = document.createElement('img');
      img.src = lg;
      img.alt = '';
      markEl.append(img);
    } else {
      markEl.textContent = eff('mark') || (card && card.mark) || markEl.dataset.initials || '';
    }
  }
  function drawLogo() {
    const src = effLogo();
    logoImg.hidden = !src;
    if (src) logoImg.src = src; else logoImg.removeAttribute('src');
    const uploaded = (logo && pendingLogo !== 'remove') || (pendingLogo && pendingLogo.url);
    logoRemove.hidden = !uploaded;
    logoUpload.textContent = uploaded ? t('Replace the logo') : t('Upload a logo');
  }
  function draw() { drawLogo(); drawCard(); drawHead(); mark(); }
  markEl.dataset.initials = markEl.textContent;

  // ---- The fields ----
  form.addEventListener('input', (e) => {
    const p = e.target.closest('[data-color-for]');
    if (p) {
      const text = form.querySelector(`[name="${p.dataset.colorFor}"]`);
      text.value = p.value;
      Forms.fieldError(text, '');
      p.classList.remove('unset');
    }
    draw();
  });
  form.addEventListener('change', (e) => {
    // A style is a whole look: picking one starts from it, Fine-tune's
    // colours, fonts and buttons emptied (the mark stays).
    if (e.target.name === 'preset') {
      for (const el of form.querySelectorAll('#look-fine [name]')) if (el.name !== 'mark') el.value = '';
      for (const p of form.querySelectorAll('[data-color-for]')) { p.value = '#888888'; p.classList.add('unset'); }
    }
    draw();
  });
  logoImg.addEventListener('error', () => { logoImg.hidden = true; });

  coverList.addEventListener('click', (e) => {
    const li = e.target.closest('li[data-work]');
    if (!li || !card) return;
    const w = Number(li.dataset.work);
    const n = picked.indexOf(w);
    if (n >= 0) picked.splice(n, 1);
    else if (picked.length < card.maxPicked) picked.push(w);
    draw();
  });
  function fillCovers() {
    coverList.replaceChildren();
    for (const c of card.choices) {
      const li = document.createElement('li');
      li.dataset.work = c.workId;
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'card-image-cover';
      btn.title = c.title + (c.artist ? ' — ' + c.artist : '') + (c.album ? ' (' + c.album + ')' : '');
      btn.setAttribute('aria-label', btn.title);
      const img = document.createElement('img');
      img.src = c.image;
      img.alt = '';
      img.loading = 'lazy';
      img.referrerPolicy = 'no-referrer';
      const order = document.createElement('span');
      order.className = 'card-image-order';
      order.setAttribute('aria-hidden', 'true');
      btn.append(img, order);
      li.append(btn);
      coverList.append(li);
    }
  }

  // The logo is kept on Save, like the rest; picking one shows it at once.
  logoUpload.addEventListener('click', () => logoFile.click());
  logoFile.addEventListener('change', async () => {
    const f = logoFile.files[0];
    if (!f) return;
    logoFile.value = '';
    logoStatus.textContent = '';
    if (/svg/i.test(f.type) || /\.svg$/i.test(f.name)) {
      logoStatus.textContent = t('SVG images can’t be used: export the logo as a PNG, then upload that.');
      return;
    }
    // Too big, or a format the server doesn't take (HEIC, GIF…): made to
    // fit here (image-shrink.js), not sent back to the user to resize.
    let up = f;
    if (window.ImageShrink) {
      try { up = await ImageShrink.fit(f); } catch (e) { up = f; }
    }
    if (pendingLogo && pendingLogo.url) URL.revokeObjectURL(pendingLogo.url);
    pendingLogo = { file: up, url: URL.createObjectURL(up) };
    draw();
  });
  logoRemove.addEventListener('click', () => {
    if (pendingLogo && pendingLogo.url) URL.revokeObjectURL(pendingLogo.url);
    pendingLogo = logo ? 'remove' : null;
    draw();
  });

  // A look made from the songs' covers: the colours, fonts and buttons,
  // in Fine-tune; the logo, mark and card stay. Nothing saved yet.
  $('look-from-songs').addEventListener('click', async (e) => {
    const out = $('look-from-songs-status');
    const btn = e.currentTarget;
    Forms.busy(btn, true);
    const res = await fetch(endpoint + '/brand/from-songs').catch(() => null);
    Forms.busy(btn, false);
    const d = res && res.ok ? await res.json() : null;
    if (!d || !d.covers) { out.textContent = t('No covers found for the songs yet.'); return; }
    const keep = {};
    for (const k of ['logoURL', 'mark']) keep[k] = form.querySelector(`[name="${k}"]`)?.value;
    fill({ ...d.brand, ...keep, preset: '' });
    $('look-fine').open = true;
    out.textContent = t('Made from %d covers of %d songs (%s). Save to use it.', d.covers, d.songs, t(d.mood) + (d.genres && d.genres.length ? ', ' + d.genres.slice(0, 3).join(', ') : ''));
    draw();
  });

  // ---- Saving ----
  async function call(url, opts, fallback) {
    const res = await fetch(url, opts).catch(() => null);
    if (res && res.ok) return res.json().catch(() => ({}));
    if (res && res.status === 413) throw new Error(t('This image is larger than 2 MB. Make it smaller, then try again.'));
    if (res && res.status === 415) throw new Error(t('This file isn’t an image the app can use. Upload a PNG, JPEG or WebP image.'));
    if (res && res.status === 400 && fallback.look) throw new Error(t('Some of these settings aren’t valid. Check the colors and links, then try again.'));
    throw new Error(fallback.msg);
  }
  const json = (body) => ({ method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    if (!Forms.validate(form)) return;
    if (mode() === 'picked' && !picked.length) {
      Forms.status(form, t('Pick at least one cover.'), 'error');
      return;
    }
    const look = collect();
    const lookChanged = JSON.stringify(look) !== savedLook;
    const cardChanged = card && cardValue() !== savedCard;
    const logoChanged = !!pendingLogo;
    Forms.run(form, saveBtn, async () => {
      if (pendingLogo === 'remove') {
        await call(endpoint + '/brand/logo', { method: 'DELETE' }, { msg: t('Couldn’t remove the image. Try again.') });
      } else if (pendingLogo) {
        const body = new FormData();
        body.append('file', pendingLogo.file);
        await call(endpoint + '/brand/logo', { method: 'PUT', body }, { msg: t('Couldn’t upload the image. Try again.') });
      }
      pendingLogo = null;
      if (lookChanged) await call(endpoint + '/brand', json(look), { msg: t('Couldn’t save the look. Try again.'), look: true });
      if (cardChanged) {
        const s = await call(endpoint + '/card-image', json(JSON.parse(cardValue())), { msg: t('Couldn’t save the picture. Try again.') });
        load(s);
      }
      // The page is drawn in the look: a new one shows on a new page.
      if (lookChanged || logoChanged) { location.reload(); return; }
      Forms.status(form, t('Saved: the band’s card shows it now.'), 'ok');
      draw();
    }).finally(mark);
  });

  $('look-reset').addEventListener('click', async (e) => {
    if (!confirm(t('Back to the app’s own look? This clears the style, colours, fonts and logo for everyone in the band.'))) return;
    const btn = e.currentTarget;
    await Forms.run(form, btn, async () => {
      if (logo) await call(endpoint + '/brand/logo', { method: 'DELETE' }, { msg: t('Couldn’t remove the image. Try again.') });
      await call(endpoint + '/brand', json({}), { msg: t('Couldn’t save the look. Try again.') });
      location.reload();
    });
  });

  // ---- Loading ----
  function load(s) {
    card = s;
    picked = (s.works || []).filter((w) => s.choices.some((c) => c.workId === w));
    const radio = form.querySelector(`input[name="mode"][value="${s.mode || ''}"]`);
    if (radio) radio.checked = true;
    fillCovers();
    savedCard = cardValue();
  }
  Promise.all([
    fetch(endpoint + '/brand').then((r) => (r.ok ? r.json() : {})).catch(() => ({})),
    fetch(endpoint + '/card-image').then((r) => (r.ok ? r.json() : null)).catch(() => null),
  ]).then(([b, s]) => {
    brand = b || {};
    logo = brand.logoImage || '';
    const look = { ...brand };
    delete look.logoImage; delete look.tagline; delete look.website;
    fill(look);
    savedLook = JSON.stringify(collect());
    // Fine-tune opens by itself when something in it is set.
    if (['background', 'surface', 'text', 'muted', 'accent', 'accent2', 'header', 'fontDisplay', 'fontUI', 'shape', 'mark'].some((k) => look[k])) $('look-fine').open = true;
    if (look.logoURL) form.querySelector('.look-logo-url').open = true;
    if (s) load(s);
    draw();
  });
})();

// The band's card picture (band_settings.html "Logo / image"): what its
// card on the home page shows — the logo, a mosaic of the songs' covers,
// or the covers the band picks — with the card drawn beside the choices
// as it will look, by the same rules as the server's (home.go
// choosePicture, store.CardMosaic). Saved as a whole (POST
// {endpoint}/card-image); the logo is the look's own (PUT
// {endpoint}/brand/logo), uploaded from here too.
(function () {
  const root = document.getElementById('card-image');
  if (!root) return;
  const endpoint = root.dataset.endpoint;
  const form = document.getElementById('card-image-form');
  const pic = document.getElementById('card-image-pic');
  const picker = document.getElementById('card-image-picker');
  const list = document.getElementById('card-image-covers');
  const none = document.getElementById('card-image-none');
  const logoBox = document.getElementById('card-image-logo');
  const logoPreview = document.getElementById('card-image-logo-preview');
  const logoFile = document.getElementById('card-image-logo-file');
  const logoUpload = document.getElementById('card-image-logo-upload');
  const logoStatus = document.getElementById('card-image-logo-status');
  let state = null;
  let picked = [];

  const mode = () => form.querySelector('input[name="mode"]:checked')?.value ?? '';
  // A square mosaic: 9, 4 or 1 covers, at most max.
  const square = (have, max) => [9, 4, 1].find((n) => n <= max && have >= n) || 0;

  function mosaic(m) {
    if (m === 'picked' && picked.length) {
      const byWork = new Map(state.choices.map((c) => [c.workId, c]));
      const list = picked.map((w) => byWork.get(w)).filter(Boolean);
      return list.slice(0, square(list.length, state.maxPicked));
    }
    return state.choices.slice(0, square(state.choices.length, state.cardMosaic));
  }

  // What the card shows, as the home page decides it.
  function picture() {
    const m = mode();
    if (m === 'logo') return { logo: state.logo };
    if (m === 'mosaic' || m === 'picked') {
      const covers = mosaic(m);
      return covers.length ? { covers } : { logo: state.logo };
    }
    if (state.logo) return { logo: state.logo };
    return { covers: mosaic('') };
  }

  function draw() {
    const p = picture();
    pic.replaceChildren();
    pic.className = 'home-pic group-pic';
    pic.style.cssText = '';
    if (state.tileBg) pic.style.setProperty('--tile-bg', state.tileBg);
    if (state.tileFg) pic.style.setProperty('--tile-fg', state.tileFg);
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
      const mark = document.createElement('span');
      mark.className = 'home-pic-mark';
      mark.textContent = state.mark;
      pic.append(mark);
    }
    const m = mode();
    picker.hidden = m !== 'picked' || !state.choices.length;
    none.hidden = state.choices.length > 0 || (m !== 'mosaic' && m !== 'picked');
    logoBox.hidden = m !== 'logo';
    logoPreview.hidden = !state.logo;
    if (state.logo) logoPreview.src = state.logo;
    logoUpload.textContent = state.logo ? t('Replace the logo') : t('Upload a logo');
    for (const li of list.children) {
      const n = picked.indexOf(Number(li.dataset.work));
      const btn = li.querySelector('button');
      btn.setAttribute('aria-pressed', n >= 0 ? 'true' : 'false');
      li.querySelector('.card-image-order').textContent = n >= 0 ? String(n + 1) : '';
    }
  }

  function fillCovers() {
    list.replaceChildren();
    for (const c of state.choices) {
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
      list.append(li);
    }
  }

  function load(s) {
    state = s;
    picked = (s.works || []).filter((w) => s.choices.some((c) => c.workId === w));
    const radio = form.querySelector(`input[name="mode"][value="${s.mode || ''}"]`);
    if (radio) radio.checked = true;
    fillCovers();
    draw();
  }

  form.addEventListener('change', (e) => { if (e.target.name === 'mode') draw(); });

  list.addEventListener('click', (e) => {
    const li = e.target.closest('li[data-work]');
    if (!li) return;
    const w = Number(li.dataset.work);
    const n = picked.indexOf(w);
    if (n >= 0) picked.splice(n, 1);
    else if (picked.length < state.maxPicked) picked.push(w);
    draw();
  });

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    const m = mode();
    if (m === 'picked' && !picked.length) {
      Forms.status(form.querySelector('.form-status'), t('Pick at least one cover.'), 'error');
      return;
    }
    Forms.run(form, document.getElementById('card-image-save'), async () => {
      const res = await fetch(endpoint + '/card-image', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: m, works: m === 'picked' ? picked : [] }),
      }).catch(() => null);
      if (!res || !res.ok) throw new Error(t('Couldn’t save the picture. Try again.'));
      load(await res.json());
      Forms.status(form, t('Saved: the band’s card shows it now.'), 'ok');
    });
  });

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
    if (f.size > 2 * 1024 * 1024) {
      logoStatus.textContent = t('This image is larger than 2 MB. Make it smaller, then try again.');
      return;
    }
    const body = new FormData();
    body.append('file', f);
    Forms.busy(logoUpload, true);
    const res = await fetch(endpoint + '/brand/logo', { method: 'PUT', body }).catch(() => null);
    Forms.busy(logoUpload, false);
    if (!res || !res.ok) {
      logoStatus.textContent = res && res.status === 413
        ? t('This image is larger than 2 MB. Make it smaller, then try again.')
        : res && res.status === 415 ? t('This file isn’t an image the app can use. Upload a PNG, JPEG or WebP image.')
          : t('Couldn’t upload the image. Try again.');
      return;
    }
    const b = await res.json();
    state.logo = b.logoImage || state.logo;
    logoStatus.textContent = t('Logo saved: it shows on this space’s pages now.');
    draw();
  });

  fetch(endpoint + '/card-image').then((r) => (r.ok ? r.json() : null)).then((s) => { if (s) load(s); }).catch(() => {});
})();

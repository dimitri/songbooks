// Small UI pieces for the Guitar Pro file editor (gp-editor.js,
// gp-palette.js): a popover anchored to a button, a short form in one
// (instead of the browser's prompt()), a grid of variants to pick from,
// a confirmation, tooltips with the keyboard shortcut, and the editor's
// action icons. One popover at a time; Escape or a click outside closes
// it and gives focus back to where it was.
(function () {
  let open = null; // { el, close }

  function place(el, anchor) {
    const r = anchor.getBoundingClientRect();
    const w = el.offsetWidth, h = el.offsetHeight;
    const vw = document.documentElement.clientWidth, vh = window.innerHeight;
    // Right of a rail button, under anything else; kept on screen.
    const side = anchor.closest('.gpe-rail') && vw > 760;
    let x = side ? r.right + 8 : r.left;
    let y = side ? r.top : r.bottom + 6;
    if (x + w > vw - 8) x = Math.max(8, vw - w - 8);
    if (y + h > vh - 8) y = Math.max(8, (side ? vh : r.top - 6) - h - (side ? 8 : 0));
    el.style.left = x + window.scrollX + 'px';
    el.style.top = y + window.scrollY + 'px';
  }

  function close() {
    if (!open) return;
    const o = open;
    open = null;
    o.close();
  }

  // popover opens a panel next to anchor; build(panel, done) fills it and
  // calls done(value) to close with a result. Resolves with that value, or
  // null when dismissed.
  function popover(anchor, build, opts = {}) {
    close();
    return new Promise((resolve) => {
      const back = document.activeElement;
      const el = document.createElement('div');
      el.className = 'gpe-pop' + (opts.className ? ' ' + opts.className : '');
      el.setAttribute('role', 'dialog');
      if (opts.title) {
        const h = document.createElement('div');
        h.className = 'gpe-pop-title';
        h.textContent = opts.title;
        if (opts.kbd) { const k = document.createElement('kbd'); k.textContent = opts.kbd; h.appendChild(k); }
        el.appendChild(h);
      }
      let result = null;
      const done = (v) => { result = v; close(); };
      const onKey = (e) => { if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); } };
      const onDown = (e) => { if (!el.contains(e.target) && e.target !== anchor && !anchor.contains(e.target)) close(); };
      open = {
        el,
        close: () => {
          document.removeEventListener('keydown', onKey, true);
          document.removeEventListener('pointerdown', onDown, true);
          el.remove();
          if (back && back.focus) back.focus({ preventScroll: true });
          resolve(result);
        },
      };
      build(el, done);
      document.body.appendChild(el);
      place(el, anchor);
      document.addEventListener('keydown', onKey, true);
      setTimeout(() => document.addEventListener('pointerdown', onDown, true), 0);
      const first = el.querySelector('input, select, button.on, button');
      if (first) first.focus({ preventScroll: true });
    });
  }

  // ask shows a short form: fields [{ name, label, value, type: 'text' |
  // 'number' | 'select', options: [[value, label]], placeholder, min,
  // max, hint }]. Resolves with { name: value } (strings trimmed, numbers
  // as numbers or null when empty), { cleared: true } for "Remove" when
  // clearable, or null when dismissed. check(values) may return an error
  // message to keep the form open.
  function ask(anchor, { title, fields, submit = t('Apply'), clearable = false, check, hint }) {
    return popover(anchor, (el, done) => {
      const form = document.createElement('form');
      form.className = 'gpe-form';
      const inputs = {};
      for (const f of fields) {
        const label = document.createElement('label');
        const span = document.createElement('span');
        span.textContent = f.label;
        let input;
        if (f.type === 'select') {
          input = document.createElement('select');
          for (const [v, l] of f.options) input.add(new Option(l, v));
        } else {
          input = document.createElement('input');
          input.type = f.type === 'number' ? 'number' : 'text';
          if (f.min !== undefined) input.min = f.min;
          if (f.max !== undefined) input.max = f.max;
          if (f.placeholder) input.placeholder = f.placeholder;
          input.autocomplete = 'off';
        }
        if (f.value !== undefined && f.value !== null) input.value = f.value;
        inputs[f.name] = input;
        label.append(span, input);
        if (f.hint) { const h = document.createElement('small'); h.textContent = f.hint; label.appendChild(h); }
        form.appendChild(label);
      }
      if (hint) { const p = document.createElement('p'); p.className = 'gpe-form-hint'; p.textContent = hint; form.appendChild(p); }
      const err = document.createElement('p');
      err.className = 'gpe-form-error';
      err.hidden = true;
      form.appendChild(err);
      const row = document.createElement('div');
      row.className = 'gpe-form-actions';
      if (clearable) {
        const rm = document.createElement('button');
        rm.type = 'button';
        rm.className = 'ghost-btn';
        rm.textContent = t('Remove');
        rm.addEventListener('click', () => done({ cleared: true }));
        row.appendChild(rm);
      }
      const ok = document.createElement('button');
      ok.type = 'submit';
      ok.className = 'primary';
      ok.textContent = submit;
      row.appendChild(ok);
      form.appendChild(row);
      form.addEventListener('input', () => { err.hidden = true; });
      form.addEventListener('submit', (e) => {
        e.preventDefault();
        const out = {};
        for (const f of fields) {
          const raw = inputs[f.name].value.trim();
          out[f.name] = f.type === 'number' ? (raw === '' ? null : Number(raw)) : raw;
        }
        const msg = check ? check(out) : '';
        if (msg) { err.textContent = msg; err.hidden = false; return; }
        done(out);
      });
      el.appendChild(form);
    }, { title });
  }

  // choose shows a grid of options [{ value, label, glyph, name, on }];
  // resolves with the value picked, or null.
  function choose(anchor, { title, kbd, options, columns = 3 }) {
    return popover(anchor, (el, done) => {
      const grid = document.createElement('div');
      grid.className = 'gpe-choose';
      grid.style.gridTemplateColumns = `repeat(${columns}, minmax(2.6rem, auto))`;
      for (const o of options) {
        const b = document.createElement('button');
        b.type = 'button';
        if (o.glyph) { b.textContent = o.glyph; b.classList.add('smufl'); } else b.textContent = o.label;
        if (o.name) { b.title = o.name; b.setAttribute('aria-label', o.name); }
        if (o.on) b.classList.add('on');
        b.addEventListener('click', () => done(o.value));
        grid.appendChild(b);
      }
      el.appendChild(grid);
    }, { title, kbd, className: 'gpe-pop-choose' });
  }

  function confirmAt(anchor, { title, body, ok = t('Delete'), danger = true }) {
    return popover(anchor, (el, done) => {
      if (body) { const p = document.createElement('p'); p.className = 'gpe-form-hint'; p.textContent = body; el.appendChild(p); }
      const row = document.createElement('div');
      row.className = 'gpe-form-actions';
      const no = document.createElement('button');
      no.type = 'button';
      no.className = 'ghost-btn';
      no.textContent = t('Cancel');
      no.addEventListener('click', () => done(false));
      const yes = document.createElement('button');
      yes.type = 'button';
      yes.className = danger ? 'danger' : 'primary';
      yes.textContent = ok;
      yes.addEventListener('click', () => done(true));
      row.append(no, yes);
      el.appendChild(row);
    }, { title }).then((v) => v === true);
  }

  // Tooltips: any element under root with data-tip (and data-kbd) gets a
  // styled one on hover or keyboard focus, after a short wait.
  function tooltips(root) {
    const tip = document.createElement('div');
    tip.className = 'gpe-tip';
    tip.setAttribute('role', 'tooltip');
    tip.hidden = true;
    document.body.appendChild(tip);
    let timer = null, current = null;
    const show = (t) => {
      current = t;
      tip.replaceChildren(document.createTextNode(t.dataset.tip));
      if (t.dataset.kbd) { const k = document.createElement('kbd'); k.textContent = t.dataset.kbd; tip.appendChild(k); }
      if (t.dataset.more) { const m = document.createElement('small'); m.textContent = t.dataset.more; tip.appendChild(m); }
      tip.hidden = false;
      const r = t.getBoundingClientRect();
      const vw = document.documentElement.clientWidth;
      let x = r.left + r.width / 2 - tip.offsetWidth / 2;
      x = Math.max(6, Math.min(vw - tip.offsetWidth - 6, x));
      let y = r.bottom + 6;
      if (y + tip.offsetHeight > window.innerHeight - 6) y = r.top - tip.offsetHeight - 6;
      tip.style.left = x + window.scrollX + 'px';
      tip.style.top = y + window.scrollY + 'px';
    };
    const hide = () => { clearTimeout(timer); tip.hidden = true; current = null; };
    const enter = (e) => {
      const t = e.target.closest && e.target.closest('[data-tip]');
      if (!t || !root.contains(t) || t === current) return;
      clearTimeout(timer);
      timer = setTimeout(() => show(t), e.type === 'focusin' ? 0 : 450);
    };
    root.addEventListener('pointerover', enter);
    root.addEventListener('focusin', enter);
    root.addEventListener('pointerout', (e) => { if (!e.relatedTarget || !current || !current.contains(e.relatedTarget)) hide(); });
    root.addEventListener('focusout', hide);
    root.addEventListener('pointerdown', hide);
  }

  // Action icons, drawn like the app's own (icons.html): 20×20, 1.5 stroke.
  const PATHS = {
    back: '<path d="M12.5 4.5 7 10l5.5 5.5"/>',
    undo: '<path d="M7.5 6.5 4 10l3.5 3.5"/><path d="M4.5 10H12a4 4 0 0 1 0 8h-2"/>',
    redo: '<path d="M12.5 6.5 16 10l-3.5 3.5"/><path d="M15.5 10H8a4 4 0 0 0 0 8h2"/>',
    play: '<path d="M6.5 4.5v11l9-5.5z" fill="currentColor" stroke="none"/>',
    pause: '<path d="M6.5 4.5v11M13.5 4.5v11" stroke-width="2.4"/>',
    stop: '<rect x="5.5" y="5.5" width="9" height="9" rx="1" fill="currentColor" stroke="none"/>',
    loop: '<path d="M4 9V8a3 3 0 0 1 3-3h8l-2-2M16 11v1a3 3 0 0 1-3 3H5l2 2"/>',
    countin: '<path d="M10 3.5v2M4.5 10h-2M17.5 10h-2M5.8 5.8 4.4 4.4M14.2 5.8l1.4-1.4"/><circle cx="10" cy="11.5" r="4.5"/><path d="M10 9.5v2h1.5"/>',
    metronome: '<path d="M7.5 3.5h5l3 13h-11z"/><path d="M10 13.5l4-7"/>',
    plus: '<path d="M10 4v12M4 10h12"/>',
    more: '<circle cx="5" cy="10" r="1.2" fill="currentColor" stroke="none"/><circle cx="10" cy="10" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="10" r="1.2" fill="currentColor" stroke="none"/>',
    keyboard: '<rect x="2.5" y="5" width="15" height="10" rx="1.5"/><path d="M5.5 8h1M9.5 8h1M13.5 8h1M5.5 11h1M8 12h4M13.5 11h1"/>',
    rail: '<rect x="3" y="4" width="14" height="12" rx="1.5"/><path d="M8 4v12"/>',
    beatAdd: '<path d="M7 4.5v11M13.5 7.5v6M10.5 10.5h6"/>',
    beatDel: '<path d="M6 4.5v11M10.5 10.5h6"/>',
    barBefore: '<path d="M12 4v12M16 4v12M3.5 10h5M6 7.5v5"/>',
    barAfter: '<path d="M4 4v12M8 4v12M11.5 10h5M14 7.5v5"/>',
    barDel: '<path d="M6 4v12M10 4v12M13 10h4"/>',
    copy: '<rect x="7" y="7" width="9" height="9" rx="1.5"/><path d="M4 13V5.5A1.5 1.5 0 0 1 5.5 4H13"/>',
    paste: '<rect x="5" y="4.5" width="10" height="12" rx="1.5"/><path d="M8 4.5V3.5h4v1M7.5 9h5M7.5 12h5"/>',
    guitar: '<path d="M11.5 8.5 16 4M14.5 3l2.5 2.5"/><path d="M9.5 8.5c-2-.6-4.6.4-5.4 2.6-.9 2.5 1 4.9 3.4 4.4 1.7-.3 2.2-2 3.9-2.4 1.4-.3 2.2-1.5 1.6-3-.4-1-1.5-1.4-2.5-1.1"/><circle cx="8" cy="12.2" r="1"/>',
    bass: '<path d="M11 9l6-6M15.5 2l2.5 2.5"/><path d="M9.5 9c-2.4-.4-5 .9-5.6 3.3-.6 2.6 1.6 4.7 4 4 1.6-.4 2-2 3.5-2.6 1.3-.5 1.9-1.8 1.2-3.1"/>',
    drums: '<ellipse cx="10" cy="8" rx="6.5" ry="2.5"/><path d="M3.5 8v4.5c0 1.4 2.9 2.5 6.5 2.5s6.5-1.1 6.5-2.5V8"/><path d="M6 3l3 4M14 3l-3 4"/>',
    note: '<path d="M8 15.5V4.5l7-1.5v10"/><ellipse cx="6" cy="15.5" rx="2" ry="1.6"/><ellipse cx="13" cy="13" rx="2" ry="1.6"/>',
    effect: '<path d="M2.5 11c1.2-3 2.4-3 3.6 0s2.4 3 3.6 0 2.4-3 3.6 0 2.4 3 3.6 0"/>',
    bar: '<path d="M3 5h14M3 8h14M3 11h14M3 14h14M11 3.5v12"/>',
    notation: '<path d="M4 15.5 9.5 4.5 15 15.5M6 11.5h7"/>',
    automation: '<path d="M3 15c3 0 3-9 7-9s4 5 7 5"/><circle cx="10" cy="6" r="1.4" fill="currentColor" stroke="none"/>',
    voice: '<circle cx="7" cy="10" r="3"/><circle cx="13" cy="10" r="3"/>',
    lyrics: '<path d="M4 6h12M4 10h8M4 14h10"/>',
    kit: '<ellipse cx="10" cy="7.5" rx="6" ry="2.2"/><path d="M4 7.5v5c0 1.2 2.7 2.2 6 2.2s6-1 6-2.2v-5"/>',
    keys: '<rect x="3" y="3.5" width="14" height="13" rx="1.5"/><path d="M7 7h1M12 7h1M7 10h1M12 10h1M7 13h6"/>',
    edit: '<path d="M4 16l1-4 8-8 3 3-8 8z"/>',
  };
  function icon(name, cls) {
    return `<svg class="icon${cls ? ' ' + cls : ''}" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${PATHS[name] || ''}</svg>`;
  }

  window.GPUI = { popover, ask, choose, confirm: confirmAt, tooltips, icon, close };
})();

// proposal-flow.js — proposing a song to the band's vote, through the
// new song-adding flow (docs/band-votes.md; band_proposals.go): three
// steps in one dialog.
//
//   1. Find: a YouTube link (its title and artist read from it) or
//      "Title — Artist". The song the band may link is found (the
//      catalogue's, or the band's own); a song already in the songbook
//      takes the video as another recording of it (a new reference);
//      else a new song is made for the band, the video its reference,
//      its lyrics and chord grids fetched, its recording downloaded.
//   2. Check: the new song's title, artist, label, key and tempo, its
//      sections and each one's grid, to fix in place (the lyrics stay
//      with their section, not shown); a note "why this song" and where
//      it should go: the songbook, or one of the band's set lists.
//   3. Sent: it's up for a vote.
//
// Any element with data-propose opens it; the band and its set lists
// come from #propose-config (JSON). Strings through t() (js-band-votes).
(function () {
  const cfgEl = document.getElementById('propose-config');
  if (!cfgEl) return;
  let cfg = {};
  try { cfg = JSON.parse(cfgEl.textContent); } catch (e) { return; }
  const bandId = cfg.bandId;
  const setlists = cfg.setlists || [];

  const el = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  let uid = 0;
  // A labelled field: {wrap, input}.
  function field(label, input, opts = {}) {
    const wrap = el('div', 'field' + (opts.cls ? ' ' + opts.cls : ''));
    input.id = input.id || `bandprop-f${++uid}`;
    const lab = el('label', '', label);
    lab.htmlFor = input.id;
    if (opts.optional) lab.append(' ', el('span', 'field-optional', t('(optional)')));
    wrap.append(lab);
    if (opts.hint) {
      const h = el('p', 'field-hint', opts.hint);
      h.id = input.id + '-hint';
      input.setAttribute('aria-describedby', h.id);
      wrap.append(h);
    }
    wrap.append(input);
    return { wrap, input };
  }
  const input = (type, value, attrs = {}) => {
    const i = el(type === 'textarea' ? 'textarea' : 'input');
    if (type !== 'textarea') i.type = type;
    if (value != null) i.value = value;
    Object.entries(attrs).forEach(([k, v]) => i.setAttribute(k, v));
    return i;
  };
  const getJSON = async (url) => {
    const res = await fetch(url).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : {};
    if (!res || !res.ok) throw new Error(data.message || t('Couldn’t reach the server. Check your connection and try again.'));
    return data;
  };
  const send = async (method, url, body) => {
    const res = await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : {};
    return { res, data };
  };
  const looksLink = (s) => /^(https?:\/\/|www\.|youtu\.?be|m\.youtube)/i.test(s.trim());
  const thumbOf = (videoId) => (videoId ? `https://i.ytimg.com/vi/${videoId}/mqdefault.jpg` : '');

  let dlg, body, stepsEl, st = {};

  function build() {
    dlg = el('dialog', 'addsong-dialog bandprop-dialog');
    dlg.setAttribute('aria-labelledby', 'bandprop-title');
    const head = el('div', 'addsong-head');
    const h = el('h2', '', t('Propose a song'));
    h.id = 'bandprop-title';
    const close = el('button', 'icon-btn addsong-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', t('Close'));
    close.addEventListener('click', () => leave());
    head.append(h, close);
    stepsEl = el('ol', 'bandprop-steps');
    [t('Find'), t('Check'), t('Send')].forEach((label, i) => {
      const li = el('li');
      li.append(el('span', 'bandprop-step-num', String(i + 1)), el('span', 'bandprop-step-label', label));
      stepsEl.append(li);
    });
    body = el('div', 'bandprop-body');
    dlg.append(head, stepsEl, body);
    dlg.addEventListener('cancel', (e) => { e.preventDefault(); leave(); });
    document.body.append(dlg);
  }

  function step(n) {
    [...stepsEl.children].forEach((li, i) => {
      li.classList.toggle('is-done', i < n - 1);
      if (i === n - 1) li.setAttribute('aria-current', 'step'); else li.removeAttribute('aria-current');
    });
    body.replaceChildren();
  }

  // Closing: a song made for a proposal never sent is dropped (asked).
  async function leave() {
    if (st.draft && !st.sent) {
      if (!confirm(t('Drop “%s”? The song made for this proposal goes with it.', st.draft.title))) return;
      fetch(`/api/bands/${bandId}/proposal-drafts/${st.draft.songId}`, { method: 'DELETE' }).catch(() => {});
    }
    dlg.close();
    if (st.sent) location.href = `/bands/${bandId}/catalog/vote?sort=newest#p${st.sent}`;
  }

  // opts: {q} a link or "Title — Artist" looked up at once; {onSent(id)}
  // once the proposal is sent (Discover marks its song proposed).
  function open(opts = {}) {
    if (!dlg) build();
    st = { onSent: opts.onSent };
    findStep(opts.q);
    dlg.showModal();
    body.querySelector('input').focus();
    if (opts.q) body.querySelector('form').requestSubmit();
  }

  // ---- 1. Find ---------------------------------------------------------
  function findStep(q) {
    step(1);
    const form = el('form', 'form-stack bandprop-find');
    form.noValidate = true;
    const qi = input('search', q || '', { autocomplete: 'off', enterkeyhint: 'search', spellcheck: 'false', required: '' });
    const f = field(t('YouTube link, or title — artist'), qi, {
      hint: t('Paste the video’s link: its title and artist are read from it. Or type “Title — Artist”.'),
    });
    const actions = el('div', 'form-actions');
    const go = el('button', 'primary', t('Look it up'));
    go.type = 'submit';
    actions.append(go);
    const result = el('div', 'bandprop-result');
    result.setAttribute('aria-live', 'polite');
    form.append(f.wrap, actions, result);
    body.append(form);
    let seq = 0;
    async function run() {
      const term = qi.value.trim();
      if (!term) { Forms.fieldError(qi, t('Paste a YouTube link, or type a title and its artist.')); qi.focus(); return; }
      Forms.fieldError(qi, '');
      const mine = ++seq;
      Forms.busy(go, true);
      result.replaceChildren(el('p', 'hint', looksLink(term) ? t('Reading the video…') : t('Looking it up…')));
      try {
        const d = await getJSON(`/api/bands/${bandId}/proposal-lookup?q=${encodeURIComponent(term)}`);
        if (mine === seq) {
          result.replaceChildren(found(d));
          // One primary action: the result's, once there is one.
          go.classList.toggle('primary', !result.querySelector('.primary'));
        }
      } catch (e) {
        if (mine === seq) result.replaceChildren(el('p', 'form-status is-error', e.message));
      } finally {
        Forms.busy(go, false);
      }
    }
    form.addEventListener('submit', (e) => { e.preventDefault(); run(); });
    qi.addEventListener('paste', () => setTimeout(() => { if (looksLink(qi.value)) run(); }, 0));
    qi.addEventListener('input', () => { go.classList.add('primary'); if (qi.getAttribute('aria-invalid')) Forms.fieldError(qi, ''); });
  }

  function songLine(title, artist, extra) {
    const p = el('p', 'addsong-quick-what');
    p.append(el('strong', '', title));
    if (artist) p.append(' ', el('span', 'addsong-artist', artist));
    if (extra) p.append(' ', extra);
    return p;
  }

  function videoRow(videoId, title) {
    if (!videoId) return null;
    const a = el('a', 'bandprop-video');
    a.href = `https://www.youtube.com/watch?v=${videoId}`;
    a.target = '_blank';
    a.rel = 'noopener';
    const img = el('img');
    img.src = thumbOf(videoId);
    img.alt = '';
    img.loading = 'lazy';
    img.referrerPolicy = 'no-referrer';
    a.append(img, el('span', '', title ? t('Its video: %s', title) : t('Watch the video')));
    return a;
  }

  // What the lookup found, and the one thing to do with it.
  function found(d) {
    const card = el('div', 'addsong-quick-card bandprop-card');
    const actions = el('div', 'form-actions addsong-quick-actions');
    const status = el('p', 'form-status');
    status.setAttribute('role', 'status');
    const m = d.match;
    if (d.openProposal) {
      card.append(songLine(m.title, m.artist), el('p', 'hint', t('It’s up for a vote already.')));
      const a = el('a', 'button-link', t('Rate it'));
      a.href = `/bands/${bandId}/proposals/${d.openProposal}`;
      actions.append(a);
      card.append(actions);
      return card;
    }
    if (m && d.inRepertoire) {
      if (d.videoId && !d.sameReference) {
        card.append(songLine(m.title, m.artist, el('span', 'owner-badge', t('In the songbook'))),
          el('p', 'hint', t('Propose this video as another recording of it: once accepted, it’s the song’s reference.')));
        const v = videoRow(d.videoId);
        if (v) card.append(v);
        const b = el('button', 'primary', t('Continue'));
        b.type = 'button';
        b.addEventListener('click', () => { Object.assign(st, { kind: 'new_reference', songId: m.id, title: m.title, artist: m.artist, url: d.reference, videoId: d.videoId }); checkStep(); });
        actions.append(b);
      } else {
        card.append(songLine(m.title, m.artist, el('span', 'owner-badge', t('In the songbook'))),
          el('p', 'hint', d.sameReference ? t('That video is its reference already.') : t('Paste the link of another recording to propose it as a new reference.')));
      }
      card.append(actions);
      return card;
    }
    if (m) {
      const badge = el('span', 'owner-badge' + (m.public ? ' is-public' : ''), m.own ? t('the band’s own') : t('the shared song'));
      card.append(songLine(m.title, m.artist, badge),
        el('p', 'hint', m.own ? t('The band has this song already, outside its songbook: it’s proposed as it is.') : t('Found in the catalogue: it’s linked, not copied. Fix it once it’s in.')));
      const v = videoRow(d.videoId);
      if (v) card.append(v);
      const b = el('button', 'primary', t('Use this song'));
      b.type = 'button';
      b.addEventListener('click', () => { Object.assign(st, { kind: 'new_song', songId: m.id, title: m.title, artist: m.artist, videoId: d.videoId }); checkStep(); });
      actions.append(b);
      card.append(actions, status);
      return card;
    }
    // A new song: its title and artist to confirm, its video, its chart.
    card.append(el('p', 'addsong-quick-what', t('New song')));
    const row = el('div', 'addsong-quick-fields');
    const ti = field(t('Title'), input('text', d.title || '', { autocomplete: 'off', required: '' }));
    const ar = field(t('Artist'), input('text', d.artist || '', { autocomplete: 'off' }), { optional: true });
    row.append(ti.wrap, ar.wrap);
    card.append(row);
    const vbox = el('div', 'bandprop-video-box');
    const say = (text) => { vbox.replaceChildren(); const v = videoRow(d.videoId, text); if (v) vbox.append(v); else vbox.append(el('p', 'hint', text)); };
    if (d.reference) say('');
    else {
      say(t('Looking for its video…'));
      fetch(`/api/import/youtube/find?title=${encodeURIComponent(d.title || '')}&artist=${encodeURIComponent(d.artist || '')}`)
        .then((r) => (r.ok ? r.json() : null)).catch(() => null)
        .then((x) => {
          const c = x && x.candidates && x.candidates[0];
          if (c && c.url) { d.reference = c.url; d.videoId = (/[?&]v=([\w-]{11})/.exec(c.url) || /youtu\.be\/([\w-]{11})/.exec(c.url) || [])[1] || ''; say(c.title); }
          else say(t('No video found: add its reference later, from the song.'));
        });
    }
    card.append(vbox);
    if (d.lyrics) card.append(el('p', 'hint', t('Its lyrics and chord grids are fetched next, for you to check.')));
    const b = el('button', 'primary', d.lyrics ? t('Create and fetch the chart') : t('Create the song'));
    b.type = 'button';
    b.addEventListener('click', async () => {
      const title = ti.input.value.trim();
      if (!title) { Forms.fieldError(ti.input, t('Give the song its title.')); ti.input.focus(); return; }
      Forms.fieldError(ti.input, '');
      Forms.busy(b, true);
      Forms.status(status, d.lyrics ? t('Fetching the lyrics and chord grids… this can take half a minute.') : t('Creating it…'));
      const { res, data } = await send('POST', `/api/bands/${bandId}/proposal-drafts`, { title, artist: ar.input.value.trim(), reference: d.reference || '' });
      Forms.busy(b, false);
      if (!res || !res.ok) { Forms.status(status, data.message || t('Couldn’t create the song. Try again.'), 'error'); return; }
      Object.assign(st, { kind: 'new_song', songId: data.songId, draft: data, title: data.title, artist: data.artist, videoId: data.videoId });
      checkStep();
    });
    actions.append(b);
    card.append(actions, status);
    return card;
  }

  // ---- 2. Check --------------------------------------------------------
  function checkStep() {
    step(2);
    const form = el('form', 'form-stack bandprop-check');
    form.noValidate = true;
    const status = el('p', 'form-status');
    status.setAttribute('role', 'status');
    let draftForm = null;
    if (st.draft) {
      draftForm = draftFields(st.draft);
      form.append(draftForm.el);
    } else {
      const card = el('div', 'addsong-quick-card');
      card.append(el('p', 'bandprop-kind', st.kind === 'new_reference' ? t('Another recording of') : t('A song for the songbook')),
        songLine(st.title, st.artist));
      const v = videoRow(st.videoId);
      if (v) card.append(v);
      if (st.kind === 'new_song') {
        const a = el('a', '', t('Open the song'));
        a.href = `/song/${st.songId}`;
        a.target = '_blank';
        card.append(a);
      }
      form.append(card);
    }

    const fs = el('fieldset', 'field-group bandprop-what');
    fs.append(el('legend', '', t('Your proposal')));
    const note = input('textarea', '', { rows: '2', maxlength: '500', class: 'prose' });
    const nf = field(t('Why this song?'), note, { optional: true, hint: t('A word for the band: what you like in it, where it would fit.') });
    note.placeholder = t('e.g. would work as an opener');
    fs.append(nf.wrap);
    let targetSel = null;
    if (st.kind === 'new_song' && setlists.length) {
      const where = el('div', 'field-group bandprop-where');
      where.setAttribute('role', 'radiogroup');
      const lab = el('p', 'bandprop-where-label', t('Where should it go?'));
      lab.id = 'bandprop-where-label';
      where.setAttribute('aria-labelledby', lab.id);
      const r1 = input('radio', 'repertoire', { name: 'propose-target', checked: '' });
      const r2 = input('radio', 'setlist', { name: 'propose-target' });
      const l1 = el('label', 'check'); l1.append(r1, ' ', t('The songbook'));
      const l2 = el('label', 'check'); l2.append(r2, ' ', t('The songbook and a set list:'));
      targetSel = el('select');
      targetSel.setAttribute('aria-label', t('Set list'));
      setlists.forEach((s) => { const o = el('option', '', s.title); o.value = s.id; targetSel.append(o); });
      targetSel.disabled = true;
      l2.append(' ', targetSel);
      [r1, r2].forEach((r) => r.addEventListener('change', () => { targetSel.disabled = !r2.checked; if (r2.checked) targetSel.focus(); }));
      where.append(lab, l1, l2);
      fs.append(where);
    }
    form.append(fs);

    const actions = el('div', 'form-actions');
    const back = el('button', '', t('Back'));
    back.type = 'button';
    back.addEventListener('click', () => findStep());
    const go = el('button', 'primary', t('Send the proposal'));
    go.type = 'submit';
    actions.append(back, go);
    form.append(actions, status);
    body.append(form);
    if (st.draft) back.hidden = true; // the song is made: fix it here, or close
    (form.querySelector('input:not([type=radio]), textarea') || go).focus();

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      Forms.run(form, go, async () => {
        if (draftForm) {
          const saved = await draftForm.save();
          if (!saved) throw new Error(t('Fix what’s marked, then send it.'));
          st.draft = saved;
          st.title = saved.title;
        }
        const payload = { note: note.value.trim() };
        if (st.kind === 'new_reference') payload.url = st.url;
        if (targetSel && !targetSel.disabled) { payload.target = 'setlist'; payload.setlistId = Number(targetSel.value); }
        const { res, data } = await send('POST', `/api/bands/${bandId}/proposals`, { kind: st.kind, targetVariantId: st.songId, payload });
        if (res && res.status === 409 && data.catalogVariantId) {
          throw new Error(data.message || t('This song can’t be proposed.'));
        }
        if (!res || !res.ok) throw new Error(data.message || t('Couldn’t send the proposal. Try again.'));
        st.sent = data.id;
        if (st.onSent) await st.onSent(data.id);
        sentStep();
      });
    });
  }

  // The new song's header and sections, to fix in place.
  function draftFields(d) {
    const box = el('div', 'bandprop-draft');
    const src = d.source || {};
    const facts = el('div', 'bandprop-source');
    const lyr = el('p', '', src.lyrics ? t('Lyrics found: %s.', 'Ultimate Guitar') : t('No lyrics found: add them later from the song’s editor (Import).'));
    lyr.prepend(el('span', 'bandprop-source-dot ' + (src.lyrics ? 'is-ok' : 'is-warn'), src.lyrics ? '✓' : '!'), ' ');
    const grids = d.sections.length
      ? t('Chord grids in %d of %d sections: %s.', src.grids || 0, d.sections.length, src.ireal ? t('iReal Pro where its chart matches, Ultimate Guitar elsewhere') : 'Ultimate Guitar')
      : t('No sections found.');
    const gp = el('p', '', grids);
    gp.prepend(el('span', 'bandprop-source-dot ' + (src.grids ? 'is-ok' : 'is-warn'), src.grids ? '✓' : '!'), ' ');
    facts.append(lyr, gp);
    if (src.notes && src.notes.length) {
      const det = el('details', 'bandprop-notes');
      det.append(el('summary', '', t('What the import says')));
      const ul = el('ul');
      src.notes.forEach((n) => ul.append(el('li', '', n)));
      det.append(ul);
      facts.append(det);
    }
    const v = videoRow(d.videoId);
    const top = el('div', 'bandprop-draft-top');
    if (v) top.append(v);
    top.append(facts);
    box.append(top);

    const song = el('fieldset', 'field-group bandprop-song');
    song.append(el('legend', '', t('The song')));
    const grid2 = el('div', 'bandprop-song-fields');
    const ti = field(t('Title'), input('text', d.title, { autocomplete: 'off', required: '', maxlength: '200' }));
    const ar = field(t('Artist'), input('text', d.artist, { autocomplete: 'off', maxlength: '200' }), { optional: true });
    const vn = field(t('Label'), input('text', d.versionName || '', { autocomplete: 'off', maxlength: '80' }), { optional: true, hint: t('Tells this recording from others: Live, Acoustic, 2011…') });
    const key = field(t('Key'), input('text', d.key || '', { autocomplete: 'off', maxlength: '8', spellcheck: 'false', autocapitalize: 'none' }), { optional: true, cls: 'field-short' });
    const tempo = field(t('Tempo'), input('number', d.tempo || '', { min: '20', max: '400', step: '1', inputmode: 'numeric' }), { optional: true, cls: 'field-short' });
    key.input.placeholder = 'Am';
    tempo.input.placeholder = t('bpm');
    grid2.append(ti.wrap, ar.wrap, vn.wrap, key.wrap, tempo.wrap);
    song.append(grid2);
    box.append(song);

    const secs = el('fieldset', 'field-group bandprop-sections-box');
    secs.append(el('legend', '', t('Sections and chord grids')));
    const hint = el('p', 'field-hint', t('Fix what the import got wrong: a section’s name, its bars (| Am | F | C | G |), their order. The lyrics stay with their section.'));
    hint.id = 'bandprop-sections-hint';
    const list = el('ol', 'bandprop-sections');
    list.setAttribute('aria-describedby', hint.id);
    const rows = d.sections.map((s, i) => ({ from: i, label: s.label, grid: s.grid, lines: s.lines, repeat: s.repeat }));
    const add = el('button', 'link-button bandprop-add-section', '+ ' + t('Add a section'));
    add.type = 'button';
    add.addEventListener('click', () => { rows.push({ from: -1, label: '', grid: '', lines: 0 }); draw(rows.length - 1, 'label'); });
    secs.append(hint, list, add);
    box.append(secs);

    function iconBtn(label, text, fn, disabled) {
      const b = el('button', 'icon-btn bandprop-sec-btn', text);
      b.type = 'button';
      b.setAttribute('aria-label', label);
      b.title = label;
      b.disabled = !!disabled;
      b.addEventListener('click', fn);
      return b;
    }
    function draw(focusIndex, focusWhat) {
      list.replaceChildren();
      rows.forEach((r, i) => {
        const li = el('li', 'bandprop-section');
        const n = el('span', 'bandprop-sec-num', String(i + 1));
        const label = input('text', r.label, { autocomplete: 'off', maxlength: '60', 'aria-label': t('Section %d: name', i + 1) });
        label.className = 'bandprop-sec-label';
        label.placeholder = t('Verse');
        label.dataset.index = i;
        label.addEventListener('input', () => { r.label = label.value; Forms.fieldError(label, ''); });
        const g = input('text', r.grid, { autocomplete: 'off', spellcheck: 'false', autocapitalize: 'none', 'aria-label': t('Section %d: chord grid', i + 1) });
        g.className = 'bandprop-sec-grid';
        g.placeholder = r.repeat ? t('(as before)') : '| Am | F | C | G |';
        g.dataset.index = i;
        g.addEventListener('input', () => { r.grid = g.value; Forms.fieldError(g, ''); });
        const meta = el('span', 'bandprop-sec-meta', r.lines ? tn(r.lines, '%d line of lyrics', '%d lines of lyrics') : r.from < 0 ? t('new') : t('no lyrics'));
        const tools = el('span', 'bandprop-sec-tools');
        tools.append(
          iconBtn(t('Move section %d up', i + 1), '↑', () => { [rows[i - 1], rows[i]] = [rows[i], rows[i - 1]]; draw(i - 1, 'up'); }, i === 0),
          iconBtn(t('Move section %d down', i + 1), '↓', () => { [rows[i + 1], rows[i]] = [rows[i], rows[i + 1]]; draw(i + 1, 'down'); }, i === rows.length - 1),
          iconBtn(t('Remove section %d', i + 1), '×', () => { rows.splice(i, 1); draw(Math.min(i, rows.length - 1), 'label'); }, rows.length === 1),
        );
        li.append(n, label, g, meta, tools);
        list.append(li);
        if (i === focusIndex) {
          const target = focusWhat === 'label' ? label : focusWhat === 'up' ? tools.children[0] : focusWhat === 'down' ? tools.children[1] : null;
          if (target) setTimeout(() => (target.disabled ? label : target).focus(), 0);
        }
      });
    }
    draw();

    async function save() {
      [ti.input, tempo.input].forEach((i) => Forms.fieldError(i, ''));
      if (!ti.input.value.trim()) { Forms.fieldError(ti.input, t('Give the song its title.')); ti.input.focus(); return null; }
      if (!tempo.input.checkValidity()) { Forms.fieldError(tempo.input, t('A tempo is a number of beats per minute, 20 to 400.')); tempo.input.focus(); return null; }
      for (const [i, r] of rows.entries()) {
        if (!r.label.trim()) { const lab = list.querySelector(`.bandprop-sec-label[data-index="${i}"]`); Forms.fieldError(lab, t('Name the section (Verse, Chorus…).')); lab.focus(); return null; }
      }
      const { res, data } = await send('PUT', `/api/bands/${bandId}/proposal-drafts/${d.songId}`, {
        version: d.version, title: ti.input.value.trim(), artist: ar.input.value.trim(), versionName: vn.input.value.trim(),
        key: key.input.value.trim(), tempo: tempo.input.value.trim(),
        sections: rows.map((r) => ({ from: r.from, label: r.label.trim(), grid: r.grid.trim() })),
      });
      if (res && res.ok) {
        d.version = data.version;
        rows.forEach((r, i) => { r.from = i; });
        return data;
      }
      const msg = data.message || t('Couldn’t save the song. Try again.');
      const byField = { title: ti.input, artist: ar.input, versionName: vn.input, tempo: tempo.input };
      let target = byField[data.field];
      if (!target && (data.field === 'label' || data.field === 'grid')) {
        target = list.querySelector(`.bandprop-sec-${data.field}[data-index="${data.index}"]`);
      }
      if (target) { Forms.fieldError(target, msg); target.focus(); return null; }
      throw new Error(msg);
    }
    return { el: box, save };
  }

  // ---- 3. Sent ---------------------------------------------------------
  function sentStep() {
    step(3);
    [...stepsEl.children].forEach((li) => li.classList.add('is-done'));
    const done = el('div', 'bandprop-sent');
    done.append(el('p', 'bandprop-sent-mark', '✓'));
    const h = el('h3', '', t('“%s” is up for a vote', st.title));
    h.tabIndex = -1;
    done.append(h, el('p', 'hint', (cfg.canVote ? t('Every core member rates it 1 to 4 stars, or says no thanks. You can rate it too.') : t('Every core member rates it 1 to 4 stars, or says no thanks.'))));
    const actions = el('div', 'form-actions');
    const again = el('button', '', t('Propose another'));
    again.type = 'button';
    again.addEventListener('click', () => { st = { sent: st.sent }; findStep(); body.querySelector('input').focus(); });
    const see = el('button', 'primary', t('See it on the list'));
    see.type = 'button';
    see.addEventListener('click', () => leave());
    actions.append(again, see);
    done.append(actions);
    body.append(done);
    h.focus();
  }

  document.querySelectorAll('[data-propose]').forEach((b) => b.addEventListener('click', () => open()));
  window.ProposeFlow = { open };
  if (location.hash === '#propose') open();
})();

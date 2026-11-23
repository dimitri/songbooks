// A student proposes a song of their own to the coach
// (httpapi/coach_proposals.go, docs/coaching.md "A student proposes a
// song"): one dialog, from the song's page (⋯ "Propose to Inès…", the
// song given) or from the coaching Songbooks section ("Propose a song…":
// a song of their own songbook to pick, or one found or made there and
// then with the Add / Create dialog). A note, and if they like the key
// they'd sing it in (− / +, their range with the song's melody over it).
//
//   ProposeSong.open({ community, coach, variant, title, key, mySongbook, onSent })
//
// Buttons with data-propose-community open it too (data-coach,
// data-variant, data-title, data-key). Strings through t() (the
// js-coach-proposals catalog).
(() => {
  const el = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  const MAJOR = ['C', 'Db', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'Ab', 'A', 'Bb', 'B'];
  const MINOR = ['C', 'C#', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'G#', 'A', 'Bb', 'B'];
  const PC = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };
  // "A (+2)", "G (original)": a key moved by n, named as the server does.
  const keyText = (key, n) => {
    const off = n ? (n > 0 ? '+' + n : '−' + -n) : t('original');
    const m = /^([A-G])([#b]?)(m(?!aj))?/.exec(key || '');
    if (!m) return off;
    const pc = (((PC[m[1]] + (m[2] === '#' ? 1 : m[2] === 'b' ? -1 : 0) + n) % 12) + 12) % 12;
    return `${m[3] ? MINOR[pc] + 'm' : MAJOR[pc]} (${off})`;
  };
  const clamp = (n) => Math.max(-12, Math.min(12, n));

  let dlg = null;
  let st = {};

  function build() {
    dlg = el('dialog', 'program-plan-dialog propose-dialog');
    dlg.setAttribute('aria-labelledby', 'propose-title');
    const form = el('form', 'form-stack propose-form');
    form.method = 'dialog';
    form.noValidate = true;
    const h = el('h2', 'propose-title');
    h.id = 'propose-title';
    const intro = el('p', 'hint propose-intro');

    // The song: given, or picked from their own songbook.
    const songField = el('div', 'field propose-song-field');
    const songLabel = el('label', '', t('Song'));
    songLabel.htmlFor = 'propose-song';
    const select = el('select');
    select.id = 'propose-song';
    select.required = true;
    const fixed = el('p', 'propose-fixed');
    const findBtn = el('button', 'link-like propose-find', t('Find or create another song…'));
    findBtn.type = 'button';
    const songHint = el('p', 'hint propose-song-hint');
    songField.append(songLabel, select, fixed, songHint, findBtn);

    const noteField = el('div', 'field');
    const noteLabel = el('label', '', t('A note (optional)'));
    noteLabel.htmlFor = 'propose-note';
    const note = el('textarea');
    note.id = 'propose-note';
    note.rows = 3;
    note.maxLength = 1000;
    note.placeholder = t('Why this song, what you’d like to work on in it…');
    noteField.append(noteLabel, note);

    const keyField = el('div', 'field propose-key-field');
    const wishLabel = el('label', 'check');
    const wish = el('input');
    wish.type = 'checkbox';
    wish.id = 'propose-wish';
    wishLabel.append(wish, ' ', t('I’d like it in a key of mine'));
    const keyRow = el('div', 'cbook-add-key propose-key');
    keyRow.hidden = true;
    const keyLabel = el('span', 'cbook-add-key-label', t('Key'));
    const picker = el('span', 'coach-key');
    const down = el('button', 'coach-key-down', '−');
    down.type = 'button';
    down.setAttribute('aria-label', t('down a semitone'));
    const out = el('output');
    out.setAttribute('aria-live', 'polite');
    const up = el('button', 'coach-key-up', '+');
    up.type = 'button';
    up.setAttribute('aria-label', t('up a semitone'));
    picker.append(down, out, up);
    keyRow.append(keyLabel, picker);
    const strip = el('div', 'voice-strip propose-strip');
    strip.hidden = true;
    keyField.append(wishLabel, keyRow, strip);

    const status = el('p', 'form-status');
    status.setAttribute('role', 'status');
    const actions = el('div', 'form-actions');
    const send = el('button', 'primary propose-send');
    send.type = 'submit';
    const cancel = el('button', '', t('Cancel'));
    cancel.type = 'button';
    cancel.addEventListener('click', () => dlg.close());
    actions.append(send, cancel);
    form.append(h, intro, songField, noteField, keyField, status, actions);
    dlg.append(form);
    document.body.append(dlg);
    Object.assign(st, { form, h, intro, select, fixed, findBtn, songHint, note, wish, keyRow, out, strip, status, send });

    const showKey = () => {
      out.textContent = keyText(st.key, st.semis);
      window.VoiceStrip?.key(strip, st.semis);
    };
    st.showKey = showKey;
    const showStrip = () => {
      if (wish.checked && st.variant) window.VoiceStrip?.show(strip, { variant: st.variant, community: st.community, semitones: st.semis });
      else window.VoiceStrip?.hide(strip);
    };
    st.showStrip = showStrip;
    down.addEventListener('click', () => { st.semis = clamp(st.semis - 1); showKey(); });
    up.addEventListener('click', () => { st.semis = clamp(st.semis + 1); showKey(); });
    wish.addEventListener('change', () => { keyRow.hidden = !wish.checked; showKey(); showStrip(); });
    select.addEventListener('change', () => pickSong(Number(select.value)));
    findBtn.addEventListener('click', findOrCreate);
    form.addEventListener('submit', submit);
  }

  function pickSong(id) {
    const s = st.songs.find((x) => x.id === id);
    st.variant = s ? s.id : 0;
    st.key = s ? s.key : '';
    st.semis = 0;
    st.songHint.textContent = s && s.state === 'waiting' ? t('Proposed already: sending it again updates your note and key.') : '';
    st.showKey();
    st.showStrip();
  }

  // Their own songbook's songs, those that may go first.
  async function loadSongs(select) {
    st.select.replaceChildren(el('option', '', t('Loading…')));
    st.select.disabled = true;
    let d;
    try {
      const r = await fetch(`/api/communities/${st.community}/song-proposals/songs`);
      if (!r.ok) throw new Error();
      d = await r.json();
    } catch (e) {
      st.select.replaceChildren(el('option', '', t('Couldn’t load your songs.')));
      return;
    }
    st.songs = d.songs || [];
    st.select.replaceChildren();
    const ph = el('option', '', st.songs.length ? t('Pick a song of your songbook') : t('Your songbook has no songs yet'));
    ph.value = '';
    st.select.append(ph);
    for (const s of st.songs) {
      let label = s.artist ? `${s.title} — ${s.artist}` : s.title;
      if (s.state === 'in') label += ` · ${t('in your songbook with %s', st.coach)}`;
      if (s.state === 'waiting') label += ` · ${t('proposed')}`;
      if (s.state === 'no') label += ` · ${s.why || t('can’t be proposed')}`;
      const o = el('option', '', label);
      o.value = s.id;
      o.disabled = s.state === 'in' || s.state === 'no';
      st.select.append(o);
    }
    st.select.disabled = false;
    if (select) {
      st.select.value = String(select);
      pickSong(select);
    }
  }

  // Not in their songbook yet: the Add / Create dialog, into My songbook,
  // then picked here.
  function findOrCreate() {
    const go = () => {
      const picked = (id) => {
        document.querySelector('dialog.addsong-dialog')?.close();
        loadSongs(id);
      };
      window.AddSong.open({
        songbookId: st.mySongbook, reload: false, addLabel: t('Add to my songbook'),
        onAdded: (id) => picked(id), onCreated: (id) => picked(id), onQuick: (id) => picked(id),
      });
    };
    if (window.AddSong) { go(); return; }
    const s = document.createElement('script');
    s.src = '/static/js/add-song.js';
    s.onload = () => { if (window.AddSong) go(); };
    document.head.append(s);
  }

  async function submit(e) {
    e.preventDefault();
    if (!st.variant) {
      if (window.Forms) Forms.fieldError(st.select, t('Pick a song.')); else st.status.textContent = t('Pick a song.');
      return;
    }
    const body = { variantId: st.variant, note: st.note.value.trim(), semitones: st.wish.checked ? st.semis : null };
    st.send.disabled = true;
    st.status.textContent = '';
    try {
      const r = await fetch(`/api/communities/${st.community}/song-proposals`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
      }).catch(() => null);
      if (!r || !r.ok) {
        const d = r ? await r.json().catch(() => ({})) : {};
        throw new Error(d.error || t('Couldn’t send it. Try again.'));
      }
      st.status.classList.remove('is-error');
      st.status.textContent = t('Sent. %s answers here and in your notifications.', st.coach);
      st.send.textContent = t('Sent');
      setTimeout(() => { dlg.close(); if (st.onSent) st.onSent(); }, 1200);
    } catch (err) {
      st.send.disabled = false;
      st.status.classList.add('is-error');
      st.status.textContent = err.message;
    }
  }

  function open(o = {}) {
    if (!dlg) build();
    Object.assign(st, {
      community: String(o.community), coach: o.coach || '', variant: Number(o.variant) || 0, key: o.key || '',
      mySongbook: Number(o.mySongbook) || 0, onSent: o.onSent || null, songs: [], semis: 0,
    });
    st.h.textContent = t('Propose a song to %s', st.coach);
    st.intro.textContent = t('%s listens to it, then adds it to your songbook with them, in a key, or tells you why not. It stays in your own songbook either way.', st.coach);
    st.send.textContent = t('Send to %s', st.coach);
    st.send.disabled = false;
    st.status.textContent = '';
    st.status.classList.remove('is-error');
    st.note.value = '';
    st.wish.checked = false;
    st.keyRow.hidden = true;
    st.songHint.textContent = '';
    window.VoiceStrip?.hide(st.strip);
    const given = !!st.variant;
    st.select.hidden = given;
    st.select.previousElementSibling.hidden = given;
    st.findBtn.hidden = given || !st.mySongbook;
    st.fixed.hidden = !given;
    st.fixed.textContent = given ? o.title || '' : '';
    if (given) {
      st.songs = [{ id: st.variant, key: st.key }];
      if (o.waiting) st.songHint.textContent = t('Proposed already: sending it again updates your note and key.');
    } else {
      loadSongs(Number(o.select) || 0);
    }
    st.showKey();
    dlg.showModal();
    (given ? st.note : st.select).focus();
  }

  window.ProposeSong = { open };
  document.addEventListener('click', (e) => {
    const b = e.target.closest('[data-propose-community]');
    if (!b) return;
    e.preventDefault();
    open({
      community: b.dataset.proposeCommunity, coach: b.dataset.coach, variant: b.dataset.variant, title: b.dataset.title,
      key: b.dataset.key, mySongbook: b.dataset.mySongbook, waiting: b.dataset.waiting === '1',
      onSent: () => { if (b.dataset.reload === '1') location.reload(); },
    });
  });
})();

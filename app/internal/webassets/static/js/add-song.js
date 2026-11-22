// The one Add / Create dialog, and the Add to… picker
// (docs/ownership-model.md §1, §4; steps 4–5).
//
// AddSong.open({songbookId, onAdded, onCreated, addLabel}) — from any
// songbook (a band's repertoire, a band's, a community's or a person's
// songbook, a set list, onboarding): one search over everything the
// person sees, each song with whose it is and who sees it, the versions
// of one work together. "Add" links the song in the songbook, as it is
// ("Mercy plays the shared version until someone edits Mercy's
// version"); a song the songbook can't hold (another band's private
// song, a closed songbook's) says why instead. "Create" makes a new song
// in "Goes into" (the songbook it was opened from, or any other the
// person adds to), private, or public when they may publish there.
//
// AddTo.open({songId, title, onAdded}) — from a song (the catalogue, The
// Big Songbook): the songbooks the person adds to, each saying whether
// it holds the song already or why it can't.
//
// Both build their own <dialog> once per page; strings through t() (the
// js-ownership catalog).
(function () {
  const el = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  const getJSON = async (url) => {
    const res = await fetch(url).catch(() => null);
    if (!res || !res.ok) throw new Error(t('Couldn’t reach the server. Check your connection and try again.'));
    return res.json();
  };
  const post = (url, body) => fetch(url, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  }).catch(() => null);
  // A refusal's message, from the server's 409/403 answer.
  const refusal = async (res, fallback) => {
    const d = res ? await res.json().catch(() => ({})) : {};
    return d.message || (res && res.status === 403 && d.error) || fallback;
  };

  // The badge of a song: "Mercy · private".
  function badge(song) {
    const b = el('span', 'owner-badge' + (song.public ? ' is-public' : '') + (song.closed ? ' is-closed' : ''), song.badge);
    return b;
  }

  // "Link" says what a link is, for the songbook's owner.
  function linkHint(target) {
    if (!target) return t('“Add” links the song as it is: it plays the shared version until someone makes their own.');
    if (target.ownerKind === 'person') return t('“Add” links the song as it is: you play the shared version until you make your own.');
    return t('“Add” links the song as it is: %s plays the shared version until someone edits %s’s version.', target.ownerName, target.ownerName);
  }

  // ---- The Add / Create dialog -------------------------------------
  let dlg = null;
  let state = {};

  function build() {
    dlg = el('dialog', 'addsong-dialog');
    dlg.setAttribute('aria-labelledby', 'addsong-title');
    const head = el('div', 'addsong-head');
    const h = el('h2', '', t('Add a song'));
    h.id = 'addsong-title';
    const close = el('button', 'icon-btn addsong-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', t('Close'));
    close.addEventListener('click', () => dlg.close());
    head.append(h, close);
    const into = el('p', 'hint addsong-into');

    const search = el('form', 'addsong-search');
    search.setAttribute('role', 'search');
    const label = el('label', 'sr-only', t('Find a song'));
    label.htmlFor = 'addsong-q';
    const q = el('input');
    q.type = 'search';
    q.id = 'addsong-q';
    q.autocomplete = 'off';
    q.enterKeyHint = 'search';
    q.placeholder = t('Title or artist');
    search.append(label, q);
    search.addEventListener('submit', (e) => { e.preventDefault(); run(); });
    let timer;
    q.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(run, 220); });

    const scopes = el('div', 'space-views addsong-scopes');
    scopes.setAttribute('role', 'group');
    scopes.setAttribute('aria-label', t('Show'));
    for (const [scope, text] of [['', t('find|All')], ['mine', t('find|Mine')], ['bands', t('My bands')], ['communities', t('My communities')], ['public', t('find|Public')]]) {
      const b = el('button', 'space-view' + (scope === '' ? ' active' : ''), text);
      b.type = 'button';
      b.dataset.scope = scope;
      b.setAttribute('aria-pressed', String(scope === ''));
      b.addEventListener('click', () => {
        scopes.querySelectorAll('button').forEach((x) => { x.classList.toggle('active', x === b); x.setAttribute('aria-pressed', String(x === b)); });
        state.scope = scope;
        run();
      });
      scopes.append(b);
    }
    const hint = el('p', 'hint addsong-link-hint');
    const results = el('ul', 'addsong-results');
    results.setAttribute('aria-live', 'polite');
    const status = el('p', 'form-status');
    status.setAttribute('role', 'status');

    // Create.
    const create = el('details', 'addsong-create');
    const sum = el('summary', '', t('Not there? Create a new song'));
    const form = el('form', 'form-stack addsong-create-form');
    form.noValidate = true;
    const field = (id, text, input, optional) => {
      const f = el('div', 'field');
      const l = el('label', '', text);
      l.htmlFor = id;
      if (optional) l.append(' ', el('span', 'field-optional', t('(optional)')));
      input.id = id;
      f.append(l, input);
      return f;
    };
    const title = el('input'); title.type = 'text'; title.autocomplete = 'off'; title.required = true;
    const artist = el('input'); artist.type = 'text'; artist.autocomplete = 'off';
    const ref = el('input'); ref.type = 'url'; ref.autocomplete = 'off'; ref.placeholder = t('e.g. %s', 'https://youtube.com/watch?v=…');
    ref.setAttribute('autocapitalize', 'none'); ref.spellcheck = false;
    const goes = el('select');
    const who = el('fieldset', 'field-group addsong-who');
    who.append(el('legend', '', t('Who sees it')));
    for (const [v, text, h2] of [['private', t('badge|private'), ''], ['public', t('badge|public'), t('everyone signed in sees it, links it, makes their own version')]]) {
      const l = el('label', 'addsong-who-choice');
      const r = el('input'); r.type = 'radio'; r.name = 'addsong-who'; r.value = v; r.checked = v === 'private';
      const span = el('span', '', text);
      l.append(r, ' ', span);
      if (h2) l.append(' ', el('span', 'hint', '— ' + h2));
      who.append(l);
    }
    const whoHint = el('p', 'hint addsong-who-none');
    const createBtn = el('button', 'primary', t('Create song'));
    createBtn.type = 'submit';
    const actions = el('div', 'form-actions');
    actions.append(createBtn);
    const createStatus = el('p', 'form-status');
    createStatus.setAttribute('role', 'status');
    form.append(field('addsong-title-in', t('Title'), title), field('addsong-artist-in', t('Artist'), artist, true),
      field('addsong-ref-in', t('Reference video'), ref, true), field('addsong-goes', t('Goes into'), goes), who, whoHint, actions, createStatus);
    create.append(sum, form);
    goes.addEventListener('change', showWho);
    form.addEventListener('submit', (e) => { e.preventDefault(); doCreate(); });
    create.addEventListener('toggle', () => {
      if (create.open && !title.value) title.value = q.value.trim();
      if (create.open) title.focus();
    });

    dlg.append(head, into, search, scopes, hint, results, status, create);
    dlg.addEventListener('close', () => { if (state.added && state.reload !== false) location.reload(); });
    document.body.append(dlg);
    Object.assign(state, { q, results, status, hint, into, create, title, artist, ref, goes, who, whoHint, createBtn, createStatus, h });
  }

  function showWho() {
    const tgt = state.targets[Number(state.goes.value)];
    const can = tgt && tgt.canPublish && !tgt.closed;
    state.who.hidden = !can;
    if (!can) state.who.querySelector('input[value="private"]').checked = true;
    state.whoHint.hidden = can || !tgt;
    if (tgt && tgt.big) {
      state.who.hidden = true;
      state.whoHint.hidden = false;
      state.whoHint.textContent = t('A song of The Big Songbook is public: everyone signed in sees it.');
    } else if (tgt && tgt.closed) {
      state.whoHint.textContent = t('A closed songbook’s songs are private: they never go anywhere else.');
    } else if (tgt) {
      state.whoHint.textContent = t('Private: %s’s members see it. Making it public is for whoever may publish there.', tgt.ownerName || t('Yours'));
    }
  }

  async function run() {
    const { q, results, status } = state;
    const term = q.value.trim();
    const seq = (state.seq = (state.seq || 0) + 1);
    status.textContent = '';
    const params = new URLSearchParams({ q: term });
    if (state.scope) params.set('scope', state.scope);
    if (state.songbookId) params.set('songbook', state.songbookId);
    let data;
    try {
      data = await getJSON('/api/find?' + params);
    } catch (e) {
      if (seq === state.seq) Forms.status(status, t('Couldn’t search. Try again.'), 'error');
      return;
    }
    if (seq !== state.seq) return;
    results.replaceChildren();
    const works = data.works || [];
    if (!works.length) {
      results.append(el('li', 'hint', term ? t('No songs match that.') : t('Type a title or an artist.')));
      return;
    }
    for (const w of works) results.append(workRow(w));
  }

  function workRow(w) {
    const li = el('li', 'addsong-work');
    if (w.songs.length > 1) {
      const head = el('div', 'addsong-work-head');
      head.append(el('span', 'addsong-work-title', w.title));
      head.append(el('span', 'addsong-versions', tn(w.songs.length, '%d version', '%d versions')));
      li.append(head);
    }
    const ul = el('ul', 'addsong-songs');
    for (const s of w.songs) ul.append(songRow(s, w.songs.length > 1));
    li.append(ul);
    return li;
  }

  function songRow(s, several) {
    const li = el('li', 'addsong-song');
    const text = el('div', 'addsong-song-text');
    const a = el('a', 'addsong-song-title');
    a.href = `/song/${s.id}`;
    a.target = '_blank';
    a.rel = 'noopener';
    a.textContent = several ? [s.artist || s.title, s.versionName].filter(Boolean).join(' — ') : (s.versionName ? `${s.title} — ${s.versionName}` : s.title);
    text.append(a);
    if (!several && s.artist) text.append(' ', el('span', 'addsong-artist', s.artist));
    text.append(' ', badge(s));
    li.append(text);
    if (s.inSongbook && !state.addInSongbook) {
      li.append(el('span', 'addsong-state', t('In the songbook')));
    } else if (!s.linkable && !s.inSongbook) {
      const why = el('span', 'addsong-state is-refused', t('Can’t be added'));
      why.title = s.refusalMsg || '';
      li.append(why);
      if (s.refusalMsg) text.append(el('span', 'hint addsong-why', s.refusalMsg));
    } else {
      const b = el('button', 'addsong-add', state.addLabel || t('Add'));
      b.type = 'button';
      b.setAttribute('aria-label', t('Add %s', s.title));
      b.addEventListener('click', () => doAdd(s, b));
      li.append(b);
    }
    return li;
  }

  async function doAdd(s, btn) {
    Forms.busy(btn, true);
    Forms.status(state.status, '');
    try {
      if (state.songbookId && !s.inSongbook) {
        const res = await post(`/api/songbooks/${state.songbookId}/entries`, { variantId: s.id });
        if (!res || !res.ok) throw new Error(await refusal(res, t('Couldn’t add “%s”. Try again.', s.title)));
      }
      if (state.onAdded) await state.onAdded(s.id, s);
      state.added = true;
      const done = el('span', 'addsong-state is-added', t('Added'));
      btn.replaceWith(done);
    } catch (e) {
      Forms.busy(btn, false);
      Forms.status(state.status, e.message, 'error');
    }
  }

  async function doCreate() {
    const { title, artist, ref, goes, createBtn, createStatus } = state;
    if (!title.value.trim()) { Forms.fieldError(title, t('Give it a title.')); return; }
    const tgt = state.targets[Number(goes.value)];
    if (!tgt) { Forms.fieldError(goes, t('Pick where it goes.')); return; }
    const pub = !!state.who.querySelector('input[value="public"]:checked') && !state.who.hidden;
    Forms.busy(createBtn, true);
    Forms.status(createStatus, '');
    const body = { title: title.value.trim(), artist: artist.value.trim(), referenceUrl: ref.value.trim(), public: pub };
    const res = await post(tgt.big ? '/api/catalog/songs' : `/api/songbooks/${tgt.id}/songs`, body);
    if (!res || !res.ok) {
      Forms.busy(createBtn, false);
      Forms.status(createStatus, await refusal(res, t('Couldn’t create “%s”. Try again.', body.title)), 'error');
      return;
    }
    const data = await res.json().catch(() => ({}));
    try {
      if (state.onCreated) { await state.onCreated(data.variantId, tgt); return; }
    } catch (e) {
      Forms.busy(createBtn, false);
      Forms.status(createStatus, e.message, 'error');
      return;
    }
    const from = tgt.big ? '' : `&songbook=${tgt.id}`;
    // Straight into edit mode: a new song has an empty [Verse] to fill.
    location.href = `/song/${data.variantId}?edit=1${from}`;
  }

  async function open(opts = {}) {
    if (!dlg) build();
    state = Object.assign(state, {
      songbookId: opts.songbookId || 0, onAdded: opts.onAdded, addInSongbook: !!opts.addInSongbook, onCreated: opts.onCreated, addLabel: opts.addLabel,
      reload: opts.reload, scope: '', added: false, targets: [],
    });
    state.h.textContent = opts.heading || t('Add a song');
    state.q.value = opts.query || '';
    state.results.replaceChildren();
    state.status.textContent = '';
    state.createStatus.textContent = '';
    state.create.open = !!opts.create;
    state.title.value = state.artist.value = state.ref.value = '';
    dlg.querySelectorAll('.addsong-scopes button').forEach((x) => { x.classList.toggle('active', !x.dataset.scope); x.setAttribute('aria-pressed', String(!x.dataset.scope)); });
    if (!dlg.open) dlg.showModal();
    state.q.focus();
    // Where Create puts a song: the songbook it was opened from first.
    try {
      const d = await getJSON(`/api/songbook-targets${state.songbookId ? '?keep=' + state.songbookId : ''}`);
      state.targets = d.targets || [];
    } catch (e) {
      state.targets = [];
    }
    const here = state.targets.find((x) => x.id === state.songbookId);
    state.into.textContent = here ? t('Into %s', here.label) : '';
    state.hint.textContent = linkHint(here);
    state.goes.replaceChildren();
    state.targets.forEach((x, i) => {
      const o = el('option', '', x.label + (x.closed ? ' · ' + t('badge|closed') : ''));
      o.value = i;
      if (x === here) o.selected = true;
      state.goes.append(o);
    });
    showWho();
    run();
  }

  // ---- The Add to… picker -------------------------------------------
  let pick = null;
  let pickState = {};

  function buildPick() {
    pick = el('dialog', 'addsong-dialog addto-dialog');
    pick.setAttribute('aria-labelledby', 'addto-title');
    const head = el('div', 'addsong-head');
    const h = el('h2', '', t('Add to…'));
    h.id = 'addto-title';
    const close = el('button', 'icon-btn addsong-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', t('Close'));
    close.addEventListener('click', () => pick.close());
    head.append(h, close);
    const song = el('p', 'addto-song');
    const hint = el('p', 'hint', t('A link: the songbook plays the song as it is, in its own key, until someone makes their own version.'));
    const list = el('ul', 'addto-list');
    const status = el('p', 'form-status');
    status.setAttribute('role', 'status');
    pick.append(head, song, hint, list, status);
    document.body.append(pick);
    Object.assign(pickState, { song, list, status });
  }

  async function openPick(opts = {}) {
    if (!pick) buildPick();
    pickState.songId = opts.songId;
    pickState.onAdded = opts.onAdded;
    pickState.song.replaceChildren(el('strong', '', opts.title || ''));
    if (opts.badge) pickState.song.append(' ', el('span', 'owner-badge', opts.badge));
    pickState.list.replaceChildren(el('li', 'hint', t('Loading…')));
    pickState.status.textContent = '';
    if (!pick.open) pick.showModal();
    let data;
    try {
      data = await getJSON(`/api/songbook-targets?song=${opts.songId}`);
    } catch (e) {
      pickState.list.replaceChildren(el('li', 'hint', e.message));
      return;
    }
    pickState.list.replaceChildren();
    const targets = data.targets || [];
    if (!targets.length) pickState.list.append(el('li', 'hint', t('No songbook of yours to add it to.')));
    for (const x of targets) {
      const li = el('li', 'addsong-song');
      const text = el('div', 'addsong-song-text');
      text.append(el('span', 'addto-name', x.label));
      if (x.closed) text.append(' ', el('span', 'owner-badge is-closed', t('badge|closed')));
      li.append(text);
      if (x.inSongbook) {
        li.append(el('span', 'addsong-state', t('In it already')));
      } else if (!x.linkable) {
        li.append(el('span', 'addsong-state is-refused', t('Can’t be added')));
        if (x.refusalMsg) text.append(el('span', 'hint addsong-why', x.refusalMsg));
      } else {
        const b = el('button', 'addsong-add', t('Add'));
        b.type = 'button';
        b.setAttribute('aria-label', t('Add to %s', x.label));
        b.addEventListener('click', async () => {
          Forms.busy(b, true);
          const res = await post(`/api/songbooks/${x.id}/entries`, { variantId: Number(opts.songId) });
          if (!res || !res.ok) {
            Forms.busy(b, false);
            Forms.status(pickState.status, await refusal(res, t('Couldn’t add it. Try again.')), 'error');
            return;
          }
          b.replaceWith(el('span', 'addsong-state is-added', t('Added')));
          if (pickState.onAdded) pickState.onAdded(x);
        });
        li.append(b);
      }
      pickState.list.append(li);
    }
  }

  window.AddSong = { open };
  window.AddTo = { open: openPick };

  // Buttons that open them: data-add-song="songbookId", data-add-to="songId".
  document.addEventListener('click', (e) => {
    const a = e.target.closest('[data-add-song]');
    if (a) { e.preventDefault(); open({ songbookId: Number(a.dataset.addSong) || 0, create: a.dataset.create === '1' }); return; }
    const b = e.target.closest('[data-add-to]');
    if (b) { e.preventDefault(); openPick({ songId: Number(b.dataset.addTo), title: b.dataset.title || '', badge: b.dataset.badge || '' }); }
  });
})();

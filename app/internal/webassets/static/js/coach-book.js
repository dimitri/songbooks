// The coach's Songbooks tab in a coaching community (coaching_book.html,
// httpapi/coaching_book.go): the songs as rows, a chip per student or
// group who has the song (in their key), "+ Assign" for the others; the
// search and the filter by student; the requests waiting. Assigning, a
// key, taking a song out: the existing endpoints (the song page's
// coach-keys, the community's songbook songs), the coach's only.
(() => {
  const page = document.querySelector('.cbk');
  if (!page) return;
  const community = page.dataset.community;
  const api = `/api/communities/${community}`;
  const send = (url, method, body) => fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) })
    .then((r) => { if (!r.ok) return r.json().catch(() => ({})).then((d) => { throw new Error(d.error || ''); }); return r; });
  // A key moved by n, named as the server does (chordsym.KeyName).
  const MAJOR = ['C', 'Db', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'Ab', 'A', 'Bb', 'B'];
  const MINOR = ['C', 'C#', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'G#', 'A', 'Bb', 'B'];
  const PC = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };
  const keyName = (key, n) => {
    const m = /^([A-G])([#b]?)(m(?!aj))?/.exec(key || '');
    if (!m) return '';
    const pc = (((PC[m[1]] + (m[2] === '#' ? 1 : m[2] === 'b' ? -1 : 0) + n) % 12) + 12) % 12;
    return m[3] ? MINOR[pc] + 'm' : MAJOR[pc];
  };
  const signed = (n) => (n > 0 ? `+${n}` : n < 0 ? `−${-n}` : '');
  // "A (+2)", "A (original)": the picker's.
  const keyText = (key, n) => {
    const name = keyName(key, n), off = n ? signed(n) : t('original');
    return name ? `${name} (${off})` : off;
  };
  // "G −2", "G": a chip's.
  const chipKey = (key, n) => [keyName(key, n), signed(n)].filter(Boolean).join(' ');
  const clamp = (n) => Math.max(-12, Math.min(12, n));

  // ---- Search and filter ------------------------------------------------
  const search = document.getElementById('cbk-search');
  const filter = document.getElementById('cbk-filter');
  const none = document.getElementById('cbk-none');
  const fold = (s) => s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
  function apply() {
    const q = fold(search?.value.trim() || '');
    const who = filter?.value || '';
    let shown = 0;
    page.querySelectorAll('.cbk-row').forEach((row) => {
      const ws = row.dataset.who ? row.dataset.who.split(' ') : [];
      const ok = (!q || fold(row.dataset.search).includes(q))
        && (!who || (who === 'none' ? ws.length === 0 : ws.includes(who)));
      row.hidden = !ok;
      shown += ok ? 1 : 0;
    });
    if (none) none.hidden = shown > 0;
  }
  search?.addEventListener('input', apply);
  filter?.addEventListener('change', apply);
  // The filter by student remembered in the address (?who=s:56).
  const who = new URLSearchParams(location.search).get('who');
  if (who && filter && [...filter.options].some((o) => o.value === who)) { filter.value = who; apply(); }
  filter?.addEventListener('change', () => {
    const u = new URL(location.href);
    if (filter.value) u.searchParams.set('who', filter.value); else u.searchParams.delete('who');
    history.replaceState(null, '', u);
  });

  // ---- Add a song: the one Add / Create dialog where there is one
  // (add-song.js), else the repertoire's own page and its dialog. -------
  const addBtn = document.getElementById('cbk-add');
  addBtn?.addEventListener('click', (e) => {
    if (window.AddSong) return; // add-song.js takes the click (data-add-song)
    e.preventDefault();
    const go = () => { location.href = addBtn.dataset.fallback; };
    const s = document.createElement('script');
    s.src = '/static/js/add-song.js';
    s.onload = () => {
      if (window.AddSong) window.AddSong.open({ songbookId: Number(addBtn.dataset.addSong), onAdded: () => location.reload(), onCreated: () => location.reload() });
      else go();
    };
    s.onerror = go;
    document.head.append(s);
  }, { capture: true });

  // ---- A chip, "+ Assign": one dialog ------------------------------------
  const dlg = page.querySelector('.cbk-dialog');
  const form = dlg.querySelector('form');
  const title = dlg.querySelector('.cbk-dialog-title');
  const targetsBox = dlg.querySelector('.cbk-targets');
  const choices = dlg.querySelector('.cbk-choices');
  const out = dlg.querySelector('.cbook-add-key output');
  const strip = dlg.querySelector('.cbk-strip');
  const status = dlg.querySelector('.form-status');
  const save = dlg.querySelector('.cbk-save');
  const remove = dlg.querySelector('.cbk-remove');
  let cur = null; // {row, variant, key, target, semis, chip}
  const showKey = () => { out.textContent = keyText(cur.key, cur.semis); window.VoiceStrip?.key(strip, cur.semis); };
  const showStrip = () => {
    const x = cur.target;
    if (x && x.kind === 'student') window.VoiceStrip?.show(strip, { variant: cur.variant, community, person: x.personId, coach: true, semitones: cur.semis });
    else window.VoiceStrip?.hide(strip);
  };
  dlg.querySelector('.coach-key-down').addEventListener('click', () => { cur.semis = clamp(cur.semis - 1); showKey(); });
  dlg.querySelector('.coach-key-up').addEventListener('click', () => { cur.semis = clamp(cur.semis + 1); showKey(); });
  dlg.querySelector('.cbk-cancel').addEventListener('click', () => dlg.close());

  // Whom the song can go to (the song page's coach-keys): this
  // community's students and groups, the key each has.
  async function targetsFor(variant) {
    const r = await fetch(`/api/songs/${variant}/coach-keys?community=${community}`);
    if (!r.ok) throw new Error();
    const d = await r.json();
    return { key: d.key || '', targets: (d.targets || []).filter((x) => String(x.communityId) === community) };
  }
  const same = (x, chip) => (chip.dataset.kind === 'group' ? x.kind === 'group' && String(x.groupId) === chip.dataset.group : x.kind === 'student' && String(x.personId) === chip.dataset.person);

  async function open(row, chip) {
    const variant = Number(row.dataset.variant);
    cur = { row, variant, key: '', target: null, semis: chip ? Number(chip.dataset.semitones) || 0 : 0, chip };
    status.textContent = '';
    choices.replaceChildren();
    window.VoiceStrip?.hide(strip);
    title.textContent = chip ? `${chip.dataset.name} · ${row.dataset.title}` : t('Assign %s', row.dataset.title);
    save.textContent = chip ? t('Save the key') : t('Assign');
    remove.hidden = !chip;
    if (chip) remove.textContent = t('Take it out of %s’s songbook', chip.dataset.name);
    targetsBox.hidden = !!chip;
    out.textContent = '';
    dlg.showModal();
    let d;
    try { d = await targetsFor(variant); } catch (e) { status.textContent = t('Couldn’t load your students.'); return; }
    cur.key = d.key;
    if (chip) {
      cur.target = d.targets.find((x) => same(x, chip)) || null;
    } else {
      const free = d.targets.filter((x) => !x.inSongbook);
      free.forEach((x, i) => {
        const label = document.createElement('label');
        label.className = 'program-plan-choice';
        const input = document.createElement('input');
        input.type = 'radio';
        input.name = 'target';
        input.value = i;
        input.addEventListener('change', () => { cur.target = x; cur.semis = 0; showKey(); showStrip(); });
        const name = document.createElement('strong');
        name.textContent = x.name;
        const hint = document.createElement('span');
        hint.className = 'hint';
        hint.textContent = [x.kind === 'group' ? t('group') : '', x.nextSessionLabel || ''].filter(Boolean).join(' · ');
        label.append(input, ' ', name, ' ', hint);
        choices.append(label);
      });
      if (!free.length) choices.textContent = t('Every student and group has it already.');
    }
    showKey();
    showStrip();
  }

  // A chip, made or brought up to date, after a save.
  function putChip(target, semis, songbookId) {
    const box = cur.row.querySelector('.cbk-assigned');
    let chip = cur.chip || [...box.querySelectorAll('.cbk-chip')].find((c) => same(target, c));
    if (!chip) {
      chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'cbk-chip' + (target.kind === 'group' ? ' is-group' : '');
      chip.dataset.kind = target.kind;
      chip.dataset.person = target.personId || 0;
      chip.dataset.group = target.groupId || 0;
      chip.dataset.name = target.name;
      chip.title = t('%s: the key, or take it out', target.name);
      const n = document.createElement('span');
      n.className = 'cbk-chip-name';
      n.textContent = target.kind === 'group' ? target.name : target.name.split(/\s+/)[0];
      chip.append(n);
      box.insertBefore(chip, box.querySelector('.cbk-assign'));
    }
    chip.dataset.semitones = semis;
    chip.dataset.songbook = songbookId;
    chip.querySelector('.cbk-chip-key')?.remove();
    const k = chipKey(cur.key, semis);
    if (k) {
      const s = document.createElement('span');
      s.className = 'cbk-chip-key';
      s.textContent = k;
      chip.append(' ', s);
    }
    whoOf(cur.row);
  }
  function whoOf(row) {
    row.dataset.who = [...row.querySelectorAll('.cbk-chip')].map((c) => (c.dataset.kind === 'group' ? `g:${c.dataset.group}` : `s:${c.dataset.person}`)).join(' ');
  }

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const x = cur.target;
    if (!x) { status.textContent = t('Pick a student or a group.'); return; }
    save.disabled = true;
    try {
      const body = { communityId: Number(community), semitones: cur.semis };
      if (x.kind === 'group') body.groupId = x.groupId; else body.personId = x.personId;
      const r = await send(`/api/songs/${cur.variant}/coach-keys`, 'POST', body);
      const d = await r.json();
      putChip(x, d.semitones, d.songbookId);
      dlg.close();
    } catch (err) {
      status.textContent = err.message || t('Couldn’t add it.');
    } finally {
      save.disabled = false;
    }
  });
  remove.addEventListener('click', async () => {
    const chip = cur.chip;
    remove.disabled = true;
    try {
      await send(`${api}/songbooks/${chip.dataset.songbook}/songs/${cur.variant}`, 'DELETE');
      chip.remove();
      whoOf(cur.row);
      dlg.close();
    } catch (err) {
      status.textContent = err.message || t('Couldn’t take it out.');
    } finally {
      remove.disabled = false;
    }
  });
  page.addEventListener('click', (e) => {
    const chip = e.target.closest('.cbk-chip');
    const assign = e.target.closest('.cbk-assign');
    if (chip) open(chip.closest('.cbk-row'), chip);
    else if (assign) open(assign.closest('.cbk-row'), null);
  });

  // ---- The requests: added (in the key picked) or not now ---------------
  page.querySelectorAll('.cbook-request').forEach((row) => {
    const rout = row.querySelector('output');
    const rstrip = row.querySelector('.cbook-request-strip');
    const move = (d) => {
      const n = clamp((Number(row.dataset.semitones) || 0) + d);
      row.dataset.semitones = n;
      if (rout) rout.textContent = keyText(row.dataset.key, n);
      window.VoiceStrip?.key(rstrip, n);
    };
    window.VoiceStrip?.show(rstrip, { variant: Number(row.dataset.variant), community, person: Number(row.dataset.person), coach: true, semitones: 0 });
    row.querySelector('.coach-key-down')?.addEventListener('click', () => move(-1));
    row.querySelector('.coach-key-up')?.addEventListener('click', () => move(1));
    const gone = () => {
      row.remove();
      const badge = page.querySelector(`.cbook-badge[data-person="${row.dataset.person}"]`);
      if (badge) {
        const n = Number(badge.dataset.count) - 1;
        badge.dataset.count = n;
        if (n > 0) badge.textContent = tn(n, '%d request', '%d requests'); else badge.remove();
      }
      const left = page.querySelectorAll('.cbook-request').length;
      const count = page.querySelector('.cbook-requests .cbook-count');
      if (count) count.textContent = left;
      if (!left) page.querySelector('.cbook-requests')?.remove();
    };
    row.querySelector('.cbook-request-add').addEventListener('click', (e) => {
      e.target.disabled = true;
      send(`${api}/song-requests/${row.dataset.request}/add`, 'POST', { semitones: Number(row.dataset.semitones) || 0 })
        .then(() => { gone(); window.SongbooksUnread?.refresh(); setTimeout(() => location.reload(), 300); })
        .catch((err) => { e.target.disabled = false; alert(err.message || t('Couldn’t add it.')); });
    });
    row.querySelector('.cbook-request-decline').addEventListener('click', (e) => {
      e.target.disabled = true;
      send(`${api}/song-requests/${row.dataset.request}/decline`, 'POST')
        .then(() => { gone(); window.SongbooksUnread?.refresh(); })
        .catch((err) => { e.target.disabled = false; alert(err.message || t('Couldn’t save.')); });
    });
  });
  page.querySelector('.cbook-request.is-open')?.scrollIntoView({ block: 'center' });
})();

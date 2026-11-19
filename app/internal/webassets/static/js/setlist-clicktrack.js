// Setlist click-track mode: a self-contained, deliberately small metronome
// (Clock/Metronome engines from clock.js, same lookahead scheduler as the
// standalone practice tool, but its own compact control surface — a
// performance/rehearsal aid that counts the band in and shows the song's
// click-track note, not the standalone tool's full practice-technique/
// accent/sound-pattern surface) stepping through a pre-loaded setlist.
(() => {
  const themeBtn = document.getElementById('theme-toggle');
  function applyTheme(t) { document.body.dataset.theme = t || ''; }
  try { applyTheme(localStorage.getItem('songbooks-theme') || ''); } catch (e) { /* ignore */ }
  themeBtn.addEventListener('click', () => {
    const order = ['', 'light', 'dark'];
    const next = order[(order.indexOf(document.body.dataset.theme || '') + 1) % order.length];
    applyTheme(next);
    try { localStorage.setItem('songbooks-theme', next); } catch (e) { /* ignore */ }
  });

  const dataEl = document.getElementById('clicktrack-data');
  const songs = JSON.parse(dataEl.textContent || '[]');
  if (!songs.length) return;

  const root = document.getElementById('clicktrack-root');
  const setlistId = root.dataset.setlistId;
  const canEdit = root.dataset.canEdit === '1';

  // Flatten songs into steps, inserting a "break" pseudo-step right
  // before the first song of every set after the first — an explicit
  // step the user advances through, not a silent jump.
  const steps = [];
  songs.forEach(song => {
    if (song.isFirstInSet && song.setIndex > 0) {
      steps.push({ kind: 'break', setLabel: song.setLabel, setIndex: song.setIndex });
    }
    song.step = steps.length;
    steps.push({ kind: 'song', song });
  });

  const posEl = document.getElementById('clicktrack-position');
  const titleEl = document.getElementById('clicktrack-title');
  const metaEl = document.getElementById('clicktrack-meta');
  const entryNoteEl = document.getElementById('clicktrack-entry-note');
  const prevBtn = document.getElementById('clicktrack-prev');
  const nextBtn = document.getElementById('clicktrack-next');
  const nowPlayingCard = document.getElementById('clicktrack-now-playing');
  const breakCard = document.getElementById('clicktrack-break');
  const breakLabel = document.getElementById('clicktrack-break-label');
  const breakContinue = document.getElementById('clicktrack-break-continue');
  const metronomeWrap = document.getElementById('clicktrack-metronome-wrap');
  const songListEl = document.getElementById('clicktrack-songlist');
  const songsFold = document.getElementById('clicktrack-songs-fold');
  const wide = window.matchMedia('(min-width: 960px)');

  // ---- Compact metronome engine — same clock.js classes the standalone
  // page uses, own small control surface. ------------------------------
  const compactEl = document.getElementById('metronome-compact');
  const bpmValueEl = document.getElementById('mc-bpm-value');
  const dotsEl = document.getElementById('mc-beat-dots');
  const playBtn = document.getElementById('mc-play');
  const tapBtn = document.getElementById('mc-tap');
  const muteBtn = document.getElementById('mc-mute');

  function beatsForSig(sig) {
    const table = { '2/4': 2, '3/4': 3, '4/4': 4, '5/4': 5, '6/8': 2, '9/8': 3, '12/8': 4 };
    return table[sig] || 4;
  }
  function isCompoundSig(sig) { return sig === '6/8' || sig === '9/8' || sig === '12/8'; }

  let bpm = 120;
  const clock = new Clock(bpm, 4);
  const metronome = new Metronome(clock);
  metronome.customAccents = new Set([0]);

  function setBpm(v) {
    bpm = Math.max(20, Math.min(300, v));
    bpmValueEl.textContent = String(Math.round(bpm));
    clock.setBpm(bpm);
  }
  function setTimeSig(sig) {
    clock.beatsPerBar = beatsForSig(sig);
    metronome.isCompound = isCompoundSig(sig);
    applyClickMode();
    renderDots();
  }

  document.getElementById('mc-bpm-down').addEventListener('click', () => setBpm(bpm - 1));
  document.getElementById('mc-bpm-up').addEventListener('click', () => setBpm(bpm + 1));
  tapBtn.addEventListener('click', () => {
    const t = clock.tapDetermine();
    if (t) setBpm(t);
  });

  // ---- Display/sound preferences: size, portrait/landscape, every-beat/
  // beat-1-only, mute. These persist across every song in the setlist
  // AND silently follow the person's account, not just this browser: a
  // local copy applies instantly (and covers the offline/logged-out
  // case), a server copy (GET on load, debounced PUT on every change) is
  // what makes it "recall whatever I last set, on any device." No
  // explicit save action — every change writes through on its own.
  // (The old 1/2/4-bar grouping of the dots is gone: the dots always
  // show one bar, as big as the screen allows.)
  const PREF_DEFAULTS = { size: 'md', orientation: 'portrait', clickMode: 'every', muted: false };
  let prefs = { ...PREF_DEFAULTS };
  try { Object.assign(prefs, JSON.parse(localStorage.getItem('songbooks-clicktrack-prefs') || '{}')); } catch (e) { /* ignore */ }
  delete prefs.group;

  let savePrefsTimer;
  function savePrefs() {
    try { localStorage.setItem('songbooks-clicktrack-prefs', JSON.stringify(prefs)); } catch (e) { /* ignore */ }
    clearTimeout(savePrefsTimer);
    savePrefsTimer = setTimeout(() => {
      fetch('/api/me/click-track-prefs', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(prefs),
      }).catch(() => { /* best-effort — the local copy already applied */ });
    }, 500);
  }

  function applyMute() {
    metronome.enabled = !prefs.muted;
    muteBtn.innerHTML = document.getElementById(prefs.muted ? 'icon-speaker-mute-tpl' : 'icon-speaker-tpl').innerHTML;
    muteBtn.setAttribute('aria-pressed', prefs.muted ? 'true' : 'false');
    muteBtn.title = prefs.muted ? t('unmute') : t('mute sound');
  }
  muteBtn.addEventListener('click', () => {
    prefs.muted = !prefs.muted;
    applyMute();
    savePrefs();
  });

  // "Beat 1 only" — both the audible click AND the dot flash fire once
  // per bar instead of every beat, for a drummer who just wants a bar
  // reference, not a full click track. Reuses clock.js's existing
  // gap-click practice mode (play 1 beat, silence the rest of the bar,
  // repeat) rather than a second silencing mechanism — the "gap" cycle
  // is just set to exactly one bar.
  function applyClickMode() {
    if (prefs.clickMode === 'beat1') {
      metronome.practiceMode = 'gap';
      metronome.gapOnBeats = 1;
      metronome.gapOffBeats = Math.max(1, clock.beatsPerBar - 1);
    } else {
      metronome.practiceMode = 'none';
    }
  }

  const pickerEffects = {
    size: v => { compactEl.dataset.size = v; },
    orientation: v => { compactEl.classList.toggle('mc-landscape', v === 'landscape'); },
    clickMode: () => { applyClickMode(); },
  };
  const pickerApply = {};
  function wireSeg(id, field) {
    const row = document.getElementById(id);
    function apply(value) {
      row.querySelectorAll('button').forEach(b => b.classList.toggle('active', b.dataset.value === value));
      row.dataset.value = value;
      pickerEffects[field](value);
    }
    row.addEventListener('click', e => {
      const btn = e.target.closest('button');
      if (!btn) return;
      prefs[field] = btn.dataset.value;
      apply(btn.dataset.value);
      savePrefs();
    });
    pickerApply[field] = apply;
    apply(prefs[field]);
  }
  wireSeg('mc-size-picker', 'size');
  wireSeg('mc-orientation-picker', 'orientation');
  wireSeg('mc-click-picker', 'clickMode');

  // The local copy above applies immediately so the page never flashes
  // defaults-then-settings; the server fetch only overrides fields it
  // actually has a saved value for (omitted/zero fields in the response
  // mean "never set on any device," not "explicitly reset to default"),
  // covering the case where the person's last change was on a different
  // device than this one.
  fetch('/api/me/click-track-prefs').then(r => (r.ok ? r.json() : null)).then(server => {
    if (!server) return;
    let changed = false;
    for (const field of ['size', 'orientation', 'clickMode']) {
      if (server[field] && server[field] !== prefs[field]) { prefs[field] = server[field]; pickerApply[field](server[field]); changed = true; }
    }
    if (typeof server.muted === 'boolean' && server.muted !== prefs.muted) { prefs.muted = server.muted; applyMute(); changed = true; }
    if (changed) { try { localStorage.setItem('songbooks-clicktrack-prefs', JSON.stringify(prefs)); } catch (e) { /* ignore */ } }
  }).catch(() => { /* offline/logged-out — local/default prefs already applied */ });

  // One dot per beat of one bar; --beats lets the CSS size them to fill
  // the width (capped by the Size option).
  function renderDots() {
    dotsEl.innerHTML = '';
    const n = clock.beatsPerBar;
    dotsEl.style.setProperty('--beats', String(n));
    for (let k = 0; k < n; k++) {
      const d = document.createElement('span');
      d.className = 'beat-dot' + (k === 0 ? ' downbeat' : '');
      dotsEl.appendChild(d);
    }
  }
  function tickDots() {
    const n = clock.beatsPerBar;
    const dots = dotsEl.children;
    if (dots.length === n) {
      const idx = clock.running ? Math.floor(clock.currentBeat()) % n : -1;
      for (let k = 0; k < dots.length; k++) {
        const lit = k === idx && (prefs.clickMode !== 'beat1' || k === 0);
        dots[k].classList.toggle('lit', lit);
      }
    }
    requestAnimationFrame(tickDots);
  }

  const iconPlay = playBtn.innerHTML;
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor"><rect x="5" y="4" width="3.4" height="12" rx="1"/><rect x="11.6" y="4" width="3.4" height="12" rx="1"/></svg>';
  function setPlaying(playing) {
    if (playing) {
      metronome._ensureCtx();
      clock.start();
      metronome.start();
      playBtn.title = t('stop');
      playBtn.innerHTML = iconPause;
    } else {
      clock.pause();
      metronome.stop();
      clock.seekToBeat(0);
      playBtn.title = t('start');
      playBtn.innerHTML = iconPlay;
    }
    compactEl.classList.toggle('playing', playing);
    document.getElementById('ct-bar')?.classList.toggle('is-playing', playing);
    try { if ('mediaSession' in navigator) navigator.mediaSession.playbackState = playing ? 'playing' : 'paused'; } catch (e) { /* none */ }
  }
  playBtn.addEventListener('click', () => setPlaying(!clock.running));
  applyMute();
  renderDots();
  tickDots();
  setBpm(bpm);

  // ---- The song's click-track note: big, right under the title, and —
  // for whoever may edit the set list — tap to edit in place, saved on
  // Enter or when the field loses focus (Escape cancels). ---------------
  const noteEl = document.getElementById('clicktrack-clicknote');
  const noteInput = document.getElementById('clicktrack-clicknote-input');
  const noteStatus = document.getElementById('clicktrack-clicknote-status');

  function showNote(song) {
    const text = song.clickNote || '';
    if (!canEdit) {
      noteEl.textContent = text;
      noteEl.hidden = !text;
      return;
    }
    // Still editing (moved on without the field losing focus): the blur
    // saves what was typed against the song it was typed for.
    if (!noteInput.hidden) noteInput.blur();
    noteEl.textContent = text || t('Tap to add a note for this song');
    noteEl.classList.toggle('empty', !text);
    noteStatus.textContent = '';
  }

  let editingSong = null;
  let cancelEdit = false;
  if (canEdit) {
    noteEl.addEventListener('click', () => {
      const step = steps[cur];
      if (step.kind !== 'song') return;
      editingSong = step.song;
      cancelEdit = false;
      noteInput.value = editingSong.clickNote || '';
      noteEl.hidden = true;
      noteInput.hidden = false;
      noteInput.focus();
      noteInput.setSelectionRange(noteInput.value.length, noteInput.value.length);
    });
    noteInput.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); noteInput.blur(); }
      else if (e.key === 'Escape') { e.preventDefault(); cancelEdit = true; noteInput.blur(); }
    });
    noteInput.addEventListener('blur', () => {
      const song = editingSong;
      editingSong = null;
      noteInput.hidden = true;
      noteEl.hidden = false;
      if (!song) return;
      const value = noteInput.value.trim();
      if (cancelEdit || value === (song.clickNote || '')) {
        cancelEdit = false;
        if (steps[cur].song === song) showNote(song);
        return;
      }
      saveNote(song, value);
    });
  }

  function saveNote(song, value) {
    const previous = song.clickNote || '';
    // Every entry of this song in the set list shares the note.
    const same = songs.filter(s => s.variantId === song.variantId);
    same.forEach(s => { s.clickNote = value; });
    if (steps[cur].song === song) showNote(song);
    noteStatus.textContent = '';
    fetch(`/api/setlists/${setlistId}/clicktrack-notes/${song.variantId}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ note: value }),
    }).then(r => (r.ok ? r.json() : Promise.reject(r))).then(res => {
      same.forEach(s => { s.clickNote = res.note || ''; });
      if (steps[cur].song === song) { showNote(song); noteStatus.textContent = t('Saved'); }
      renderSongList();
    }).catch(() => {
      same.forEach(s => { s.clickNote = previous; });
      if (steps[cur].song === song) { showNote(song); noteStatus.textContent = t('Could not save the note.'); }
    });
  }

  // ---- The song list: every song in order under its set's name; tap
  // one to jump there and start the click on it. ------------------------
  document.getElementById('clicktrack-songs-count').textContent = String(songs.length);
  function renderSongList() {
    songListEl.innerHTML = '';
    let lastSet = -1;
    songs.forEach(song => {
      if (song.setIndex !== lastSet && song.setLabel) {
        const h = document.createElement('li');
        h.className = 'clicktrack-songlist-set';
        h.textContent = song.setLabel;
        songListEl.appendChild(h);
      }
      lastSet = song.setIndex;
      const li = document.createElement('li');
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'clicktrack-songlist-item';
      btn.dataset.step = String(song.step);
      if (song.step === cur) btn.setAttribute('aria-current', 'true');
      const num = document.createElement('span');
      num.className = 'clicktrack-songlist-num';
      num.textContent = String(song.index + 1);
      const title = document.createElement('span');
      title.className = 'clicktrack-songlist-title';
      title.textContent = song.title;
      const tempo = document.createElement('span');
      tempo.className = 'clicktrack-songlist-tempo';
      tempo.textContent = `${song.tempo} bpm`;
      btn.append(num, title, tempo);
      li.appendChild(btn);
      songListEl.appendChild(li);
    });
  }
  songListEl.addEventListener('click', e => {
    const btn = e.target.closest('.clicktrack-songlist-item');
    if (!btn) return;
    goTo(parseInt(btn.dataset.step, 10), true);
    // On a phone the list sits under the metronome: fold it and bring
    // the dots back into view.
    // On a phone the panel covers the dots: it closes on a pick.
    if (!wide.matches) shell?.open(false);
  });

  // ---- Stepper: advance through the setlist, re-dialing the compact
  // engine to each song's own tempo/time-signature. ---------------------
  const totalSongs = songs.length;
  let cur = 0;

  function render() {
    const step = steps[cur];
    if (clock.running) setPlaying(false);
    songListEl.querySelectorAll('.clicktrack-songlist-item').forEach(b => {
      if (parseInt(b.dataset.step, 10) === cur) b.setAttribute('aria-current', 'true');
      else b.removeAttribute('aria-current');
    });

    if (step.kind === 'break') {
      nowPlayingCard.hidden = true;
      metronomeWrap.hidden = true;
      breakCard.hidden = false;
      breakLabel.textContent = step.setLabel ? t('Before %s — take a break, then continue when ready.', step.setLabel) : t('Before the next set — take a break, then continue when ready.');
      prevBtn.disabled = cur === 0;
      nextBtn.disabled = false;
      renderBar();
      return;
    }

    breakCard.hidden = true;
    nowPlayingCard.hidden = false;
    metronomeWrap.hidden = false;

    const song = step.song;
    posEl.textContent = `${song.setLabel || t('Set')} · ${t('song %d of %d', song.index + 1, totalSongs)}`;
    titleEl.textContent = song.title;
    metaEl.textContent = [song.artist, song.key ? t('key %s', song.key) : '', `${song.tempo} bpm`, song.timeSig]
      .filter(Boolean).join(' · ');
    if (song.note) { entryNoteEl.textContent = song.note; entryNoteEl.hidden = false; }
    else { entryNoteEl.hidden = true; }
    showNote(song);

    setBpm(song.tempo);
    setTimeSig(song.timeSig);

    prevBtn.disabled = cur === 0;
    nextBtn.disabled = cur === steps.length - 1;
    renderBar();
  }

  // Moving to another song while the click runs keeps it running, on
  // the new song from its first beat; picking a song from the list (or
  // "Start next set" after a break) starts it. A break step always stops.
  function goTo(idx, start) {
    if (idx < 0 || idx >= steps.length) return;
    const wasRunning = clock.running;
    cur = idx;
    render();
    if ((start || wasRunning) && steps[cur].kind === 'song') setPlaying(true);
  }

  prevBtn.addEventListener('click', () => goTo(cur - 1, false));
  nextBtn.addEventListener('click', () => goTo(cur + 1, false));
  breakContinue.addEventListener('click', () => goTo(cur + 1, true));

  // ---- The player's bar and panel (player-shell.js) ---------------------
  // The bar: the song before, ▶, the song after, where you are with a copy
  // of the beat dots, the tempo. Its "Running order" opens the panel: the
  // songs and the display and sound options (open in it). The same keys
  // as every player, and the lock screen's (Media Session): the track
  // buttons go to the song before or after.
  const barTitle = document.getElementById('ct-bar-title');
  const barPos = document.getElementById('ct-bar-pos');
  const barDots = document.getElementById('ct-bar-dots');
  songsFold.open = true;
  document.getElementById('clicktrack-options-fold').open = true;
  const shell = window.PlayerShell ? window.PlayerShell.panel({
    root: document.getElementById('ct-panel-wrap'),
    card: document.getElementById('ct-panel'),
    openers: [document.getElementById('ct-panel-btn')],
    closers: document.querySelectorAll('[data-ct-close]'),
    focus: () => document.querySelector(wide.matches ? '.ct-close' : '.ct-grab'),
    swipeIgnore: '.clicktrack-songlist, input, textarea',
    onChange: (on) => { if (on) songListEl.querySelector('[aria-current="true"]')?.scrollIntoView({ block: 'nearest' }); },
  }) : null;
  document.getElementById('ct-panel-btn')?.addEventListener('click', () => shell?.open(!shell.isOpen()));
  function renderBar() {
    const step = steps[cur];
    if (step.kind === 'break') {
      barTitle.textContent = t('Break');
      barPos.textContent = step.setLabel || '';
    } else {
      barTitle.textContent = step.song.title;
      barPos.textContent = t('%d of %d', step.song.index + 1, totalSongs) + (steps[cur + 1] && steps[cur + 1].song ? ' · ' + t('next: %s', steps[cur + 1].song.title) : '');
    }
    media?.meta({ title: barTitle.textContent, artist: step.song ? step.song.artist || '' : '' });
  }
  // The dots, copied as they light.
  if (barDots && window.MutationObserver) {
    const copy = () => { barDots.innerHTML = dotsEl.innerHTML; };
    new MutationObserver(copy).observe(dotsEl, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'] });
    copy();
  }
  const media = window.PlayerShell ? window.PlayerShell.media({
    title: '', play: () => setPlaying(true), pause: () => setPlaying(false),
    previous: () => goTo(cur - 1, false), next: () => goTo(cur + 1, false),
  }) : null;
  if (window.PlayerShell) window.PlayerShell.keys({
    title: t('The click track’s keys'),
    own: '#ct-bar, #ct-panel',
    active: () => !document.querySelector('.clicktrack-clicknote-input:not([hidden]):focus'),
    keys: {
      ' ': () => setPlaying(!clock.running), k: () => setPlaying(!clock.running),
      ArrowLeft: () => goTo(cur - 1, false), ArrowRight: () => goTo(cur + 1, false),
      'Shift+ArrowLeft': () => goTo(cur - 1, false), 'Shift+ArrowRight': () => goTo(cur + 1, false),
      '[': () => setBpm(bpm - 1), ']': () => setBpm(bpm + 1), '{': () => setBpm(bpm - 5), '}': () => setBpm(bpm + 5),
      '\\': () => { if (steps[cur].song) setBpm(steps[cur].song.tempo); },
      m: () => muteBtn.click(), t: () => tapBtn.click(),
      f: () => shell?.open(!shell.isOpen()),
    },
    help: [
      [t('Play / stop'), ' ', 'k'],
      [t('Previous / next song'), 'ArrowLeft', 'ArrowRight'],
      [t('Tempo −1 / +1 (⇧ for 5) · \\ back to the song’s'), '[', ']', '\\'],
      [t('Mute · tap the tempo'), 'm', 't'],
      [t('Open / close the running order · close · these shortcuts'), 'f', 'Escape', '?'],
    ],
  });

  renderSongList();
  render();
})();

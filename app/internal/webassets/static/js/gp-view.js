// Draws a song's Guitar Pro file on its page (docs/gp-file-model.md §5):
//
// - every ".gp-score" placeholder a "gp[Track]: bars" line left in a
//   section (gpview.go, play.html) gets those bars of that track, or of
//   every track, in tab, staff or both;
// - the Score view (#gp-score-page) shows the whole file the Guitar Pro
//   way: tracks to pick, playback with a cursor, the file's own backing
//   track, speed, loop, metronome and count-in;
// - a file stored before facts existed gets them read here and sent back
//   (PUT /api/songs/{id}/gp-file/facts), so drift checks start working.
//
// The file is fetched once per page (the server sends an ETag, so the
// browser keeps it) and parsed again for each drawing: alphaTab's model
// is changed by what it draws (which staves show), so drawings don't
// share one.
(function () {
  if (typeof alphaTab === 'undefined' || typeof GPFacts === 'undefined') return;
  const sheet = document.getElementById('sheet');
  const url = sheet && sheet.dataset.gpFile;
  if (!url) return;
  const variantId = sheet.dataset.variantId;

  let bytesPromise = null;
  function fileBytes() {
    if (!bytesPromise) {
      bytesPromise = fetch(url, { credentials: 'same-origin' })
        .then((r) => { if (!r.ok) throw new Error('could not load the Guitar Pro file'); return r.arrayBuffer(); })
        .then((b) => new Uint8Array(b));
    }
    return bytesPromise;
  }
  // Guitar Pro files often mark chords "show diagram" with no fingering
  // at all (only the name was wanted): drawn, those are empty boxes. raw
  // leaves the model as the file has it, for editing and saving.
  async function loadScore(raw) {
    const score = alphaTab.importer.ScoreLoader.loadScoreFromBytes(await fileBytes(), new alphaTab.Settings());
    if (raw) return score;
    for (const t of score.tracks) {
      for (const st of t.staves) {
        if (!st.chords) continue;
        for (const c of st.chords.values()) if (!(c.strings || []).some((f) => f >= 0)) c.showDiagram = false;
      }
    }
    return score;
  }

  const css = () => getComputedStyle(document.body);
  function color(name, fallback) {
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(css().getPropertyValue(name).trim());
    return m ? new alphaTab.model.Color(parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)) : fallback;
  }

  function baseSettings() {
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.display.staveProfile = alphaTab.StaveProfile.Default;
    s.display.layoutMode = alphaTab.LayoutMode.Page;
    s.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
    const fg = color('--fg', new alphaTab.model.Color(0x24, 0x1e, 0x18));
    const r = s.display.resources;
    r.staffLineColor = color('--fg-muted', fg);
    r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = fg;
    return s;
  }

  function hide(s, names) {
    for (const n of names) if (alphaTab.NotationElement[n] !== undefined) s.notation.elements.set(alphaTab.NotationElement[n], false);
  }
  const SCORE_INFO = ['ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright'];

  // What kind of part a track is, for the reader's notation choice.
  function kindOf(track) {
    const st = track.staves[0];
    if (st.isPercussion) return 'drums';
    const p = track.playbackInfo ? track.playbackInfo.program : 0;
    if ((p >= 32 && p <= 39) || (st.tuning && st.tuning.length && st.tuning.length <= 5 && Math.min(...st.tuning) < 40)) return 'bass';
    return 'guitar';
  }

  // The notation to draw a track in: what the reader chose for that kind
  // of part, when they chose; else what the line asks for; else the
  // reader's default (tab for guitar, both for bass).
  function notationFor(track, asked) {
    const kind = kindOf(track);
    if (kind === 'drums') return 'staff';
    const chosen = (document.body.dataset.notationChosen || '').split(',').includes(kind);
    const mine = window.songbooksNotation ? window.songbooksNotation.get(kind) : (kind === 'bass' ? 'both' : 'tab');
    const v = chosen || !asked ? mine : asked;
    return v === 'tab+staff' ? 'both' : v;
  }

  function applyNotation(track, notation) {
    for (const st of track.staves) {
      if (st.isPercussion) { st.showStandardNotation = true; st.showTablature = false; continue; }
      st.showSlash = notation === 'slash';
      st.showTablature = notation === 'tab' || notation === 'both';
      st.showStandardNotation = notation === 'staff' || notation === 'both';
      st.showNumbered = false;
    }
  }

  function failed(el, msg) {
    el.innerHTML = '';
    const p = document.createElement('p');
    p.className = 'gp-score-error';
    p.textContent = msg;
    el.appendChild(p);
  }

  // ---- Section placeholders ---------------------------------------------

  async function drawSection(fig) {
    const body = fig.querySelector('.gp-score-body');
    if (!body || body.dataset.drawn) return;
    if (!body.clientWidth) return; // hidden in this view; drawn when shown
    body.dataset.drawn = '1';
    let score;
    try { score = await loadScore(); } catch (e) { failed(body, e.message); return; }
    let tracks;
    if (fig.dataset.trackName) {
      let i = Number(fig.dataset.track);
      if (!(i >= 0)) i = GPFacts.findTrack(score.tracks, fig.dataset.trackName);
      if (i < 0) { failed(body, 'The file has no track “' + fig.dataset.trackName + '”.'); return; }
      tracks = [i];
    } else {
      tracks = score.tracks.map((t, i) => i);
    }
    const ranges = (fig.dataset.bars || '').split(',').filter(Boolean).map((r) => r.split('-').map(Number));
    for (const [k, [from, to]] of ranges.entries()) {
      if (k > 0) score = await loadScore();
      for (const i of tracks) applyNotation(score.tracks[i], notationFor(score.tracks[i], fig.dataset.notation));
      const host = document.createElement('div');
      host.className = 'gp-score-range';
      body.appendChild(host);
      const s = baseSettings();
      s.player.enablePlayer = false;
      s.display.startBar = from;
      s.display.barCount = to - from + 1;
      hide(s, SCORE_INFO.concat(['GuitarTuning', 'ChordDiagrams', tracks.length === 1 ? 'TrackNames' : '', 'EffectTempo', 'EffectDynamics']));
      try {
        const api = new alphaTab.AlphaTabApi(host, s);
        api.renderScore(score, tracks);
      } catch (e) {
        failed(host, 'Couldn’t draw these bars.');
      }
    }
  }

  function drawSections() {
    sheet.querySelectorAll('.gp-score').forEach((fig) => { drawSection(fig); });
  }

  // ---- The Score view -----------------------------------------------------

  const page = document.getElementById('gp-score-page');
  let full = null; // { api, score, shown: Set }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text) e.textContent = text;
    return e;
  }

  async function showFull() {
    if (!page || full) return;
    const canvas = page.querySelector('.gp-score-canvas');
    const trackBar = page.querySelector('.gp-tracks');
    let score;
    try { score = await loadScore(); } catch (e) { failed(canvas, e.message); return; }
    const s = baseSettings();
    s.player.enablePlayer = true;
    s.player.enableCursor = true;
    s.player.enableUserInteraction = true;
    s.player.soundFont = '/static/js/vendor/alphatab/soundfont/sonivox.sf2';
    s.player.scrollElement = page.querySelector('.gp-score-viewport');
    if (alphaTab.PlayerMode) s.player.playerMode = alphaTab.PlayerMode.EnabledAutomatic;
    hide(s, ['ScoreCopyright']);
    const api = new alphaTab.AlphaTabApi(canvas, s);
    full = { api, score, shown: new Set([0]) };

    // Start on the tracks the song's own lines name, else the first.
    const named = [...sheet.querySelectorAll('.gp-score[data-track]')].map((f) => Number(f.dataset.track)).filter((i) => i >= 0);
    if (named.length) full.shown = new Set(named);
    if (score.tracks.length <= 3 && !named.length) full.shown = new Set(score.tracks.map((t, i) => i));

    score.tracks.forEach((t, i) => {
      const b = el('button', 'gp-track-chip', t.name || ('Track ' + (i + 1)));
      b.type = 'button';
      b.setAttribute('aria-pressed', full.shown.has(i));
      b.addEventListener('click', () => {
        if (full.shown.has(i) && full.shown.size > 1) full.shown.delete(i); else full.shown.add(i);
        b.setAttribute('aria-pressed', full.shown.has(i));
        renderFull();
      });
      trackBar.appendChild(b);
    });

    const notationSel = page.querySelector('.gp-notation');
    notationSel?.addEventListener('change', renderFull);
    const play = page.querySelector('.gp-play');
    play?.addEventListener('click', () => api.playPause());
    page.querySelector('.gp-stop')?.addEventListener('click', () => api.stop());
    page.querySelector('.gp-speed')?.addEventListener('change', (e) => { api.playbackSpeed = Number(e.target.value) || 1; });
    page.querySelector('.gp-loop')?.addEventListener('change', (e) => { api.isLooping = e.target.checked; });
    page.querySelector('.gp-metronome')?.addEventListener('change', (e) => { api.metronomeVolume = e.target.checked ? 1 : 0; });
    page.querySelector('.gp-countin')?.addEventListener('change', (e) => { api.countInVolume = e.target.checked ? 1 : 0; });
    api.playerReady.on(() => { if (play) play.disabled = false; });
    api.playerStateChanged.on((e) => {
      if (!play) return;
      const playing = e.state === 1;
      play.textContent = playing ? '❚❚' : '▶';
      play.setAttribute('aria-label', playing ? 'Pause' : 'Play');
    });
    const time = page.querySelector('.gp-time');
    const fmt = (ms) => { const t = Math.max(0, Math.round(ms / 1000)); return Math.floor(t / 60) + ':' + String(t % 60).padStart(2, '0'); };
    api.playerPositionChanged.on((e) => { if (time) time.textContent = fmt(e.currentTime) + ' / ' + fmt(e.endTime); });
    renderFull();
  }

  function renderFull() {
    if (!full) return;
    const pick = page.querySelector('.gp-notation');
    const tracks = [...full.shown].sort((a, b) => a - b);
    for (const i of tracks) {
      const t = full.score.tracks[i];
      applyNotation(t, pick && pick.value ? (kindOf(t) === 'drums' ? 'staff' : pick.value) : notationFor(t, ''));
    }
    // alphaTab adds a new drawing's parts next to the previous ones.
    const surface = page.querySelector('.gp-score-canvas .at-surface');
    if (surface) surface.replaceChildren();
    full.api.renderScore(full.score, tracks);
  }

  // ---- Facts for files stored before they existed ---------------------------

  async function backfillFacts() {
    if (!sheet.dataset.gpFactsMissing || !variantId) return;
    try {
      const facts = GPFacts.extract(await loadScore());
      const res = await fetch(`/api/songs/${variantId}/gp-file/facts`, {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sha256: sheet.dataset.gpSha, facts }),
      });
      if (res.ok) delete sheet.dataset.gpFactsMissing;
    } catch (e) { /* tried again on the next visit */ }
  }

  // ---- Wiring ---------------------------------------------------------------

  function onMode() {
    const mode = sheet.dataset.renderMode;
    if (mode === 'score') showFull();
    else drawSections();
  }
  new MutationObserver(onMode).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });
  document.addEventListener('songbooks:notation', () => {
    sheet.querySelectorAll('.gp-score-body').forEach((b) => { b.innerHTML = ''; delete b.dataset.drawn; });
    drawSections();
    renderFull();
  });
  function start() {
    onMode();
    backfillFacts();
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start); else start();

  // saveScore writes score as a new version of the song's file (GPFile.save).
  async function saveScore(score) {
    const out = await GPFile.save(variantId, score, sheet.dataset.gpName);
    sheet.dataset.gpName = out.name;
    return out;
  }

  window.GPView = { showFull, drawSections, fileBytes, loadScore, saveScore };
})();

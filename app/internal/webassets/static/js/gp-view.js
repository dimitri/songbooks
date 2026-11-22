// Draws a song's Guitar Pro file on its page (docs/gp-file-model.md §5):
//
// - every ".gp-score" placeholder a "gp[Track]: bars" line left in a
//   section (gpview.go, play.html) gets those bars of that track, or of
//   every track, in tab, staff or both;
// - the Score view (#gp-score-page) shows the whole file as a sheet of
//   music (A4 pages), its tracks to pick, played in the song's player
//   (the dock) with a cursor that follows it; a song with no recording
//   plays the file through alphaTab's synthesizer (see "The Score view");
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
        .then((r) => { if (!r.ok) throw new Error(t('Could not load the Guitar Pro file.')); return r.arrayBuffer(); })
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
      if (i < 0) { failed(body, t('The file has no track “%s”.', fig.dataset.trackName)); return; }
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
        failed(host, t('Couldn’t draw these bars.'));
      }
    }
  }

  function drawSections() {
    sheet.querySelectorAll('.gp-score').forEach((fig) => { drawSection(fig); });
  }

  // ---- The Score view -----------------------------------------------------
  // The whole file as a sheet of music: A4 pages (210 × 297) stacked down
  // the screen, white on the page's background, four bars a line where
  // they fit (fewer on a phone). The page stays paper-white in dark mode
  // too: notation is drawn once, in ink, and reads best as it prints.
  //
  // It plays in the song's player (audio-player.js, the dock), like every
  // other view: the song's recording when it has one, else the file played
  // by alphaTab's synthesizer — rendered here once, offline, and handed to
  // the dock as its sound (songbooksPlayer.offerSynth), so the bar shows a
  // waveform and every tool works on it. alphaTab's own player follows
  // the dock (PlayerMode.EnabledExternalMedia): the cursor moves with the
  // dock's bar.beat; a click on a note moves the dock there; bars dragged
  // across become the dock's loop, and the dock's loop is shaded on the
  // score.
  //
  // A file next to the chart (or inside it: not "content: gp") is the
  // Score view's only: there the dock plays the file's sound even when the
  // song has a recording, the recording following it bar by bar (the
  // simplified mixer: Recording and Score, a level and a mute each, the
  // full Mixer a tap away); leaving the view gives the dock back its
  // recording, or none. The song's other views never play the file's
  // score.

  const page = document.getElementById('gp-score-page');
  const SOUNDFONT = '/static/js/vendor/alphatab/soundfont/sonivox.sf2';
  const ink = (hex) => new alphaTab.model.Color(parseInt(hex.slice(1, 3), 16), parseInt(hex.slice(3, 5), 16), parseInt(hex.slice(5, 7), 16));
  // { api, score, shown: Set, geo, played: [MasterBarTickLookup], barMs: [], quiet, lastMs, playing, cycleKey }
  let full = null;

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text) e.textContent = text;
    return e;
  }

  // The page's size, from the room there is: about an A4 sheet on a
  // screen (860 px wide at most), its margins in proportion; on a phone
  // the whole width, the notation a little smaller, and as many bars a
  // line as stay readable (170 px of notation each).
  function geometry() {
    const wrap = page.querySelector('.gp-sheet-wrap');
    const avail = Math.max(280, wrap.clientWidth);
    const phone = avail < 600;
    const pageW = Math.min(avail, 860);
    const mx = Math.round(pageW * (phone ? 0.04 : 0.075));
    const my = Math.round(pageW * (phone ? 0.06 : 0.07));
    const scale = phone ? 0.82 : 1;
    const contentW = pageW - 2 * mx;
    const perRow = Math.max(1, Math.min(4, Math.floor(contentW / scale / 170)));
    return { phone, pageW, pageH: Math.round((pageW * 297) / 210), mx, my, gap: phone ? 14 : 28, scale, perRow, contentW };
  }
  function applyGeometry(g) {
    const sheetEl = page.querySelector('.gp-sheet');
    sheetEl.style.width = `${g.pageW}px`;
    sheetEl.style.setProperty('--page-h', `${g.pageH}px`);
    sheetEl.style.setProperty('--page-gap', `${g.gap}px`);
    const canvas = page.querySelector('.gp-score-canvas');
    canvas.style.marginLeft = `${g.mx}px`;
    canvas.style.width = `${g.contentW}px`;
  }

  // Lays alphaTab's lines out on pages: each line (a "partial", one
  // system) goes on the page it fits on, never across two; the sheet
  // grows by whole pages. Everything alphaTab knows about where things
  // are (boundsLookup: the cursor, clicks, the loop's shading) moves with
  // its line.
  function paginate() {
    if (!full) return;
    const g = full.geo;
    const surface = page.querySelector('.gp-score-canvas .at-surface');
    const lookup = full.api.renderer.boundsLookup;
    if (!surface || !lookup || lookup._paged) return;
    lookup._paged = true;
    surface.querySelectorAll('svg text').forEach((tx) => { if (/rendered by alphaTab/i.test(tx.textContent)) tx.closest('div')?.remove(); });
    const parts = [...surface.children]
      .map((div) => ({ div, top: parseFloat(div.style.top) || 0, h: parseFloat(div.style.height) || div.offsetHeight }))
      .filter((p) => p.h > 0)
      .sort((a, b) => a.top - b.top);
    const pageTop = (k) => k * (g.pageH + g.gap);
    let k = 0, y = g.my;
    const shifts = [];
    for (const p of parts) {
      if (y + p.h > pageTop(k) + g.pageH - g.my && y > pageTop(k) + g.my + 1) { k++; y = pageTop(k) + g.my; }
      shifts.push({ top: p.top, bottom: p.top + p.h, dy: y - p.top });
      p.div.style.top = `${y}px`;
      y += p.h;
    }
    const pages = k + 1;
    const height = pageTop(k) + g.pageH;
    surface.style.height = `${height}px`;
    const sheetEl = page.querySelector('.gp-sheet');
    sheetEl.style.height = `${height}px`;
    const pagesEl = page.querySelector('.gp-pages');
    pagesEl.replaceChildren(...Array.from({ length: pages }, (_, i) => {
      const d = el('div', 'gp-page');
      d.style.top = `${pageTop(i)}px`;
      d.dataset.page = String(i + 1);
      return d;
    }));
    page.querySelector('.gp-sheet-count').textContent = pages > 1 ? tn(pages, '%d page', '%d pages') : '';
    // Move what alphaTab measured with its line (every bounds object once).
    const dyAt = (y0) => (shifts.find((s) => y0 >= s.top - 1 && y0 < s.bottom + 1) || { dy: 0 }).dy;
    const seen = new Set();
    const shift = (o, dy) => {
      if (!o || typeof o !== 'object' || seen.has(o)) return;
      seen.add(o);
      for (const key of ['visualBounds', 'realBounds', 'lineAlignedBounds', 'noteHeadBounds']) {
        const b = o[key];
        if (b && !seen.has(b) && typeof b.y === 'number') { b.y += dy; seen.add(b); }
      }
      for (const key of ['bars', 'beats', 'notes']) if (Array.isArray(o[key])) o[key].forEach((c) => shift(c, dy));
    };
    for (const sys of lookup.staffSystems || []) {
      const b = sys.realBounds || sys.visualBounds;
      if (b) shift(sys, dyAt(b.y + Math.min(2, b.h / 2)));
    }
    full.cycleKey = null; // shade the loop again, where it now is
  }

  // The time in alphaTab's own clock (ms, the synthesizer's) when each
  // bar starts, as played (repeats unfolded): from the tempo changes
  // alphaTab's tick cache records for each bar (960 ticks a quarter note).
  function barTimes(api, score) {
    const played = api.tickCache ? api.tickCache.masterBars : [];
    const out = [];
    let ms = 0, tempo = score.tempo || 120;
    for (const mb of played) {
      out.push(ms);
      let tick = mb.start;
      for (const tc of mb.tempoChanges || []) {
        if (tc.tick > tick) { ms += ((tc.tick - tick) * 60000) / (tempo * 960); tick = tc.tick; }
        tempo = tc.tempo;
      }
      ms += ((mb.end - tick) * 60000) / (tempo * 960);
    }
    out.push(ms);
    return { played, barMs: out };
  }
  // Played bar (fractional) ↔ alphaTab's clock.
  function msAtBar(x) {
    const t = full.barMs, n = t.length - 1;
    if (n < 1) return 0;
    const i = Math.max(0, Math.min(n - 1, Math.floor(x)));
    return t[i] + Math.max(0, Math.min(1, x - i)) * (t[i + 1] - t[i]);
  }
  function barAtMs(ms) {
    const t = full.barMs, n = t.length - 1;
    let i = 0;
    while (i < n - 1 && t[i + 1] <= ms) i++;
    const span = t[i + 1] - t[i];
    return i + (span > 0 ? Math.max(0, Math.min(1, (ms - t[i]) / span)) : 0);
  }
  const playedAtTick = (tick) => {
    const p = full.played.findIndex((mb) => tick >= mb.start && tick < mb.end);
    return p < 0 ? (tick >= (full.played.at(-1)?.end ?? 0) ? full.played.length - 1 : 0) : p;
  };
  // While this script moves alphaTab's player itself, it isn't the reader.
  function quietly(fn) {
    full.quiet = true;
    try { fn(); } finally { full.quiet = false; }
  }

  // The sound of a song with no recording: the file through alphaTab's
  // synthesizer, rendered offline, a few seconds at a time, into a WAV
  // the dock plays (and draws the waveform of).
  async function renderSynth() {
    const P = window.songbooksPlayer;
    if (!P || full.synthStarted) return;
    full.synthStarted = true;
    const { recorded } = await P.ready;
    // Next to a chart the file's sound is rendered all the same: the
    // Score view plays it, the recording beside it.
    if (recorded && !mixMode()) return;
    const api = full.api, score = full.score;
    P.synthProgress(t('Preparing the sound…'));
    try {
      const sf = await fetch(SOUNDFONT).then((r) => { if (!r.ok) throw new Error('soundfont'); return r.arrayBuffer(); });
      const opts = new alphaTab.synth.AudioExportOptions();
      opts.soundFonts = [new Uint8Array(sf)];
      opts.sampleRate = 44100;
      opts.useSyncPoints = false;
      opts.metronomeVolume = 0;
      opts.masterVolume = 1;
      const exporter = await api.exportAudio(opts);
      const chunks = [];
      let n = 0;
      try {
        for (;;) {
          const c = await exporter.render(4000);
          if (!c) break;
          chunks.push(c.samples);
          n += c.samples.length;
          if (c.endTime > 0) P.synthProgress(t('Preparing the sound… %d%', Math.min(99, Math.round((100 * c.currentTime) / c.endTime))));
        }
      } finally {
        exporter.destroy();
      }
      if (!n) throw new Error('no sound');
      const beats = full.played.map((mb) => mb.masterBar.timeSignatureNumerator || 4);
      P.offerSynth({
        bytes: wav(chunks, n, 44100), type: 'audio/wav',
        barTimes: full.barMs.map((ms) => ms / 1000), beats,
        sections: fileSections(), bpm: score.tempo || 0,
        keep: mixMode(),
      });
      if (mixMode() && sheet.dataset.renderMode === 'score') P.useSynth();
    } catch (e) {
      P.synthFailed(t('Couldn’t prepare the sound of this file.'));
      if (full.mix) full.mix.off(t('Couldn’t prepare the sound of this file.'), true);
    }
  }
  // The file's own sections (its markers), as played, for a song whose
  // chart counts its bars otherwise.
  function fileSections() {
    const out = [];
    full.played.forEach((mb, p) => {
      const sec = mb.masterBar.section;
      if (p === 0 || (sec && (!full.played[p - 1] || full.played[p - 1].masterBar !== mb.masterBar))) {
        if (p > 0 && !sec) return;
        out.push({ index: -1 - out.length, label: (sec && (sec.text || sec.marker)) || '', startBar: p, barCount: 0 });
      }
    });
    out.forEach((s, i) => { s.barCount = (out[i + 1] ? out[i + 1].startBar : full.played.length) - s.startBar; });
    return out;
  }
  // Interleaved stereo float samples → a 16-bit WAV, brought up to a
  // steady level (a synthesizer's output is often quiet).
  function wav(chunks, n, rate) {
    let peak = 0;
    for (const c of chunks) for (let i = 0; i < c.length; i++) { const v = Math.abs(c[i]); if (v > peak) peak = v; }
    const gain = peak > 0 ? Math.min(4, 0.89 / peak) : 1;
    const buf = new ArrayBuffer(44 + n * 2);
    const v = new DataView(buf);
    const str = (o, s) => { for (let i = 0; i < s.length; i++) v.setUint8(o + i, s.charCodeAt(i)); };
    str(0, 'RIFF'); v.setUint32(4, 36 + n * 2, true); str(8, 'WAVE');
    str(12, 'fmt '); v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, 2, true);
    v.setUint32(24, rate, true); v.setUint32(28, rate * 4, true); v.setUint16(32, 4, true); v.setUint16(34, 16, true);
    str(36, 'data'); v.setUint32(40, n * 2, true);
    const out = new Int16Array(buf, 44, n);
    let o = 0;
    for (const c of chunks) for (let i = 0; i < c.length; i++) { const s = Math.max(-1, Math.min(1, c[i] * gain)); out[o++] = s < 0 ? s * 0x8000 : s * 0x7fff; }
    return buf;
  }

  // alphaTab's player, made to follow the dock: what alphaTab would do
  // to a media element (play, pause, go to a time) goes to the dock; the
  // dock's place comes back as alphaTab's time, every frame.
  function mediaHandler() {
    const P = () => window.songbooksPlayer;
    const playBtn = () => document.getElementById('audio-play-btn');
    return {
      get backingTrackDuration() { return full.barMs.at(-1) || 0; },
      get playbackRate() { return P() ? P().speed() : 1; },
      set playbackRate(v) { /* the dock's speed */ },
      get masterVolume() { return 1; },
      set masterVolume(v) { /* the dock's */ },
      // alphaTab's times run at its playback speed (the dock's): ms at 90 %
      // are the synthesizer's ms / 0.9.
      seekTo(ms) {
        if (full.quiet || !P() || Math.abs(ms - full.lastMs) < 2) return;
        full.lastMs = ms;
        P().seekBar(barAtMs(ms * P().speed()), false);
      },
      play() { if (!full.quiet && P() && !P().playing()) playBtn()?.click(); },
      pause() { if (!full.quiet && P() && P().playing()) playBtn()?.click(); },
    };
  }
  function follow() {
    if (!full || sheet.dataset.renderMode !== 'score') { if (full) full.following = false; return; }
    requestAnimationFrame(follow);
    const api = full.api, P = window.songbooksPlayer, out = api.player && api.player.output;
    if (!P || !out || !full.barMs.length) return;
    const rate = P.speed();
    if (api.playbackSpeed !== rate) quietly(() => { api.playbackSpeed = rate; });
    const playing = P.playing();
    if (playing !== full.playing) {
      full.playing = playing;
      quietly(() => { if (playing) api.play(); else api.pause(); });
    }
    const pos = window.songbooksReferencePosition && window.songbooksReferencePosition();
    if (pos && pos.bar >= 0) {
      const ms = msAtBar(pos.bar) / rate;
      if (Math.abs(ms - full.lastMs) > 1) { full.lastMs = ms; out.updatePosition(ms); }
    }
    // The dock's loop, shaded on the score.
    const c = P.cycle();
    const key = c ? `${c.from}-${c.to}` : '';
    if (key !== full.cycleKey) {
      full.cycleKey = key;
      quietly(() => {
        // The first beat of the loop's first bar to the last of its last, in
        // the first track shown.
        const beatOf = (p, end) => {
          const mb = full.played[p], tr = api.tracks && api.tracks[0];
          const beats = mb && tr && tr.staves[0].bars[mb.masterBar.index]?.voices[0]?.beats;
          return beats && beats.length ? beats[end ? beats.length - 1 : 0] : null;
        };
        const first = c && beatOf(c.from, false);
        const last = c && beatOf(Math.min(full.played.length, c.to) - 1, true);
        if (first && last) api.highlightPlaybackRange(first, last); else api.clearPlaybackRangeHighlight();
      });
    }
  }
  function keepInSight(bb) {
    const r = bb && bb.barBounds && bb.barBounds.masterBarBounds && bb.barBounds.masterBarBounds.realBounds;
    const surface = page.querySelector('.gp-score-canvas .at-surface');
    if (!r || !surface) return;
    const top = surface.getBoundingClientRect().top + r.y, bottom = top + r.h;
    const dock = document.getElementById('audio-dock');
    const d = dock && dock.getBoundingClientRect();
    const floor = (d && d.height ? Math.min(window.innerHeight, d.top) : window.innerHeight) - 12;
    if (top < 56 || bottom > floor) window.scrollBy({ top: top - Math.max(64, window.innerHeight * 0.2), behavior: 'smooth' });
  }
  function startFollowing() {
    if (!full || full.following) return;
    full.following = true;
    requestAnimationFrame(follow);
  }

  async function showFull() {
    if (!page) return;
    if (full) {
      if (full.mix) window.songbooksPlayer?.useSynth();
      startFollowing();
      return;
    }
    const canvas = page.querySelector('.gp-score-canvas');
    const trackBar = page.querySelector('.gp-tracks');
    let score;
    try { score = await loadScore(); } catch (e) { failed(canvas, e.message); return; }
    if (full) return;
    // The file's own recording, for the simplified mixer when the song has
    // none (timed by its sync points, read before they go).
    const own = mixMode() && score.backingTrack && score.backingTrack.rawAudioFile
      ? { bytes: score.backingTrack.rawAudioFile, times: fileBarTimes(score) } : null;
    // alphaTab's clock is the synthesizer's: no backing track, no sync points.
    score.backingTrack = null;
    for (const mb of score.masterBars) mb.syncPoints = undefined;
    const g = geometry();
    applyGeometry(g);
    const s = baseSettings();
    const r = s.display.resources;
    r.staffLineColor = ink('#7a7064');
    r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = ink('#241e18');
    s.display.scale = g.scale;
    s.display.barsPerRow = g.perRow;
    s.display.padding = [0, 0, 0, 0];
    s.player.enablePlayer = true;
    s.player.playerMode = alphaTab.PlayerMode.EnabledExternalMedia;
    s.player.enableCursor = true;
    s.player.enableAnimatedBeatCursor = true;
    s.player.enableUserInteraction = true;
    s.player.scrollMode = alphaTab.ScrollMode.Continuous;
    // A phone has the song's name at the top of the page already, and no
    // room for it again at the score's size.
    hide(s, g.phone ? SCORE_INFO : ['ScoreCopyright']);
    const api = new alphaTab.AlphaTabApi(canvas, s);
    full = { api, score, shown: new Set([0]), geo: g, played: [], barMs: [], quiet: false, lastMs: -1, playing: false, cycleKey: null };
    // The page scrolls, not alphaTab: only while it plays, and only when
    // the line being played leaves the room between the top of the screen
    // and the dock; it then comes up to a fifth of the way down.
    api.customScrollHandler = {
      forceScrollTo(bb) { keepInSight(bb); },
      onBeatCursorUpdating(bb) { if (full.playing) keepInSight(bb); },
    };
    api.postRenderFinished.on(() => requestAnimationFrame(paginate));
    api.midiLoaded.on(() => {
      Object.assign(full, barTimes(api, score));
      renderSynth();
    });
    api.playerReady.on(() => {
      const out = api.player && api.player.output;
      if (out) out.handler = mediaHandler();
    });
    // Bars dragged across: the dock's loop (it plays them, again and
    // again); alphaTab's own range is let go, so it never stops on it.
    api.playbackRangeChanged.on((e) => {
      if (full.quiet || !e.playbackRange || !full.played.length) return;
      const from = playedAtTick(e.playbackRange.startTick);
      const to = playedAtTick(Math.max(e.playbackRange.startTick, e.playbackRange.endTick - 1)) + 1;
      quietly(() => { api.playbackRange = null; });
      full.cycleKey = null;
      window.songbooksPlayer?.setCycle(from, to);
    });
    // A click elsewhere takes alphaTab's shading away: the dock's loop comes back.
    api.playbackRangeHighlightChanged?.on(() => { if (!full.quiet) full.cycleKey = null; });

    // Start on the tracks the song's own lines name, else the first.
    const named = [...sheet.querySelectorAll('.gp-score[data-track]')].map((f) => Number(f.dataset.track)).filter((i) => i >= 0);
    if (named.length) full.shown = new Set(named);
    if (score.tracks.length <= 3 && !named.length) full.shown = new Set(score.tracks.map((tr, i) => i));

    score.tracks.forEach((tr, i) => {
      const b = el('button', 'gp-track-chip', tr.name || t('Track %d', i + 1));
      b.type = 'button';
      b.setAttribute('aria-pressed', full.shown.has(i));
      b.addEventListener('click', () => {
        if (full.shown.has(i) && full.shown.size > 1) full.shown.delete(i); else full.shown.add(i);
        b.setAttribute('aria-pressed', full.shown.has(i));
        renderFull();
      });
      trackBar.appendChild(b);
    });
    page.querySelector('.gp-notation')?.addEventListener('change', renderFull);

    // The tip, once: dismissed, it stays away.
    const tip = page.querySelector('.gp-tip');
    if (tip) {
      let seen = false;
      try { seen = localStorage.getItem('songbooks:gp-tip') === '1'; } catch (e) { /* show it */ }
      tip.hidden = seen;
      tip.querySelector('button')?.addEventListener('click', () => {
        tip.hidden = true;
        try { localStorage.setItem('songbooks:gp-tip', '1'); } catch (e) { /* this page only */ }
      });
    }

    // A narrower or wider window: the page's size again, and the bars a
    // line when that changes (alphaTab redraws on its own for the width).
    let resizeTimer = null;
    window.addEventListener('resize', () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        if (!full || sheet.dataset.renderMode !== 'score') return;
        const ng = geometry();
        const old = full.geo;
        full.geo = ng;
        applyGeometry(ng);
        if (ng.perRow !== old.perRow || ng.scale !== old.scale || ng.phone !== old.phone) {
          api.settings.display.barsPerRow = ng.perRow;
          api.settings.display.scale = ng.scale;
          api.updateSettings();
          api.render();
        }
      }, 150);
    });
    if (mixMode()) full.mix = setUpMix(own);
    renderFull(true);
    startFollowing();
  }

  // ---- The simplified mixer ---------------------------------------------------
  // Not "content: gp": the file sits next to the chart, or inside it.
  const mixMode = () => !sheet.dataset.gpOnly && !!page && !!page.querySelector('.gp-mix');

  // Seconds into the file's own recording where each played bar starts,
  // from its sync points; null with none.
  function fileBarTimes(score) {
    const played = GPFacts.played(score.masterBars);
    const known = [];
    const seen = new Map();
    played.forEach((w, pos) => {
      const occ = seen.get(w) || 0;
      seen.set(w, occ + 1);
      for (const sp of score.masterBars[w - 1].syncPoints || []) {
        if ((sp.syncPointValue.barOccurence || 0) === occ && !(sp.ratioPosition > 0)) known.push({ bar: pos, t: sp.syncPointValue.millisecondOffset / 1000 });
      }
    });
    const mb = score.masterBars[0] || {};
    const nominal = (mb.timeSignatureNumerator || 4) * (4 / (mb.timeSignatureDenominator || 4)) * 60 / (score.tempo || 120);
    return GPAlign.barTimes(known, played.length, nominal);
  }

  // The dock plays the file's sound (its bars are the file's, as played);
  // the song's recording follows it here, bar by bar, through the song's
  // own bar sync (its sync points or section markers) and which bar of
  // the chart each bar of the file is (gpOrder: gp_start, or the
  // sections' gp[...] lines) — GPAlign.follow. Recording: that element's
  // level; Score: the dock's.
  function setUpMix(own) {
    const bar = page.querySelector('.gp-mix');
    const P = () => window.songbooksPlayer;
    const ch = (name) => {
      const e = bar.querySelector(`[data-ch="${name}"]`);
      return { el: e, on: e.querySelector('.gp-mix-on'), vol: e.querySelector('.gp-mix-vol') };
    };
    const rec = ch('rec'), sc = ch('score');
    const why = bar.querySelector('.gp-mix-why');
    const prefKey = `songbooks:gp-mix:${variantId}`;
    let prefs = {};
    try { prefs = JSON.parse(localStorage.getItem(prefKey) || '{}') || {}; } catch (e) { /* defaults */ }
    const st = {
      rec: { on: prefs.recOn !== false, vol: prefs.recVol ?? 80 },
      score: { on: prefs.scoreOn !== false, vol: prefs.scoreVol ?? 80 },
      audio: null, map: null, times: null, guardUntil: 0,
    };
    const save = () => {
      try { localStorage.setItem(prefKey, JSON.stringify({ recOn: st.rec.on, recVol: st.rec.vol, scoreOn: st.score.on, scoreVol: st.score.vol })); } catch (e) { /* not kept */ }
    };
    const paint = (c, v) => {
      c.on.setAttribute('aria-pressed', String(v.on));
      c.el.classList.toggle('is-muted', !v.on);
      c.vol.value = v.vol;
    };
    const live = () => !!(st.audio && st.times && st.rec.on && st.rec.vol > 0);
    const applyScore = () => { P()?.setLevel(st.score.on ? st.score.vol / 100 : 0); paint(sc, st.score); };
    const applyRec = () => {
      paint(rec, st.rec);
      if (st.audio) st.audio.volume = st.rec.vol / 100;
      if (!live() && st.audio) st.audio.pause();
    };
    sc.on.addEventListener('click', () => { st.score.on = !st.score.on; applyScore(); save(); });
    sc.vol.addEventListener('input', () => { st.score.vol = Number(sc.vol.value); applyScore(); save(); });
    rec.on.addEventListener('click', () => { st.rec.on = !st.rec.on; applyRec(); save(); });
    rec.vol.addEventListener('input', () => { st.rec.vol = Number(rec.vol.value); applyRec(); save(); });
    bar.querySelector('.gp-mix-full')?.addEventListener('click', () => document.querySelector('#mode-segmented button[data-value="mixer"]')?.click());
    applyScore();
    applyRec();
    const off = (msg, scoreToo) => {
      rec.on.disabled = true;
      rec.vol.disabled = true;
      rec.el.classList.add('is-off');
      rec.on.title = msg;
      why.textContent = msg;
      why.hidden = false;
      if (scoreToo) { sc.on.disabled = true; sc.vol.disabled = true; }
    };

    (async () => {
      try {
        const [mx, au] = await Promise.all([
          fetch(`/api/songs/${variantId}/mixer`, { credentials: 'same-origin' }).then((r) => (r.ok ? r.json() : {})),
          fetch(`/api/songs/${variantId}/audio`, { credentials: 'same-origin' }).then((r) => (r.ok ? r.json() : { categories: [] })),
        ]);
        const cats = {};
        (au.categories || []).forEach((c) => { cats[c.category] = c; });
        let choice = null;
        try { choice = localStorage.getItem(`songbooks:audio-category-choice:${variantId}`); } catch (e) { /* none */ }
        const cat = choice && cats[choice] ? choice : ['youtube', 'band', 'gp'].find((c) => cats[c]);
        let src;
        if (cat) {
          if (!mx.gpOrder) { off(t('The chart isn’t lined up with the file: the recording can’t follow the score.')); return; }
          st.map = GPAlign.chartBarsForFile(mx.gpOrder, GPFacts.played(full.score.masterBars));
          st.times = GPAlign.barTimes(GPAlign.knownBars(cats[cat], mx.sections), mx.totalBars, (mx.timeNum || 4) * (4 / (mx.timeDen || 4)) * 60 / (mx.tempo || 120));
          if (!st.times) { off(t('The recording isn’t lined up with the song’s bars yet (Mixer, Align recording): it can’t follow the score.')); return; }
          src = new Blob([await window.songbooksAudioBytes(variantId, cat, cats[cat])], { type: 'audio/mpeg' });
          rec.on.title = cat === 'youtube' ? t('the YouTube recording') : cat === 'band' ? t('the band recording') : t('the Guitar Pro file’s recording');
        } else if (own) {
          if (!own.times) { off(t('The file’s recording has no sync points: it can’t follow the score.')); return; }
          st.map = own.times.slice(0, -1).map((x, i) => i);
          st.times = own.times;
          src = new Blob([own.bytes], { type: 'audio/mpeg' });
          rec.on.title = t('the Guitar Pro file’s recording');
        } else {
          off(t('This song has no recording.'));
          return;
        }
        const a = new Audio(URL.createObjectURL(src));
        a.preload = 'auto';
        a.preservesPitch = true;
        a.mozPreservesPitch = true;
        a.webkitPreservesPitch = true;
        st.audio = a;
        rec.on.disabled = false;
        rec.vol.disabled = false;
        applyRec();
      } catch (e) {
        off(t('Couldn’t load the recording.'));
      }
    })();

    // Its own timer, not the drawing's frames: those stop in a tab in the
    // background, the sound doesn't.
    setInterval(() => { if (full && full.mix && sheet.dataset.renderMode === 'score') full.mix.sync(); }, 40);
    return {
      off,
      // While the Score view is shown: where the recording should be for
      // the dock's place in the file.
      sync() {
        const a = st.audio;
        if (!a) return;
        const p = P();
        const pos = p && p.source() === 'synth' && window.songbooksReferencePosition && window.songbooksReferencePosition();
        if (!live() || !pos || !pos.playing || !full.barMs.length) { if (!a.paused) a.pause(); return; }
        const i = Math.max(0, Math.floor(pos.bar));
        const synthBarSec = ((full.barMs[i + 1] ?? full.barMs[i]) - full.barMs[i]) / 1000;
        const f = GPAlign.follow(st.map, st.times, i, pos.bar - i, synthBarSec);
        if (!f || (a.duration && f.t >= a.duration)) { if (!a.paused) a.pause(); return; }
        const speed = p.speed();
        const rate = f.ratio > 0 ? speed / f.ratio : speed;
        if (Math.abs(a.playbackRate - rate) > 0.004) a.playbackRate = Math.max(0.25, Math.min(4, rate));
        const now = performance.now();
        if (a.paused) {
          a.currentTime = f.t;
          a.play().catch(() => {});
          st.guardUntil = now + 400;
        } else if (now > st.guardUntil && Math.abs(a.currentTime - f.t) > 0.15) {
          a.currentTime = f.t;
          st.guardUntil = now + 400;
        }
      },
      pause() { if (st.audio) st.audio.pause(); },
      state: () => ({ rec: { ...st.rec }, score: { ...st.score }, live: live(), audioTime: st.audio ? st.audio.currentTime : null, paused: st.audio ? st.audio.paused : null, why: why.hidden ? '' : why.textContent, source: P() ? P().source() : null }),
    };
  }

  function renderFull(first) {
    if (!full) return;
    const pick = page.querySelector('.gp-notation');
    const tracks = [...full.shown].sort((a, b) => a - b);
    for (const i of tracks) {
      const tr = full.score.tracks[i];
      applyNotation(tr, pick && pick.value ? (kindOf(tr) === 'drums' ? 'staff' : pick.value) : notationFor(tr, ''));
    }
    // "Edit the file" opens on the first track shown.
    const edit = page.querySelector('.gp-edit-link');
    if (edit) edit.href = `/song/${variantId}/gp-edit?track=%23${tracks[0] + 1}`;
    // The same score drawn again (not loaded again: the player keeps its place).
    if (first === true) full.api.renderScore(full.score, tracks);
    else full.api.renderTracks(tracks.map((i) => full.score.tracks[i]));
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
    else {
      // Next to a chart the file plays in its own view only: leaving it
      // stops it, and the dock goes back to the song's recording.
      if (full && full.mix) {
        full.mix.pause();
        window.songbooksPlayer?.leaveSynth();
      }
      drawSections();
    }
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

  // "Change bars" / "+ Bars from the Guitar Pro file" on a section (editors):
  // the bar picker, saving each choice to the song's text right away.
  document.addEventListener('click', (e) => {
    const b = e.target.closest('[data-gp-pick]');
    if (!b || !window.GPBarPicker) return;
    let sections = [];
    try { sections = JSON.parse(sheet.dataset.pickerSections || '[]'); } catch (err) { return; }
    const at = sections.findIndex((x) => String(x.index) === b.dataset.gpPick);
    const req = (method, index, body, track) => fetch(`/api/songs/${variantId}/sections/${index}/gp${method === 'DELETE' ? '?track=' + encodeURIComponent(track || '') : ''}`, {
      method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined,
    }).then(async (r) => { if (!r.ok) throw new Error((await r.text()) || t('Saving failed')); });
    GPBarPicker.open({
      variantId, sections, current: Math.max(0, at), track: b.dataset.track !== undefined && b.hasAttribute('data-track') ? b.dataset.track : undefined,
      onApply: ({ index, track, spec }) => req('PUT', index, { track, spec }),
      onRemove: ({ index, track }) => req('DELETE', index, null, track),
      onClose: (changed) => { if (changed) location.reload(); },
    });
  });

  // scoreApi: the Score view's alphaTab, for tests and a look from the console.
  window.GPView = { showFull, drawSections, fileBytes, loadScore, saveScore, scoreApi: () => full && full.api, mixState: () => (full && full.mix ? full.mix.state() : null) };
})();

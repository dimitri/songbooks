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
  // Each track's level as Guitar Pro plays it (gp-levels.js): a Guitar
  // Pro 8 file keeps it where alphaTab doesn't read it, and its score then
  // sounds silent (no waveform in the player, nothing to hear) — and would
  // be saved silent.
  let levelsPromise = null;
  const fileLevels = () => (levelsPromise ||= fileBytes().then((b) => (window.GPLevels ? GPLevels.read(b) : [])));
  // Guitar Pro files often mark chords "show diagram" with no fingering
  // at all (only the name was wanted): drawn, those are empty boxes. raw
  // leaves the model as the file has it, for editing and saving.
  async function loadScore(raw) {
    const score = alphaTab.importer.ScoreLoader.loadScoreFromBytes(await fileBytes(), new alphaTab.Settings());
    if (window.GPLevels) GPLevels.apply(score, await fileLevels());
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
  // other view: the song's recording when it has one (a Guitar Pro file's
  // backing track, imported as the song's "gp" recording, an MP3, the
  // band's), in every view; only a song with no recording at all plays the
  // file through alphaTab's synthesizer — rendered here once, offline, and
  // handed to the dock as its sound (songbooksPlayer.offerSynth), so the
  // bar shows a waveform and every tool works on it. alphaTab's own player
  // follows the dock (PlayerMode.EnabledExternalMedia): the cursor moves
  // with the dock's bar.beat (the recording's bars are the chart's, found
  // on the file through the song's bar mapping: gp[...] lines, gp_start);
  // a click on a note moves the dock there; bars dragged across become the
  // dock's loop, and the dock's loop is shaded on the score.
  //
  // The dock's simplified mixer ("The dock's mixer" below): Recording and
  // MIDI, a switch and a level each, in every view: the file's MIDI sound
  // can play along with the recording, following it bar by bar; off by
  // default when there's a recording. The full Mixer is a tap away.
  //
  // The score is made ready at load in every view (prepare: hidden, it
  // isn't drawn, but its sound and bars are there), drawn when shown.

  const page = document.getElementById('gp-score-page');
  // The soundfont: the score's own subset of GeneralUser GS, as its
  // instruments are chosen (gp-soundfont.js); Sonivox without it.
  const soundFont = (score) => (window.GPSoundFont ? GPSoundFont.bytesFor(score)
    : fetch('/static/js/vendor/alphatab/soundfont/sonivox.sf2').then((r) => { if (!r.ok) throw new Error('soundfont'); return r.arrayBuffer(); }).then((b) => new Uint8Array(b)));
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
    const { recorded } = await P.recordings;
    if (recorded) return;
    await synthesize(P);
  }
  // Next to a chart the file's sound is rendered all the same, recording
  // or not: the Score view plays it, the recording following it.
  async function synthesizeForMix() {
    const P = window.songbooksPlayer;
    if (!P || full.synthStarted) return;
    full.synthStarted = true;
    await (P.recordings || P.ready);
    await synthesize(P);
  }
  async function synthesize(P) {
    const score = full.score;
    P.synthProgress(t('Preparing the sound…'));
    try {
      const bytes = await renderWav((pct) => P.synthProgress(t('Preparing the sound… %d%', pct)));
      const beats = full.played.map((mb) => mb.masterBar.timeSignatureNumerator || 4);
      P.offerSynth({
        bytes, type: 'audio/wav',
        barTimes: full.barMs.map((ms) => ms / 1000), beats,
        sections: fileSections(), bpm: score.tempo || 0,
        keep: mixMode(),
      });
      full.synthDone = true;
      full.wav = bytes;
      // The score sounds: its instruments can be chosen.
      if (full.soundBox) full.soundBox.hidden = false;
      if (full.mix) full.mix.sounds();
    } catch (e) {
      P.synthFailed(t('Couldn’t prepare the sound of this file.'));
      if (full.mix) full.mix.off(t('Couldn’t prepare the sound of this file.'), true);
    }
    if (full.again) resynthesize();
  }
  // The file through the synthesizer, as a WAV; progress(percent) as it goes.
  async function renderWav(progress) {
    const opts = new alphaTab.synth.AudioExportOptions();
    opts.soundFonts = [await soundFont(full.score)];
    opts.sampleRate = 44100;
    opts.useSyncPoints = false;
    opts.metronomeVolume = 0;
    opts.masterVolume = 1;
    const exporter = await full.api.exportAudio(opts);
    const chunks = [];
    let n = 0;
    try {
      for (;;) {
        const c = await exporter.render(4000);
        if (!c) break;
        chunks.push(c.samples);
        n += c.samples.length;
        if (c.endTime > 0 && progress) progress(Math.min(99, Math.round((100 * c.currentTime) / c.endTime)));
      }
    } finally {
      exporter.destroy();
    }
    if (!n) throw new Error('no sound');
    return wav(chunks, n, 44100);
  }

  // ---- The instruments ------------------------------------------------------
  // What each track plays with (gp-instruments.js): the file's program,
  // the song's choice (its editors') or this person's. A new choice
  // renders the sound again and the dock carries on with it, where it was.
  function resynthesize() {
    const P = window.songbooksPlayer;
    if (!P || !P.replaceSynth) return;
    // Still preparing the first sound, or the last choice's: again after it.
    if (full.rendering || (full.synthStarted && !full.synthDone)) { full.again = true; return; }
    full.again = false;
    if (!full.synthDone) return;
    const status = page.querySelector('.gp-sound-status');
    const say = (msg) => { if (status) status.textContent = msg; };
    say(t('Preparing the sound…'));
    full.rendering = renderWav((pct) => say(t('Preparing the sound… %d%', pct)))
      .then((bytes) => { P.replaceSynth(bytes); full.wav = bytes; if (full.mix) full.mix.sounds(true); say(''); })
      .catch(() => say(t('Couldn’t prepare the sound of this file.')))
      .finally(() => { full.rendering = null; if (full.again) resynthesize(); });
  }
  function setUpInstruments() {
    const box = page.querySelector('.gp-sound');
    const GI = window.GPInstruments;
    if (!box || !GI) return;
    const list = box.querySelector('.gp-sound-tracks');
    const tracks = full.score.tracks.map((tr, i) => ({ tr, i })).filter(({ tr }) => !GI.isDrums(tr));
    if (!tracks.length) return;
    const status = box.querySelector('.gp-sound-status');
    for (const { tr, i } of tracks) {
      const row = el('label', 'gp-sound-track');
      row.appendChild(el('span', 'gp-sound-name', tr.name || t('Track %d', i + 1)));
      row.appendChild(GI.pickerFor(variantId, i, { onError: (e) => { status.textContent = e.message; } }));
      list.appendChild(row);
    }
    full.soundBox = box;
    if (full.synthDone) box.hidden = false;
    document.addEventListener('songbooks:gp-instrument', (e) => {
      if (e.detail.variantId !== String(variantId) || !full) return;
      if (GI.apply(variantId, full.score)) resynthesize();
    });
    // An editor can write the choice into the file: a new version of it.
    box.querySelector('.gp-sound-save')?.addEventListener('click', async (e) => {
      const b = e.currentTarget;
      b.disabled = true;
      status.textContent = t('Saving…');
      try {
        const raw = await loadScore(true);
        GI.knowFile(variantId, raw);
        GI.apply(variantId, raw);
        await saveScore(raw);
        await GI.savedInFile(variantId, raw);
        status.textContent = t('Saved in the file: a new version of it (History).');
      } catch (err) {
        status.textContent = err.message || t('Saving failed');
      } finally {
        b.disabled = false;
      }
    });
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
        const b = toDock(barAtMs(ms * P().speed()));
        if (b !== null) P().seekBar(b, false);
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
    const y = pos && pos.bar >= 0 ? fromDock(pos.bar) : null;
    if (y !== null) {
      const ms = msAtBar(y) / rate;
      if (Math.abs(ms - full.lastMs) > 1) { full.lastMs = ms; out.updatePosition(ms); }
    }
    // The dock's loop, shaded on the score.
    const c = cycleOnFile(P.cycle());
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
  // The player moved (a click, a key, ⏮): the cursor goes there at once,
  // stopped too — at the start the player's place reads as none, and the
  // cursor stayed where it was (⏮ looked dead). Back at the start, the
  // score's first line comes back into sight when scrolled past.
  document.addEventListener('songbooks:player-seek', (e) => {
    if (!full || sheet.dataset.renderMode !== 'score' || !full.barMs.length) return;
    const out = full.api.player && full.api.player.output;
    const bar = e.detail ? e.detail.bar : null;
    if (!out || bar === null || bar === undefined) return;
    const P = window.songbooksPlayer;
    const y = fromDock(Math.max(0, bar));
    if (y === null) return;
    const ms = msAtBar(y) / ((P && P.speed()) || 1);
    full.lastMs = ms;
    quietly(() => out.updatePosition(ms));
    if (bar > 0.01) return;
    const sys = full.api.renderer.boundsLookup && full.api.renderer.boundsLookup.staffSystems && full.api.renderer.boundsLookup.staffSystems[0];
    const surface = page.querySelector('.gp-score-canvas .at-surface');
    const b = sys && (sys.realBounds || sys.visualBounds);
    if (!b || !surface) return;
    const top = surface.getBoundingClientRect().top + b.y;
    if (top < 56) window.scrollBy({ top: top - Math.max(64, window.innerHeight * 0.2), behavior: 'smooth' });
  });
  // The dock's bars and the file's (its played bars): the same when the
  // dock plays the file's sound; else the dock's are the chart's (a
  // recording), each found on the file through the song's bar mapping
  // (full.map: the chart's bar for each played bar of the file; full.inv:
  // the file's for each chart bar). null where one has no bar for the other.
  const onFile = () => { const P = window.songbooksPlayer; return !full || !full.map || !P || P.source() === 'synth'; };
  const fromDock = (x) => (onFile() ? x : GPAlign.toFile(full.inv, x));
  const toDock = (y) => (onFile() ? y : GPAlign.toChart(full.map, y));
  function cycleOnFile(c) {
    if (!c) return null;
    const a = fromDock(c.from), b = fromDock(c.to - 1);
    return a === null || b === null ? null : { from: Math.floor(a), to: Math.floor(b) + 1 };
  }
  function startFollowing() {
    if (!full || full.following) return;
    full.following = true;
    requestAnimationFrame(follow);
  }

  // The Score view shown: drawn at the page's size, its cursor following.
  async function showFull() {
    if (!page) return;
    await prepare();
    if (!full) return;
    relayout(true);
    startFollowing();
  }
  // The page's size again (shown, or the window resized), the bars a line
  // and the score's heading when they change; drawn when never drawn.
  function relayout(shown) {
    if (!full || sheet.dataset.renderMode !== 'score') return;
    const ng = geometry();
    const old = full.geo;
    full.geo = ng;
    applyGeometry(ng);
    // Drawn once already, or being drawn (made in sight): only a new size
    // draws again; made out of sight, alphaTab skipped it: now.
    if (ng.perRow !== old.perRow || ng.scale !== old.scale || ng.phone !== old.phone || (shown && full.hiddenAtStart && !full.drawn)) {
      const api = full.api;
      api.settings.display.barsPerRow = ng.perRow;
      api.settings.display.scale = ng.scale;
      for (const n of SCORE_INFO) if (alphaTab.NotationElement[n] !== undefined) api.settings.notation.elements.set(alphaTab.NotationElement[n], !ng.phone && n !== 'ScoreCopyright');
      api.updateSettings();
      api.render();
    }
  }
  // The score, its player and its sound, ready in every view (hidden, it
  // isn't drawn: alphaTab skips a container with no width), so the dock
  // has the file's sound and bars wherever the song is read.
  let preparing = null;
  function prepare() {
    if (!page) return Promise.resolve();
    if (!preparing) preparing = makeFull();
    return preparing;
  }
  async function makeFull() {
    const canvas = page.querySelector('.gp-score-canvas');
    const trackBar = page.querySelector('.gp-tracks');
    let score;
    try { score = await loadScore(); } catch (e) { failed(canvas, e.message); return; }
    if (full) return;
    // The chords over the bars: the file's, else the chart's (gp-chords.js).
    const chordsFor = chartChords(score);
    // The instruments chosen for this song (gp-instruments.js).
    if (window.GPInstruments) GPInstruments.apply(variantId, score);
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
    if (window.GPChords) GPChords.style(s, alphaTab);
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
    full = { api, score, shown: new Set([0]), geo: g, played: [], barMs: [], quiet: false, lastMs: -1, playing: false, cycleKey: null, hiddenAtStart: !canvas.getClientRects().length };
    // The page scrolls, not alphaTab: only while it plays, and only when
    // the line being played leaves the room between the top of the screen
    // and the dock; it then comes up to a fifth of the way down.
    api.customScrollHandler = {
      forceScrollTo(bb) { keepInSight(bb); },
      onBeatCursorUpdating(bb) { if (full.playing) keepInSight(bb); },
    };
    api.postRenderFinished.on(() => { full.drawn = true; requestAnimationFrame(paginate); });
    api.midiLoaded.on(() => {
      Object.assign(full, barTimes(api, score));
      setUpSound();
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
      const a = toDock(from), b = toDock(to - 1);
      if (a !== null && b !== null) window.songbooksPlayer?.setCycle(Math.floor(a), Math.floor(b) + 1);
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
      resizeTimer = setTimeout(() => relayout(false), 150);
    });
    // The song's bar mapping: the chart's bar each played bar of the file
    // is (gpOrder: gp[...] lines, gp_start), for a recording the dock plays.
    const mx = await mixerData();
    if (mx.gpOrder && window.GPAlign) {
      full.map = GPAlign.chartBarsForFile(mx.gpOrder, GPFacts.played(score.masterBars));
      full.inv = GPAlign.fileBarsForChart(full.map, mx.totalBars || mx.gpOrder.length);
    }
    full.mix = setUpDockMix();
    setUpInstruments();
    full.chords = await chordsFor;
    renderFull(true);
  }

  // ---- The chords over the bars ---------------------------------------------
  // The file's own chord symbols when it has any (alphaTab draws them);
  // else the chart's, each on the bar of the file it plays (gp-chords.js),
  // over the first track shown that isn't drums. Only drawn: the file
  // itself is never changed by them (they're not in loadScore(true)).
  let mixerPromise = null;
  function mixerData() {
    if (!mixerPromise) {
      mixerPromise = fetch(`/api/songs/${variantId}/mixer`, { credentials: 'same-origin' })
        .then((r) => (r.ok ? r.json() : {})).catch(() => ({}));
    }
    return mixerPromise;
  }
  async function chartChords(score) {
    if (!window.GPChords || !window.GPAlign || GPChords.hasOwn(score)) return null;
    const mx = await mixerData();
    if (!mx.gpOrder) return null;
    const played = GPFacts.played(score.masterBars);
    const chords = GPChords.forWrittenBars(GPAlign.chartBarsForFile(mx.gpOrder, played), played, GPChords.byChartBar(mx.sections));
    return chords.size ? chords : null;
  }
  function makeChord(name) {
    const c = new alphaTab.model.Chord();
    c.name = name;
    c.showDiagram = false;
    c.showFingering = false;
    return c;
  }
  function placeChords(tracks) {
    if (!full || !full.chords) return;
    const top = tracks.find((i) => !full.score.tracks[i].staves[0].isPercussion);
    GPChords.put(full.score, top ?? -1, full.chords, makeChord);
  }

  // ---- The file's sound, and the dock's mixer -----------------------------------
  // The dock plays the song's recording when it has one, in every view;
  // the file's MIDI sound (the synthesizer's) only when there's none. The
  // dock's simplified mixer, beside its Mixer button, in every view where
  // the song has a file: Recording and MIDI, a switch and a level each.
  //
  // - No recording: the dock plays the MIDI; its switch and level are the
  //   dock's.
  // - A recording, the file lined up with the chart (full.map): the MIDI,
  //   off by default, plays along with the recording when switched on —
  //   rendered then, once — following it bar by bar (its own <audio>, its
  //   time and speed set from the recording's place, as the Score view's
  //   cursor is); the recording's switch and level are the dock's.
  // - A recording, the file not lined up: the MIDI can't follow it; switched
  //   on, the dock plays the MIDI alone, the recording off, until it's
  //   switched off again.
  const mixMode = () => !!(full && full.recorded);
  async function setUpSound() {
    const P = window.songbooksPlayer;
    if (!P || full.soundKnown) return;
    full.soundKnown = true;
    const { recorded } = await (P.recordings || P.ready);
    full.recorded = !!recorded;
    if (full.mix) full.mix.apply();
    if (!recorded) renderSynth();
  }

  function setUpDockMix() {
    const bar = page.querySelector('.gp-mix');
    if (!bar) return null;
    const P = () => window.songbooksPlayer;
    // In the dock, beside its Mixer button (or Practice).
    const at = document.getElementById('audio-mixer-btn') || document.getElementById('audio-panel-btn');
    if (at) at.before(bar);
    bar.hidden = false;
    const ch = (name) => {
      const e = bar.querySelector(`[data-ch="${name}"]`);
      return { el: e, on: e.querySelector('.gp-mix-on'), vol: e.querySelector('.gp-mix-vol') };
    };
    const rec = ch('rec'), midi = ch('midi');
    const why = bar.querySelector('.gp-mix-why');
    const prefKey = `songbooks:gp-mix:${variantId}`;
    let prefs = {};
    try { prefs = JSON.parse(localStorage.getItem(prefKey) || '{}') || {}; } catch (e) { /* defaults */ }
    // midi.on null: as the song goes (on with no recording, off with one).
    const st = {
      rec: { on: prefs.recOn !== false, vol: prefs.recVol ?? 100 },
      midi: { on: typeof prefs.midiOn === 'boolean' ? prefs.midiOn : null, vol: prefs.midiVol ?? 80 },
      audio: null, guardUntil: 0, failed: '',
    };
    const save = () => {
      try { localStorage.setItem(prefKey, JSON.stringify({ recOn: st.rec.on, recVol: st.rec.vol, midiOn: st.midi.on, midiVol: st.midi.vol })); } catch (e) { /* not kept */ }
    };
    const midiOn = () => !st.failed && (st.midi.on ?? full.recorded === false);
    const layered = () => !!(full.recorded && full.map);
    const say = (msg) => {
      why.textContent = msg || '';
      bar.title = msg || '';
      bar.classList.toggle('has-why', !!msg);
    };
    const paint = (c, on, vol) => {
      c.on.setAttribute('aria-pressed', String(on));
      c.el.classList.toggle('is-muted', !on);
      c.vol.value = vol;
    };
    function apply() {
      const p = P();
      const none = full.recorded === false;
      rec.on.disabled = rec.vol.disabled = none;
      rec.el.classList.toggle('is-off', none);
      midi.on.disabled = midi.vol.disabled = !!st.failed;
      if (p && none) {
        // The dock's sound is the MIDI.
        p.setLevel(midiOn() ? st.midi.vol / 100 : 0);
        say(st.failed || t('This song has no recording: its MIDI plays.'));
      } else if (p && full.recorded) {
        p.setRecordingLevel?.(st.rec.on ? st.rec.vol / 100 : 0);
        if (midiOn() && !full.synthDone) {
          synthesizeForMix();
          say(t('Preparing the sound…'));
        } else if (midiOn() && !layered()) {
          if (p.source() !== 'synth') p.useSynth();
          p.setLevel(st.midi.vol / 100);
          say(t('The chart isn’t lined up with the file: the MIDI plays alone, not with the recording.'));
        } else {
          if (!midiOn() && p.source() === 'synth') p.leaveSynth();
          say(st.failed);
        }
        if (st.audio) st.audio.volume = st.midi.vol / 100;
        if (st.audio && !midiOn()) st.audio.pause();
      }
      paint(rec, !none && st.rec.on && !(midiOn() && full.recorded && !layered()), st.rec.vol);
      paint(midi, midiOn(), st.midi.vol);
    }
    rec.on.addEventListener('click', () => { st.rec.on = !st.rec.on; save(); apply(); });
    rec.vol.addEventListener('input', () => { st.rec.vol = Number(rec.vol.value); st.rec.on = true; save(); apply(); });
    midi.on.addEventListener('click', () => { st.midi.on = !midiOn(); save(); apply(); });
    midi.vol.addEventListener('input', () => { st.midi.vol = Number(midi.vol.value); if (!midiOn()) st.midi.on = true; save(); apply(); });
    bar.querySelector('.gp-mix-full')?.addEventListener('click', () => document.querySelector('#mode-segmented button[data-value="mixer"]')?.click());
    apply();

    // Its own timer, not the drawing's frames: those stop in a tab in the
    // background, the sound doesn't.
    setInterval(() => { if (full && full.mix) full.mix.sync(); }, 40);
    return {
      apply,
      // The file's sound is ready (again: another instrument).
      sounds(again) {
        if (full.recorded && full.wav) {
          if (st.audio && again) { st.audio.pause(); URL.revokeObjectURL(st.audio.src); st.audio = null; }
          if (!st.audio) {
            const a = new Audio(URL.createObjectURL(new Blob([full.wav], { type: 'audio/wav' })));
            a.preload = 'auto';
            a.preservesPitch = a.mozPreservesPitch = a.webkitPreservesPitch = true;
            st.audio = a;
          }
        }
        apply();
      },
      off(msg) {
        st.failed = msg;
        apply();
      },
      // Where the MIDI should be for the recording's place in the chart.
      sync() {
        const a = st.audio, p = P();
        if (!a || !p) return;
        const pos = p.source() !== 'synth' && window.songbooksReferencePosition && window.songbooksReferencePosition();
        if (!midiOn() || !layered() || !pos || !pos.playing || !full.barMs.length) { if (!a.paused) a.pause(); return; }
        const b = Math.floor(pos.bar);
        const t0 = p.timeAtBar ? p.timeAtBar(b) : null, t1 = p.timeAtBar ? p.timeAtBar(b + 1) : null;
        if (t0 === null || t1 === null || !(t1 > t0)) {
          say(t('The recording isn’t lined up with the song’s bars yet (Mixer, Align recording): the MIDI can’t follow it.'));
          if (!a.paused) a.pause();
          return;
        }
        const y = GPAlign.toFile(full.inv, pos.bar);
        if (y === null) { if (!a.paused) a.pause(); return; }
        const i = Math.floor(y);
        const synthBarSec = ((full.barMs[i + 1] ?? full.barMs[i]) - full.barMs[i]) / 1000;
        const target = msAtBar(y) / 1000;
        if (a.duration && target >= a.duration) { if (!a.paused) a.pause(); return; }
        // The bar lasts as long in both: the recording's, at the dock's speed.
        const rate = p.speed() * (synthBarSec > 0 ? synthBarSec / (t1 - t0) : 1);
        if (Math.abs(a.playbackRate - rate) > 0.004) a.playbackRate = Math.max(0.25, Math.min(4, rate));
        const now = performance.now();
        if (a.paused) {
          a.currentTime = target;
          a.play().catch(() => {});
          st.guardUntil = now + 400;
        } else if (now > st.guardUntil && Math.abs(a.currentTime - target) > 0.15) {
          a.currentTime = target;
          st.guardUntil = now + 400;
        }
      },
      pause() { if (st.audio) st.audio.pause(); },
      state: () => ({
        rec: { ...st.rec }, midi: { on: midiOn(), vol: st.midi.vol }, recorded: full.recorded, layered: layered(),
        midiTime: st.audio ? st.audio.currentTime : null, midiPaused: st.audio ? st.audio.paused : null,
        why: why.textContent, source: P() ? P().source() : null,
      }),
    };
  }

  function renderFull(first) {
    if (!full) return;
    const pick = page.querySelector('.gp-notation');
    const tracks = [...full.shown].sort((a, b) => a - b);
    placeChords(tracks);
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
    // The file's sound and the dock's mixer in every view.
    prepare();
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
  window.GPView = { showFull, prepare, drawSections, fileBytes, loadScore, saveScore, scoreApi: () => full && full.api, mixState: () => (full && full.mix ? full.mix.state() : null) };
})();

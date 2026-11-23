// Reference-audio player: waveform (hand-drawn via Web Audio decodeAudioData
// + <canvas>, no CDN library — see app/docs/mp3-reference-audio.md §3),
// transport controls, and section markers synced against the song's own
// structure strip (#song-structure-strip's .structure-seg buttons, the
// SAME elements/CSS classes the metronome's own playback highlight uses
// in song.js — reused here, not reimplemented, just driven by
// audio.currentTime instead of the BPM clock).
//
// Two reference CATEGORIES per song ("a personal choice of reference
// version (yt video, band recording: two categories only)" — store.
// AudioCategory's own doc comment): "youtube" and "band". Upload/replace/
// remove controls for both live in the Reference drawer (wired at the
// bottom of this file); THIS player is playback-only, with a toggle
// between categories when a song has both. Which category is selected is
// a per-browser preference (localStorage, keyed by book/stem), not song
// data — two listeners can prefer different reference versions of the
// same song without fighting over a shared setting.

// Shared by audio-player.js and mixer.js: one cached download per
// (song, category, size) — the size changes when a recording is replaced.
// With stem, one of the recording's parts (cmd/stemsd).
window.songbooksAudioBytes = window.songbooksAudioBytes || (function () {
  const cache = new Map();
  return function (variantId, category, meta, stem) {
    const url = `/song/${variantId}/audio.mp3?category=${category}${stem ? '&stem=' + stem : ''}&v=${(meta && meta.byteSize) || ''}`;
    if (!cache.has(url)) {
      cache.set(url, fetch(url).then((r) => {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.arrayBuffer();
      }).catch((e) => { cache.delete(url); throw e; }));
    }
    return cache.get(url);
  };
})();

// The bytes Web Audio's decodeAudioData will take. Some MP3s — Guitar
// Pro embeds them like this — open with a few KB of zero padding before
// the first frame: <audio> skips it, decodeAudioData rejects the whole
// file (no waveform, no auto-align). Padding is not audio, so dropping it
// shifts nothing in time.
window.songbooksDecodable = function (buf) {
  const bytes = new Uint8Array(buf);
  if (bytes.length === 0 || bytes[0] !== 0) return buf;
  let i = 0;
  const limit = Math.min(bytes.length, 1 << 20);
  while (i < limit && bytes[i] === 0) i++;
  return i < limit ? buf.slice(i) : buf;
};

(function () {
  const root = document.getElementById('audio-player');
  if (!root) return; // not on a page with the player
  const variantId = root.dataset.variantId;
  const canEdit = root.dataset.canEdit === 'true';
  // "-choice": only an explicit toggle click is remembered. The old key
  // also saved whatever category was auto-picked on load, so a song that
  // once had only a band clip kept preferring it after a real Guitar Pro
  // recording was added.
  const prefKey = `songbooks:audio-category-choice:${variantId}`;
  const $ = (id) => document.getElementById(id);

  const categoryToggle = $('audio-category-toggle');
  const emptyState = $('audio-empty-state');
  const playerBody = $('audio-player-body');
  const canvas = $('audio-waveform');
  const playedCanvas = $('audio-waveform-played');
  const playedClip = $('audio-played');
  const waveEl = $('audio-wave');
  const rulerEl = $('audio-ruler');
  const rulerMarks = $('audio-ruler-marks');
  const cycleEl = $('audio-cycle');
  const cycleShade = $('audio-cycle-shade');
  const bargridEl = $('audio-bargrid');
  const playhead = $('audio-playhead');
  const hoverEl = $('audio-hover');
  const hoverLabel = $('audio-hover-label');
  const playBtn = $('audio-play-btn');
  const lcdPos = $('audio-lcd-pos');
  const lcdSection = $('audio-lcd-section');
  const timeLabel = $('audio-time-label');
  // The dock's thin line (play.html): where the playhead is in the whole
  // recording, the sections ticked on it; a click goes there.
  const dockLine = $('audio-dock-line'), dockPlayed = $('audio-dock-played');
  const optsBtn = $('audio-opts-btn');
  const optsEl = $('audio-opts');
  // On a phone the dock is a card that opens into a sheet ("Phone sheet").
  const dock = $('audio-dock');
  const dockBackdrop = $('audio-dock-backdrop');
  const dockSections = $('audio-dock-sections');
  const lcdEl = $('audio-lcd');
  const lcdState = $('audio-lcd-state');
  const phoneMQ = matchMedia('(max-width: 720px)');
  const cycleBtn = $('audio-cycle-btn');
  const countInBtn = $('audio-countin-btn');
  const countInLabel = $('audio-countin-label');
  const clickBtn = $('audio-click-btn');

  const speedBpm = $('audio-speed-bpm');
  const trainerBtn = $('audio-trainer-btn');
  const trainerLabel = $('audio-trainer-label');
  const iconPlay = playBtn ? playBtn.innerHTML : '▶';
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.5" height="12" rx="1"/><rect x="11.5" y="4" width="3.5" height="12" rx="1"/></svg>';

  const audio = new Audio();
  audio.preload = 'metadata';
  // Slowing down keeps the pitch (the browsers' default, made explicit).
  audio.preservesPitch = true;
  audio.mozPreservesPitch = true;
  audio.webkitPreservesPitch = true;
  let peaks = null; // Float32Array of per-bucket peak amplitudes, 0..1
  let duration = 0;
  let categories = {}; // {gp|youtube|band|synth: {markers, barSyncPoints, byteSize}}
  let activeCategory = null;
  // The Guitar Pro file played by alphaTab's synthesizer, when the song has
  // no recording (offerSynth below, gp-view.js): {bytes, type, beats,
  // sections, bpm, ownBars}.
  let synth = null;

  // ---- The song: tempo, metre, sections (from the page) -------------------
  const transportEl = $('transport');
  // The song's tempo; the Guitar Pro file's when its synthesizer plays
  // (offerSynth below).
  let bpm = transportEl ? parseFloat(transportEl.dataset.bpm) || 0 : 0;
  let songBpm = bpm;
  const beatsPerBar = transportEl ? parseInt(transportEl.dataset.beatsPerBar, 10) || 4 : 4;
  const timeNum = transportEl ? parseInt(transportEl.dataset.timeNum, 10) || 4 : 4;
  const timeDen = transportEl ? parseInt(transportEl.dataset.timeDen, 10) || 4 : 4;
  // 6/8, 9/8, 12/8: each main pulse is a dotted quarter, clicked with its
  // two softer eighths like the metronome does.
  const isCompound = timeDen === 8 && [6, 9, 12].includes(timeNum);
  let secondsPerBar = bpm > 0 ? (60 / bpm) * beatsPerBar : 0;
  // The beats in bar b: the song's metre, or the Guitar Pro file's own,
  // bar by bar, when its synthesizer plays.
  const beatsIn = (b) => (activeCategory === 'synth' && synth && synth.beats[b]) || beatsPerBar;
  // A song opening with a pickup numbers it 0, so the first full bar is 1;
  // a "lead:" line of several bars (before the first section, in none)
  // numbers them -1, 0. leadIn is how many
  // bars come before bar 1 (each mode draws its own copy of a bar: counted
  // once, by its played number).
  const leadIn = () => new Set([...document.querySelectorAll('#sheet .bar-pickup[data-global-bar]')].map((el) => el.dataset.globalBar)).size;
  const hasPickup = () => leadIn() > 0;
  // The song's "lead:" bars alone (a written pickup bar isn't one).
  const leadBarCount = () => new Set([...document.querySelectorAll('#sheet .bar-lead[data-global-bar]')].map((el) => el.dataset.globalBar)).size;
  const barLabel = (b) => String(b - leadIn() + 1);

  function sections() {
    // The synthesizer's own sections, when the song's bars aren't the file's.
    if (activeCategory === 'synth' && synth && synth.ownBars) return synth.sections;
    const out = [];
    const seen = new Set();
    document.querySelectorAll('.sheet-section[data-start-bar]').forEach((el) => {
      const startBar = parseInt(el.dataset.startBar, 10);
      const barCount = parseInt(el.dataset.barCount, 10) || 0;
      const index = parseInt(el.dataset.sectionIndex, 10);
      // A section can be on the page twice (a view's copy beside the
      // chart's): once here.
      if (Number.isNaN(startBar) || barCount <= 0 || seen.has(index)) return;
      seen.add(index);
      out.push({ index, label: el.dataset.label || '', startBar, barCount, hasLyrics: el.dataset.hasLyrics === 'true' });
    });
    return out.sort((a, b) => a.startBar - b.startBar);
  }
  const totalBars = () => sections().reduce((m, s) => Math.max(m, s.startBar + s.barCount), 0);
  const sectionAt = (bar) => sections().find((s) => bar >= s.startBar && bar < s.startBar + s.barCount) || null;

  // ---- Practice settings, remembered per song in this browser -------------
  const optsKey = `songbooks:player:${variantId}`;
  const prefs = {
    speed: 1, countIn: 0, countInEach: false,
    click: false, clickSound: 'click', clickVolume: 70,
    trainer: { on: false, start: 60, end: 100, step: 10, every: 2 },
    cycle: null, // {from, to, on}: bars [from, to)
    semitones: 0, // the recording's key, up or down
    voice: 100, // the voice's volume, in %, when the recording has its parts
    voiceBack: 100, // the level the voice's switch brings back (player-mix.js)
  };
  try {
    const saved = JSON.parse(localStorage.getItem(optsKey) || '{}');
    Object.assign(prefs, saved, { trainer: { ...prefs.trainer, ...(saved.trainer || {}), on: false }, cycle: null });
    // The voice on or off, kept with the song (an older save's voiceOff,
    // the last level under 100, is dropped: player-mix.js voiceFromSaved).
    const v = window.SongbooksMix.voiceFromSaved(saved);
    prefs.voice = v.voice;
    prefs.voiceBack = v.back;
    delete prefs.voiceOff;
  } catch (e) { /* defaults */ }
  // A loop — the cycle range, and the trainer that runs on it — is for
  // this sitting only: every visit starts playing the song straight.
  // Opened from a student's songbook (a coaching community): the song is
  // in the student's key, the coach's to set, not this browser's.
  const studentKey = root.dataset.studentKey !== undefined ? parseInt(root.dataset.studentKey, 10) || 0 : null;
  const savedSemitones = prefs.semitones;
  if (studentKey !== null) prefs.semitones = studentKey;
  const savePrefs = () => {
    const keep = { ...prefs, cycle: null, trainer: { ...prefs.trainer, on: false } };
    if (studentKey !== null) keep.semitones = savedSemitones;
    if (prefs.trainer.on) keep.speed = 1;
    try { localStorage.setItem(optsKey, JSON.stringify(keep)); } catch (e) { /* ignore */ }
  };

  function fmtTime(s) {
    if (!isFinite(s) || s < 0) s = 0;
    const m = Math.floor(s / 60), sec = Math.floor(s % 60);
    return `${m}:${String(sec).padStart(2, '0')}`;
  }

  // ---- Waveform: decode once per category switch, draw peaks into a
  // fixed-width bucket array (independent of canvas pixel width, which
  // can change on resize) so a resize just re-renders from cached peaks,
  // no re-decode. ------------------------------------------------------
  // One download per recording, shared with the Mixer (mixer.js): the
  // bytes feed both the <audio> element (as a blob URL) and the waveform.
  // Streaming the URL into <audio> AND fetching it again for the waveform
  // left half-read media connections open, exhausting the browser's six
  // connections per host until every other request on the page stalled.
  async function decodeWaveform() {
    peaks = null;
    const cat = activeCategory;
    let bytes;
    try { bytes = cat === 'synth' ? synth.bytes : await window.songbooksAudioBytes(variantId, cat, categories[cat]); } catch (e) {
      if (cat === activeCategory) setState('error', navigator.onLine === false ? t('Offline: this recording can’t be read.') : t('Couldn’t load this recording.'));
      return;
    }
    if (cat !== activeCategory) return;
    if (audio.dataset.blobFor !== cat) {
      if (audio.src.startsWith('blob:')) URL.revokeObjectURL(audio.src);
      audio.src = URL.createObjectURL(new Blob([bytes], { type: cat === 'synth' ? synth.type : 'audio/mpeg' }));
      audio.dataset.blobFor = cat;
      audio.playbackRate = prefs.speed;
      applyVoice();
    }
    const arrayBuffer = window.songbooksDecodable(bytes.slice(0));
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    let audioBuffer;
    try {
      audioBuffer = await ctx.decodeAudioData(arrayBuffer);
    } catch (e) {
      if (root.dataset.state === 'loading') setState('');
      return; // unsupported/corrupt file — the player still works via <audio>, just no waveform
    } finally {
      ctx.close();
    }
    const channel = audioBuffer.getChannelData(0);
    const buckets = 800; // fixed resolution, downsampled further at draw time if canvas is narrower
    const samplesPerBucket = Math.floor(channel.length / buckets) || 1;
    const out = new Float32Array(buckets);
    for (let i = 0; i < buckets; i++) {
      let max = 0;
      const start = i * samplesPerBucket;
      const end = Math.min(start + samplesPerBucket, channel.length);
      for (let j = start; j < end; j++) {
        const v = Math.abs(channel[j]);
        if (v > max) max = v;
      }
      out[i] = max;
    }
    peaks = out;
    if (root.dataset.state === 'loading') setState('');
    detectBars(audioBuffer, cat);
    rebuildGrid();
    drawWaveform();
  }

  // ---- Unaligned recordings: no per-bar sync points and no section
  // markers. Rather than leave the chart without a playing bar, find the
  // count-in and downbeats the way the Mixer's Auto-align does
  // (mixer-autoalign.js) and follow that, in memory only: saving an
  // alignment stays an explicit choice in the Mixer.
  let detected = null; // [{barIndex, timeSeconds}] for the active category
  // The recording, mono at ~11 kHz, kept for the detection and for the
  // Sync sheet's snapping (sync-sheet.js asks for the downbeats).
  let analysisInput = null; // {x, sr, cat}
  function keepAnalysisInput(audioBuffer, cat) {
    const f = Math.max(1, Math.round(audioBuffer.sampleRate / 11025));
    const c0 = audioBuffer.getChannelData(0);
    const c1 = audioBuffer.numberOfChannels > 1 ? audioBuffer.getChannelData(1) : c0;
    const x = new Float32Array(Math.floor(audioBuffer.length / f));
    for (let i = 0, j = 0; i < x.length; i++) {
      let sum = 0;
      for (let k = 0; k < f; k++, j++) sum += c0[j] + c1[j];
      x[i] = sum / (2 * f);
    }
    analysisInput = { x, sr: audioBuffer.sampleRate / f, cat };
  }
  // Where the chart's bars fall in the recording, found by ear (the
  // Mixer's Auto-align): [time of bar 0, bar 1, …], or null.
  let detectedFull = null;
  function runDetection() {
    if (detectedFull && detectedFull.cat === activeCategory) return detectedFull.times;
    if (!window.SongbooksAutoAlign || !transportEl || !analysisInput || analysisInput.cat !== activeCategory) return null;
    const total = totalBars();
    if (!total || !bpm) return null;
    try {
      const ticksPerBar = (timeNum * 3840) / timeDen;
      const barTicks = [];
      for (let b = 0; b <= total; b++) barTicks.push(b * ticksPerBar);
      const res = window.SongbooksAutoAlign.analyze(analysisInput, {
        tempo: bpm, barTicks, onsets: [], barChords: [],
        firstFull: leadIn(),
        lead: leadBarCount(),
      });
      if (!res || !res.barTimes) return null;
      detectedFull = { cat: activeCategory, times: res.barTimes };
      return res.barTimes;
    } catch (e) { return null; /* no highlight beats a wrong one */ }
  }
  function detectBars(audioBuffer, cat) {
    detected = null;
    detectedFull = null;
    keepAnalysisInput(audioBuffer, cat);
    const meta = categories[cat] || {};
    if ((meta.barSyncPoints && meta.barSyncPoints.length) || (meta.markers && meta.markers.length)) return;
    const times = cat === activeCategory ? runDetection() : null;
    if (times) detected = times.map((t, i) => ({ barIndex: i, timeSeconds: t }));
  }

  // ---- The bar grid: grid[b] is when bar b starts in the recording, for
  // every bar the recording covers. From the per-bar sync points (a Guitar
  // Pro import, an auto-align, or the in-memory detection above), else
  // the section markers plus the song's tempo. Everything bar-based —
  // the LCD, the cycle range, the click, bar skipping — reads it.
  let grid = [];
  const hasBars = () => grid.length > 1;
  // The Sync sheet's markers while it's open and not saved: the chart,
  // the counter and the click follow them, so a tap can be heard.
  let preview = null;
  function currentMarkers() { return preview || (categories[activeCategory] && categories[activeCategory].markers) || []; }
  function rebuildGrid() {
    grid = [];
    if (!duration) { refreshTimeline(); renderSync(); return; }
    const synced = preview ? [] : (categories[activeCategory] && categories[activeCategory].barSyncPoints) || [];
    const pts = (synced.length ? synced : (!preview && detected) || []).slice().sort((a, b) => a.barIndex - b.barIndex);
    let at = null;
    if (pts.length > 1) {
      at = (b) => {
        let i = 0;
        while (i < pts.length - 2 && pts[i + 1].barIndex <= b) i++;
        const p = pts[i], q = pts[i + 1];
        return p.timeSeconds + ((b - p.barIndex) / (q.barIndex - p.barIndex)) * (q.timeSeconds - p.timeSeconds);
      };
    } else {
      // Section markers: each section's bars spread evenly up to the next
      // section's marker (as the mixer draws them), so a chart whose tempo
      // isn't the recording's still meets every section on time; before
      // the first marker and after the last, the chart's tempo, else the
      // pace between the markers.
      const secs = sections();
      const marks = currentMarkers()
        .map((m) => ({ t: m.timeSeconds, start: (secs.find((s) => s.index === m.sectionIndex) || {}).startBar }))
        .filter((m) => m.start !== undefined)
        .sort((a, b) => a.start - b.start);
      let pace = secondsPerBar;
      if (marks.length > 1) {
        const first = marks[0], last = marks[marks.length - 1];
        if (last.start > first.start) pace = pace || (last.t - first.t) / (last.start - first.start);
      }
      if (marks.length && pace) {
        at = (b) => {
          let i = 0;
          while (i < marks.length - 1 && marks[i + 1].start <= b) i++;
          const m = marks[i], next = marks[i + 1];
          if (b >= m.start && next && next.start > m.start) {
            return m.t + ((b - m.start) / (next.start - m.start)) * (next.t - m.t);
          }
          return m.t + (b - m.start) * pace;
        };
      }
    }
    if (at) {
      for (let b = 0; b < 5000; b++) {
        const t = at(b);
        grid.push(t);
        if (t > duration) break;
      }
      // The song's "lead:" bars, in no section: exactly N bars ending
      // where its first section starts, at that section's measured pace
      // (else the chart's tempo), whatever the sync placed there — a
      // detection or an extrapolation can run them back over a whole
      // pre-roll. Squeezed into the time there is when it's shorter, kept
      // before the recording (counted in) when the first section starts
      // at 0:00 (lead-time.js). The time before them is no bar at all.
      const lead = leadBarCount();
      const LT = window.SongbooksLeadTime;
      if (lead > 0 && LT) {
        const first = sections().find((s) => s.barCount > 0);
        grid = LT.placeLead(grid, lead, LT.firstPace(grid, lead, first ? first.barCount : 1) || secondsPerBar);
      }
    }
    refreshTimeline();
    renderSync();
  }
  function timeAtBar(b) {
    if (!hasBars()) return null;
    const n = grid.length - 1;
    const i = Math.max(0, Math.min(n - 1, Math.floor(b)));
    return grid[i] + (b - i) * (grid[i + 1] - grid[i]);
  }
  function barAtTime(t) {
    if (!hasBars()) return null;
    let lo = 0, hi = grid.length - 1;
    if (t < grid[0]) hi = 1;
    else if (t >= grid[hi]) lo = hi - 1;
    else {
      while (hi - lo > 1) {
        const mid = (lo + hi) >> 1;
        if (grid[mid] <= t) lo = mid; else hi = mid;
      }
    }
    return lo + (t - grid[lo]) / (grid[lo + 1] - grid[lo]);
  }
  // Where the recording is in the chart, as a fractional performed bar
  // (2.5 = halfway through bar index 2); song.js reads it to highlight the
  // playing bar and drive the tab cursor. null when the recording isn't
  // playing or hasn't been started.
  window.songbooksReferencePosition = () => {
    if (!duration || (audio.paused && !countingIn && audio.currentTime === 0)) return null;
    // The synthesizer's bars are the file's: only the Score view follows them
    // when the song's chart counts its bars otherwise.
    if (activeCategory === 'synth' && synth && synth.ownBars && $('sheet')?.dataset.renderMode !== 'score') return null;
    const bar = barAtTime(audio.currentTime);
    return bar === null ? null : { bar, playing: !audio.paused || countingIn, time: audio.currentTime, category: activeCategory };
  };
  // When bar b starts in the recording (lyric-lines.js), null without a grid.
  window.songbooksReferenceTimeAtBar = (b) => timeAtBar(b);

  // ---- Sync state: estimated or set ----------------------------------------
  // Three things place the chart on a recording: this player's own
  // detection (in memory), a program's proposal saved as markers (the
  // import wizard, the Mixer's Auto-align) and a person (the Sync sheet,
  // a flag dragged in the Mixer, a Guitar Pro file's own sync). The first
  // two are estimates until someone checks them by ear: the badge in the
  // readout says so, and the panel's Sync line opens the Sync sheet.
  const syncBadge = $('audio-sync-badge');
  const syncPop = $('audio-pop-sync');
  function syncInfo() {
    const meta = categories[activeCategory] || {};
    const points = meta.barSyncPoints || [];
    const marks = preview || meta.markers || [];
    if (!preview && points.length > 1) return { state: meta.syncOrigin === 'auto' ? 'auto' : 'set', source: 'points' };
    if (marks.length) {
      const auto = marks.filter((m) => m.origin === 'auto').length;
      return { state: auto ? 'auto' : 'set', source: 'markers', auto, total: marks.length };
    }
    if (detected) return { state: 'auto', source: 'detected' };
    return { state: 'none', source: '' };
  }
  // The tempo the section markers make the counter run at, against the
  // chart's: the chart's bars are spread evenly between two markers, so
  // a section with more bars written than the recording plays runs fast
  // (song 334: 23 bars written, 17 played, the counter at 122 bpm over a
  // 90 bpm recording). Per section, and the bar-weighted median.
  function syncTempos() {
    const secs = sections();
    const marks = currentMarkers()
      .map((m) => ({ t: m.timeSeconds, sec: secs.find((s) => s.index === m.sectionIndex) }))
      .filter((m) => m.sec)
      .sort((a, b) => a.sec.startBar - b.sec.startBar);
    const per = [];
    for (let i = 0; i + 1 < marks.length; i++) {
      const bars = marks[i + 1].sec.startBar - marks[i].sec.startBar, dt = marks[i + 1].t - marks[i].t;
      if (bars > 0 && dt > 0) per.push({ index: marks[i].sec.index, label: marks[i].sec.label, bars, bpm: (60 * beatsPerBar * bars) / dt });
    }
    let median = 0;
    if (per.length) {
      const sorted = per.slice().sort((a, b) => a.bpm - b.bpm);
      const total = sorted.reduce((s, p) => s + p.bars, 0);
      let acc = 0;
      for (const p of sorted) { acc += p.bars; if (acc >= total / 2) { median = p.bpm; break; } }
    }
    const off = (v) => bpm > 0 && v > 0 && Math.abs(v / bpm - 1) > 0.08;
    return { per: per.map((p) => ({ ...p, off: off(p.bpm) })), median, off: off(median) };
  }
  // One indicator (player-proposal.md §3.6 with #74's states): the
  // player's data-sync — "estimated" (detected, or saved by a program and
  // not checked by ear: "≈ 21.1", the ruler's ticks dashed), "set", "none"
  // (no bars) — and the badge in the readout, shown while estimated or
  // while the counter runs off the recording's tempo. The badge opens the
  // panel's Sync line: where the bars come from, what's off, and for an
  // editor the Sync sheet (or the Mixer) and "It's right: confirm".
  function renderSync() {
    const info = syncInfo();
    const tempos = info.source === 'markers' ? syncTempos() : { per: [], off: false };
    const s = !duration ? '' : !hasBars() ? 'none' : info.state === 'auto' ? 'estimated' : 'set';
    if (s) root.dataset.sync = s; else delete root.dataset.sync;
    if (syncBadge) {
      syncBadge.hidden = !(s === 'estimated' || (s === 'set' && tempos.off));
      syncBadge.dataset.state = info.state;
      syncBadge.classList.toggle('warn', tempos.off);
      syncBadge.textContent = s === 'estimated' ? t('sync estimated') : t('Sync set');
      syncBadge.title = tempos.off ? t('Here the counter runs at about %d bpm, but the chart says %d bpm', Math.round(tempos.median), Math.round(bpm))
        : s === 'estimated' ? t('the chart follows an estimated sync: check it by ear') : t('the chart follows a sync checked by ear');
    }
    const text = $('audio-sync-text');
    const synth = activeCategory === 'synth';
    if (synth && syncBadge) syncBadge.hidden = true;
    if (text && synth) {
      text.textContent = t('The Guitar Pro file, played by the synthesizer: the bars are the file’s.');
    } else if (text) {
      text.textContent = s === 'none' || !s ? t('No bars: the recording plays by time; ←/→ step 5 s.')
        : info.source === 'detected' ? t('Estimated: the player placed the bars by listening; they can be off by a beat or a section. Loops still snap to bars.')
        : info.source === 'points' ? (info.state === 'set' ? t('Every bar is placed: the Guitar Pro file’s own sync, or one checked by ear.') : t('Every bar is placed from the recording’s analysis or by Auto-align, not checked by ear yet.'))
        : info.state === 'auto' ? (info.auto === info.total ? t('Each section’s start was placed automatically (the import wizard or Auto-align) and nobody has checked it by ear yet.') : t('%d of %d section starts are still automatic estimates, not checked by ear.', info.auto, info.total))
        : t('Each section’s start was placed or checked by ear.');
    }
    const fix = $('audio-sync-fix');
    if (fix) fix.hidden = !canEdit || synth;
    const confirm = $('audio-sync-confirm');
    if (confirm) confirm.hidden = synth || !(canEdit && info.state === 'auto' && (info.source !== 'points' || (categories[activeCategory] || {}).markers?.length) && hasBars());
    if (!syncPop) return;
    const say = (cls, line) => { const p = document.createElement('p'); p.className = cls; p.textContent = line; return p; };
    const out = [];
    if (tempos.off) {
      out.push(say('audio-sync-warn', t('Here the counter runs at about %d bpm, but the chart says %d bpm: some sections must have more or fewer bars in the chart than in the recording. Count them while listening, then fix the chart or the sync.', Math.round(tempos.median), Math.round(bpm))));
      const odd = tempos.per.filter((p) => p.off).slice(0, 4).map((p) => t('%s: %d bpm', p.label, Math.round(p.bpm)));
      if (odd.length) out.push(say('hint', odd.join(' · ')));
    }
    if (!canEdit && s && s !== 'set') out.push(say('hint', t('Someone who can edit this song can correct it.')));
    syncPop.replaceChildren(...out);
    syncPop.hidden = !out.length;
  }
  // For someone who can edit the song, the badge opens the Sync sheet
  // itself: correcting is what it's for. Anyone else gets the panel's line.
  syncBadge?.addEventListener('click', () => {
    if (canEdit && window.songbooksSyncSheet) { setSheet(false); window.songbooksSyncSheet.open(); return; }
    openLine('audio-line-sync');
  });
  $('audio-sync-confirm')?.addEventListener('click', confirmSync);
  // Correct it: the Sync sheet (#74) where the page has it, else the Mixer.
  $('audio-sync-fix')?.addEventListener('click', () => {
    setSheet(false);
    if (window.songbooksSyncSheet && window.songbooksSyncSheet.open) { window.songbooksSyncSheet.open(); return; }
    document.querySelector('#mode-segmented button[data-value="mixer"]')?.click();
  });
  // The estimate is right: every section's start as it is, saved as set
  // (the detection's too, which then stays put).
  async function confirmSync() {
    const marks = currentSyncMarkers().map((m) => ({ ...m, origin: 'set' }));
    if (!marks.length) return;
    const status = $('audio-sync-status');
    const ok = await saveMarkers(marks);
    if (status && window.Forms) Forms.status(status, ok ? t('Saved.') : t("Couldn't save. Try again."), ok ? 'ok' : 'error');
  }
  // Each section's start as the chart follows it now, with its origin.
  function currentSyncMarkers() {
    const meta = categories[activeCategory] || {};
    if (!preview && meta.markers && meta.markers.length) return meta.markers.map((m) => ({ sectionIndex: m.sectionIndex, timeSeconds: m.timeSeconds, origin: m.origin || 'set' }));
    if (!hasBars()) return preview ? preview.slice() : [];
    return sections().map((s) => ({ sectionIndex: s.index, timeSeconds: Math.round(timeAtBar(s.startBar) * 1000) / 1000, origin: 'auto' }))
      .filter((m) => m.timeSeconds >= 0 && m.timeSeconds < duration);
  }
  // One request for the whole sync (PUT …/markers); the player then
  // reloads it.
  async function saveMarkers(markers, cat = activeCategory) {
    try {
      const res = await fetch(`/api/songs/${variantId}/audio/${cat}/markers`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ markers }),
      });
      if (!res.ok) return false;
      if (categories[cat]) {
        categories[cat].markers = markers.map((m) => ({ ...m }));
        categories[cat].syncOrigin = markers.some((m) => m.origin === 'auto') ? 'auto' : (markers.length ? 'set' : '');
      }
      if (cat === activeCategory) { preview = null; detected = null; rebuildGrid(); }
      // The save may have dropped or confirmed the per-bar points (which
      // the grid follows first): the server's sync, as saved.
      await refreshSync(cat);
      document.dispatchEvent(new CustomEvent('songbooks:sync-saved', { detail: { category: cat } }));
      return true;
    } catch (e) { return false; }
  }
  // A recording's sync (markers, per-bar points, their origin) read again
  // from the server, without reloading the recording itself.
  async function refreshSync(cat = activeCategory) {
    try {
      const res = await fetch(`/api/songs/${variantId}/audio`);
      if (!res.ok) return;
      const c = ((await res.json()).categories || []).find((x) => x.category === cat);
      if (!c || !categories[cat]) return;
      Object.assign(categories[cat], { markers: c.markers || [], barSyncPoints: c.barSyncPoints || [], syncOrigin: c.syncOrigin || '' });
      if (cat === activeCategory) { detected = null; rebuildGrid(); renderPosition(); }
    } catch (e) { /* kept as it was */ }
  }

  // ---- Drawing ------------------------------------------------------------
  const pctOf = (t) => (duration ? (t / duration) * 100 : 0);
  function paint(cv, color) {
    const ctx2 = cv.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    const w = waveEl.clientWidth, h = waveEl.clientHeight;
    if (!w || !h) return;
    cv.style.width = `${w}px`;
    cv.width = w * dpr; cv.height = h * dpr;
    ctx2.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx2.clearRect(0, 0, w, h);
    ctx2.fillStyle = color;
    const mid = h / 2;
    // Rounded bars with a gap, SoundCloud-style, about one per 3px.
    const n = Math.max(1, Math.floor(w / 3));
    const step = peaks.length / n;
    for (let i = 0; i < n; i++) {
      let p = 0;
      for (let j = Math.floor(i * step); j < Math.floor((i + 1) * step); j++) p = Math.max(p, peaks[j] || 0);
      const amp = Math.max(p, 0.03) * (mid - 1);
      ctx2.fillRect(i * 3, mid - amp, 2, amp * 2);
    }
  }
  function drawWaveform() {
    if (!peaks || !canvas) return;
    const style = getComputedStyle(root);
    paint(canvas, style.getPropertyValue('--waveform-color').trim() || '#bbb');
    if (playedCanvas) paint(playedCanvas, style.getPropertyValue('--waveform-played').trim() || style.getPropertyValue('--accent').trim() || '#af4e39');
  }

  // Faint ticks for bars, stronger ones where a section starts; the ruler
  // above carries bar numbers (thinned out when bars get narrow) and the
  // section names.
  function refreshTimeline() {
    if (bargridEl) bargridEl.innerHTML = '';
    if (rulerMarks) rulerMarks.innerHTML = '';
    dockLine?.querySelectorAll('.audio-dock-tick').forEach((e) => e.remove());
    if (!duration || !hasBars()) { renderCycle(); updateButtons(); wireBarClickToPlay(); return; }
    const starts = new Map(sections().map((s) => [s.startBar, s]));
    const width = waveEl.clientWidth || 600;
    const barPx = (width * (grid[Math.min(grid.length - 1, 2)] - grid[1] || secondsPerBar)) / duration;
    const every = [1, 2, 4, 8, 16].find((k) => barPx * k >= 26) || 32;
    const chartBars = totalBars();
    grid.forEach((t, b) => {
      // A pickup bar may start a moment before the recording does.
      if (t < 0 && b === 0) t = 0;
      if (t < 0 || t > duration) return;
      const sec = starts.get(b);
      const tick = document.createElement('div');
      tick.className = 'audio-bargrid-tick' + (sec ? ' section' : '');
      tick.style.left = `${pctOf(t)}%`;
      bargridEl.appendChild(tick);
      if (sec && dockLine && b > 0) {
        const d = document.createElement('span');
        d.className = 'audio-dock-tick';
        d.style.left = `${(t / duration) * 100}%`;
        dockLine.appendChild(d);
      }
      // Numbers only over the chart's own bars; the recording may run on.
      const numbered = b < chartBars && (((b - leadIn() + 1) % every) + every) % every === 0;
      const label = sec ? sec.label : (numbered ? barLabel(b) : '');
      if (label) {
        const m = document.createElement('span');
        m.className = sec ? 'audio-ruler-section' : 'audio-ruler-bar';
        m.style.left = `${pctOf(t)}%`;
        m.textContent = label;
        if (sec) { m.dataset.startBar = sec.startBar; m.dataset.barCount = sec.barCount; m.title = window.t('%s — click to cycle it, Shift-click to extend the cycle', sec.label); }
        rulerMarks.appendChild(m);
      }
    });
    tidyRuler();
    renderCycle();
    updateButtons();
    wireBarClickToPlay();
  }

  // The ruler's labels never overlap: a section's name stops short of the
  // next section's ("Chorus 1" cut with "…"), and a bar number under a
  // section's name gives way to it.
  function tidyRuler() {
    if (!rulerMarks || !rulerMarks.clientWidth) return;
    const secs = [...rulerMarks.querySelectorAll('.audio-ruler-section')];
    const left = (el) => el.offsetLeft;
    secs.forEach((el, i) => {
      const next = secs[i + 1];
      const room = (next ? left(next) : rulerMarks.clientWidth) - left(el) - 3;
      el.style.maxWidth = `${Math.max(0, room)}px`;
      el.hidden = room < 14;
    });
    const boxes = secs.filter((el) => !el.hidden).map((el) => [left(el), left(el) + el.offsetWidth]);
    let lastEnd = -Infinity;
    rulerMarks.querySelectorAll('.audio-ruler-bar').forEach((el) => {
      const a = left(el), b = a + el.offsetWidth;
      el.hidden = a < lastEnd + 2 || boxes.some(([x, y]) => a < y + 3 && b > x - 3);
      if (!el.hidden) lastEnd = b;
    });
  }

  function renderCycle() {
    if (typeof markCycleInChart === 'function') markCycleInChart();
    const c = prefs.cycle;
    const show = c && hasBars() && duration;
    if (cycleEl) cycleEl.hidden = !show;
    if (cycleShade) cycleShade.hidden = !(show && c.on);
    if (!show) return;
    const l = pctOf(timeAtBar(c.from)), r = pctOf(timeAtBar(c.to));
    for (const el of [cycleEl, cycleShade]) {
      el.style.left = `${Math.max(0, l)}%`;
      el.style.width = `${Math.max(0.3, Math.min(100, r) - Math.max(0, l))}%`;
    }
    cycleEl.classList.toggle('on', !!c.on);
    cycleEl.title = c.on
      ? t('cycle: bars %s–%s — click to switch off, drag to move, drag an edge to resize', barLabel(c.from), barLabel(c.to - 1))
      : t('cycle: bars %s–%s — click to switch on, drag to move, drag an edge to resize', barLabel(c.from), barLabel(c.to - 1));
  }

  function renderPosition() {
    if (!duration) return;
    const t = audio.currentTime;
    const pct = Math.max(0, Math.min(100, pctOf(t)));
    if (playhead) playhead.style.left = `${pct}%`;
    if (playedClip) playedClip.style.width = `${pct}%`;
    if (dockPlayed) dockPlayed.style.width = `${Math.max(0, Math.min(100, (t / duration) * 100))}%`;
    if (timeLabel) timeLabel.textContent = `${fmtTime(t)} / ${fmtTime(duration)}`;
    const pos = barAtTime(t);
    if (pos === null) {
      lcdPos.textContent = fmtTime(t);
      lcdSection.textContent = '';
      return;
    }
    const b = Math.floor(pos);
    const beats = beatsIn(b);
    const beat = Math.min(beats - 1, Math.floor((pos - b) * beats));
    // Before the first bar (a count, a silence): waiting at its start, not a dash.
    // Before a lead (the song's "lead:" bars), the recording's time is no
    // bar at all: a dash, not a count.
    lcdPos.textContent = b < 0 ? (leadBarCount() > 0 ? '—' : `${barLabel(0)}.1`) : `${barLabel(b)}.${beat + 1}`;
    const sec = sectionAt(b);
    lcdSection.textContent = sec ? sec.label : '';
    syncDockSections();
  }

  function updateButtons() {
    const playing = !audio.paused || countingIn;
    if (playBtn) {
      const want = playing ? iconPause : iconPlay;
      if (playBtn.dataset.state !== String(playing)) { playBtn.innerHTML = want; playBtn.dataset.state = String(playing); }
      playBtn.classList.toggle('active', playing);
    }
    const press = (btn, on) => { if (btn) { btn.classList.toggle('active', !!on); btn.setAttribute('aria-pressed', on ? 'true' : 'false'); } };
    press(cycleBtn, prefs.cycle && prefs.cycle.on);
    // The loop's chip reads its bars: "Boucle 21–28".
    const cyc = prefs.cycle && prefs.cycle.on && hasBars() ? prefs.cycle : null;
    const cycleLabel = $('audio-cycle-label');
    if (cycleLabel) cycleLabel.textContent = cyc ? t('Cycle %s–%s', barLabel(cyc.from), barLabel(cyc.to - 1)) : t('Cycle');
    press(countInBtn, prefs.countIn > 0);
    press(clickBtn, prefs.click);
    press(trainerBtn, prefs.trainer.on);
    if (cycleBtn) cycleBtn.disabled = !hasBars();
    if (countInLabel) countInLabel.textContent = prefs.countIn ? tn(prefs.countIn, '%d bar', '%d bars') : t('Count-in');
    if (trainerLabel) trainerLabel.textContent = prefs.trainer.on ? t('Trainer %d% → %d%', Math.round(prefs.speed * 100), prefs.trainer.end) : t('Trainer');
    const meter = $('audio-trainer-meter');
    if (meter) {
      const tr = prefs.trainer;
      const span = Math.max(1, tr.end - tr.start);
      meter.style.width = tr.on ? `${Math.max(4, Math.min(100, ((prefs.speed * 100 - tr.start) / span) * 100))}%` : '0';
    }
    renderTrainer();
    renderDockState();
    syncDockSections();
    if (dock && dock.classList.contains('is-open')) renderLoopLine();
  }

  // ---- Speed (pitch kept) ---------------------------------------------------
  // The chip reads "80 % · 119 bpm"; a tap goes back to 100 % and, again,
  // to the last speed used (80 % the first time). Its ▾: a stepper, ±5 %,
  // and 70 / 80 / 90 / 100 %.
  const speedBtn = $('audio-speed-btn'), speedLabel = $('audio-speed-label'), speedAmount = $('audio-speed-amount');
  // "80 %": a percentage as French writes it (a narrow no-break space).
  const pctText = (p) => t('%d%', p);
  function setSpeed(v, save = true) {
    const pct = Math.round(Math.max(25, Math.min(150, v * 100)));
    prefs.speed = pct / 100;
    if (pct !== 100) prefs.speedLast = prefs.speed;
    audio.playbackRate = prefs.speed;
    const on = pct !== 100;
    speedBtn?.classList.toggle('active', on);
    speedBtn?.setAttribute('aria-pressed', String(on));
    const bpmNow = bpm ? `${Math.round(bpm * prefs.speed)} bpm` : '';
    if (speedLabel) speedLabel.textContent = on ? [pctText(pct), bpmNow].filter(Boolean).join(' · ') : t('Speed');
    if (speedAmount) speedAmount.textContent = pctText(pct);
    if (speedBpm) speedBpm.textContent = bpmNow;
    syncSpeedSeg();
    resetClicks();
    if (save) savePrefs();
    updateButtons();
  }
  const toggleSpeed = () => setSpeed(prefs.speed !== 1 ? 1 : (prefs.speedLast && prefs.speedLast !== 1 ? prefs.speedLast : 0.8));
  speedBtn?.addEventListener('click', toggleSpeed);
  $('audio-speed-down')?.addEventListener('click', () => setSpeed(prefs.speed - 0.05));
  $('audio-speed-up')?.addEventListener('click', () => setSpeed(prefs.speed + 0.05));
  function syncSpeedSeg() {
    $('audio-speed-seg')?.querySelectorAll('button').forEach((b) => b.classList.toggle('active', +b.dataset.v === Math.round(prefs.speed * 100)));
  }
  $('audio-speed-seg')?.addEventListener('click', (e) => { const b = e.target.closest('button[data-v]'); if (b) setSpeed(+b.dataset.v / 100); });

  // ---- Click: the metronome's own sounds (clock.js), placed on the
  // recording's bar grid so it follows a live tempo, not a fixed BPM ----
  /* global Metronome */
  const clicker = typeof Metronome === 'function' ? new Metronome({}) : null;
  let clicksUntil = null; // recording time up to which clicks are scheduled
  const resetClicks = () => { clicksUntil = null; };
  function clickAt(when, beatInBar, soft) {
    if (!clicker) return;
    clicker.sound = prefs.clickSound;
    clicker._click(when, beatInBar === 0 && !soft, soft, prefs.clickVolume / 70);
  }
  // Every beat (and compound-metre eighth) with a recording time in [a, b),
  // from bar 0 on: the recording's time before the chart's first bar (its
  // lead's, or its first section's) is no bar, so it gets no click.
  function beatsBetween(a, b) {
    const out = [];
    const first = Math.max(0, Math.floor(barAtTime(a)));
    for (let bar = first; bar < first + 64; bar++) {
      const t0 = timeAtBar(bar), t1 = timeAtBar(bar + 1);
      if (t0 >= b) break;
      const beats = beatsIn(bar);
      const beatDur = (t1 - t0) / beats;
      for (let k = 0; k < beats; k++) {
        const subs = isCompound ? 3 : 1;
        for (let s = 0; s < subs; s++) {
          const t = t0 + (k + s / subs) * beatDur;
          if (t >= a && t < b) out.push({ t, k, soft: s > 0 });
        }
      }
    }
    return out;
  }
  function scheduleClicks() {
    if (!prefs.click || !clicker || !hasBars() || audio.paused) return;
    const ctx = clicker._ensureCtx();
    watchCtx(ctx);
    const now = audio.currentTime, rate = audio.playbackRate || 1;
    const until = now + 0.12 * rate;
    const from = clicksUntil === null ? now : Math.max(clicksUntil, now);
    const late = outputDelay();
    for (const beat of beatsBetween(from, until)) clickAt(ctx.currentTime + late + (beat.t - now) / rate, beat.k, beat.soft);
    clicksUntil = until;
  }

  // ---- Transport ----------------------------------------------------------
  let countingIn = false;
  let countInTimer = null;
  let passes = 0; // completed cycle passes since play, for the trainer

  function cycleRange() {
    const c = prefs.cycle;
    if (!c || !c.on || !hasBars()) return null;
    return { t0: timeAtBar(c.from), t1: timeAtBar(c.to) };
  }

  // Counts in prefs.countIn bars at the tempo of the bar about to play
  // (and the current speed), then starts the recording at t, on the beat.
  function countInThen(t) {
    const ctx = clicker ? clicker._ensureCtx() : null;
    watchCtx(ctx);
    const bars = prefs.countIn;
    if (!ctx || !bars || !hasBars()) { startAt(t); return; }
    const b = Math.round(barAtTime(Math.max(t, grid[0])));
    const rate = audio.playbackRate || 1;
    const barDur = (timeAtBar(b + 1) - timeAtBar(b)) / rate;
    const beats = beatsIn(b);
    const beatDur = barDur / beats;
    const t0 = ctx.currentTime + 0.12;
    for (let i = 0; i < bars * beats; i++) {
      clickAt(t0 + i * beatDur, i % beats, false);
      if (isCompound) for (let s = 1; s < 3; s++) clickAt(t0 + (i + s / 3) * beatDur, 1, true);
    }
    // A start before 0 (a pickup bar starting before the recording does)
    // waits that much longer, from the recording's first sample.
    // Transposed, the recording comes out of the pitch shifter a little
    // late: it starts that much earlier.
    const startWall = t0 + bars * barDur + Math.max(0, -t) / rate - outputDelay();
    audio.currentTime = Math.max(0, t);
    countingIn = true;
    clearTimeout(countInTimer);
    countInTimer = setTimeout(() => {
      countingIn = false;
      startAt(Math.max(0, t), true);
    }, Math.max(0, (startWall - ctx.currentTime) * 1000 - 15));
    updateButtons();
  }
  function startAt(t, already) {
    if (!already) audio.currentTime = Math.max(0, t);
    resetClicks();
    document.dispatchEvent(new CustomEvent('songbooks:reference-playing'));
    // The click's context made now, in the click, not later in a tick.
    if (prefs.click && clicker) watchCtx(clicker._ensureCtx());
    // Routed, the recording is heard only while its context runs: resumed
    // here (a click, or the count-in's), and if it still isn't running a
    // moment later, the player stops and says so rather than run silent.
    const ctx = graph ? graph.ctx : null;
    if (ctx && ctx.state !== 'running') {
      ctx.resume().catch(() => {});
      setTimeout(() => { if (graph && graph.ctx.state !== 'running' && isPlaying()) refusedResume(); }, 1500);
    }
    playOrSay(audio);
    // The parts beside it start in the same gesture (Safari lets them).
    followParts();
  }

  function play() {
    if (!duration || countingIn) return;
    clearNotice();
    const cyc = cycleRange();
    let t = audio.currentTime;
    if (cyc && (t < cyc.t0 - 0.05 || t >= cyc.t1 - 0.05)) t = cyc.t0;
    if (prefs.trainer.on && cyc) { passes = 0; setSpeed(prefs.trainer.start / 100); }
    if (prefs.countIn && hasBars()) {
      // Count into the bar being played: from its downbeat.
      if (!cyc) t = timeAtBar(Math.floor(barAtTime(t) + 0.02));
      countInThen(t);
    } else {
      startAt(t);
    }
  }
  function pause() {
    clearTimeout(countInTimer);
    countingIn = false;
    audio.pause();
    updateButtons();
  }
  const isPlaying = () => !audio.paused || countingIn;
  function seek(t, keepPlaying = true) {
    if (!duration) return;
    const wasCounting = countingIn;
    if (wasCounting) pause();
    audio.currentTime = Math.max(0, Math.min(duration - 0.05, t));
    resetClicks();
    renderPosition();
    // Where it is now, at once, for what follows it (the score's cursor,
    // gp-view.js), stopped too, and at the start (⏮), where
    // songbooksReferencePosition says nothing.
    document.dispatchEvent(new CustomEvent('songbooks:player-seek', { detail: { bar: barAtTime(audio.currentTime), time: audio.currentTime } }));
    if (wasCounting && keepPlaying) play();
  }
  function seekToBar(b) {
    const t = timeAtBar(b);
    if (t !== null) seek(t);
  }

  // The cycle's end: back to its start — after a count-in when asked to
  // count in every pass — speeding up by the trainer's step every N passes.
  function onCycleEnd(cyc) {
    passes++;
    const tr = prefs.trainer;
    if (tr.on && passes % Math.max(1, tr.every) === 0 && prefs.speed * 100 < tr.end) {
      setSpeed(Math.min(tr.end, prefs.speed * 100 + tr.step) / 100, false);
    }
    if (prefs.countIn && prefs.countInEach) {
      audio.pause();
      countInThen(cyc.t0);
    } else {
      audio.currentTime = cyc.t0;
      resetClicks();
    }
    updateButtons();
  }

  function tick() {
    if (!audio.paused && !countingIn) {
      const cyc = cycleRange();
      if (cyc && audio.currentTime >= cyc.t1 - 0.012) onCycleEnd(cyc);
      scheduleClicks();
    }
    followVoice();
    renderPosition();
    if (duration) mediaSession?.position(duration, audio.currentTime, audio.playbackRate, isPlaying());
  }
  // A timer, not requestAnimationFrame: the cycle wrap and the click
  // scheduler must keep running when the tab is in the background.
  setInterval(tick, 15);

  playBtn?.addEventListener('click', () => (isPlaying() ? pause() : play()));
  $('audio-start-btn')?.addEventListener('click', () => {
    const cyc = cycleRange();
    seek(cyc ? cyc.t0 : (hasBars() ? Math.max(0, grid[0]) : 0));
  });
  function stepBar(dir) {
    if (!hasBars()) { seek(audio.currentTime + dir * 5); return; }
    const cur = barAtTime(audio.currentTime);
    seekToBar(dir < 0 ? Math.max(0, Math.ceil(cur - 0.05) - 1) : Math.floor(cur) + 1);
  }
  function stepSection(dir) {
    if (!hasBars()) { seek(audio.currentTime + dir * 15); return; }
    const cur = barAtTime(audio.currentTime);
    const starts = sections().map((s) => s.startBar);
    const target = dir < 0 ? starts.filter((b) => b < cur - 0.05).pop() : starts.find((b) => b > cur + 0.01);
    if (target !== undefined) seekToBar(target);
    else if (dir < 0) seekToBar(0);
  }
  $('audio-prev-btn')?.addEventListener('click', (e) => (e.altKey ? stepSection(-1) : stepBar(-1)));
  $('audio-next-btn')?.addEventListener('click', (e) => (e.altKey ? stepSection(1) : stepBar(1)));
  audio.addEventListener('play', updateButtons);
  audio.addEventListener('pause', updateButtons);
  audio.addEventListener('ended', () => { passes = 0; updateButtons(); });
  audio.addEventListener('loadedmetadata', () => {
    duration = audio.duration;
    audio.playbackRate = prefs.speed;
    rebuildGrid();
    renderPosition();
  });

  // ---- Options ------------------------------------------------------------
  function toggleCycle() {
    if (!hasBars()) return;
    if (!prefs.cycle) {
      // No range yet: cycle the section being played, as Logic cycles a
      // marker dragged into the ruler.
      const sec = sectionAt(Math.max(0, Math.floor(barAtTime(audio.currentTime))));
      prefs.cycle = sec ? { from: sec.startBar, to: sec.startBar + sec.barCount, on: true } : { from: 0, to: Math.min(4, grid.length - 1), on: true };
    } else {
      prefs.cycle.on = !prefs.cycle.on;
    }
    passes = 0;
    savePrefs();
    renderCycle();
    updateButtons();
  }
  cycleBtn?.addEventListener('click', toggleCycle);
  countInBtn?.addEventListener('click', () => {
    prefs.countIn = (prefs.countIn + 1) % 3;
    savePrefs();
    updateButtons();
  });
  function toggleClick() {
    // No recording: the click is the song's metronome (the follow-along
    // clock), on or off — the panel's switch and M alike.
    if (root.dataset.state === 'none') { toggleFollowClick(); return; }
    prefs.click = !prefs.click;
    // Its context made in this click, not later in a tick (Safari would
    // keep that one suspended).
    if (prefs.click && clicker) watchCtx(clicker._ensureCtx());
    resetClicks();
    savePrefs();
    updateButtons();
  }
  clickBtn?.addEventListener('click', toggleClick);
  // ---- Key and voice --------------------------------------------------------
  // The recording in another key (Signalsmith Stretch, an AudioWorklet,
  // static/js/vendor) and its voice turned down (its parts, split in the
  // background by cmd/stemsd: no_vocals plays, vocals at the voice's
  // volume beside it). Both go through Web Audio, set up the first time
  // either is used: until then the plain <audio>, which a locked phone
  // keeps playing.
  /* global SignalsmithStretch */
  const voiceAudio = new Audio();
  voiceAudio.preload = 'auto';
  voiceAudio.preservesPitch = true;
  voiceAudio.webkitPreservesPitch = true;
  let graph = null; // {ctx, bus, mainGain, gains: {vocals, drums, bass}, stretch}

  // ---- Sound that can't silently vanish (Safari) ----------------------------
  // Once routed through Web Audio (createMediaElementSource, for good: an
  // element can't be unrouted), the recording is heard only while its
  // context runs. Safari starts a context made outside a click suspended,
  // and may suspend or interrupt a running one, while the <audio> plays
  // on, silent. So the recording is routed only from a click, while
  // paused, or into a context already running (else it waits: routeLater);
  // the context is resumed on the next click or key; and when it stops
  // while the recording plays, the player pauses and says so.
  let lastGesture = -Infinity;
  const inGesture = () => !!(navigator.userActivation && navigator.userActivation.isActive) || performance.now() - lastGesture < 1000;
  const playerCtx = () => (graph ? graph.ctx : clicker && clicker._ctx) || null;
  const canRoute = () => audio.paused || inGesture() || (playerCtx() || {}).state === 'running';
  const routeLater = { parts: false, key: false };
  function routeNow() {
    if (routeLater.parts) { routeLater.parts = false; applyParts(); }
    if (routeLater.key) { routeLater.key = false; applyKey(); }
  }
  function onGesture() {
    lastGesture = performance.now();
    const ctx = playerCtx();
    if (ctx && ctx.state !== 'running' && ctx.state !== 'closed') ctx.resume().catch(() => {});
    routeNow();
  }
  for (const type of ['pointerdown', 'keydown', 'touchend']) window.addEventListener(type, onGesture, true);
  audio.addEventListener('pause', routeNow);
  // A short line in the readout, until the recording plays again.
  function notice(msg) { if (lcdError) lcdError.textContent = msg; }
  // Gone when Play is pressed again (play() below), not on the element's
  // own "playing", which can come after a refusal it raced.
  const clearNotice = () => { if (lcdError && root.dataset.state !== 'error') lcdError.textContent = ''; };
  function refused(msg) { pause(); resetClicks(); notice(msg); }
  // Play pressed, the context asked to run, and still not running.
  const refusedResume = () => refused(t('This browser won’t play the sound right now: press Play again, or reload the page.'));
  // play() refused (no click to start it: the count-in's timer, a part
  // catching up, a swapped source): stopped and said, not swallowed. An
  // AbortError (paused or swapped meanwhile) is no refusal.
  function playOrSay(el, after) {
    el.play().then(() => {}, (e) => {
      if (after) after();
      if (e && e.name === 'NotAllowedError' && (el === audio || !audio.paused)) refused(t('The browser blocked the sound: press Play again.'));
    });
  }
  const watchedCtx = new WeakSet();
  function watchCtx(ctx) {
    if (!ctx || watchedCtx.has(ctx)) return;
    watchedCtx.add(ctx);
    ctx.addEventListener('statechange', () => {
      if (ctx.state === 'running') return;
      // Suspended, interrupted or closed: the routed recording (or the
      // count-in) would go on unheard.
      if (countingIn || (graph && graph.ctx === ctx && !audio.paused)) refused(t('The browser stopped the sound: press Play to go on.'));
    });
  }
  // The levels the mix plays at, on the graph's gains (set again when the
  // graph comes after the mix).
  let mainLevel = 100;
  function setGains() {
    if (!graph) return;
    graph.mainGain.gain.value = mainLevel / 100;
    for (const s of Object.keys(followers)) graph.gains[s].gain.value = playedLevel(s) / 100;
  }

  function ensureGraph() {
    if (graph) return graph;
    if (!canRoute()) return null;
    const ctx = clicker ? clicker._ensureCtx() : new (window.AudioContext || window.webkitAudioContext)();
    watchCtx(ctx);
    const bus = ctx.createGain();
    const mainGain = ctx.createGain();
    ctx.createMediaElementSource(audio).connect(mainGain).connect(bus);
    // The parts that play beside it ("My parts"), each at its level.
    const gains = {};
    for (const [stem, el] of Object.entries(followers)) {
      gains[stem] = ctx.createGain();
      ctx.createMediaElementSource(el).connect(gains[stem]).connect(bus);
    }
    bus.connect(ctx.destination);
    graph = { ctx, bus, mainGain, gains, voiceGain: gains.vocals, stretch: null };
    setGains();
    return graph;
  }
  let stretchLib = null;
  function loadStretch() {
    stretchLib = stretchLib || new Promise((resolve, reject) => {
      const el = document.createElement('script');
      el.src = '/static/js/vendor/signalsmith-stretch/SignalsmithStretch.js';
      el.onload = () => resolve(window.SignalsmithStretch);
      el.onerror = () => { stretchLib = null; reject(new Error('could not load the pitch shifter')); };
      document.head.append(el);
    });
    return stretchLib;
  }
  // How late the sound comes out of the pitch shifter: the click and the
  // count-in wait for it.
  const outputDelay = () => (graph && graph.stretch && graph.routed ? graph.stretch.latency() : 0);

  // Key: the chords on screen follow (song.js's view transpose, clicked
  // as many times); the page's own −/+ doesn't move the recording (it's
  // also a capo's arithmetic).
  const keyBtn = $('audio-key-btn');
  const keyLabel = $('audio-key-label');
  const keyAmount = $('audio-key-amount');
  const keyName = $('audio-key-name');
  async function applyKey() {
    const n = prefs.semitones;
    document.dispatchEvent(new CustomEvent('songbooks:key', { detail: { semitones: n } }));
    const sign = n > 0 ? '+' : '';
    renderSing();
    renderKeyChip();
    renderDockState();
    if (keyAmount) keyAmount.textContent = `${sign}${n}`;
    if (keyName) keyName.textContent = $('key-transposed') && !$('key-transposed').hidden ? t('in %s', $('key-transposed').textContent) : '';
    if (!n && !graph) return;
    const g = ensureGraph();
    // Playing, with no click to route it in: on the next one, or at pause.
    if (!g) { routeLater.key = true; return; }
    if (n && !g.stretch) {
      try {
        const make = await loadStretch();
        g.stretch = await make(g.ctx);
        g.stretch.connect(g.ctx.destination);
        g.stretch.start();
      } catch (e) {
        if (keyName) keyName.textContent = t('Couldn’t change the key in this browser.');
        return;
      }
    }
    const route = !!(n && g.stretch);
    if (route !== !!g.routed) {
      g.bus.disconnect();
      g.bus.connect(route ? g.stretch : g.ctx.destination);
      g.routed = route;
      resetClicks();
    }
    if (g.stretch) g.stretch.schedule({ semitones: n, formantCompensation: true });
  }
  // A coach saves the key for a student or a group (play.html
  // #audio-key-coach, coach_keys.go): loaded the first time the Key panel
  // (or the "Voice & key" panel) opens, its buttons following the key
  // being tried.
  const coachBox = $('audio-key-coach'), coachList = $('audio-key-targets');
  let coachTargets = null, coachKey = root.dataset.songKey || '';
  // A coaching community's key per student (play.html, the key's panel;
  // audio-player.js "Sing"): sing, the panel for the key;
  // coachCommunity, its key per student —
  // that community's students, the coming week's lessons first, a name
  // picked, the key goes to theirs (singTarget).
  const sing = root.dataset.sing === '1';
  const coachCommunity = parseInt(root.dataset.coachCommunity || '0', 10);
  let singTarget = null, singSaved = null; // singSaved: whose key was just saved here
  // The song's key moved by n, named as the server does (chordsym.KeyName):
  // coachKey is the song's own key, from its key: header or its chords.
  const MAJOR = ['C', 'Db', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'Ab', 'A', 'Bb', 'B'];
  const MINOR = ['C', 'C#', 'D', 'Eb', 'E', 'F', 'F#', 'G', 'G#', 'A', 'Bb', 'B'];
  const PC = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };
  const keyIn = (key, n) => {
    const m = /^([A-G])([#b]?)(m(?!aj))?/.exec(key || '');
    if (!m) return '';
    const pc = (((PC[m[1]] + (m[2] === '#' ? 1 : m[2] === 'b' ? -1 : 0) + n) % 12) + 12) % 12;
    return m[3] ? MINOR[pc] + 'm' : MAJOR[pc];
  };
  const keyNow = () => keyIn(coachKey, prefs.semitones);
  const fmtKey = (name, n) => {
    const off = n ? `${n > 0 ? '+' : ''}${n}` : t('original');
    return name ? `${name} (${off})` : off;
  };
  // One student or group: who, the key they have, a save in the key being
  // tried. With the key per student (coachCommunity): their next lesson's
  // date, and the name is a choice (pickTarget).
  let othersOpen = false;
  function coachRow(x, i) {
    const n = prefs.semitones;
    const many = new Set(coachTargets.map((y) => y.communityId)).size > 1;
    const li = document.createElement('li');
    const picked = !!coachCommunity && singTarget === x;
    li.classList.toggle('picked', picked);
    const who = document.createElement(coachCommunity ? 'button' : 'span');
    who.className = 'who';
    if (coachCommunity) {
      who.type = 'button';
      who.setAttribute('aria-pressed', String(picked));
      who.title = t('Their key, to try and save');
      who.addEventListener('click', () => pickTarget(x));
    }
    who.textContent = x.name;
    if (x.kind === 'group' || many) {
      const tag = document.createElement('small');
      tag.textContent = [x.kind === 'group' ? t('group') : '', many ? x.communityName : ''].filter(Boolean).join(' · ');
      who.append(' ', tag);
    }
    const now = document.createElement('span');
    now.className = 'now';
    now.textContent = x.inSongbook ? fmtKey(x.keyName, x.semitones) : t('not in their songs yet');
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'audio-pill-btn';
    const same = x.inSongbook && x.semitones === n;
    btn.disabled = same;
    btn.textContent = same ? '✓' : (x.inSongbook ? t('Save %s', keyNow() || fmtKey('', n)) : t('Add in %s', keyNow() || fmtKey('', n)));
    btn.addEventListener('click', () => saveKeyFor(i, btn));
    if (coachCommunity) {
      const when = document.createElement('span');
      when.className = 'when';
      when.textContent = x.nextSessionLabel || '';
      li.append(who, when, now, btn);
    } else {
      li.append(who, now, btn);
    }
    return li;
  }
  async function saveKeyFor(i, btn) {
    const x = coachTargets[i];
    if (btn) btn.disabled = true;
    try {
      const res = await fetch(`/api/songs/${variantId}/coach-keys`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ communityId: x.communityId, personId: x.personId || 0, groupId: x.groupId || 0, semitones: prefs.semitones }),
      });
      if (!res.ok) throw new Error(await res.text());
      const d = await res.json();
      coachTargets[i] = { ...x, inSongbook: true, songbookId: d.songbookId, semitones: d.semitones, keyName: d.keyName };
      if (singTarget === x) singTarget = singSaved = coachTargets[i];
    } catch (e) {
      alert(t('Couldn’t save the key.'));
    }
    renderCoachKeys();
  }
  // The list, in the server's order. With the key per student: the coming
  // week's lessons first (coach_keys.go orderByCalendar), the soonest
  // first; everyone else folded under "Other students" (open when nobody
  // has a lesson soon).
  function renderCoachKeys() {
    renderSing();
    if (!coachBox || !coachTargets || !coachTargets.length) return;
    coachBox.hidden = false;
    if (!coachCommunity) {
      const rows = coachTargets.map((x, i) => coachRow(x, i));
      if (coachList.tagName === 'UL') {
        coachList.replaceChildren(...rows);
      } else {
        const ul = document.createElement('ul');
        ul.append(...rows);
        coachList.replaceChildren(ul);
      }
    } else {
      const soon = [], later = [];
      coachTargets.forEach((x, i) => (x.nextSession ? soon : later).push(coachRow(x, i)));
      const list = (rows) => { const ul = document.createElement('ul'); ul.append(...rows); return ul; };
      const parts = [];
      if (soon.length) {
        const h = document.createElement('p');
        h.className = 'audio-key-group';
        h.textContent = t('Lessons in the coming week');
        parts.push(h, list(soon));
      }
      if (later.length && soon.length) {
        const d = document.createElement('details');
        d.className = 'audio-key-others';
        d.open = othersOpen || later.some((li) => li.classList.contains('picked'));
        d.addEventListener('toggle', () => { othersOpen = d.open; });
        const s = document.createElement('summary');
        s.textContent = t('Other students (%d)', later.length);
        d.append(s, list(later));
        parts.push(d);
      } else if (later.length) {
        parts.push(list(later));
      }
      coachList.replaceChildren(...parts);
    }
  }
  let coachLoading = null;
  function loadCoachKeys() {
    if (!coachBox) return Promise.resolve();
    coachLoading = coachLoading || (async () => {
      coachTargets = [];
      try {
        const res = await fetch(`/api/songs/${variantId}/coach-keys${coachCommunity ? `?community=${coachCommunity}` : ''}`);
        if (res.ok) {
          const d = await res.json();
          coachKey = d.key || coachKey;
          // The coming week's lessons first, in the server's order (only
          // with the key per student); then who already has the song.
          // Kept as it is while keys are saved, so rows don't jump.
          const all = d.targets || [], later = all.filter((x) => !x.nextSession);
          coachTargets = [...all.filter((x) => x.nextSession), ...later.filter((x) => x.inSongbook), ...later.filter((x) => !x.inSongbook)];
        }
      } catch (e) { /* nobody to coach */ }
      // Opened from a student's (or a group's) songbook, with the key per
      // student: the key is theirs, saved for them.
      const sb = parseInt(root.dataset.keySongbook || '0', 10);
      if (coachCommunity && sb && !root.dataset.keyMine) singTarget = coachTargets.find((x) => x.songbookId === sb) || null;
      renderCoachKeys();
    })();
    return coachLoading;
  }
  $('audio-key-btn')?.addEventListener('click', loadCoachKeys);

  // Anyone keeps the key in My songbook, or in a songbook they can change
  // that has the song (play.html #audio-key-books, songbook_keys.go): the
  // song then opens in it from there, the same song in another key.
  const booksBox = $('audio-key-books'), booksList = $('audio-key-book-list');
  let bookKeys = null;
  function renderBookKeys() {
    if (!booksBox || !bookKeys || !bookKeys.length) return;
    booksBox.hidden = false;
    const n = prefs.semitones;
    booksList.replaceChildren(...bookKeys.map((x, i) => {
      const li = document.createElement('li');
      const who = document.createElement('span');
      who.className = 'who';
      who.textContent = x.name;
      const now = document.createElement('span');
      now.className = 'now';
      now.textContent = x.inSongbook ? fmtKey(x.keyName, x.semitones) : t('not in it yet');
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'audio-pill-btn';
      const same = x.inSongbook && x.semitones === n;
      btn.disabled = same;
      btn.textContent = same ? '✓' : (x.inSongbook ? t('Save %s', keyNow() || fmtKey('', n)) : t('Add in %s', keyNow() || fmtKey('', n)));
      btn.setAttribute('aria-label', same ? t('%s: in this key', x.name) : t('Keep %s in %s', keyNow() || fmtKey('', n), x.name));
      btn.addEventListener('click', async () => {
        btn.disabled = true;
        try {
          const res = await fetch(`/api/songbooks/${x.songbookId}/entries/${variantId}/key`, {
            method: 'PUT', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ semitones: prefs.semitones }),
          });
          if (!res.ok) throw new Error(await res.text());
          const d = await res.json();
          bookKeys[i] = { ...x, inSongbook: true, semitones: d.semitones, keyName: d.keyName };
        } catch (e) {
          alert(t('Couldn’t save the key.'));
        }
        renderBookKeys();
      });
      li.append(who, now, btn);
      return li;
    }));
  }
  async function loadBookKeys() {
    if (bookKeys || !booksBox) return;
    bookKeys = [];
    try {
      const res = await fetch(`/api/songs/${variantId}/songbook-keys`);
      if (res.ok) {
        const d = await res.json();
        if (!coachKey) coachKey = d.key || '';
        bookKeys = d.songbooks || [];
      }
    } catch (e) { /* nowhere to keep it */ }
    renderBookKeys();
  }
  $('audio-key-btn')?.addEventListener('click', loadBookKeys);

  function setSemitones(n) {
    n = Math.max(-7, Math.min(7, Math.round(n)));
    const delta = n - prefs.semitones;
    prefs.semitones = n;
    if (delta) singSaved = null;
    savePrefs();
    shiftChords(delta);
    applyKey();
    setTimeout(() => { renderCoachKeys(); renderBookKeys(); }, 0);
  }
  // The Mixer plays in the same key, and sets it here (mixer.js "Key"):
  // one key per song, whichever view it was set from.
  window.songbooksPlayerKey = { get: () => prefs.semitones, set: (n) => setSemitones(n) };
  // The recording's clock and parts, for pitch-check.js: where it is in
  // seconds, whether it plays, which recording and its parts; and a seek.
  window.songbooksReferenceClock = () => {
    const c = categories[activeCategory] || {};
    return { time: audio.currentTime, playing: !audio.paused, duration, variantId, category: activeCategory, meta: c, stems: c.stems || [] };
  };
  // The page keeps room for the dock, as tall as it is (the waveform,
  // the pitch row when open): --dock-h on the body (app.css).
  if (dock && window.ResizeObserver) {
    new ResizeObserver(() => document.body.style.setProperty('--dock-h', dock.offsetHeight + 'px')).observe(dock);
  }
  window.songbooksReferenceSeek = (t) => { if (duration) audio.currentTime = Math.max(0, Math.min(duration - 0.1, t)); };
  function shiftChords(delta) {
    if (window.songbooksShiftChords) { window.songbooksShiftChords(delta); return; }
    const btn = $(delta > 0 ? 'transpose-up' : 'transpose-down');
    for (let i = 0; btn && i < Math.abs(delta); i++) btn.click();
  }
  $('audio-key-up')?.addEventListener('click', () => setSemitones(prefs.semitones + 1));
  $('audio-key-down')?.addEventListener('click', () => setSemitones(prefs.semitones - 1));
  $('audio-key-reset')?.addEventListener('click', () => setSemitones(0));

  // ---- My parts ("Je joue") ---------------------------------------------------
  // A level for each part the reader plays here (play.html data-parts,
  // httpapi/player_parts.go: this song's choice, this band's or
  // community's, their profile's, else the voice), from the recording's
  // parts as Demucs splits them (cmd/stemsd): vocals, drums, bass, other.
  // An instrument is heard in one of these (internal/instruments): voice
  // in vocals, bass in bass, drums in drums, guitar, piano, horns… in
  // "other"; two in the same part (guitar and horns) share one level, and
  // say so. 0 takes the part out, 100 leaves the recording as it is.
  //
  // How it plays: the recording as it is when every level is 100; with
  // only the voice down, the backing (no_vocals) and the voice beside it
  // at its level; with any other part down, the four parts side by side,
  // each at its level. The parts keep with the recording that drives the
  // clock (followParts): same play, pause, speed and place.
  const STEMS = ['vocals', 'drums', 'bass', 'other'];
  const partsData = (() => { try { return JSON.parse(root.dataset.parts || '{}'); } catch (e) { return {}; } })();
  let myParts = Array.isArray(partsData.parts) ? partsData.parts : ['voice'];
  let partsFrom = partsData.from || 'default';
  const partsCtx = partsData.context || { kind: '' };
  const allInstruments = partsData.all || [];
  const instrument = (id) => allInstruments.find((x) => x.id === id) || { id, label: id, stem: id === 'voice' ? 'vocals' : 'other' };
  prefs.levels = Object.assign({ drums: 100, bass: 100, other: 100 }, prefs.levels || {});
  prefs.levelsOff = Object.assign({ drums: 0, bass: 0, other: 0 }, prefs.levelsOff || {});
  const stemsHere = () => (categories[activeCategory] || {}).stems || [];
  // The voice alone (the backing and the voice), or all four parts.
  const hasParts = () => stemsHere().includes('no_vocals') && stemsHere().includes('vocals');
  const hasFour = () => ['vocals', 'drums', 'bass', 'other'].every((s) => stemsHere().includes(s));
  // My parts by the recording's part they're heard in, in Demucs's order.
  function partGroups() {
    const by = new Map();
    for (const id of myParts) {
      const stem = instrument(id).stem;
      if (!by.has(stem)) by.set(stem, []);
      by.get(stem).push(id);
    }
    return STEMS.filter((s) => by.has(s)).map((stem) => ({
      stem, ids: by.get(stem),
      adjustable: stem === 'vocals' ? hasParts() : hasFour(),
    }));
  }
  const groupLabel = (g) => (g.ids.length === 1 && g.ids[0] === 'voice' ? t('Voice') : g.ids.map((id) => instrument(id).label).join(' + '));
  const levelOf = (stem) => (stem === 'vocals' ? prefs.voice : prefs.levels[stem]);
  // A part's level as played: the voice's, whenever the recording has it
  // apart (the karaoke switch is everyone's, singing here or not); another
  // part's, when I play it here; else 100.
  const playedLevel = (stem) => {
    if (stem === 'vocals') return hasParts() ? prefs.voice : 100;
    return partGroups().some((g) => g.stem === stem && g.adjustable) ? levelOf(stem) : 100;
  };
  const voiceOn = () => hasParts();
  const Mix = window.SongbooksMix;
  const engineMode = () => Mix.mode(stemsHere(), playedLevel);
  const followers = { vocals: voiceAudio };
  for (const s of ['drums', 'bass']) {
    const el = new Audio();
    el.preload = 'auto';
    el.preservesPitch = true;
    el.webkitPreservesPitch = true;
    followers[s] = el;
  }
  // Where each follower was last sent (followParts), and whether it is
  // getting there.
  for (const el of Object.values(followers)) {
    el.addEventListener('seeking', () => { el.dataset.seeking = '1'; });
    el.addEventListener('seeked', () => { delete el.dataset.seeking; });
  }
  let playing = []; // the followers playing now
  let partsToken = 0;
  // Loading a part: the voice's control says so (a spinner), and a part
  // that can't be loaded leaves the recording as it is (partsFailed).
  let partsLoading = false, partsFailed = false;
  async function applyParts() {
    renderParts();
    renderSing();
    renderDockState();
    const meta = categories[activeCategory] || {};
    const cat = activeCategory, token = ++partsToken;
    let mode = engineMode();
    // Everything the new mix needs, fetched before anything changes: the
    // recording (and its voice) keeps playing as it is until the parts
    // are there, never a gap with no voice at all; and if a part can't be
    // had, the recording stays as it is.
    const tag = mode.main ? `${cat}:${mode.main}` : cat;
    const swap = !!(audio.dataset.blobFor && audio.dataset.blobFor !== tag);
    const need = mode.followers.filter((s) => followers[s].dataset.blobFor !== `${cat}:${s}`);
    let mainBytes = null, partBytes = {};
    // (Set each time: a call overtaken while it loaded leaves it to this one.)
    partsLoading = !!(swap || need.length);
    if (!partsLoading) partsFailed = false;
    if (partsLoading) {
      renderVoice();
      try {
        const got = await Promise.all([
          swap ? window.songbooksAudioBytes(variantId, cat, meta, mode.main) : null,
          ...need.map((s) => window.songbooksAudioBytes(variantId, cat, meta, s)),
        ]);
        mainBytes = got[0];
        need.forEach((s, i) => { partBytes[s] = got[i + 1]; });
        partsFailed = false;
      } catch (e) {
        if (token !== partsToken) return;
        // The parts can't be had (offline, a server error): the recording
        // as it is, and the voice's control says so.
        partsFailed = true;
        partsLoading = false;
        mode = { main: '', mainLevel: 100, followers: [] };
        if (audio.dataset.blobFor && audio.dataset.blobFor !== cat) {
          try { mainBytes = await window.songbooksAudioBytes(variantId, cat, meta, ''); } catch (e2) { return; }
          if (token !== partsToken) return;
          swapSource(mainBytes, cat);
        }
        for (const s of Object.keys(followers)) followers[s].pause();
        playing = [];
        mainLevel = 100;
        setGains();
        root.dataset.mix = 'recording';
        renderVoice();
        return;
      }
      if (token !== partsToken) return;
      partsLoading = false;
    }
    // Web Audio only for levels between 0 and 100: the accompaniment alone
    // (the voice off) or parts at full level play as plain <audio>, which
    // no suspended context can silence. Routing waits for a click, or a
    // pause, when the recording plays and the context doesn't run yet: the
    // mix stays as it is until then.
    const needsGraph = mode.mainLevel < 100 || mode.followers.some((s) => playedLevel(s) < 100);
    if (needsGraph && !ensureGraph()) {
      routeLater.parts = true;
      renderVoice();
      return;
    }
    mainLevel = mode.mainLevel;
    setGains();
    if (mainBytes) swapSource(mainBytes, tag);
    for (const s of Object.keys(followers)) if (!mode.followers.includes(s)) followers[s].pause();
    for (const s of need) {
      const el = followers[s];
      if (el.src.startsWith('blob:')) URL.revokeObjectURL(el.src);
      el.src = URL.createObjectURL(new Blob([partBytes[s]], { type: 'audio/mpeg' }));
      el.dataset.blobFor = `${cat}:${s}`;
      delete el.dataset.sentAt;
    }
    playing = mode.followers.slice();
    // Started from here, in the same turn as the slider or the switch (a
    // user gesture: Safari lets a part start then).
    followParts();
    renderVoice();
    // What plays, for whoever looks (tests, a bug report): "no_vocals+vocals".
    root.dataset.mix = [mode.main || 'recording', ...playing].join('+');
  }
  const applyVoice = applyParts;
  // A new source for the recording's element, at the same place, playing
  // if it was. Two swaps in a row (a slider dragged across 100 and back):
  // the second keeps the first one's place and state, not the element's
  // in between (paused, at 0, while it loads).
  let swapping = null;
  function swapSource(bytes, tag) {
    const state = swapping || { t: audio.currentTime, wasPlaying: !audio.paused };
    swapping = state;
    if (audio.src.startsWith('blob:')) URL.revokeObjectURL(audio.src);
    audio.src = URL.createObjectURL(new Blob([bytes], { type: 'audio/mpeg' }));
    audio.dataset.blobFor = tag;
    audio.playbackRate = prefs.speed;
    const src = audio.src;
    audio.addEventListener('loadedmetadata', () => {
      if (audio.src !== src) return; // another swap since: its own listener
      swapping = null;
      audio.currentTime = state.t;
      if (state.wasPlaying) playOrSay(audio);
      followParts();
    }, { once: true });
    audio.addEventListener('error', () => { if (audio.src === src) swapping = null; }, { once: true });
  }
  // The parts keep with the part that drives the clock: same play, pause,
  // speed and place (Mix.shouldSeek: within 80 ms, and not again while
  // one is still getting to the last place it was sent).
  function followParts() {
    const now = performance.now();
    for (const s of playing) {
      const el = followers[s];
      if (!el.src) continue;
      if (el.playbackRate !== audio.playbackRate) el.playbackRate = audio.playbackRate;
      if (audio.paused || swapping) { if (!el.paused) el.pause(); continue; }
      if (Mix.shouldSeek({ time: el.currentTime, seeking: !!el.dataset.seeking, sentAt: +el.dataset.sentAt || 0 }, audio.currentTime, now)) {
        el.currentTime = audio.currentTime;
        el.dataset.sentAt = String(now);
      }
      if (el.paused) {
        el.dataset.sentAt = String(now);
        playOrSay(el, () => { delete el.dataset.sentAt; });
      }
    }
  }
  const followVoice = followParts;
  function setLevel(stem, v) {
    const n = Math.max(0, Math.min(100, Math.round(v)));
    if (stem === 'vocals') {
      prefs.voice = n;
    } else {
      prefs.levels[stem] = n;
      if (n < 100) prefs.levelsOff[stem] = n;
    }
    savePrefs();
    applyParts();
    if (stem === 'vocals') document.dispatchEvent(new CustomEvent('songbooks:voice-changed'));
  }
  // A tap on a part: out (or down to where it was last), and back.
  function toggleLevel(stem) {
    if (stem === 'vocals') { toggleVoice(); return; }
    const off = prefs.levelsOff[stem];
    setLevel(stem, levelOf(stem) < 100 ? 100 : off);
  }
  // The voice: on or off, its level kept for when it comes back
  // (Mix.toggleVoice); the slider sets the level, and once let go, the
  // level the switch comes back to.
  function setVoice(v, committed) {
    const next = Mix.setVoice({ voice: prefs.voice, back: prefs.voiceBack }, v, committed);
    prefs.voiceBack = next.back;
    setLevel('vocals', next.voice);
  }
  function toggleVoice() {
    if (!hasParts()) return false;
    const next = Mix.toggleVoice({ voice: prefs.voice, back: prefs.voiceBack });
    prefs.voiceBack = next.back;
    setLevel('vocals', next.voice);
  }
  const cardMicEl = $('audio-card-mic');
  cardMicEl?.addEventListener('click', () => toggleVoice());

  // The chips (in the bar) and the levels (in the "Je joue" panel): one
  // per part I play that the recording has.
  const partsEl = $('audio-parts'), partsChips = $('audio-parts-chips'), partsLevels = $('audio-parts-levels');
  const partIcon = (g) => {
    const key = g.stem === 'other' ? (['guitar', 'keys'].find((k) => g.ids.includes(k)) || 'other') : g.stem;
    return $('audio-part-icons')?.querySelector(`[data-stem="${key}"]`)?.innerHTML || '';
  };
  // The part's names as French says them, for the hints: « autres »…
  const stemName = (s) => ({ vocals: t('stem|vocals'), drums: t('stem|drums'), bass: t('stem|bass'), other: t('stem|other') }[s]);
  function partControl(g, big) {
    const wrap = document.createElement('span');
    wrap.className = 'audio-part' + (big ? ' audio-part-big' : '');
    wrap.dataset.stem = g.stem;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'audio-toggle audio-chip';
    btn.innerHTML = partIcon(g);
    const label = document.createElement('span');
    label.className = 'audio-part-label';
    btn.append(label);
    btn.addEventListener('click', () => toggleLevel(g.stem));
    const range = document.createElement('input');
    range.type = 'range';
    range.min = '0'; range.max = '100'; range.step = '5';
    range.addEventListener('input', () => setLevel(g.stem, +range.value));
    // Held, the slider stays where it is, whatever the level (at 100 the
    // bar's would hide under the pointer: Safari doesn't focus it).
    range.addEventListener('pointerdown', () => {
      wrap.classList.add('is-held');
      window.addEventListener('pointerup', () => wrap.classList.remove('is-held'), { once: true });
    });
    const out = document.createElement('output');
    wrap.append(btn, range, out);
    return wrap;
  }
  let partsShape = '';
  function renderParts() {
    // The voice has its own switch and line (renderVoice): here, the
    // other parts I play.
    const groups = partGroups().filter((g) => g.adjustable && g.stem !== 'vocals');
    if (partsEl) partsEl.hidden = !groups.length;
    const shape = groups.map((g) => `${g.stem}:${g.ids.join('+')}`).join(',');
    if (shape !== partsShape) {
      partsShape = shape;
      partsChips?.replaceChildren(...groups.map((g) => partControl(g, false)));
      partsLevels?.replaceChildren(...groups.map((g) => partControl(g, true)));
    }
    document.querySelectorAll('.audio-part').forEach((wrap) => {
      const g = groups.find((x) => x.stem === wrap.dataset.stem);
      if (!g) return;
      const v = levelOf(g.stem), name = groupLabel(g);
      const down = v < 100;
      const btn = wrap.querySelector('button'), range = wrap.querySelector('input'), out = wrap.querySelector('output');
      wrap.classList.toggle('is-on', down);
      btn.classList.toggle('active', down);
      btn.classList.toggle('is-off', v === 0);
      btn.setAttribute('aria-pressed', String(down));
      btn.title = t('%s: out or back (the slider: anywhere between)', name);
      // The name stays as it is (out: the icon struck through, "0 %"), so
      // the slider beside it doesn't move under the pointer.
      wrap.querySelector('.audio-part-label').textContent = name;
      if (+range.value !== v) range.value = String(v);
      range.setAttribute('aria-label', t('Level: %s', name));
      range.setAttribute('aria-valuetext', pctText(v));
      out.textContent = pctText(v);
    });
    renderVoice();
    renderPartsPanel();
    renderStems();
  }

  // ---- The voice: the karaoke switch ------------------------------------------
  // The recording's lead voice, on or off, for anyone, whenever the
  // recording has it apart (cmd/stemsd): the bar's mic (#audio-voice-btn,
  // a switch: on, the voice is heard), the phone card's, V; the panel's
  // Voice line has the same switch and a slider for anything between.
  // Off keeps the level for when it comes back (player-mix.js), with the
  // song's other settings (this browser, per song).
  const voiceBtn = $('audio-voice-btn'), voiceLabel = $('audio-voice-label');
  const voiceSwitch = $('audio-voice-switch'), voiceRange = $('audio-voice-range'), voiceOut = $('audio-voice-out');
  const voiceTool = $('audio-voice');
  function renderVoice() {
    const meta = categories[activeCategory] || {};
    const pending = meta.stemStatus === 'queued' || meta.stemStatus === 'running';
    const failed = meta.stemStatus === 'failed';
    const there = hasParts();
    const v = there ? prefs.voice : 100;
    const heard = v > 0;
    const state = !there ? (pending ? t('Voice · splitting…') : failed ? t('Voice unavailable') : '')
      : partsFailed ? t('Voice unavailable')
      : v === 0 ? t('No voice') : v < 100 ? t('Voice %d%', v) : t('Voice');
    const name = heard ? t('Original voice: on') : t('Original voice: off');
    for (const b of [voiceBtn, voiceSwitch, cardMicEl]) {
      if (!b) continue;
      b.disabled = !there;
      b.setAttribute('aria-checked', String(heard));
      b.classList.toggle('is-off', !heard);
      b.classList.toggle('active', there && v < 100);
      b.classList.toggle('is-loading', partsLoading);
      b.classList.toggle('is-pending', !there && pending);
      b.classList.toggle('is-failed', (!there && failed) || partsFailed);
      b.title = there ? `${name} · ${t('V: the voice on or off, to sing it yourself')}` : state;
    }
    if (voiceBtn) voiceBtn.setAttribute('aria-label', there ? t('Original voice') : state);
    if (cardMicEl) { cardMicEl.hidden = !there; cardMicEl.setAttribute('aria-label', t('Original voice')); }
    if (voiceTool) voiceTool.hidden = !there && !pending && !failed;
    if (voiceLabel) voiceLabel.textContent = state;
    if (voiceRange) {
      voiceRange.disabled = !there;
      if (+voiceRange.value !== v && !voiceRange.matches(':active')) voiceRange.value = String(v);
      voiceRange.setAttribute('aria-valuetext', pctText(v));
    }
    if (voiceOut) voiceOut.textContent = pctText(v);
    const vs = $('audio-voice-state');
    if (vs) {
      vs.textContent = partsFailed ? t('The voice couldn’t be loaded: the recording plays as it is.')
        : partsLoading ? t('Loading the voice…')
        : !there ? (pending ? t('The recording is being split into its parts; the voice comes in a few minutes, without a reload.') : failed ? t('The recording couldn’t be split into its parts.') : t('This recording isn’t split into its parts.'))
        : heard ? t('Off: the recording without its lead voice, to sing it yourself.') : t('Off: sing it yourself. On again: the voice comes back at %s.', pctText(prefs.voiceBack || 100));
    }
  }
  voiceBtn?.addEventListener('click', () => toggleVoice());
  voiceSwitch?.addEventListener('click', () => toggleVoice());
  voiceRange?.addEventListener('input', () => setVoice(+voiceRange.value, false));
  voiceRange?.addEventListener('change', () => setVoice(+voiceRange.value, true));

  // The "Je joue" panel: the levels, what the split can't tell apart, the
  // instruments to pick, for this song or for here, saved for the reader.
  const COMMON = ['voice', 'guitar', 'bass', 'drums', 'keys', 'sax', 'trumpet', 'trombone', 'violin', 'other'];
  let showAll = false;
  let partsScope = partsFrom === 'song' ? 'song' : 'context';
  let partsDirty = false;
  const ctxName = () => (partsCtx.kind ? partsCtx.name || '' : '');
  function renderPartsPanel() {
    if (!$('audio-line-parts')) return;
    const from = $('audio-parts-from');
    if (from) {
      from.textContent = partsDirty ? t('not saved yet')
        : partsFrom === 'song' ? t('your choice for this song')
        : partsFrom === 'context' ? (ctxName() ? t('your choice with %s', ctxName()) : t('your choice outside your bands and communities'))
        : partsFrom === 'profile' ? t('from your profile')
        : t('nothing said: the voice');
    }
    // What the split can't do: two instruments in one part, a recording
    // split into the voice and the backing only, no part at all.
    const notes = [];
    const apart = partGroups().filter((g) => !g.adjustable && g.stem !== 'vocals' && hasParts()).map(groupLabel);
    if (apart.length) notes.push(t('This recording is split into the voice and the backing only: %s can’t be set apart.', apart.join(', ')));
    for (const g of partGroups()) {
      if (g.adjustable && g.ids.length > 1) notes.push(t('%s are one part of the recording, “%s”: the split can’t tell them apart.', groupLabel(g), stemName(g.stem)));
    }
    if (!myParts.length) notes.push(t('Nothing: you listen, the recording plays as it is.'));
    const note = $('audio-parts-note');
    if (note) { note.hidden = !notes.length; note.textContent = notes.join(' '); }
    const pick = $('audio-parts-pick');
    if (pick) {
      const ids = showAll ? allInstruments.map((x) => x.id) : [...new Set([...COMMON, ...myParts])].filter((id) => allInstruments.some((x) => x.id === id));
      const shape = ids.join(',');
      if (pick.dataset.shape !== shape) {
        pick.dataset.shape = shape;
        pick.replaceChildren(...ids.map((id) => {
          const b = document.createElement('button');
          b.type = 'button';
          b.className = 'audio-pick';
          b.dataset.id = id;
          b.textContent = instrument(id).label;
          b.addEventListener('click', () => {
            myParts = myParts.includes(id) ? myParts.filter((x) => x !== id) : [...myParts, id].slice(-6);
            partsDirty = true;
            applyParts();
          });
          return b;
        }));
      }
      pick.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', String(myParts.includes(b.dataset.id))));
    }
    const more = $('audio-parts-more');
    if (more) { more.textContent = showAll ? t('Fewer instruments') : t('Other instruments…'); more.setAttribute('aria-expanded', String(showAll)); }
    const scopeCtx = $('audio-parts-scope-context');
    if (scopeCtx) scopeCtx.textContent = ctxName() ? t('With %s, all its songs', ctxName()) : t('All my other songs');
    $('audio-parts-scope')?.querySelectorAll('button').forEach((b) => {
      b.classList.toggle('active', b.dataset.v === partsScope);
      b.setAttribute('aria-checked', String(b.dataset.v === partsScope));
    });
    const save = $('audio-parts-save');
    if (save) save.disabled = !partsDirty && partsFrom === partsScope;
    const reset = $('audio-parts-reset');
    if (reset) {
      reset.hidden = partsFrom !== partsScope;
      reset.textContent = partsScope === 'song' ? t('Forget it for this song') : t('Back to my profile');
    }
  }
  $('audio-parts-more')?.addEventListener('click', () => { showAll = !showAll; renderPartsPanel(); });
  $('audio-parts-scope')?.addEventListener('click', (e) => {
    const b = e.target.closest('button[data-v]');
    if (!b) return;
    partsScope = b.dataset.v;
    renderPartsPanel();
  });
  async function putParts(body) {
    const status = $('audio-parts-status');
    if (status) status.textContent = '';
    const res = await fetch(`/api/songs/${variantId}/parts`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(Object.assign({ context: { kind: partsCtx.kind || '', id: partsCtx.id || 0 }, scope: partsScope }, body)),
    }).catch(() => null);
    if (!res || !res.ok) { if (status) status.textContent = t('Couldn’t save that.'); return; }
    const d = await res.json();
    myParts = d.parts || [];
    partsFrom = d.from;
    partsDirty = false;
    if (status) status.textContent = t('Saved.');
    applyParts();
  }
  $('audio-parts-save')?.addEventListener('click', () => putParts({ instruments: myParts }));
  $('audio-parts-reset')?.addEventListener('click', () => putParts({ clear: true }));

  // ---- The bar: the main tools, the others in the panel --------------------
  // The bar keeps the transport, where you are, and two tools; every other
  // tool is in the panel (Practice, F), and shows on the bar only while it
  // is on (a click, a count-in, a part turned down...: what's on stays in
  // sight, and a tap turns it off). Which two (data-bar on the dock):
  //   "sing"  a coaching community (#sheet data-coaching): the original
  //           voice, on or off (karaoke), and the key, a semitone either
  //           way — what a singer and their coach change most;
  //   "play"  anywhere else: the loop and the speed — a band musician
  //           works a passage over and over, slower, then up to tempo
  //           (Soundslice, Moises and Capo keep these two on their bar).
  // Then, where they don't all fit (a narrow window, a zoom, French
  // labels), those that are off show their icons only, then leave the bar
  // from the right, one by one, into "+N" (#audio-opts-btn); a tool that is
  // on keeps its place and its label (PlayerShell.fit, player-shell.js).
  // On a phone the card has the voice (and in a coaching community the
  // key); everything else is in its sheet.
  const dockTransport = dock ? dock.querySelector('.audio-transport') : null;
  const moreCount = $('audio-more-count');
  const tools = () => (optsEl ? [...optsEl.querySelectorAll(':scope > [data-tool]')] : []);
  const coaching = !!$('sheet')?.dataset.coaching;
  const mainTools = coaching ? ['voice', 'key'] : ['loop', 'speed'];
  if (dock) dock.dataset.bar = coaching ? 'sing' : 'play';
  for (const name of [...mainTools].reverse()) {
    const el = optsEl?.querySelector(`:scope > [data-tool="${name}"]`);
    if (el) optsEl.prepend(el);
  }
  tools().forEach((el) => { el.dataset.bar = mainTools.includes(el.dataset.tool) ? 'main' : 'more'; });
  $('audio-key-bar-down')?.addEventListener('click', () => setSemitones(prefs.semitones - 1));
  $('audio-key-bar-up')?.addEventListener('click', () => setSemitones(prefs.semitones + 1));
  const queueFit = dock && window.PlayerShell ? window.PlayerShell.fit({
    bar: dock, row: dockTransport, tray: optsEl, count: moreCount,
    skip: () => phoneMQ.matches || (playerBody && playerBody.hidden) || sheetOpen(),
    onFit: (n) => {
      if (optsBtn && !phoneMQ.matches) {
        optsBtn.title = n ? tn(n, '%d more tool', '%d more tools') : t('practice options');
        optsBtn.setAttribute('aria-label', optsBtn.title);
      }
    },
  }) : () => {};
  phoneMQ.addEventListener?.('change', queueFit);

  // ---- Sing: a coaching community's key per student -------------------------
  // The panel's Key line (play.html, with the option on: handlers_pages.go),
  // opened by the key's chip: the key, a semitone either way, with whose it
  // is, and the coach's students (coachCommunity: picked, the key goes to
  // theirs, saved for them). The same key as the chip's (setSemitones).
  const singKeyName = $('audio-sing-keyname'), singFor = $('audio-sing-for');
  const singSave = $('audio-sing-save'), singReset = $('audio-sing-reset'), singOriginal = $('audio-sing-original');
  // −1, +2: a semitone count as a singer reads it (a real minus).
  const semis = (n) => (n ? `${n > 0 ? '+' : '−'}${Math.abs(n)}` : t('original'));
  // "Poppy": a first name, unless another student has it too.
  const shortName = (x) => {
    const first = (x.name || '').split(/\s+/)[0];
    if (x.kind === 'group' || !first) return x.name;
    return (coachTargets || []).some((y) => y !== x && y.kind !== 'group' && (y.name || '').split(/\s+/)[0] === first) ? x.name : first;
  };
  // The key's chip: "Tonalité −1 · C#m", and whose ("· Poppy", "· la vôtre").
  let keyShort = '';
  if (!sing && studentKey !== null) keyShort = root.dataset.keyMine ? t('key|yours') : (root.dataset.keyFor || '').split(/\s+/)[0];
  function renderKeyChip() {
    const keyLabel = $('audio-key-label');
    if (!keyLabel) return;
    const n = prefs.semitones;
    const name = keyIn(coachKey, n);
    const mine = studentKey !== null && n === studentKey;
    const who = sing || mine ? keyShort : '';
    // In a coaching community the bar reads the key even unchanged: "Key · D".
    const text = n ? [t('chip|Key %s', semis(n)), name, who] : who ? [name || t('Key'), who] : coaching && name ? [t('Key'), name] : [t('Key')];
    keyLabel.textContent = text.filter(Boolean).join(' · ');
    const cardKey = $('audio-card-key');
    if (cardKey) {
      cardKey.hidden = !coaching;
      $('audio-card-key-label').textContent = n ? semis(n) : (name || '');
      cardKey.setAttribute('aria-label', [t('Key'), name ? `${name} (${semis(n)})` : semis(n)].filter(Boolean).join(', '));
      cardKey.classList.toggle('active', !!n);
    }
    const down = $('audio-key-bar-down'), up = $('audio-key-bar-up');
    if (down) down.disabled = n <= -7;
    if (up) up.disabled = n >= 7;
    keyBtn?.classList.toggle('active', !!n || !!who);
    keyBtn?.setAttribute('aria-label', [t('Key'), name ? `${name} (${semis(n)})` : semis(n), who].filter(Boolean).join(', '));
  }
  function renderSing() {
    if (!sing) return;
    // The key: its name and how far from the song's, then whose.
    const n = prefs.semitones;
    const name = keyIn(coachKey, n);
    if (singKeyName) singKeyName.textContent = name ? `${name} (${semis(n)})` : semis(n);
    let who = '', short = '', save = '', reset = false;
    const picking = !!coachCommunity && !!(coachTargets && coachTargets.length);
    if (picking && singTarget) {
      who = t('for %s', shortName(singTarget));
      short = shortName(singTarget);
      if (!singTarget.inSongbook || singTarget.semitones !== n) save = t('Save for %s', shortName(singTarget));
    } else if (picking) {
      who = t('choose a student');
    } else if (root.dataset.keyMine && studentKey !== null) {
      // A student: their key, the coach's; another is only tried.
      who = n === studentKey ? t('key|yours') : t('key|trying');
      short = who;
      reset = n !== studentKey;
    } else if (root.dataset.keyFor && studentKey !== null && n === studentKey) {
      who = t('for %s', root.dataset.keyFor);
      short = root.dataset.keyFor.split(/\s+/)[0];
    }
    if (singFor) singFor.textContent = who ? ` · ${who}` : '';
    // Just saved: said so, until the key moves again.
    const saved = !save && singTarget && singSaved === singTarget;
    if (singSave) {
      singSave.hidden = !save && !saved;
      singSave.textContent = saved ? `✓ ${t('Saved for %s', shortName(singTarget))}` : save;
      singSave.disabled = !!saved;
    }
    if (singReset) singReset.hidden = !reset;
    if (singOriginal) singOriginal.disabled = n === 0;
    // The dock's way in: "C#m −1 · Poppy".
    const chip = [name ? (n ? `${name} ${semis(n)}` : name) : semis(n), short].filter(Boolean).join(' · ');
    keyShort = short;
    renderKeyChip();
    renderStrip();
  }
  // The student's range with the song's melody over it, in the key being
  // tried (voice-strip.js, coaching): the student the coach picked, or a
  // student's own; nothing for a group, or before a student is picked.
  function renderStrip() {
    const stripBox = $('audio-voice-strip');
    if (!stripBox || !window.VoiceStrip || !coachCommunity) return;
    let person = null;
    if (singTarget) person = singTarget.kind === 'student' ? singTarget.personId : null;
    else if (root.dataset.keyMine) person = 0;
    if (person === null) { VoiceStrip.hide(stripBox); return; }
    if (stripBox.dataset.stripFor !== `${variantId}/${coachCommunity}/${person}`) {
      VoiceStrip.show(stripBox, { variant: variantId, community: coachCommunity, person, coach: person !== 0, semitones: prefs.semitones });
    } else {
      VoiceStrip.key(stripBox, prefs.semitones);
    }
  }
  // A coach picks whose key it is: the key goes to theirs when they have
  // the song, else stays as it is, to give it them.
  function pickTarget(x) {
    singTarget = x;
    if (x.inSongbook && x.semitones !== prefs.semitones) setSemitones(x.semitones);
    else renderCoachKeys();
  }
  if (sing) {
    $('audio-sing-down')?.addEventListener('click', () => setSemitones(prefs.semitones - 1));
    $('audio-sing-up')?.addEventListener('click', () => setSemitones(prefs.semitones + 1));
    singOriginal?.addEventListener('click', () => setSemitones(0));
    singReset?.addEventListener('click', () => setSemitones(studentKey || 0));
    singSave?.addEventListener('click', () => {
      const i = coachTargets ? coachTargets.indexOf(singTarget) : -1;
      if (i >= 0) saveKeyFor(i, singSave);
    });
    // With the key per student, whose key it is shows in the dock from
    // the start (a student's songbook: theirs).
    if (coachCommunity) loadCoachKeys();
    renderSing();
    // The strip's script comes after this one (voice-strip.js).
    document.addEventListener('songbooks:voice-strip', renderStrip);
  }


  // ---- The panel ----------------------------------------------------------
  // The bar opens into the panel (play.html .audio-panel; app/reports/
  // player-proposal.md §3.2–3.3), with PlayerShell.panel's mechanics. On a
  // wide screen it rises from the bar over the lower two thirds of the
  // screen: on the left where you are, big — the bar.beat, the waveform,
  // the sections to loop, the transport; on the right the tools in three
  // groups, Loop & tempo, Sound, Sing, each tool's settings on its line.
  // On a phone the card opens into a sheet: the same, the groups as tabs
  // (the last one used comes back). The chart above scrolls so the line
  // being played stays in sight. Closed by Escape, ⌄, F, a click on the
  // dimmed page, or a swipe down.
  const panelBtn = $('audio-panel-btn');
  const tabKey = 'songbooks:player-tab';
  const shell = dock && window.PlayerShell ? window.PlayerShell.panel({
    root: dock, card: dock,
    openers: [panelBtn, optsBtn, lcdEl].filter(Boolean),
    closers: document.querySelectorAll('[data-dock-close]'),
    focus: () => (phoneMQ.matches ? $('audio-dock-grab') : dock.querySelector('.audio-panel-close')),
    swipeIgnore: 'input, select, .audio-dock-line, .audio-dock-sections, .audio-stepper, .audio-seg, .audio-panel-tabs, .audio-parts-pick, .audio-wave, .audio-ruler',
    onChange: (on) => {
      if (dockBackdrop) dockBackdrop.hidden = !on;
      if (on) {
        dock.setAttribute('aria-label', t('Player'));
        dock.scrollTop = 0;
        dock.querySelector('.audio-panel')?.scrollTo?.(0, 0);
        renderDockSections();
        renderLoopLine();
        loadCoachKeys();
        loadBookKeys();
        renderSing();
        renderTrainer();
        if (!phoneMQ.matches) keepPlayingLineInSight();
      }
      // The waveform takes its new size.
      requestAnimationFrame(() => { drawWaveform(); refreshTimeline(); queueFit(); });
    },
  }) : null;
  function sheetOpen() { return !!shell && shell.isOpen(); }
  function setSheet(on) { shell?.open(on); }
  // The chart under a wide screen's panel: the bar being played moved up
  // into the third of the screen the panel leaves.
  function keepPlayingLineInSight() {
    const el = document.querySelector('#sheet .bar.playing, #sheet .is-playing, #sheet .current-line, #sheet .lyric-line.current') || document.querySelector('#sheet .sheet-section.current, #sheet .sheet-section.playing');
    if (!el) return;
    const r = el.getBoundingClientRect();
    const room = dock.getBoundingClientRect().top;
    if (r.top < 60 || r.bottom > room - 10) window.scrollBy({ top: r.top - Math.max(60, room / 3), behavior: 'smooth' });
  }
  // The groups as tabs on a phone (all three side by side on a wide screen).
  const groups = [...document.querySelectorAll('.audio-group')];
  const tabs = [...document.querySelectorAll('.audio-panel-tabs [role="tab"]')];
  function showGroup(name, remember) {
    if (!groups.some((g) => g.dataset.group === name)) name = 'loop';
    // No recording: only the Sound group has anything (the click, its
    // tempo, the Drummer view and the Mixer).
    if (root.dataset.state === 'none') { name = 'sound'; remember = false; }
    groups.forEach((g) => g.classList.toggle('is-active', g.dataset.group === name));
    tabs.forEach((b) => {
      const on = b.dataset.group === name;
      b.setAttribute('aria-selected', String(on));
      b.tabIndex = on ? 0 : -1;
    });
    if (remember) { try { localStorage.setItem(tabKey, name); } catch (e) { /* this page only */ } }
  }
  let lastTab = coaching ? 'sound' : 'loop';
  try { lastTab = localStorage.getItem(tabKey) || lastTab; } catch (e) { /* the default */ }
  showGroup(lastTab, false);
  tabs.forEach((b) => b.addEventListener('click', () => showGroup(b.dataset.group, true)));
  document.querySelector('.audio-panel-tabs')?.addEventListener('keydown', (e) => {
    const i = tabs.findIndex((b) => b.getAttribute('aria-selected') === 'true');
    const d = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
    if (d === undefined || i < 0) return;
    e.preventDefault();
    const next = tabs[(i + d + tabs.length) % tabs.length];
    showGroup(next.dataset.group, true);
    next.focus();
  });
  // A tool's line: the panel opens there, its group shown, the line lit.
  function openLine(id) {
    const line = $(id);
    if (!line) { setSheet(true); return; }
    const group = line.closest('.audio-group');
    if (group) showGroup(group.dataset.group, true);
    if (id === 'audio-line-parts') { const d = $('audio-parts-choose'); if (d && !myParts.length) d.open = true; }
    setSheet(true);
    requestAnimationFrame(() => {
      line.scrollIntoView({ block: 'nearest' });
      line.classList.remove('is-lit');
      void line.offsetWidth;
      line.classList.add('is-lit');
    });
  }
  document.querySelectorAll('#audio-player [data-open-line]').forEach((b) => b.addEventListener('click', () => openLine(b.dataset.openLine)));
  panelBtn?.addEventListener('click', () => setSheet(!sheetOpen()));
  // The page around the player opens it at a tool's line too: the header's
  // key, the song bar's metronome (play.html), with data-open-line — or
  // window.songbooksPlayer.openLine.
  document.querySelectorAll('[data-player-line]').forEach((b) => b.addEventListener('click', () => openLine(b.dataset.playerLine)));
  // "+N": the panel, at the first tool that left the bar; on a phone, ⋯
  // opens the sheet.
  optsBtn?.addEventListener('click', () => {
    if (phoneMQ.matches) { setSheet(!sheetOpen()); return; }
    const first = tools().find((el) => el.dataset.folded && !el.hidden);
    if (first && first.dataset.line) openLine(first.dataset.line); else setSheet(true);
  });
  // The bar.beat display opens it too: the card on a phone, the readout on a wide screen.
  lcdEl?.addEventListener('click', () => { if (!sheetOpen()) setSheet(true); });
  lcdEl?.addEventListener('keydown', (e) => {
    if (!sheetOpen() && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); e.stopPropagation(); setSheet(true); }
  });
  if (lcdEl) {
    lcdEl.setAttribute('role', 'button');
    lcdEl.tabIndex = 0;
  }
  // The panel's own switches mirror the bar's chips.
  const loopSwitch = $('audio-loop-switch'), clickSwitch = $('audio-click-switch');
  loopSwitch?.addEventListener('change', () => { if (!!(prefs.cycle && prefs.cycle.on) !== loopSwitch.checked) toggleCycle(); });
  clickSwitch?.addEventListener('change', () => { if (clickOn() !== clickSwitch.checked) toggleClick(); });
  $('audio-loop-section')?.addEventListener('click', () => {
    const sec = sectionAt(Math.max(0, Math.floor(barAtTime(audio.currentTime) || 0)));
    if (sec) { setCycle(sec.startBar, sec.startBar + sec.barCount); seekToBar(sec.startBar); }
  });
  function renderLoopLine() {
    const c = prefs.cycle;
    const loopSwitch = $('audio-loop-switch'), clickSwitch = $('audio-click-switch');
    if (loopSwitch) { loopSwitch.checked = !!(c && c.on); loopSwitch.disabled = !hasBars(); }
    if (clickSwitch) clickSwitch.checked = clickOn();
    renderViewLines();
    const range = $('audio-loop-range');
    if (range) range.textContent = c && hasBars() ? t('bars %s → %s', barLabel(c.from), barLabel(c.to - 1)) : t('drag in the ruler, or tap a section');
    const lcdLoop = $('audio-lcd-loop');
    if (lcdLoop) lcdLoop.textContent = c && c.on && hasBars() ? t('loop %s → %s', `${barLabel(c.from)}.1`, `${barLabel(c.to)}.1`) : '';
    const stems = $('audio-line-stems');
    if (stems) {
      const names = { vocals: t('Voice'), drums: t('Drums'), bass: t('Bass'), other: t('Others') };
      const here = STEMS.filter((s) => stemsHere().includes(s));
      stems.textContent = here.length ? here.map((s) => names[s]).join(' · ') : t('not split into its parts');
    }
    const pitchBtn = $('audio-pitch-btn'), pitchPanel = $('audio-pitch-panel-btn');
    if (pitchPanel && pitchBtn) {
      pitchPanel.disabled = pitchBtn.hidden;
      pitchPanel.setAttribute('aria-pressed', pitchBtn.getAttribute('aria-pressed') || 'false');
      pitchPanel.classList.toggle('active', pitchBtn.getAttribute('aria-pressed') === 'true');
      const hint = $('audio-pitch-panel-hint');
      if (hint) hint.textContent = pitchBtn.hidden ? t('the recording’s voice isn’t split out yet') : t('headphones on; the microphone listens while it plays');
    }
  }
  $('audio-pitch-panel-btn')?.addEventListener('click', () => { $('audio-pitch-btn')?.click(); requestAnimationFrame(renderLoopLine); });
  if (window.MutationObserver && $('audio-pitch-btn')) new MutationObserver(renderLoopLine).observe($('audio-pitch-btn'), { attributes: true, attributeFilter: ['aria-pressed', 'hidden'] });
  // ---- Views from the panel ----------------------------------------------
  // The Drummer view and the Mixer are the song page's render modes
  // (song.js, #mode-segmented), but tools rather than ways of reading it
  // (menu-proposal.md §3.5): they open from the panel's Sound group, and
  // the same button takes the page back to the view it was on.
  const modeEl = document.getElementById('mode-segmented');
  const extraViews = ['drummer', 'mixer', 'reference'];
  let readingView = 'normal';
  const currentView = () => (modeEl && modeEl.dataset.value) || 'normal';
  const hasDrums = () => [...document.querySelectorAll('[data-realbars]')].some((e) => !e.dataset.restonly);
  function showView(v) { document.querySelector(`#mode-segmented button[data-value="${v}"]`)?.click(); }
  function renderViewLines() {
    const drummer = $('audio-line-drummer');
    if (drummer) drummer.hidden = !hasDrums();
    const mixer = $('audio-line-mixer');
    if (mixer) mixer.classList.toggle('has-score', root.dataset.score === '1');
    document.querySelectorAll('#audio-player .audio-view-btn').forEach((b) => {
      const on = currentView() === b.dataset.view;
      b.setAttribute('aria-pressed', String(on));
      b.textContent = on ? t('Back to the chart') : (b.dataset.view === 'mixer' ? t('Open the mixer') : t('Show the drums'));
    });
  }
  if (modeEl) {
    new MutationObserver(() => {
      if (!extraViews.includes(currentView())) readingView = currentView();
      renderViewLines();
    }).observe(modeEl, { attributes: true, attributeFilter: ['data-value'] });
  }
  document.querySelectorAll('#audio-player .audio-view-btn').forEach((b) => b.addEventListener('click', () => {
    setSheet(false);
    showView(currentView() === b.dataset.view ? readingView : b.dataset.view);
  }));
  renderViewLines();
  // The bar's Mixer button: the mixer, or (on) back to the view it left.
  {
    const mixerBtn = $('audio-mixer-btn');
    const syncMixerBtn = () => {
      mixerBtn?.setAttribute('aria-pressed', String(currentView() === 'mixer'));
      mixerBtn?.classList.toggle('active', currentView() === 'mixer');
    };
    mixerBtn?.addEventListener('click', () => {
      setSheet(false);
      showView(currentView() === 'mixer' ? readingView : 'mixer');
    });
    if (mixerBtn && modeEl) new MutationObserver(syncMixerBtn).observe(modeEl, { attributes: true, attributeFilter: ['data-value'] });
    syncMixerBtn();
  }
  // The dock's line: a click goes there; in the phone sheet, where it is
  // a scrubber, a drag too.
  if (dockLine) {
    let scrubbing = false;
    const scrubTo = (e) => {
      const r = dockLine.getBoundingClientRect();
      if (duration && r.width) seek(Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * duration);
    };
    dockLine.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      scrubbing = true;
      dockLine.setPointerCapture?.(e.pointerId);
      scrubTo(e);
    });
    dockLine.addEventListener('pointermove', (e) => { if (scrubbing) scrubTo(e); });
    const stop = () => { scrubbing = false; };
    dockLine.addEventListener('pointerup', stop);
    dockLine.addEventListener('pointercancel', stop);
  }
  // The sections, as chips: one tap loops that section from its start,
  // another stops the loop (as in the mixer on a phone).
  function renderDockSections() {
    if (!dockSections) return;
    const secs = hasBars() ? sections() : [];
    dockSections.hidden = secs.length < 2;
    // "Verse 1, Verse 2": a name that comes back is numbered.
    const seen = {}, count = {};
    secs.forEach((sec) => { count[sec.label] = (count[sec.label] || 0) + 1; });
    dockSections.replaceChildren(...secs.map((sec) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'audio-sec-chip';
      b.dataset.from = String(sec.startBar);
      b.dataset.to = String(sec.startBar + sec.barCount);
      seen[sec.label] = (seen[sec.label] || 0) + 1;
      b.textContent = count[sec.label] > 1 && !/\d$/.test(sec.label) ? `${sec.label} ${seen[sec.label]}` : sec.label;
      b.addEventListener('click', () => {
        const c = prefs.cycle;
        if (c && c.on && c.from === sec.startBar && c.to === sec.startBar + sec.barCount) {
          toggleCycle();
        } else {
          setCycle(sec.startBar, sec.startBar + sec.barCount);
          seekToBar(sec.startBar);
        }
      });
      return b;
    }));
    syncDockSections();
  }
  function syncDockSections() {
    if (!dockSections || dockSections.hidden || !sheetOpen()) return;
    const c = prefs.cycle;
    const pos = duration && hasBars() ? barAtTime(audio.currentTime) : null;
    const bar = pos === null ? -1 : Math.floor(pos);
    dockSections.querySelectorAll('button').forEach((b) => {
      const from = +b.dataset.from, to = +b.dataset.to;
      const looped = !!(c && c.on && c.from === from && c.to === to);
      b.classList.toggle('looped', looped);
      b.setAttribute('aria-pressed', String(looped));
      b.classList.toggle('current', bar >= from && bar < to);
    });
  }
  // What's on, under the bar in the card: Key +2 · No voice · 80% · Cycle.
  // What's on, in the phone card under the section: "⟲ 21–28 · 80 % · −1 · voix 40 %".
  function renderDockState() {
    if (!lcdState) return;
    const bits = [];
    const c = prefs.cycle;
    if (c && c.on && hasBars()) bits.push(`⟲ ${barLabel(c.from)}–${barLabel(c.to - 1)}`);
    const pct = Math.round(prefs.speed * 100);
    if (pct !== 100) bits.push(pctText(pct));
    if (prefs.semitones) bits.push(semis(prefs.semitones));
    if (hasParts() && prefs.voice < 100) bits.push(prefs.voice === 0 ? t('no voice') : t('voice %d%', prefs.voice));
    for (const g of partGroups()) {
      if (!g.adjustable || g.stem === 'vocals' || levelOf(g.stem) >= 100) continue;
      bits.push(`${groupLabel(g)} ${pctText(levelOf(g.stem))}`);
    }
    if (prefs.click) bits.push(t('Click'));
    if (prefs.countIn) bits.push($('audio-countin-label')?.textContent);
    lcdState.textContent = bits.filter(Boolean).join(' · ');
  }

  function wireSeg(id, get, set) {
    const seg = $(id);
    if (!seg) return () => {};
    const sync = () => seg.querySelectorAll('button').forEach((b) => b.classList.toggle('active', b.dataset.v === String(get())));
    seg.addEventListener('click', (e) => {
      const b = e.target.closest('button[data-v]');
      if (!b) return;
      set(b.dataset.v);
      savePrefs();
      sync();
      updateButtons();
    });
    sync();
    return sync;
  }
  const syncCountInSeg = wireSeg('audio-countin-seg', () => prefs.countIn, (v) => { prefs.countIn = Number(v); });
  countInBtn?.addEventListener('click', syncCountInSeg);
  const countInEach = $('audio-countin-each');
  if (countInEach) {
    countInEach.checked = !!prefs.countInEach;
    countInEach.addEventListener('change', () => { prefs.countInEach = countInEach.checked; savePrefs(); });
  }
  wireSeg('audio-click-sound-seg', () => prefs.clickSound, (v) => { prefs.clickSound = v; followClickPrefs(); testClick(); });
  const clickVolume = $('audio-click-volume');
  if (clickVolume) {
    clickVolume.value = prefs.clickVolume;
    clickVolume.addEventListener('input', () => { prefs.clickVolume = Number(clickVolume.value); followClickPrefs(); savePrefs(); });
    clickVolume.addEventListener('change', testClick);
  }
  function testClick() {
    if (!clicker) return;
    const ctx = clicker._ensureCtx();
    const beat = 60 / (bpm || 100);
    for (let i = 0; i < beatsPerBar; i++) clickAt(ctx.currentTime + 0.05 + i * beat, i, false);
  }
  $('audio-click-test')?.addEventListener('click', testClick);

  // ---- Speed trainer, as Guitar Pro has it: loop the cycle, starting at
  // one speed and going up by a step every few passes until the target.
  const TRAINER_LIMITS = { start: [25, 150], step: [1, 25], every: [1, 16], end: [25, 150] };
  function trainerSteps() {
    const tr = prefs.trainer;
    const out = [];
    for (let v = tr.start; v < tr.end && out.length < 40; v += tr.step) out.push(v);
    out.push(tr.end);
    return out;
  }
  function renderTrainer() {
    const pop = $('audio-line-trainer');
    if (!pop) return;
    const tr = prefs.trainer;
    pop.querySelectorAll('.audio-stepper').forEach((st) => {
      const k = st.dataset.key;
      st.querySelector('output').textContent = k === 'every' ? tn(tr.every, '%d loop', '%d loops') : k === 'step' ? `+${tr.step}%` : `${tr[k]}%`;
    });
    const on = $('audio-trainer-on');
    if (on) on.checked = !!tr.on;
    const steps = trainerSteps();
    const cur = Math.round(prefs.speed * 100);
    const stairs = $('audio-trainer-stairs');
    if (stairs) {
      stairs.innerHTML = '';
      const lo = Math.min(...steps), hi = Math.max(...steps);
      steps.forEach((v, i) => {
        const bar = document.createElement('div');
        const reached = tr.on && v <= cur;
        bar.className = 'audio-stair' + (reached ? ' reached' : '') + (tr.on && v === cur ? ' now' : '');
        bar.style.height = `${30 + (hi > lo ? ((v - lo) / (hi - lo)) * 70 : 70)}%`;
        bar.title = `${v}%`;
        bar.innerHTML = `<span>${v}</span>`;
        if (steps.length > 12 && i % 2 && i !== steps.length - 1) bar.querySelector('span').textContent = '';
        stairs.appendChild(bar);
      });
    }
    const summary = $('audio-trainer-summary');
    if (summary) {
      const loops = (steps.length - 1) * tr.every;
      const loopSec = cycleRange() ? (cycleRange().t1 - cycleRange().t0) : 0;
      let est = 0;
      steps.slice(0, -1).forEach((v) => { est += (tr.every * loopSec) / (v / 100); });
      const time = loopSec ? ' · ' + t('about %d min', Math.max(1, Math.round(est / 60))) : '';
      const where = prefs.cycle ? t('bars %s–%s', barLabel(prefs.cycle.from), barLabel(prefs.cycle.to - 1)) : t('the section being played');
      summary.textContent = tr.on && passes
        ? (cur < tr.end
          ? t('Now %d% · loop %d of %d at this speed, then %d%.', cur, (passes % tr.every) + 1, tr.every, Math.min(tr.end, cur + tr.step))
          : t('Now %d% · loop %d of %d at this speed — target reached.', cur, (passes % tr.every) + 1, tr.every))
        : t('%s, %s to reach %s%s, on %s.', tn(steps.length, '%d speed', '%d speeds'), tn(loops, '%d loop', '%d loops'), `${tr.end}%`, time, where);
    }
  }
  $('audio-line-trainer')?.addEventListener('click', (e) => {
    const b = e.target.closest('.audio-stepper button[data-d]');
    const preset = e.target.closest('[data-preset]');
    const tr = prefs.trainer;
    if (b) {
      const k = b.closest('.audio-stepper').dataset.key;
      const [lo, hi] = TRAINER_LIMITS[k];
      tr[k] = Math.max(lo, Math.min(hi, tr[k] + Number(b.dataset.d)));
      if (k === 'start' && tr.end <= tr.start) tr.end = Math.min(150, tr.start + 5);
      if (k === 'end' && tr.end <= tr.start) tr.start = Math.max(25, tr.end - 5);
    } else if (preset) {
      const [st, step, every, end] = preset.dataset.preset.split(',').map(Number);
      Object.assign(tr, { start: st, step, every, end });
    } else return;
    if (tr.on) { passes = 0; setSpeed(tr.start / 100, false); }
    savePrefs();
    renderTrainer();
  });
  function setTrainer(on) {
    prefs.trainer.on = on;
    passes = 0;
    if (on) {
      // It works on a loop: make sure there is one.
      if (!prefs.cycle || !prefs.cycle.on) toggleCycle();
      setSpeed(prefs.trainer.start / 100);
    } else {
      setSpeed(1);
    }
    savePrefs();
    updateButtons();
  }
  $('audio-trainer-on')?.addEventListener('change', (e) => setTrainer(e.target.checked));
  $('audio-trainer-start')?.addEventListener('click', () => {
    setTrainer(true);
    if (phoneMQ.matches) setSheet(false);
    const cyc = cycleRange();
    if (cyc) seek(cyc.t0);
    if (!isPlaying()) play();
  });

  // ---- Ruler: the cycle range, Logic / GarageBand style. Drag across the
  // ruler to set it (snapped to bars), drag its edges to resize it, drag
  // its middle to move it, click it to switch cycling on or off. A
  // section name in the ruler cycles that section; Shift extends. ---------
  const xToTime = (clientX) => {
    const r = waveEl.getBoundingClientRect();
    return (Math.max(0, Math.min(r.width, clientX - r.left)) / r.width) * duration;
  };
  const xToBar = (clientX) => barAtTime(xToTime(clientX));
  const barToX = (b) => {
    const r = waveEl.getBoundingClientRect();
    return r.left + (timeAtBar(b) / duration) * r.width;
  };
  rulerEl?.addEventListener('pointerdown', (e) => {
    if (!hasBars() || !duration || e.button !== 0) return;
    e.preventDefault();
    const x0 = e.clientX;
    const f0 = xToBar(x0);
    const c = prefs.cycle;
    let mode = 'create';
    if (c && cycleEl && !cycleEl.hidden) {
      const l = barToX(c.from), r = barToX(c.to);
      if (Math.abs(x0 - l) <= 7) mode = 'start';
      else if (Math.abs(x0 - r) <= 7) mode = 'end';
      else if (x0 > l && x0 < r) mode = 'move';
    }
    const sectionMark = e.target.closest('.audio-ruler-section');
    const orig = c ? { ...c } : null;
    const last = grid.length - 1;
    let moved = false;
    rulerEl.setPointerCapture(e.pointerId);
    const onMove = (ev) => {
      if (!moved && Math.abs(ev.clientX - x0) < 4) return;
      moved = true;
      const f = xToBar(ev.clientX);
      if (mode === 'create') {
        const a = Math.max(0, Math.floor(Math.min(f0, f)));
        const b = Math.min(last, Math.max(a + 1, Math.ceil(Math.max(f0, f))));
        prefs.cycle = { from: a, to: b, on: true };
      } else if (mode === 'start') {
        prefs.cycle = { ...orig, from: Math.max(0, Math.min(orig.to - 1, Math.round(f))) };
      } else if (mode === 'end') {
        prefs.cycle = { ...orig, to: Math.min(last, Math.max(orig.from + 1, Math.round(f))) };
      } else {
        const len = orig.to - orig.from;
        const from = Math.max(0, Math.min(last - len, orig.from + Math.round(f - f0)));
        prefs.cycle = { ...orig, from, to: from + len };
      }
      renderCycle();
    };
    const onUp = (ev) => {
      rulerEl.removeEventListener('pointermove', onMove);
      rulerEl.removeEventListener('pointerup', onUp);
      if (!moved) {
        if (sectionMark) {
          const start = +sectionMark.dataset.startBar, end = start + (+sectionMark.dataset.barCount);
          prefs.cycle = ev.shiftKey && prefs.cycle
            ? { from: Math.min(prefs.cycle.from, start), to: Math.max(prefs.cycle.to, end), on: true }
            : { from: start, to: end, on: true };
        } else if (mode === 'create') {
          seekToBar(Math.max(0, Math.floor(f0)));
        } else {
          prefs.cycle.on = !prefs.cycle.on;
        }
      }
      passes = 0;
      savePrefs();
      renderCycle();
      updateButtons();
    };
    rulerEl.addEventListener('pointermove', onMove);
    rulerEl.addEventListener('pointerup', onUp);
  });

  // ---- Waveform: hover shows bar · section · time; press and drag to
  // scrub ---------------------------------------------------------------
  function hoverAt(clientX) {
    if (!duration || !hoverEl) return;
    const r = waveEl.getBoundingClientRect();
    const t = xToTime(clientX);
    hoverEl.hidden = false;
    hoverEl.style.left = `${clientX - r.left}px`;
    const pos = barAtTime(t);
    let text = fmtTime(t);
    if (pos !== null && pos >= 0) {
      const b = Math.floor(pos), sec = sectionAt(b);
      text = `${window.t('Bar %s', barLabel(b))}${sec ? ` · ${sec.label}` : ''} · ${text}`;
    }
    hoverLabel.textContent = text;
    hoverEl.classList.toggle('flip', clientX - r.left > r.width - 140);
  }
  waveEl?.addEventListener('pointermove', (e) => hoverAt(e.clientX));
  waveEl?.addEventListener('pointerleave', () => { if (hoverEl) hoverEl.hidden = true; });
  waveEl?.addEventListener('pointerdown', (e) => {
    if (!duration || e.button !== 0) return;
    e.preventDefault();
    waveEl.setPointerCapture(e.pointerId);
    seek(xToTime(e.clientX));
    const onMove = (ev) => { seek(xToTime(ev.clientX)); hoverAt(ev.clientX); };
    const onUp = () => {
      waveEl.removeEventListener('pointermove', onMove);
      waveEl.removeEventListener('pointerup', onUp);
    };
    waveEl.addEventListener('pointermove', onMove);
    waveEl.addEventListener('pointerup', onUp);
  });

  // ---- Keyboard: one map for every player (player-proposal.md §3.5) -------
  // PlayerShell.keys, caught before song.js's own page keys: with a
  // recording loaded, Space plays it rather than the silent follow-along
  // clock. ←/→ step bars — unless the page has a set list to step through,
  // where they stay the previous/next song (page-turn pedals send them);
  // "," and "." step bars either way. K plays and pauses, as on the
  // rehearsals' player; the click moved to M (Metronome). L stays C's
  // alias for one release.
  const songNav = () => !!document.querySelector('.setlist-nav-card');
  const goSong = (dir) => {
    const a = document.querySelector(dir < 0 ? '.setlist-nav-card.prev, .topbar-prev[href]' : '.setlist-nav-card.next, .topbar-next[href]');
    if (!a || !a.href) return false;
    window.location.href = a.href;
  };
  const loopSection = () => {
    if (!hasBars()) return false;
    const sec = sectionAt(Math.max(0, Math.floor(barAtTime(audio.currentTime) || 0)));
    if (!sec) return false;
    setCycle(sec.startBar, sec.startBar + sec.barCount);
  };
  const toggleCountIn = () => {
    prefs.countIn = (prefs.countIn + 1) % 3;
    savePrefs();
    syncCountInSeg();
    updateButtons();
  };
  const togglePitch = () => { const b = $('audio-pitch-btn'); if (!b || b.hidden) return false; b.click(); };
  const speedBy = (d) => () => setSpeed(prefs.speed + d);
  if (window.PlayerShell) window.PlayerShell.keys({
    own: '#audio-player',
    title: t('The player’s keys'),
    active: () => {
      if (!duration || !playerBody || playerBody.hidden) return false;
      const sheet = $('sheet');
      if (sheet && sheet.dataset.renderMode === 'mixer') return false; // the Mixer has its own
      // The Sync sheet (#74) has its own keys while it's open (Space, T,
      // ←/→ nudge, K its click, Z undo), registered after these.
      if (window.songbooksSyncSheet && window.songbooksSyncSheet.isOpen && window.songbooksSyncSheet.isOpen()) return false;
      const drawer = $('edit-drawer');
      return !(drawer && !drawer.hidden);
    },
    keys: {
      ' ': () => (isPlaying() ? pause() : play()),
      k: () => (isPlaying() ? pause() : play()),
      ArrowLeft: () => (songNav() ? false : stepBar(-1)),
      ArrowRight: () => (songNav() ? false : stepBar(1)),
      ',': () => stepBar(-1),
      '.': () => stepBar(1),
      'Alt+ArrowLeft': () => stepSection(-1),
      'Alt+ArrowRight': () => stepSection(1),
      'Shift+ArrowLeft': () => goSong(-1),
      'Shift+ArrowRight': () => goSong(1),
      Home: () => $('audio-start-btn').click(),
      0: () => $('audio-start-btn').click(),
      c: () => toggleCycle(),
      l: () => toggleCycle(),
      'Shift+c': loopSection,
      '[': speedBy(-0.05),
      ']': speedBy(0.05),
      '{': speedBy(-0.01),
      '}': speedBy(0.01),
      '\\': () => setSpeed(1),
      '-': () => setSemitones(prefs.semitones - 1),
      '=': () => setSemitones(prefs.semitones + 1),
      '+': () => setSemitones(prefs.semitones + 1),
      v: () => (hasParts() ? toggleVoice() : false),
      m: () => toggleClick(),
      i: toggleCountIn,
      s: togglePitch,
      f: () => setSheet(!sheetOpen()),
    },
    help: [
      [t('Play / pause'), ' ', 'k'],
      [t('Back / forward one bar (on a set list: the previous / next song; , and . step bars)'), 'ArrowLeft', 'ArrowRight'],
      [t('Previous / next section'), 'Alt+ArrowLeft', 'Alt+ArrowRight'],
      [t('Previous / next song'), 'Shift+ArrowLeft', 'Shift+ArrowRight'],
      [t('Back to the start (of the loop, when looping)'), 'Home', '0'],
      [t('Loop on / off · ⇧C loops this section'), 'c', 'Shift+c'],
      [t('Speed −5 % / +5 % (⇧ for 1 %) · \\ back to 100 %'), '[', ']', '\\'],
      [t('Key down / up a semitone'), '-', '='],
      [t('Original voice on / off (sing it yourself)'), 'v'],
      [t('Metronome click · count-in · check my singing'), 'm', 'i', 's'],
      [t('Open / close the panel · close · these shortcuts'), 'f', 'Escape', '?'],
    ],
  });
  // The lock screen, headphones and media keys (Media Session): play,
  // pause, seek, and the previous / next section as the track buttons.
  const mediaSession = window.PlayerShell ? window.PlayerShell.media({
    title: lcdSection?.dataset.title || document.title,
    play: () => play(), pause: () => pause(),
    seekTo: (s) => seek(s), seekBy: (d) => seek(Math.max(0, audio.currentTime + d)),
    previous: () => stepSection(-1), next: () => stepSection(1),
  }) : null;

  // ---- States (player-proposal.md §3.6) -----------------------------------
  // The player says what it can do, in its readout and its tray, on
  // #audio-player's data-state and data-sync:
  //   data-state "loading"  the file decodes: a skeleton waveform; ▶
  //                         already plays, streaming.
  //              "error"    it can't be read (offline, a link expired, the
  //                         pitch shifter not loaded): one line in the
  //                         readout and "Réessayer"; the title stays.
  //              "none"     no recording: ▶ runs the follow-along clock
  //                         with the click on (song.js songbooksFollowClock);
  //                         the tray is "+ Enregistrement" for an editor.
  //   data-sync  "estimated"  bars the player found by listening (nothing
  //                         saved): "≈ 21.1" and "synchro estimée", the
  //                         ruler's ticks dashed; its line in the panel
  //                         opens the Sync sheet (window.songbooksSyncSheet,
  //                         when the page has it) or the Mixer.
  //              "set"      saved bars or section starts.
  //              "none"     no bars at all: a time readout, ←/→ 5 s.
  // Stems still being split: the voice's switch greyed with a spinner
  // ("séparation…"), live with the 20 s poll; failed, "Voix indisponible",
  // with a retry for editors (renderStems, renderVoice).
  const lcdError = $('audio-lcd-error'), retryBtn = $('audio-retry');
  function setState(state, message) {
    if (state) root.dataset.state = state; else delete root.dataset.state;
    if (lcdError) lcdError.textContent = state === 'error' ? (message || t('Couldn’t play this file.')) : '';
    if (retryBtn) retryBtn.hidden = state !== 'error';
  }
  retryBtn?.addEventListener('click', () => {
    setState('loading');
    if (!Object.keys(categories).length) { loadMeta(); return; }
    audio.dataset.blobFor = '';
    selectCategory(activeCategory);
  });
  audio.addEventListener('error', () => {
    // A part of the recording that failed, or the recording itself.
    if (!audio.src || audio.src === location.href) return;
    setState('error', navigator.onLine === false ? t('Offline: this recording can’t be read.') : t('Couldn’t play this file.'));
  });
  audio.addEventListener('canplay', () => { if (root.dataset.state === 'error') setState(peaks ? '' : 'loading'); });
  // Stems: pending, failed, there.
  function renderStems() {
    const meta = categories[activeCategory] || {};
    const failed = meta.stemStatus === 'failed' && !hasParts();
    const retry = $('audio-stems-retry');
    if (retry) retry.hidden = !(failed && canEdit);
    renderVoice();
  }
  $('audio-stems-retry')?.addEventListener('click', async (e) => {
    e.stopPropagation();
    const res = await fetch(`/api/songs/${variantId}/audio/${activeCategory}/stems`, { method: 'POST' }).catch(() => null);
    if (res && res.ok && categories[activeCategory]) {
      categories[activeCategory].stemStatus = 'queued';
      renderStems();
      watchSplit();
    }
  });
  // No recording: the follow-along clock, the click on.
  let noneTimer = null;
  function enterNoRecording() {
    setState('none');
    if (playerBody) playerBody.hidden = false;
    if (emptyState) emptyState.hidden = true;
    const add = $('audio-add-recording');
    if (add) add.hidden = !canEdit;
    const fc = () => window.songbooksFollowClock;
    playBtn?.addEventListener('click', (e) => {
      if (root.dataset.state !== 'none' || !fc()) return;
      e.stopImmediatePropagation();
      fc().toggle(true);
    }, true);
    followClickPrefs();
    clearInterval(noneTimer);
    noneTimer = setInterval(() => {
      const c = fc() && fc().state();
      if (!c) return;
      const playing = !!c.running;
      if (playBtn && playBtn.dataset.state !== String(playing)) { playBtn.innerHTML = playing ? iconPause : iconPlay; playBtn.dataset.state = String(playing); }
      lcdPos.textContent = c.bar !== '' ? `${Number(c.bar) + 1}` : '1';
      lcdSection.textContent = c.section || '';
      if (timeLabel) timeLabel.textContent = t('click alone · %d bpm', Math.round(c.bpm || bpm));
      const sw = $('audio-click-switch');
      if (sw && sw.checked !== playing) sw.checked = playing;
      const amount = $('audio-tempo-amount');
      if (amount && amount.textContent !== String(Math.round(c.bpm))) amount.textContent = String(Math.round(c.bpm));
    }, 200);
  }
  $('audio-add-recording')?.addEventListener('click', () => {
    document.querySelector('#mode-segmented button[data-value="reference"]')?.click();
  });
  // Without a recording the click is the song's own metronome: the panel
  // opens on its Click line (its sound, its volume) and a Tempo line (the
  // song's tempo, or another, tapped or stepped), in place of the
  // /metronome page the song used to send you to (menu-proposal.md §3.5).
  // The switch is the metronome: on, it plays; off, it stops.
  const followClock = () => window.songbooksFollowClock;
  function clickOn() {
    if (root.dataset.state !== 'none') return !!prefs.click;
    const c = followClock() && followClock().state();
    return !!(c && c.running);
  }
  function toggleFollowClick() {
    const fc = followClock();
    if (!fc) return;
    followClickPrefs();
    fc.toggle(true);
    const sw = $('audio-click-switch');
    if (sw) sw.checked = clickOn();
  }
  function followClickPrefs() {
    const fc = followClock();
    if (!fc || !fc.setSound) return;
    fc.setSound(prefs.clickSound);
    fc.setVolume((prefs.clickVolume === undefined ? 70 : prefs.clickVolume) / 70);
  }
  function stepTempo(d) {
    const fc = followClock();
    if (!fc || !fc.setBpm) return;
    const v = fc.setBpm(Math.round(fc.state().bpm) + d);
    const amount = $('audio-tempo-amount');
    if (amount) amount.textContent = String(v);
  }
  $('audio-tempo-down')?.addEventListener('click', (e) => stepTempo(e.shiftKey ? -5 : -1));
  $('audio-tempo-up')?.addEventListener('click', (e) => stepTempo(e.shiftKey ? 5 : 1));
  $('audio-tempo-reset')?.addEventListener('click', (e) => {
    const fc = followClock();
    if (!fc || !fc.setBpm) return;
    const amount = $('audio-tempo-amount');
    const v = fc.setBpm(Number(e.currentTarget.dataset.bpm) || bpm);
    if (amount) amount.textContent = String(v);
  });
  $('audio-tempo-tap')?.addEventListener('click', () => {
    const fc = followClock();
    if (!fc || !fc.tap) return;
    const amount = $('audio-tempo-amount');
    const v = fc.tap();
    if (amount) amount.textContent = String(v);
  });

  // ---- Category toggle / load -------------------------------------------
  let synthLevel = 1; // the Score view's "Score" level (songbooksPlayer.setLevel)
  function selectCategory(cat, remember) {
    activeCategory = cat;
    audio.volume = cat === 'synth' ? synthLevel : 1;
    if (remember) { try { localStorage.setItem(prefKey, cat); } catch (e) {} }
    categoryToggle?.querySelectorAll('button').forEach(b => b.classList.toggle('active', b.dataset.category === cat));
    pause();
    audio.dataset.blobFor = '';
    duration = 0;
    grid = [];
    detected = null;
    setState('loading');
    decodeWaveform();
  }

  async function loadMeta() {
    const res = await fetch(`/api/songs/${variantId}/audio`);
    if (!res.ok) return;
    const data = await res.json();
    categories = {};
    (data.categories || []).forEach(c => { categories[c.category] = c; });
    const available = Object.keys(categories);
    // Whether there is a recording at all, for the page around the player:
    // with one, the key is the player's (play.html .topbar-key).
    root.dataset.recording = available.length ? 'yes' : 'no';
    // Which there are, for the header's reference (song.js initReference):
    // a band recording is the reference when there's no link.
    document.dispatchEvent(new CustomEvent('songbooks:audio-categories', { detail: { categories: available } }));

    if (available.length === 0) {
      if (categoryToggle) categoryToggle.hidden = true;
      enterNoRecording();
      readyResolve({ recorded: false });
      return;
    }
    readyResolve({ recorded: true });
    clearInterval(noneTimer);
    if (root.dataset.state === 'none') setState('');
    if (emptyState) emptyState.hidden = true;
    if (playerBody) playerBody.hidden = false;

    if (categoryToggle) {
      categoryToggle.hidden = available.length < 2;
      categoryToggle.querySelectorAll('button').forEach(b => { b.hidden = !categories[b.dataset.category]; });
    }

    let preferred = null;
    try { preferred = localStorage.getItem(prefKey); } catch (e) {}
    // Default to "gp" over whatever order the server happened to return
    // (available[0]) when the listener hasn't picked a preference yet —
    // it's the one category guaranteed to line up with the chart's own
    // bars (see store.AudioGP's doc comment), so it's the right silent
    // default even on a song that also has a youtube/band recording.
    // A Guitar Pro file next to the chart: its own recording is one more
    // recording, but the song's own come first (the file is for the Score
    // view).
    const beside = !!document.querySelector('#gp-score-page[data-gp-beside]');
    const first = beside ? (['youtube', 'band', 'gp'].find((c) => categories[c]) || available[0]) : (categories.gp ? 'gp' : available[0]);
    const target = (preferred && categories[preferred]) ? preferred : first;
    selectCategory(target);
    // A key kept from the last visit: the chords go there too.
    // A student's or group's key (studentKey): the chart is there already
    // (song.js, data-key-offset); only the recording moves.
    if (prefs.semitones && !keyRestored) {
      keyRestored = true;
      if (studentKey === null) shiftChords(prefs.semitones);
      applyKey();
    }
    watchSplit();
  }
  let keyRestored = false;

  // ---- The Guitar Pro file, played by the synthesizer ----------------------
  // A song with a Guitar Pro file and no recording: the Score view
  // (gp-view.js) renders the file with alphaTab's synthesizer, once, and
  // hands the sound here, where it plays as a recording would ("synth"):
  // the waveform, bar.beat, loops, speed, key, click, count-in, the keys
  // and the lock screen. Its bars are the file's, as played (repeats
  // unfolded); when the song's chart counts as many, they are the chart's
  // too, else the player shows the file's own sections (ownBars).
  //
  //   window.songbooksPlayer.recordings     → {recorded}: once the
  //                                          recordings are known;
  //   .synthProgress(text)                 the readout while it renders;
  //   .offerSynth({bytes, type, barTimes, beats, sections, bpm})
  //                                        barTimes[p]: when played bar p
  //                                        starts, in seconds (one more for
  //                                        the end);
  //   .seekBar(x, play)                    to a bar, fractional (2.5 =
  //                                        halfway through the third);
  //   .setCycle(from, to)                  loop bars [from, to);
  //   .speed(), .playing(), .cycle() → {from, to} or null
  //
  // A file next to the chart (gp-view.js "The simplified mixer"): its
  // sound plays only in the Score view, even when the song has recordings.
  //   .offerSynth({..., keep: true})        kept, not played (the song's
  //                                          recording stays on);
  //   .useSynth()                          the file's sound on (the Score
  //                                          view shown); false if none;
  //   .leaveSynth()                        back to the song's recording,
  //                                          or to none (the view left);
  //   .source()                            'synth' or the recording's
  //                                          category;
  //   .setLevel(v)                         the dock's level, 0..1;
  //   .replaceSynth(bytes)                 the file's sound rendered again
  //                                          (another instrument), played
  //                                          on from the same place.
  let readyResolve;
  let beforeSynth = null; // the recording the Score view's file took over from
  const ready = new Promise((r) => { readyResolve = r; });
  window.songbooksPlayer = Object.assign(window.songbooksPlayer || {}, {
    recordings: ready,
    synthProgress(text) {
      if (Object.keys(categories).length) return;
      clearInterval(noneTimer);
      setState('loading');
      if (timeLabel) timeLabel.textContent = text;
    },
    synthFailed(text) {
      if (activeCategory === 'synth' || Object.keys(categories).length) return;
      enterNoRecording();
      if (timeLabel && text) timeLabel.title = text;
    },
    offerSynth(src) {
      const keep = !!src.keep;
      if (!keep && Object.keys(categories).length) return;
      const chartBars = sections().reduce((m, s) => Math.max(m, s.startBar + s.barCount), 0);
      const bars = src.barTimes.length - 1;
      synth = { ...src, ownBars: chartBars !== bars };
      categories.synth = {
        category: 'synth', markers: [], stems: [],
        barSyncPoints: src.barTimes.map((t, i) => ({ barIndex: i, timeSeconds: t })),
      };
      if (keep) return;
      const fc = window.songbooksFollowClock;
      if (fc && fc.state().running) fc.toggle(false);
      clearInterval(noneTimer);
      if (src.bpm) { bpm = src.bpm; secondsPerBar = (60 / bpm) * beatsPerBar; }
      root.dataset.source = 'synth';
      $('audio-add-recording')?.setAttribute('hidden', '');
      if (playerBody) playerBody.hidden = false;
      setSpeed(prefs.speed, false);
      selectCategory('synth');
      // A key kept from the last visit, as loadMeta does for a recording.
      if (prefs.semitones && !keyRestored) {
        keyRestored = true;
        if (studentKey === null) shiftChords(prefs.semitones);
        applyKey();
      }
    },
    seekBar(x, andPlay) {
      if (!duration || !hasBars()) return false;
      const c = prefs.cycle;
      if (c && c.on && (x < c.from || x >= c.to)) {
        c.on = false;
        savePrefs();
        renderCycle();
        updateButtons();
      }
      const t = timeAtBar(x);
      if (t === null) return false;
      seek(t);
      if (andPlay && !isPlaying()) play();
      return true;
    },
    setCycle(from, to) { if (duration && hasBars()) setCycle(from, to); },
    useSynth() {
      if (!categories.synth || !synth) return false;
      if (activeCategory === 'synth') return true;
      beforeSynth = activeCategory;
      clearInterval(noneTimer);
      const fc = window.songbooksFollowClock;
      if (fc && fc.state().running) fc.toggle(false);
      if (synth.bpm) { bpm = synth.bpm; secondsPerBar = (60 / bpm) * beatsPerBar; }
      root.dataset.source = 'synth';
      if (playerBody) playerBody.hidden = false;
      selectCategory('synth');
      return true;
    },
    leaveSynth() {
      if (activeCategory !== 'synth') return;
      pause();
      bpm = songBpm;
      secondsPerBar = bpm > 0 ? (60 / bpm) * beatsPerBar : 0;
      delete root.dataset.source;
      const recs = Object.keys(categories).filter((c) => c !== 'synth');
      const back = beforeSynth && categories[beforeSynth] && beforeSynth !== 'synth' ? beforeSynth : recs[0];
      beforeSynth = null;
      if (back) { selectCategory(back); return; }
      // No recording: the follow-along clock, as before (again: its play
      // button's listener stops the ones added before it).
      activeCategory = null;
      duration = 0;
      grid = [];
      audio.removeAttribute('src');
      audio.dataset.blobFor = '';
      enterNoRecording();
    },
    source: () => activeCategory,
    setLevel(v) { synthLevel = Math.max(0, Math.min(1, v)); if (activeCategory === 'synth') audio.volume = synthLevel; },
    // The file's sound rendered again (another instrument, gp-view.js):
    // the same bars, new bytes; playing on from where it was.
    replaceSynth(bytes) {
      if (!synth) return false;
      synth.bytes = bytes;
      if (activeCategory !== 'synth') return true;
      const at = audio.currentTime, was = isPlaying();
      audio.addEventListener('loadedmetadata', () => {
        if (was) startAt(at); else try { audio.currentTime = at; } catch (e) { /* from the start */ }
      }, { once: true });
      pause();
      audio.dataset.blobFor = '';
      decodeWaveform();
      return true;
    },
    speed: () => prefs.speed,
    playing: () => isPlaying(),
    cycle: () => (prefs.cycle && prefs.cycle.on && hasBars() ? { from: prefs.cycle.from, to: prefs.cycle.to } : null),
   });

  // A recording being split (just added, or replaced): its parts show up
  // here when they're ready, without a reload.
  let splitTimer = null;
  function watchSplit() {
    clearTimeout(splitTimer);
    const pending = Object.values(categories).some((c) => c.stemStatus === 'queued' || c.stemStatus === 'running');
    if (!pending) return;
    splitTimer = setTimeout(async () => {
      const res = await fetch(`/api/songs/${variantId}/audio`).catch(() => null);
      if (!res || !res.ok) return;
      const data = await res.json();
      (data.categories || []).forEach((c) => {
        if (categories[c.category]) Object.assign(categories[c.category], { stems: c.stems, stemStatus: c.stemStatus });
      });
      applyVoice();
      watchSplit();
    }, 20000);
  }

  categoryToggle?.addEventListener('click', e => {
    const btn = e.target.closest('button[data-category]');
    if (btn && categories[btn.dataset.category]) selectCategory(btn.dataset.category, true);
  });

  // Redraw on resize, and when the player itself appears: a page opened
  // on another view (e.g. the Mixer) decodes while the player is 0px wide.
  const redraw = () => { drawWaveform(); refreshTimeline(); };
  window.addEventListener('resize', redraw);
  if (waveEl && window.ResizeObserver) new ResizeObserver(redraw).observe(waveEl);

  // ---- Chart and section strip: jump the recording there -------------------
  // song.js routes a click on a section (strip or chart) here first; a
  // click on a chart bar does the same. True when handled.
  window.songbooksReferenceSeekBar = (bar) => {
    if (!duration || !hasBars()) return false;
    // Asking for a bar outside the cycle means "play from here": switch
    // the cycle off (it stays drawn) rather than snapping back into it.
    const c = prefs.cycle;
    if (c && c.on && (bar < c.from || bar >= c.to)) {
      c.on = false;
      savePrefs();
      renderCycle();
      updateButtons();
    }
    seekToBar(bar);
    if (!isPlaying()) play();
    return true;
  };
  // ---- Chart: click a bar to play from it; press on a bar and drag
  // across others to cycle them (Soundslice / Logic style); Shift-click
  // extends the cycle to that bar. Chord-grid bars and tab rows alike.
  const sheetEl = $('sheet');
  function chartBarAt(e) {
    const el = e.target && e.target.closest ? e.target.closest('.bar[data-global-bar], .jz-bar[data-global-bar]') : null;
    if (el && el.offsetParent) return playedBarOf(el);
    return window.songbooksTabBarAt ? window.songbooksTabBarAt(e.clientX, e.clientY) : null;
  }
  // A Jazz chart bar is read on every pass through a repeat or a D.S.
  // (data-played): a click means the pass nearest where the music is.
  function playedBarOf(el) {
    if (!el.dataset.played) return Number(el.dataset.globalBar);
    const all = el.dataset.played.split(' ').map(Number);
    const cur = hasBars() ? barAtTime(audio.currentTime) || 0 : 0;
    return all.reduce((best, b) => (Math.abs(b - cur) < Math.abs(best - cur) ? b : best), all[0]);
  }
  // kind: 'cycle' (the loop, kept on screen) or 'select' (a drag in progress).
  function markChartRange(from, to, kind) {
    document.querySelectorAll(`#sheet .bar.range-${kind}, #sheet .jz-bar.range-${kind}`).forEach((el) => el.classList.remove(`range-${kind}`));
    if (from !== null && to > from) {
      document.querySelectorAll('#sheet .bar[data-global-bar], #sheet .jz-bar[data-global-bar]').forEach((el) => {
        const played = el.dataset.played ? el.dataset.played.split(' ').map(Number) : [Number(el.dataset.globalBar)];
        if (played.some((b) => b >= from && b < to)) el.classList.add(`range-${kind}`);
      });
    }
    if (window.songbooksTabMarkRange) window.songbooksTabMarkRange(from, to, kind);
  }
  function markCycleInChart() {
    const c = prefs.cycle;
    if (c && c.on && hasBars()) markChartRange(c.from, c.to, 'cycle');
    else markChartRange(null, 0, 'cycle');
  }
  // Tab rows engrave after the page loads (and re-engrave on resize):
  // keep their cycle tint in step.
  setInterval(() => { if (prefs.cycle && prefs.cycle.on && hasBars()) markCycleInChart(); }, 1000);

  function setCycle(from, to) {
    prefs.cycle = { from: Math.max(0, from), to: Math.max(from + 1, to), on: true };
    passes = 0;
    savePrefs();
    renderCycle();
    updateButtons();
    const cyc = cycleRange();
    if (cyc && (audio.currentTime < cyc.t0 - 0.05 || audio.currentTime >= cyc.t1)) seek(cyc.t0);
  }

  let drag = null;
  let suppressClick = false;
  sheetEl?.addEventListener('pointerdown', (e) => {
    if (e.button !== 0 || !hasBars() || $('edit-drawer')?.hidden === false) return;
    const b = chartBarAt(e);
    if (b === null) return;
    drag = { start: b, end: b, x: e.clientX, y: e.clientY, moved: false };
  });
  document.addEventListener('pointermove', (e) => {
    if (!drag) return;
    if (!drag.moved && Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 6) return;
    drag.moved = true;
    window.getSelection()?.removeAllRanges();
    const b = chartBarAt(e);
    if (b !== null) drag.end = b;
    markChartRange(Math.min(drag.start, drag.end), Math.max(drag.start, drag.end) + 1, 'select');
  });
  document.addEventListener('pointerup', (e) => {
    if (!drag) return;
    const d = drag;
    drag = null;
    markChartRange(null, 0, 'select');
    if (d.moved) {
      setCycle(Math.min(d.start, d.end), Math.max(d.start, d.end) + 1);
    } else if (e.shiftKey) {
      const c = prefs.cycle;
      setCycle(c ? Math.min(c.from, d.start) : d.start, c ? Math.max(c.to, d.start + 1) : d.start + 1);
    } else {
      return; // a plain click: the click handler below plays from that bar
    }
    window.getSelection()?.removeAllRanges();
    suppressClick = true;
    setTimeout(() => { suppressClick = false; }, 0);
  });
  document.addEventListener('pointercancel', () => { drag = null; markChartRange(null, 0, 'select'); });

  function wireBarClickToPlay() {
    if (sheetEl) sheetEl.classList.toggle('bar-click-to-play', hasBars());
    markCycleInChart();
  }
  sheetEl?.addEventListener('click', (e) => {
    if (suppressClick || !hasBars() || e.shiftKey || $('edit-drawer')?.hidden === false) return;
    const b = chartBarAt(e);
    if (b !== null) window.songbooksReferenceSeekBar(b);
  });

  setSpeed(prefs.speed, false);
  updateButtons();

  // The mini player (mini-player.js): leaving the page, the recording
  // playing goes on on the next one, as its plain URL — there, the
  // recording as it is: no parts, no key, no count-in. Back on this song
  // (or a recording of it kept from another page), this player takes it
  // over from the same place.
  const songURL = (cat) => `/song/${variantId}/audio.mp3?category=${cat}&v=${(categories[cat] && categories[cat].byteSize) || ''}`;
  let kept = null;
  function takeOver() {
    if (!kept || !duration) return;
    const cat = new URLSearchParams(kept.state.key.split('?')[1] || '').get('category');
    if (cat && cat !== activeCategory && categories[cat]) { selectCategory(cat); return; }
    const { state, play: go } = kept;
    kept = null;
    seek(state.pos);
    if (go) play();
  }
  audio.addEventListener('loadedmetadata', takeOver);
  window.SongbooksNowPlaying?.register({
    media: audio,
    info: () => (activeCategory
      ? { key: songURL(activeCategory), src: songURL(activeCategory), title: document.title.replace(/ — Songbooks$/, ''), page: location.pathname + location.search }
      : null),
    owns: (key) => key.startsWith(`/song/${variantId}/audio.mp3?`),
    resume: (state, from) => { kept = { state, play: from.play }; takeOver(); },
  });

  // ---- Reference-drawer upload controls (YouTube download + band file
  // upload) — moved here from the main player per direct request. Both
  // just call loadMeta() again afterward so the player above picks up
  // the new category without a full page reload. --------------------------
  // The Mixer (mixer.js) plays its own copy of the recording: stop this
  // one when it starts, and reload markers/sync points it edited.
  document.addEventListener('songbooks:pause-reference', () => pause());
  document.addEventListener('songbooks:audio-changed', () => loadMeta());

  // ---- For the Sync sheet (sync-sheet.js) ------------------------------------
  // The element holds the category's recording, or one of its parts: with
  // the voice turned down it plays the accompaniment, tagged "youtube:no_vocals"
  // (swapSource), the same length and timeline as the recording.
  const holdsCategory = (cat) => {
    const f = audio.dataset.blobFor || '';
    return !!cat && (f === cat || f.startsWith(`${cat}:`));
  };
  // Added to the Score view's (above), not in place of it.
  window.songbooksPlayer = Object.assign(window.songbooksPlayer || {}, {
    variantId,
    canEdit,
    audio,
    category: () => activeCategory,
    categories: () => Object.keys(categories),
    selectCategory: (cat) => { if (categories[cat] && cat !== activeCategory) selectCategory(cat, true); },
    meta: () => categories[activeCategory] || null,
    duration: () => duration,
    ready: () => !!duration && holdsCategory(activeCategory),
    isPlaying, play, pause,
    seek: (t) => seek(t),
    sections,
    beatsPerBar, barLabel,
    // Each section's start as the chart follows it now, with its origin
    // ("auto" for the detection's).
    syncMarkers: currentSyncMarkers,
    syncInfo,
    syncTempos,
    // Downbeats found by ear, for snapping: [time of bar 0, …] or null.
    detectBars: runDetection,
    setPreview: (markers) => { preview = markers ? markers.map((m) => ({ ...m })) : null; rebuildGrid(); renderPosition(); },
    saveMarkers,
    refreshSync,
    // The voice turned down (the accompaniment plays), and the toggle.
    voiceDown: () => hasParts() && prefs.voice < 100,
    toggleVoice,
    // The Sync sheet's waveform (sync-wave.js): the recording, mono at
    // ~11 kHz ({x, sr}, or null before it's decoded), and when each bar
    // starts as the chart follows it now ([time of bar 0, …]).
    samples: () => (analysisInput && analysisInput.cat === activeCategory ? analysisInput : null),
    barTimes: () => grid.slice(),
    totalBars,
    hasPickup,
    leadIn,
    click: () => prefs.click,
    toggleClick,
    speed: () => prefs.speed,
    setSpeed: (v) => setSpeed(v),
    // The panel, at a tool's line (the song bar's Outils, the header's key).
    openLine,
   });
  // The tempo now (a saved one below, the Guitar Pro file's while its
  // synthesizer plays), not the one the page opened with.
  Object.defineProperty(window.songbooksPlayer, 'bpm', { get: () => bpm, configurable: true, enumerable: true });

  // The song's tempo saved (Tap tempo's "Use this tempo", tap-tempo.js;
  // a Source save, song.js): the chart's bar is that long at once. The
  // estimate — the in-memory detection, which starts from the tempo — is
  // run again with it, and the bars the sync doesn't place (before the
  // first marker, after the last, the lead) take it. Saved markers and
  // per-bar points are times: they never move.
  document.addEventListener('songbooks:tempo-saved', (e) => {
    const v = parseFloat(e.detail && e.detail.tempo);
    if (!(v > 0) || v === songBpm) return;
    songBpm = v;
    if (transportEl) transportEl.dataset.bpm = String(v);
    if (activeCategory === 'synth') return; // the file's own tempo plays
    bpm = v;
    secondsPerBar = (60 / bpm) * beatsPerBar;
    if (detected) {
      detectedFull = null;
      const times = runDetection();
      detected = times ? times.map((t, i) => ({ barIndex: i, timeSeconds: t })) : null;
    }
    resetClicks();
    rebuildGrid();
    renderPosition();
  });
  document.dispatchEvent(new CustomEvent('songbooks:player-ready'));


  if (!canEdit) { loadMeta(); return; }

  // Outcome lines under each category's buttons (Forms.status, forms.js).
  // The API's own error strings are developer-facing, so each failure is
  // reworded here by status code.
  const say = (el, msg, kind) => { if (el && window.Forms) Forms.status(el, msg, kind); };
  const songTitle = document.getElementById('gpimport-panel')?.dataset.songTitle;
  const songName = songTitle ? t('“%s”', songTitle) : t('this song');

  // Band recording: plain file upload.
  const bandFileInput = document.getElementById('audio-band-file');
  const bandUploadBtn = document.getElementById('audio-band-upload-btn');
  const bandReplaceBtn = document.getElementById('audio-band-replace-btn');
  const bandDeleteBtn = document.getElementById('audio-band-delete-btn');
  const bandStatus = document.getElementById('audio-band-status');
  async function uploadBand(file) {
    const btn = (bandReplaceBtn && !bandReplaceBtn.hidden) ? bandReplaceBtn : bandUploadBtn;
    const fd = new FormData();
    fd.append('file', file);
    Forms.busy(btn, true);
    say(bandStatus, t('Uploading %s…', file.name));
    try {
      const res = await fetch(`/api/songs/${variantId}/audio/band`, { method: 'PUT', body: fd });
      if (res.status === 413) { say(bandStatus, t('That file is too big. Use an audio file under 250 MB.'), 'error'); return; }
      if (!res.ok) { say(bandStatus, t("Couldn't upload that recording. Check it's an audio file (MP3 works best) and try again."), 'error'); return; }
      say(bandStatus, t('Recording uploaded.'), 'ok');
      loadMeta();
    } catch (e) {
      say(bandStatus, t("Couldn't upload that recording. Check your connection and try again."), 'error');
    } finally {
      Forms.busy(btn, false);
      bandFileInput.value = '';
    }
  }
  bandUploadBtn?.addEventListener('click', () => bandFileInput?.click());
  bandReplaceBtn?.addEventListener('click', () => bandFileInput?.click());
  bandFileInput?.addEventListener('change', () => {
    const f = bandFileInput.files && bandFileInput.files[0];
    if (f) uploadBand(f);
  });
  bandDeleteBtn?.addEventListener('click', async () => {
    if (!confirm(t("Remove the band recording from %s? This can't be undone.", songName))) return;
    const res = await fetch(`/api/songs/${variantId}/audio/band`, { method: 'DELETE' });
    if (res.ok) { say(bandStatus, t('Band recording removed.'), 'ok'); loadMeta(); }
    else say(bandStatus, t("Couldn't remove the recording. Try again."), 'error');
  });

  // YouTube: downloads audio from the video URL already saved as this
  // song's reference (see #reference-block's data-url, kept current by
  // song.js's own initReference) — no second URL field to fill in.
  const ytDownloadBtn = document.getElementById('audio-youtube-download-btn');
  const ytReplaceBtn = document.getElementById('audio-youtube-replace-btn');
  const ytDeleteBtn = document.getElementById('audio-youtube-delete-btn');
  const ytStatus = document.getElementById('audio-youtube-status');
  const referenceBlock = document.getElementById('reference-block');

  async function downloadFromReference() {
    const url = referenceBlock && referenceBlock.dataset.url;
    if (!url) {
      // No link saved yet: open the link field above instead of a popup.
      say(ytStatus, t('Add the YouTube link first, then download its audio.'), 'error');
      const editRow = document.getElementById('reference-edit-row');
      if (editRow && editRow.hidden) document.getElementById('reference-edit-btn')?.click();
      document.getElementById('reference-url-input')?.focus();
      return;
    }
    ytDownloadBtn && (ytDownloadBtn.disabled = true);
    ytReplaceBtn && (ytReplaceBtn.disabled = true);
    // A spinner on whichever button was clicked: the download can take a
    // real minute (yt-dlp fetch + transcode), and the status line alone
    // is easy to miss below the fold on a long page.
    const activeBtn = (ytReplaceBtn && !ytReplaceBtn.hidden) ? ytReplaceBtn : ytDownloadBtn;
    Forms.busy(activeBtn, true);
    say(ytStatus, t('Downloading… this can take a minute.'));
    try {
      const res = await fetch(`/api/songs/${variantId}/audio/youtube`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      if (res.status === 413) { say(ytStatus, t('That video is too long to keep as audio here.'), 'error'); return; }
      if (!res.ok) { say(ytStatus, t("Couldn't get the audio from that video. Check the link opens on YouTube, then try again."), 'error'); return; }
      say(ytStatus, t('Audio downloaded.'), 'ok');
      loadMeta();
    } catch (e) {
      say(ytStatus, t("Couldn't reach the server. Check your connection and try again."), 'error');
    } finally {
      ytDownloadBtn && (ytDownloadBtn.disabled = false);
      ytReplaceBtn && (ytReplaceBtn.disabled = false);
      Forms.busy(activeBtn, false);
    }
  }
  ytDownloadBtn?.addEventListener('click', downloadFromReference);
  ytReplaceBtn?.addEventListener('click', downloadFromReference);
  ytDeleteBtn?.addEventListener('click', async () => {
    if (!confirm(t("Remove the audio downloaded from the YouTube video for %s? The video link stays. This can't be undone.", songName))) return;
    const res = await fetch(`/api/songs/${variantId}/audio/youtube`, { method: 'DELETE' });
    if (res.ok) { say(ytStatus, t('Downloaded audio removed.'), 'ok'); loadMeta(); }
    else say(ytStatus, t("Couldn't remove the audio. Try again."), 'error');
  });

  // Reflect which categories already exist in the drawer's own
  // upload/replace/remove button visibility (upload hidden once present,
  // replace/remove shown instead) — re-run whenever loadMeta() refreshes
  // categories, not just once at load.
  // "What's in each recording": full performance or backing track, per
  // category (PUT /api/songs/{id}/audio/{category}/kind).
  const kindList = $('audio-kind-list');
  function renderKinds() {
    if (!kindList) return;
    const cats = Object.keys(categories).filter((c) => c !== 'synth');
    if (!cats.length) { kindList.innerHTML = `<span class="hint">${t('No recording yet.')}</span>`; return; }
    const names = { gp: t('Guitar Pro recording'), youtube: t('YouTube recording'), band: t('Band recording') };
    kindList.innerHTML = '';
    for (const cat of cats) {
      // A div, not a wrapping <label>: the status text beside the select
      // would otherwise become part of its name.
      const row = document.createElement('div');
      row.className = 'audio-kind-row';
      row.innerHTML = `<span><label for="audio-kind-${cat}">${names[cat] || cat}</label></span><select id="audio-kind-${cat}"><option value="">${t('Not set')}</option><option value="full">${t('Full performance — every part playing')}</option><option value="backing">${t('Backing track — the part to practise left out')}</option></select><span class="hint form-status" role="status"></span>`;
      const sel = row.querySelector('select');
      const note = row.querySelector('.form-status');
      sel.value = categories[cat].kind || '';
      // Saves on change: no form, so the outcome shows right beside it.
      sel.addEventListener('change', async () => {
        const res = await fetch(`/api/songs/${variantId}/audio/${cat}/kind`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind: sel.value }),
        }).catch(() => null);
        say(note, res && res.ok ? t('Saved.') : t("Couldn't save. Try again."), res && res.ok ? 'ok' : 'error');
        if (res && res.ok) categories[cat].kind = sel.value;
        else sel.value = categories[cat].kind || '';
      });
      kindList.appendChild(row);
    }
  }

  const origLoadMeta = loadMeta;
  loadMeta = async function () {
    await origLoadMeta();
    renderKinds();
    const hasYouTube = !!categories.youtube;
    const hasBand = !!categories.band;
    if (ytDownloadBtn) ytDownloadBtn.hidden = hasYouTube;
    if (ytReplaceBtn) ytReplaceBtn.hidden = !hasYouTube;
    if (ytDeleteBtn) ytDeleteBtn.hidden = !hasYouTube;
    if (bandUploadBtn) bandUploadBtn.hidden = hasBand;
    if (bandReplaceBtn) bandReplaceBtn.hidden = !hasBand;
    if (bandDeleteBtn) bandDeleteBtn.hidden = !hasBand;
  };
  loadMeta();

  // Beside a playlist's player (playlist.html, ?embed=playlist) the
  // recording plays in the page around this one: this one plays along,
  // muted, kept in step with it, so the chart follows the song.
  if (document.body.classList.contains('embed-playlist')) {
    window.addEventListener('message', (e) => {
      const m = e.data;
      if (e.origin !== location.origin || !m || m.type !== 'songbooks:playlist' || String(m.id) !== String(variantId)) return;
      audio.muted = true;
      audio.playbackRate = 1;
      if (m.category && categories[m.category] && activeCategory !== m.category) { selectCategory(m.category); return; }
      if (!duration) return;
      if (Math.abs(audio.currentTime - m.t) > 0.3) audio.currentTime = m.t;
      if (m.playing && audio.paused) audio.play().catch(() => {});
      if (!m.playing && !audio.paused) audio.pause();
    });
  }
})();

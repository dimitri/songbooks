// Guitar Pro import wizard (GP Import tab, play.html). Parses a .gp/
// .gp3/.gp4/.gp5/.gpx file entirely client-side, using the vendored
// alphaTab library's own importer (already shipped for web rendering —
// see alphatab-tab.js) rather than a hand-rolled Go parser: it already
// handles every GP format version, including the bend-curve/repeat/
// alternate-ending encodings a from-scratch parser would have to
// reverse-engineer from scratch. Confirmed against real files from a
// real GuitarPro lesson library this session: alphaTab.importer.
// ScoreLoader.loadScoreFromBytes() parses GP8's zip+XML container
// cleanly, exposes the embedded backing-track mp3 verbatim
// (score.backingTrack.rawAudioFile) and real per-bar audio sync
// (masterBar.syncPoints[].syncPointValue.millisecondOffset) — no manual
// zip/XML parsing needed at all.
//
// Score source written for an imported section is NOT LilyPond-subset
// text — it's the section's internal/tab.Riff encoded as plain JSON,
// decoded by the same field names Go's json package would produce (see
// tab.DecodeScoreSource/DecodeRiffJSON). A GP file already carries the
// exact fret/string a human chose for every note; re-deriving it through
// ParseScore's own assignFrets (pitch -> lowest-available-fret) could
// silently pick a different fingering than the one actually tabbed out.
//
// GP string numbering is the OPPOSITE of this app's own convention:
// GP's note.string is 1 = LOWEST (thickest) string; this app's
// Note.String is 1 = HIGHEST, matching real LilyPond/TabStaff (see
// internal/tab/tab.go's Riff.Tuning comment). Confirmed empirically this
// session (a file's very first note: GP string=1, fret=0, realValue=40
// — open low E) — every note's string is remapped on the way in
// (numStrings + 1 - gpString).
//
// A 3-step wizard (file -> align bars -> import), not a single form:
// the embedded backing track routinely starts with a count-in or
// silent bars the chart has no bar for, so "where does our bar 0
// actually start in this file" needs a real answer before anything
// saves, not a guessed number.
//
// The align step is an Audacity-3-style timeline — one continuous
// waveform with a labeled-region lane below it — not a grid of per-bar
// notation cards (an earlier version of this file did that; a direct
// review found it hard to scan and disconnected from the actual
// problem, which is a LISTENING problem, not a reading-notation one).
// Section boundaries are SHARED between adjacent sections (the end of
// one section IS the start of the next), so there's one draggable
// handle per boundary, not independent start/end handles per section —
// simpler, and it can't produce gaps or overlaps. The bar grid overlaid
// on the waveform comes from the GP file's own real per-bar sync
// points, never from audio analysis: a from-scratch energy-flux beat
// detector was prototyped and tested against a real file with known
// ground-truth bar times this session, and only matched the true grid
// on 16% of its detected onsets (worse, it reported an identical,
// clearly-bogus tempo on two unrelated songs) — nowhere near reliable
// enough to drive bar placement. GP's own sync points are real,
// human-authored-and-synced data; there's no reason to approximate them
// with an unreliable DSP guess when they're already right there.
(function () {
  const panel = document.getElementById('gpimport-panel');
  if (!panel) return;
  const variantId = panel.dataset.variantId;

  const fileInput = document.getElementById('gpimport-file');
  const status = document.getElementById('gpimport-status');
  const trackRow = document.getElementById('gpimport-track-row');
  const trackSelect = document.getElementById('gpimport-track');
  const mappingBody = document.getElementById('gpimport-mapping');
  const mappingTable = document.getElementById('gpimport-mapping-table');
  const importBtn = document.getElementById('gpimport-run');
  const includeAudio = document.getElementById('gpimport-include-audio');
  const summary = document.getElementById('gpimport-summary');

  const scrollWrap = document.getElementById('gpimport-scroll');
  const timeline = document.getElementById('gpimport-timeline');
  const waveformCanvas = document.getElementById('gpimport-waveform');
  const bargridEl = document.getElementById('gpimport-bargrid');
  const labeltrackEl = document.getElementById('gpimport-labeltrack');
  const boundariesEl = document.getElementById('gpimport-boundaries');
  const playheadEl = document.getElementById('gpimport-playhead');
  const zoomInBtn = document.getElementById('gpimport-zoom-in');
  const zoomOutBtn = document.getElementById('gpimport-zoom-out');
  const zoomLabel = document.getElementById('gpimport-zoom-label');
  const playBtn = document.getElementById('gpimport-play');
  const timeLabel = document.getElementById('gpimport-time-label');

  const steps = {
    file: document.getElementById('gpimport-step-file'),
    align: document.getElementById('gpimport-step-align'),
    import: document.getElementById('gpimport-step-import'),
  };
  const step1Next = document.getElementById('gpimport-step1-next');
  const step2Back = document.getElementById('gpimport-step2-back');
  const step2Next = document.getElementById('gpimport-step2-next');
  const step3Back = document.getElementById('gpimport-step3-back');

  function goToStep(name) {
    Object.entries(steps).forEach(([k, el]) => { el.hidden = k !== name; });
  }

  let score = null;
  let ourSections = []; // [{index, label, barCount, startBar}]
  let realIdx = [];     // real-bar-position -> GP-authored masterBar index
  let barTimes = [];    // real-bar-position -> seconds (interpolated where GP has no sync point for it)
  let audioEl = null;   // <audio>, source = a Blob URL of the embedded backing track
  let audioDuration = 0;
  let boundaries = [];  // length ourSections.length+1, each a real-bar-position (float, fractional between bars while dragging is in progress is never stored — always snapped to an integer bar)
  let pxPerSec = 20;
  const SECTION_COLORS = ['#af4e39', '#3d7a8a', '#7a5ca8', '#2f8f5b', '#b8860b', '#c0507e'];

  function setStatus(msg, cls) {
    status.textContent = msg;
    status.className = 'hint' + (cls ? ' ' + cls : '');
  }

  // ---- GP's own "real played bar count" expansion, mirroring
  // realBarCount (handlers_pages.go) on the GP side — a flat array
  // mapping each REAL (as-played) bar position to the GP-AUTHORED
  // masterBar index it plays. Repeat-free files (every file checked
  // this session) just come back as the identity mapping; a "|: :|"
  // bracket replays its bars repeatCount times (2 if unmarked); an
  // alternate ending only plays on the pass(es) its own bitmask names.
  // Not exhaustively tested against every possible multi-ending shape —
  // same pragmatic scope realBarCount itself documents on our side.
  function expandGPBars(masterBars) {
    const real = [];
    let i = 0;
    while (i < masterBars.length) {
      if (masterBars[i].isRepeatStart || !masterBars[i].isRepeatEnd) {
        let j = i;
        const openedAt = i;
        while (j < masterBars.length && !masterBars[j].isRepeatEnd) j++;
        if (j < masterBars.length && masterBars[j].isRepeatEnd) {
          const count = Math.max(2, masterBars[j].repeatCount || 2);
          for (let pass = 1; pass <= count; pass++) {
            for (let k = openedAt; k <= j; k++) {
              const mb = masterBars[k];
              if (mb.alternateEndings && !(mb.alternateEndings & (1 << (pass - 1)))) continue;
              real.push(k);
            }
          }
          i = j + 1;
          continue;
        }
      }
      real.push(i);
      i++;
    }
    return real;
  }

  // ---- Real-bar -> seconds. Two layers:
  //
  // 1. A NOMINAL timeline, walked across the real (as-played) bar
  //    sequence using the file's own tempo automations and each bar's
  //    own time signature (beats = numerator * 4/denominator, duration
  //    = beats * 60/tempo) — the same arithmetic any notation program
  //    uses to estimate playback time absent real audio analysis.
  // 2. GP's own real sync points, where present, override the nominal
  //    shape locally: interpolated between adjacent known points
  //    (captures actual human tempo drift a nominal estimate can't),
  //    and used to anchor (shift) the nominal timeline everywhere else.
  //
  // Needed because real sync-point DENSITY varies a lot file to file —
  // confirmed this session: short lesson riffs had one sync point
  // nearly every bar, but a real band's own working chart (Mottainai
  // Structure SD.gp, 45 bars) had exactly ONE. The earlier version of
  // this function only used sync points (extrapolating from the
  // nearest pair's own rate), which degenerates badly below 2 points —
  // with exactly 1, every OTHER bar silently stayed at time 0, pinning
  // the whole bar grid to the left edge. A tempo-aware nominal estimate
  // underneath fixes that for every file, not just sparse ones.
  //
  // Returns realIdx.length + 1 entries, not realIdx.length: the extra
  // synthetic entry at index realIdx.length is "one bar past the last
  // real bar," needed so the LAST boundary (the end of the last mapped
  // section) can actually reach every real bar in the file, including
  // the final one — a boundary is an exclusive end position, so without
  // this a file's last bar could never be included in any section's
  // count (confirmed live: a 13-real-bar file could only ever reach 12
  // mapped bars).
  function buildBarTimes() {
    const nominal = new Array(realIdx.length + 1).fill(0);
    let tempo = score.tempo || 120;
    let t = 0;
    for (let pos = 0; pos < realIdx.length; pos++) {
      const mb = score.masterBars[realIdx[pos]];
      if (mb.tempoAutomations && mb.tempoAutomations.length) tempo = mb.tempoAutomations[0].value || tempo;
      nominal[pos] = t;
      // A free-time bar (mb.isFreeTime — a rubato intro/outro with no
      // fixed tempo, confirmed present in a real file this session)
      // has no well-defined duration to estimate from time signature
      // and tempo alone; a 0-length guess would stack several of them
      // at the same x position on the timeline, so fall back to one
      // nominal 4-beat bar's worth of time instead — still wrong, but
      // spread out and draggable rather than invisible.
      const beats = mb.isFreeTime ? 4 : (mb.timeSignatureNumerator || 4) * (4 / (mb.timeSignatureDenominator || 4));
      t += (beats || 4) * 60 / tempo;
    }
    nominal[realIdx.length] = t;

    const known = []; // [{pos, t}]
    realIdx.forEach((gpBarIdx, pos) => {
      const sp = score.masterBars[gpBarIdx].syncPoints && score.masterBars[gpBarIdx].syncPoints[0];
      if (sp) known.push({ pos, t: sp.syncPointValue.millisecondOffset / 1000 });
    });
    if (known.length === 0) return nominal;

    const times = nominal.slice();
    if (known.length === 1) {
      const offset = known[0].t - nominal[known[0].pos];
      for (let pos = 0; pos <= realIdx.length; pos++) times[pos] = nominal[pos] + offset;
      return times;
    }
    for (let i = 0; i < known.length - 1; i++) {
      const a = known[i], b = known[i + 1];
      for (let pos = a.pos; pos <= b.pos; pos++) {
        const frac = b.pos > a.pos ? (pos - a.pos) / (b.pos - a.pos) : 0;
        times[pos] = a.t + frac * (b.t - a.t);
      }
    }
    // Before the first / after the last known point: shift the nominal
    // timeline's own shape so it passes exactly through that point,
    // rather than extrapolating a single borrowed interval's rate.
    const offsetStart = known[0].t - nominal[known[0].pos];
    for (let pos = 0; pos < known[0].pos; pos++) times[pos] = nominal[pos] + offsetStart;
    const lastK = known[known.length - 1];
    const offsetEnd = lastK.t - nominal[lastK.pos];
    for (let pos = lastK.pos + 1; pos <= realIdx.length; pos++) times[pos] = nominal[pos] + offsetEnd;
    return times;
  }

  function nearestBarForTime(t) {
    let lo = 0, hi = barTimes.length - 1;
    if (t <= barTimes[0]) return 0;
    if (t >= barTimes[hi]) return hi;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (barTimes[mid] < t) lo = mid + 1; else hi = mid;
    }
    if (lo > 0 && Math.abs(barTimes[lo - 1] - t) < Math.abs(barTimes[lo] - t)) return lo - 1;
    return lo;
  }

  async function loadFile(file) {
    setStatus('parsing…');
    try {
      const buf = new Uint8Array(await file.arrayBuffer());
      score = alphaTab.importer.ScoreLoader.loadScoreFromBytes(buf);
    } catch (e) {
      setStatus('could not parse this file: ' + e.message, 'err');
      score = null;
      return;
    }
    trackSelect.innerHTML = '';
    score.tracks.forEach((t, i) => {
      const opt = document.createElement('option');
      opt.value = i;
      opt.textContent = t.name || `Track ${i + 1}`;
      trackSelect.appendChild(opt);
    });
    trackRow.hidden = score.tracks.length < 2;
    realIdx = expandGPBars(score.masterBars);
    barTimes = buildBarTimes();
    step1Next.disabled = false;
    const res = await fetch(`/api/songs/${variantId}/structure`);
    if (!res.ok) { setStatus('could not load this song\'s sections', 'err'); return; }
    const data = await res.json();
    ourSections = (data.sections || []).filter(s => s.barCount > 0);
    boundaries = [0];
    ourSections.forEach(sec => boundaries.push(Math.min(realIdx.length, boundaries[boundaries.length - 1] + sec.barCount)));
    setStatus(`parsed "${score.title || file.name}" — ${score.tracks.length} track(s), ${realIdx.length} real bar(s), ${ourSections.length} section(s) to map.`, 'ok');
  }

  trackSelect.addEventListener('change', () => { if (steps.align.hidden === false) prepareTimeline(); });
  fileInput.addEventListener('change', () => { if (fileInput.files[0]) loadFile(fileInput.files[0]); });

  // ---- Timeline setup: decode the embedded backing track once, build
  // an <audio> element from it for playback/seeking, then lay out.
  async function prepareTimeline() {
    if (!score) return;
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) {
      scrollWrap.hidden = true;
      setStatus('This file has no embedded backing track — nothing to align against. Sections will map in their own default order; the import still writes note data for each, just without audio sync.');
      renderMappingTableOnly();
      return;
    }
    scrollWrap.hidden = false;
    if (!audioEl) {
      const bytes = score.backingTrack.rawAudioFile;
      const blob = new Blob([bytes], { type: 'audio/mpeg' });
      audioEl = new Audio(URL.createObjectURL(blob));
      audioEl.addEventListener('loadedmetadata', () => { audioDuration = audioEl.duration; layout(); });
      audioEl.addEventListener('timeupdate', () => {
        playheadEl.style.left = (audioEl.currentTime * pxPerSec) + 'px';
        timeLabel.textContent = `${fmtTime(audioEl.currentTime)} / ${fmtTime(audioDuration)}`;
      });
      audioEl.addEventListener('ended', () => { playBtn.textContent = '▶ Play'; });
    }
    layout();
  }

  function fmtTime(t) {
    if (!isFinite(t)) return '0:00';
    const m = Math.floor(t / 60), s = Math.floor(t % 60);
    return `${m}:${String(s).padStart(2, '0')}`;
  }

  function drawWaveform() {
    const ctx = waveformCanvas.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    const w = Math.max(timeline.clientWidth, 1), h = waveformCanvas.clientHeight;
    waveformCanvas.width = w * dpr; waveformCanvas.height = h * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = '#9fe870';
    const mid = h / 2;
    // Real peak data when decodeAudioData has run (best-effort, kept
    // simple — a flat silhouette if decoding hasn't resolved yet is a
    // harmless placeholder, not a correctness issue).
    const peaks = waveformCanvas._peaks;
    if (peaks) {
      for (let x = 0; x < w; x++) {
        const i = Math.floor((x / w) * peaks.length);
        const amp = (peaks[i] || 0) * mid * 0.95;
        ctx.fillRect(x, mid - amp, 1, amp * 2);
      }
    }
  }

  async function decodePeaks() {
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) return;
    try {
      const bytes = score.backingTrack.rawAudioFile;
      const ctx = new (window.AudioContext || window.webkitAudioContext)();
      const ab = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
      const audioBuffer = await ctx.decodeAudioData(ab);
      ctx.close();
      const channel = audioBuffer.getChannelData(0);
      const buckets = 2000;
      const perBucket = Math.floor(channel.length / buckets) || 1;
      const peaks = new Float32Array(buckets);
      for (let i = 0; i < buckets; i++) {
        let max = 0;
        const start = i * perBucket, end = Math.min(start + perBucket, channel.length);
        for (let j = start; j < end; j++) { const v = Math.abs(channel[j]); if (v > max) max = v; }
        peaks[i] = max;
      }
      waveformCanvas._peaks = peaks;
      drawWaveform();
    } catch (e) { /* leave the flat placeholder — playback/alignment still work without a waveform picture */ }
  }

  // ---- Bar grid: real per-bar ticks when zoomed in enough to read
  // them, else one collapsed block per section (the chart's own
  // "multi-rest" idea, applied to the timeline instead of the staff).
  const TICK_THRESHOLD_PX = 10; // below this px-per-bar, stop drawing individual ticks
  function renderBarGrid() {
    bargridEl.innerHTML = '';
    if (!barTimes.length) return;
    const avgPxPerBar = (barTimes[barTimes.length - 1] - barTimes[0]) / barTimes.length * pxPerSec;
    if (avgPxPerBar >= TICK_THRESHOLD_PX) {
      realIdx.forEach((gpBarIdx, pos) => {
        const x = barTimes[pos] * pxPerSec;
        const mb = score.masterBars[gpBarIdx];
        const line = document.createElement('div');
        line.className = 'beat1';
        line.style.left = x + 'px';
        bargridEl.appendChild(line);
        const num = document.createElement('div');
        num.className = 'barnum';
        num.style.left = (x + 2) + 'px';
        num.textContent = pos;
        bargridEl.appendChild(num);
      });
    } else {
      // Collapsed: one tinted block per CURRENT section mapping (not
      // per original GP bar) so the zoomed-out view already shows the
      // boundaries the user is placing, not an unrelated GP grouping.
      for (let i = 0; i < boundaries.length - 1; i++) {
        const block = document.createElement('div');
        block.className = 'collapsed-block';
        block.style.left = (barTimes[boundaries[i]] * pxPerSec) + 'px';
        block.style.width = ((barTimes[boundaries[i + 1]] - barTimes[boundaries[i]]) * pxPerSec) + 'px';
        bargridEl.appendChild(block);
      }
    }
  }

  function sectionBarCount(i) { return boundaries[i + 1] - boundaries[i]; }

  function renderLabelTrack() {
    labeltrackEl.innerHTML = '';
    ourSections.forEach((sec, i) => {
      const x0 = barTimes[boundaries[i]] * pxPerSec;
      const x1 = barTimes[boundaries[i + 1]] * pxPerSec;
      const el = document.createElement('div');
      const mapped = sectionBarCount(i);
      el.className = 'gpimport-section-block' + (mapped !== sec.barCount ? ' mismatch' : '');
      el.style.left = x0 + 'px';
      el.style.width = Math.max(2, x1 - x0) + 'px';
      el.style.background = mapped === sec.barCount ? '' : '';
      el.textContent = `${sec.label} (${mapped}/${sec.barCount} bars)`;
      el.title = mapped === sec.barCount ? 'Matches this section\'s real bar count' : `This section needs ${sec.barCount} bars — currently mapped to ${mapped}`;
      labeltrackEl.appendChild(el);
    });
  }

  let draggingBoundary = -1;
  function renderBoundaries() {
    boundariesEl.innerHTML = '';
    boundaries.forEach((pos, i) => {
      const handle = document.createElement('div');
      handle.className = 'gpimport-boundary' + (i === draggingBoundary ? ' dragging' : '');
      handle.style.left = (barTimes[pos] * pxPerSec) + 'px';
      handle.title = i === 0 ? 'Drag past any count-in/lead-in — this is the song\'s bar 0'
        : i === boundaries.length - 1 ? 'End of the last mapped section'
        : `Boundary between "${ourSections[i - 1].label}" and "${ourSections[i].label}"`;
      handle.addEventListener('mousedown', ev => startDrag(ev, i));
      boundariesEl.appendChild(handle);
    });
  }

  function startDrag(ev, i) {
    ev.preventDefault();
    draggingBoundary = i;
    renderBoundaries();
    const onMove = e => {
      const rect = timeline.getBoundingClientRect();
      const x = e.clientX - rect.left + scrollWrap.scrollLeft;
      const t = Math.max(0, x / pxPerSec);
      let pos = nearestBarForTime(t);
      const lo = i === 0 ? 0 : boundaries[i - 1] + 1;
      const hi = i === boundaries.length - 1 ? realIdx.length : boundaries[i + 1] - 1;
      pos = Math.max(lo, Math.min(hi, pos));
      boundaries[i] = pos;
      renderBarGrid();
      renderLabelTrack();
      renderBoundaries();
    };
    const onUp = () => {
      draggingBoundary = -1;
      renderBoundaries();
      renderMappingTableOnly();
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }

  timeline.addEventListener('click', e => {
    if (e.target.closest('.gpimport-boundary') || !audioEl) return;
    const rect = timeline.getBoundingClientRect();
    const x = e.clientX - rect.left + scrollWrap.scrollLeft;
    audioEl.currentTime = Math.max(0, x / pxPerSec);
  });

  function layout() {
    const duration = audioDuration || (barTimes[barTimes.length - 1] || 60) + 5;
    const widthPx = duration * pxPerSec;
    timeline.style.width = widthPx + 'px';
    zoomLabel.textContent = Math.round(pxPerSec / 20 * 100) + '%';
    drawWaveform();
    renderBarGrid();
    renderLabelTrack();
    renderBoundaries();
    renderMappingTableOnly();
  }

  zoomInBtn.addEventListener('click', () => { pxPerSec = Math.min(300, pxPerSec * 1.6); layout(); });
  zoomOutBtn.addEventListener('click', () => { pxPerSec = Math.max(2, pxPerSec / 1.6); layout(); });
  playBtn.addEventListener('click', () => {
    if (!audioEl) return;
    if (audioEl.paused) { audioEl.play(); playBtn.textContent = '❚❚ Pause'; }
    else { audioEl.pause(); playBtn.textContent = '▶ Play'; }
  });

  function renderMappingTableOnly() {
    if (!ourSections.length) { mappingTable.hidden = true; return; }
    mappingBody.innerHTML = '';
    ourSections.forEach((sec, i) => {
      const mapped = sectionBarCount(i);
      const tr = document.createElement('tr');
      tr.style.borderLeft = `4px solid ${SECTION_COLORS[i % SECTION_COLORS.length]}`;
      tr.innerHTML = `
        <td><input type="checkbox" class="gpimport-include" checked></td>
        <td>${sec.label}</td>
        <td>${sec.barCount}</td>
        <td${mapped !== sec.barCount ? ' style="color:#b8860b;font-weight:600"' : ''}>${mapped}</td>
        <td></td>`;
      mappingBody.appendChild(tr);
    });
    mappingTable.hidden = false;
  }

  // ---- Per-section extraction: GP real-bar range -> our Riff JSON ----
  function extractRiff(track, realBarStart, barCount) {
    const staff = track.staves[0];
    const numStrings = staff.tuning.length;
    const bars = [];
    for (let r = realBarStart; r < realBarStart + barCount; r++) {
      const gpBarIndex = realIdx[r];
      if (gpBarIndex === undefined) break;
      const gpBar = staff.bars[gpBarIndex];
      const voice = gpBar.voices[0];
      const isSoleRest = voice && voice.beats.length === 1 && voice.beats[0].isRest;
      const beats = (voice ? voice.beats : []).map(b => {
        // A bar whose ONLY event is a rest is, by definition, a full
        // bar of silence — but real GP files routinely encode that rest
        // with whatever short duration (often a quarter) regardless of
        // the actual time signature, since it doesn't affect playback
        // and GP's own alphaTab-side isFullBarRest only special-cases a
        // LITERAL Whole-duration rest (confirmed against the vendored
        // bundle: isFullBarRest = isRest && 1 beat && duration===Whole).
        // Taking the encoded duration at face value here would print a
        // single quarter-rest glyph sitting in an otherwise-empty bar
        // instead of the full-bar rest mark the bar obviously means —
        // confirmed live against a real imported file (bars with
        // duration=4/dots=0, isFullBarRest false, in a 4/4 bar that's
        // musically just "rest here").
        if (b.isRest) return { Rest: true, Duration: isSoleRest ? '1' : String(b.duration) + '.'.repeat(b.dots) };
        const notes = b.notes.map(n => ({
          MIDI: n.realValue,
          String: numStrings + 1 - n.string,
          Fret: n.fret,
          Tied: !!n.isTieDestination,
        }));
        const beat = { Duration: String(b.duration) + '.'.repeat(b.dots), Notes: notes };
        if (b.notes.some(n => n.isHammerPullOrigin)) beat.HammerPull = true;
        if (b.notes.some(n => n.isPalmMute)) beat.PalmMute = true;
        if (b.notes.length === 1 && b.notes[0].slideOutType) beat._slideOut = true;
        const bendNote = b.notes.find(n => n.hasBend && n.bendPoints && n.bendPoints.length > 1);
        if (bendNote) {
          const pts = bendNote.bendPoints;
          const maxVal = Math.max(...pts.map(p => p.value));
          const lastVal = pts[pts.length - 1].value;
          // alphaTab's BendPoint.value is documented in quarter-tone
          // units (1 = a quarter tone = 0.5 semitone) — not independently
          // re-verified against a known "full step" example this
          // session; flagged here so a visibly-wrong bend width on a
          // real import is easy to trace back to this one conversion.
          beat.BendSemitones = maxVal * 0.5;
          if (lastVal < maxVal) beat.BendRelease = true;
        }
        return beat;
      });
      bars.push({ Beats: beats });
    }
    for (let bi = 0; bi < bars.length; bi++) {
      for (let i = 0; i < bars[bi].Beats.length; i++) {
        const prev = i > 0 ? bars[bi].Beats[i - 1] : (bi > 0 ? bars[bi - 1].Beats[bars[bi - 1].Beats.length - 1] : null);
        if (prev && prev.Notes && prev.Notes.length === 1 && prev._slideOut) {
          bars[bi].Beats[i].HasSlideFrom = true;
          bars[bi].Beats[i].SlideFromFret = prev.Notes[0].Fret;
        }
      }
    }
    for (const bar of bars) for (const beat of bar.Beats) delete beat._slideOut;
    return { Tuning: staff.tuning.slice(), MinimumFret: 0, Bars: bars };
  }

  async function putScore(sectionIndex, riff) {
    const res = await fetch(`/api/songs/${variantId}/scores/${sectionIndex}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ lySource: JSON.stringify(riff) }),
    });
    if (!res.ok) throw new Error(`section ${sectionIndex}: ` + (await res.text()));
  }

  // Sync points are keyed on the SAME boundaries the score import just
  // used — not a separately-tracked global shift (the earlier version
  // of this file had exactly that bug: a shift field decoupled from
  // whatever a user had manually marked, uploading audio sync for a
  // different bar mapping than the one actually saved).
  async function importAudioAndSync() {
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) return;
    const bytes = score.backingTrack.rawAudioFile;
    const form = new FormData();
    form.append('file', new Blob([bytes], { type: 'audio/mpeg' }), 'gp-backing-track.mp3');
    const upRes = await fetch(`/api/songs/${variantId}/audio/gp`, { method: 'PUT', body: form });
    if (!upRes.ok) throw new Error('audio upload failed: ' + (await upRes.text()));

    const points = [];
    for (let pos = boundaries[0]; pos < realIdx.length; pos++) {
      const ourBar = pos - boundaries[0];
      points.push({ barIndex: ourBar, timeSeconds: barTimes[pos] });
    }
    if (points.length) {
      const spRes = await fetch(`/api/songs/${variantId}/audio/gp/sync-points`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ points }),
      });
      if (!spRes.ok) throw new Error('sync points failed: ' + (await spRes.text()));
    }
  }

  step1Next.addEventListener('click', async () => {
    goToStep('align');
    await prepareTimeline();
    decodePeaks();
  });
  step2Back.addEventListener('click', () => { if (audioEl) audioEl.pause(); goToStep('file'); });
  step2Next.addEventListener('click', () => {
    if (audioEl) audioEl.pause();
    const rows = [...mappingBody.querySelectorAll('tr')].filter(tr => tr.querySelector('.gpimport-include').checked);
    summary.textContent = rows.length
      ? `Importing ${rows.length} section(s): ${rows.map(tr => tr.children[1].textContent).join(', ')}.`
      : 'No sections selected — nothing will be imported.';
    goToStep('import');
  });
  step3Back.addEventListener('click', () => goToStep('align'));

  importBtn.addEventListener('click', async () => {
    if (!score) { setStatus('parse a file first', 'err'); return; }
    const track = score.tracks[Number(trackSelect.value) || 0];
    const checks = [...mappingBody.querySelectorAll('.gpimport-include')];
    importBtn.disabled = true;
    try {
      for (let i = 0; i < ourSections.length; i++) {
        if (!checks[i] || !checks[i].checked) continue;
        const sec = ourSections[i];
        setStatus(`importing section ${i + 1}/${ourSections.length}: ${sec.label}…`);
        const riff = extractRiff(track, boundaries[i], sectionBarCount(i));
        await putScore(sec.index, riff);
      }
      if (includeAudio.checked) {
        setStatus('uploading backing track + sync points…');
        await importAudioAndSync();
      }
      setStatus('imported. Reload the Chart tab to see it — each section still needs a "score:" line in Source pointing at it.', 'ok');
    } catch (e) {
      setStatus('import failed: ' + e.message, 'err');
    } finally {
      importBtn.disabled = false;
    }
  });
})();

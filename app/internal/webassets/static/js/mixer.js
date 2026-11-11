// Mixer: a DAW-style multitrack timeline (layout after Audacity 4 /
// GarageBand). One horizontal TIME axis, in seconds of the reference
// recording, shared by every row:
//
//   ruler (bars/beats) · arrangement (sections) · chords
//   reference recording (real audio only: Guitar Pro backing track,
//     YouTube download, band recording — never a MIDI render)
//   one lane per score track (alphaTab notation, played by AlphaSynth)
//
// barTimes[b] maps performed bar b to seconds in the recording: the Guitar
// Pro bar sync points when the reference is the GP recording, otherwise
// the per-section markers plus the song's tempo. Each score bar is drawn
// exactly (barTimes[b+1]-barTimes[b]) * pxPerSec wide (alphaTab's
// UseModelLayout + masterBar.displayWidth), so notation sits under the
// audio it belongs to. One transport plays everything: the recording is
// the master clock and AlphaSynth follows it bar by bar (speed matched to
// each bar's real duration, re-seeked on drift), so a muted recording
// still drives the score in time, and mute/solo work across both.
(function () {
  const page = document.getElementById('mixer-page');
  if (!page) return;
  const $ = (id) => document.getElementById(id);
  const variantId = page.dataset.variantId;
  const canEdit = page.dataset.canEdit === 'true';
  const dawEl = $('daw');
  const headersEl = $('daw-headers');
  const scrollEl = $('daw-scroll');
  const contentEl = $('daw-content');
  const statusEl = $('daw-status');
  const playBtn = $('daw-play');
  const lcdBar = $('daw-lcd-bar');
  const lcdTime = $('daw-lcd-time');
  const zoomInput = $('daw-zoom');
  const alignBtn = $('daw-align');
  const alignBar = $('daw-align-bar');
  const alignStatus = $('daw-align-status');

  const ICON_PLAY = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M6 4.5v11l9-5.5-9-5.5z"/></svg>';
  const ICON_PAUSE = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.4" height="12" rx="1"/><rect x="11.6" y="4" width="3.4" height="12" rx="1"/></svg>';
  const ICON_WAVE = '<svg class="daw-hdr-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><path d="M2 10h2M5 6v8M8 3v14M11 7v6M14 5v10M17 9v2"/></svg>';
  const ICON_NOTE = '<svg class="daw-hdr-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M8 15V4l9-2v11"/><circle cx="5.5" cy="15" r="2.5"/><circle cx="14.5" cy="13" r="2.5"/></svg>';
  const ICON_DRUM = '<svg class="daw-hdr-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><ellipse cx="10" cy="8" rx="7" ry="3"/><path d="M3 8v5c0 1.7 3.1 3 7 3s7-1.3 7-3V8M5 3l4 4M15 3l-4 4"/></svg>';
  const HEAD_SVG = '<svg class="daw-playhead-head" viewBox="0 0 13 16" aria-hidden="true"><path d="M0.5 0.5h12v9l-6 6-6-6z" fill="#fff" stroke="#000"/></svg>';

  const COLOR = { audio: '#66A3FF', melodic: '#74BE59', drums: '#E8C050' };
  const BODY = { melodic: '#E4F3DC', drums: '#FAF1D2' };
  const GM = { 0: 'Piano', 4: 'Electric piano', 16: 'Organ', 24: 'Nylon guitar', 25: 'Steel guitar', 26: 'Jazz guitar', 27: 'Clean guitar', 28: 'Muted guitar', 29: 'Overdrive guitar', 30: 'Distortion guitar', 32: 'Acoustic bass', 33: 'Finger bass', 34: 'Pick bass', 35: 'Fretless bass', 36: 'Slap bass', 48: 'Strings', 56: 'Trumpet', 65: 'Alto sax', 66: 'Tenor sax', 73: 'Flute', '-1': 'Drum kit' };
  const CAT_LABEL = { gp: 'Guitar Pro recording', youtube: 'YouTube', band: 'Band recording' };
  const PAD = 16;               // px before t = 0
  const ROW = { ruler: 40, sections: 30, chords: 30, audio: 116 };

  function sectionColor(label) {
    const l = label.toLowerCase();
    if (/pre.?chorus|pré/.test(l)) return '#DA8CCC';
    if (/solo/.test(l)) return '#E8C050';
    if (/chorus|refrain/.test(l)) return '#FF9E65';
    if (/verse|couplet/.test(l)) return '#9996FC';
    if (/bridge|pont/.test(l)) return '#F08080';
    if (/intro|pickup|outro|coda|end|fin/.test(l)) return '#34B494';
    return '#48BECF';
  }

  // ---- State ----------------------------------------------------------
  let data = null;              // GET /api/songs/{id}/mixer
  let audioCats = {};           // category -> meta from GET /api/songs/{id}/audio
  let refCat = null;
  let audio = null;
  let audioDuration = 0;
  let peaks = null;             // {min, max: Float32Array, perSec}
  let barTimes = [];            // totalBars + 1 entries, seconds
  let realBars = new Set();     // bars whose time is authored (sync point / marker), not estimated
  let pxPerSec = 110;           // default zoom: about a bar per 230px at 113 bpm, legible notation
  let tracks = [];              // rows that make sound: {kind, name, color, muted, solo, volume, ...}
  let playerApi = null;
  let synthReady = false;
  let synthPlaying = false;
  let synthGuardUntil = 0;
  let playing = false;
  let ticker = null;            // transport timer while playing (rAF stalls in background tabs)
  let position = 0;
  let playStartPos = 0;
  let speed = 1;
  let loop = false;
  let selected = -1;            // index into data.sections
  let aligning = false;
  let built = false;
  let playheadEl = null;
  let headEl = null;
  let waveCanvas = null;
  let rows = {};                // name -> {hdr, lane}

  const N = () => (data ? data.totalBars : 0);
  const beatsPerBar = () => (data ? data.timeNum : 4);
  const nominalBarSec = () => (data ? data.timeNum * (4 / data.timeDen) * 60 / (data.tempo || 120) : 2);
  const ticksPerBar = () => (data ? 960 * 4 * data.timeNum / data.timeDen : 3840);
  // A pickup (anacrusis) makes bar 0 shorter than the others: its length
  // in quarter-note beats comes from the score (handleGetSongMixer).
  const pickup = () => (data && data.pickupBeats > 0 ? data.pickupBeats : 0);
  const barNomSec = (b) => (b === 0 && pickup() ? pickup() * 60 / (data.tempo || 120) : nominalBarSec());
  const beatsIn = (b) => (b === 0 && pickup() ? Math.max(1, Math.round(pickup())) : beatsPerBar());
  const barLabel = (b) => String(pickup() ? b : b + 1); // full bars count from 1, a pickup is bar 0
  const barStartTick = (b) => (b <= 0 ? 0 : (pickup() ? pickup() * 960 + (b - 1) * ticksPerBar() : b * ticksPerBar()));
  const barLenTicks = (b) => (b === 0 && pickup() ? pickup() * 960 : ticksPerBar());
  const tickAtBar = (pos) => { const i = Math.floor(pos); return barStartTick(i) + (pos - i) * barLenTicks(i); };
  function barAtTick(tick) {
    if (pickup() && tick < pickup() * 960) return tick / (pickup() * 960);
    const rest = pickup() ? tick - pickup() * 960 : tick;
    return (pickup() ? 1 : 0) + rest / ticksPerBar();
  }
  const xOf = (t) => PAD + t * pxPerSec;
  const tOf = (px) => (px - PAD) / pxPerSec;
  const hasAudio = () => !!(audio && audioDuration);
  const timelineEnd = () => Math.max(audioDuration || 0, barTimes[N()] || 0) + 2;

  function fmtTime(t) {
    const sign = t < 0 ? '-' : '';
    t = Math.abs(t);
    const m = Math.floor(t / 60);
    const s = t - m * 60;
    return `${sign}${m}:${s.toFixed(1).padStart(4, '0')}`;
  }

  // ---- Bar <-> time map ------------------------------------------------
  // From known (bar, time) pairs: between two of them, bars keep their
  // nominal proportions (a pickup stays short) stretched to fit; outside
  // them, the song's nominal tempo; bar 0 at 0s when nothing is known.
  function interpolateBars(known) {
    const n = N();
    const nom = [0];
    for (let b = 0; b < n; b++) nom.push(nom[b] + barNomSec(b));
    const out = new Array(n + 1);
    known = known.filter((k) => k.bar >= 0 && k.bar <= n).sort((a, b) => a.bar - b.bar);
    if (!known.length) known = [{ bar: 0, t: 0 }];
    let ki = 0;
    for (let b = 0; b <= n; b++) {
      while (ki < known.length - 1 && known[ki + 1].bar <= b) ki++;
      const a = known[ki];
      const c = known.find((k) => k.bar > b);
      if (a.bar === b) out[b] = a.t;
      else if (a.bar < b && c) out[b] = a.t + (nom[b] - nom[a.bar]) / (nom[c.bar] - nom[a.bar]) * (c.t - a.t);
      else if (a.bar < b) out[b] = a.t + (nom[b] - nom[a.bar]);
      else out[b] = a.t - (nom[a.bar] - nom[b]);
    }
    return out;
  }

  function computeBarTimes() {
    realBars = new Set();
    const meta = refCat ? audioCats[refCat] : null;
    let known = [];
    if (meta && meta.barSyncPoints && meta.barSyncPoints.length) {
      known = meta.barSyncPoints.map((p) => ({ bar: p.barIndex, t: p.timeSeconds }));
    } else if (meta && meta.markers) {
      for (const m of meta.markers) {
        const sec = data.sections.find((s) => s.index === m.sectionIndex);
        if (sec) known.push({ bar: sec.startBar, t: m.timeSeconds });
      }
    }
    known.forEach((k) => realBars.add(k.bar));
    barTimes = interpolateBars(known);
  }

  function barAt(t) {
    const n = N();
    if (!n) return 0;
    if (t < barTimes[0]) return (t - barTimes[0]) / nominalBarSec();
    if (t >= barTimes[n]) return n + (t - barTimes[n]) / nominalBarSec();
    let lo = 0, hi = n;
    while (hi - lo > 1) {
      const mid = (lo + hi) >> 1;
      if (barTimes[mid] <= t) lo = mid; else hi = mid;
    }
    return lo + (t - barTimes[lo]) / (barTimes[lo + 1] - barTimes[lo]);
  }

  function timeAt(bar) {
    const n = N();
    if (bar <= 0) return barTimes[0] + bar * nominalBarSec();
    if (bar >= n) return barTimes[n] + (bar - n) * nominalBarSec();
    const i = Math.floor(bar);
    return barTimes[i] + (bar - i) * (barTimes[i + 1] - barTimes[i]);
  }

  const sectionSpan = (s) => [barTimes[s.startBar], barTimes[Math.min(N(), s.startBar + s.barCount)]];

  // ---- Mix (mute / solo / volume) --------------------------------------
  const audible = (tr) => !tr.muted && (!tracks.some((t) => t.solo) || tr.solo);
  const synthWanted = () => tracks.some((t) => t.kind === 'midi' && audible(t));

  function applyMix() {
    for (const tr of tracks) {
      tr.lane.classList.toggle('is-dimmed', !audible(tr));
      if (tr.kind === 'audio' && audio) {
        audio.muted = !audible(tr);
        audio.volume = Math.min(1, tr.volume);
      }
      if (tr.kind === 'midi' && playerApi && playerApi.score) {
        const st = playerApi.score.tracks[tr.index];
        if (st) {
          playerApi.changeTrackMute([st], !audible(tr));
          playerApi.changeTrackVolume([st], tr.volume);
        }
      }
    }
    if (playing && !synthWanted() && synthPlaying) playerApi.pause();
  }

  // ---- Rows ------------------------------------------------------------
  function addRow(name, height, hdrHTML, opts = {}) {
    const hdr = document.createElement('div');
    hdr.className = 'daw-hdr' + (opts.meta ? ' is-meta' : '');
    hdr.style.height = height + 'px';
    hdr.innerHTML = hdrHTML;
    const lane = document.createElement('div');
    lane.className = 'daw-lane' + (opts.laneClass ? ' ' + opts.laneClass : '');
    lane.style.height = height + 'px';
    headersEl.appendChild(hdr);
    contentEl.appendChild(lane);
    rows[name] = { hdr, lane };
    return rows[name];
  }

  function setRowHeight(name, h) {
    rows[name].hdr.style.height = h + 'px';
    rows[name].lane.style.height = h + 'px';
  }

  function trackHeader(tr, sub, extra) {
    return `<span class="daw-hdr-stripe" style="background:${tr.color}"></span>
      <div class="daw-hdr-body">
        <div class="daw-hdr-top">${tr.icon}<span class="daw-hdr-name" title="${escapeHTML(tr.name)}">${escapeHTML(tr.name)}</span>
          <button type="button" class="daw-ms is-mute" aria-pressed="false" title="mute">M</button>
          <button type="button" class="daw-ms is-solo" aria-pressed="false" title="solo">S</button></div>
        ${extra || ''}
        <div class="daw-hdr-sub">${sub}</div>
        <div class="daw-hdr-row"><input type="range" class="daw-vol" min="0" max="${tr.kind === 'audio' ? 1 : 2}" step="0.01" value="1" aria-label="volume"><span class="daw-vol-label">100%</span></div>
      </div>`;
  }

  function wireTrackHeader(tr) {
    const hdr = tr.hdr;
    const mute = hdr.querySelector('.is-mute');
    const solo = hdr.querySelector('.is-solo');
    mute.onclick = () => { tr.muted = !tr.muted; mute.setAttribute('aria-pressed', tr.muted); applyMix(); };
    solo.onclick = () => { tr.solo = !tr.solo; solo.setAttribute('aria-pressed', tr.solo); applyMix(); };
    const vol = hdr.querySelector('.daw-vol');
    const lbl = hdr.querySelector('.daw-vol-label');
    vol.oninput = () => { tr.volume = Number(vol.value); lbl.textContent = Math.round(tr.volume * 100) + '%'; applyMix(); };
  }

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  function build() {
    headersEl.innerHTML = '';
    contentEl.innerHTML = '';
    rows = {};
    tracks = [];

    addRow('ruler', ROW.ruler, '<span>Bars</span>', { meta: true, laneClass: 'is-ruler' });
    addRow('sections', ROW.sections, '<span>Sections</span>', { meta: true, laneClass: 'is-meta' });
    addRow('chords', ROW.chords, '<span>Chords</span>', { meta: true, laneClass: 'is-meta' });

    if (refCat) {
      const tr = { kind: 'audio', name: 'Reference', color: COLOR.audio, icon: ICON_WAVE, muted: false, solo: false, volume: 1 };
      const cats = Object.keys(audioCats);
      const select = cats.length > 1
        ? `<select class="daw-ref-select" aria-label="reference recording">${cats.map((c) => `<option value="${c}"${c === refCat ? ' selected' : ''}>${CAT_LABEL[c] || c}</option>`).join('')}</select>`
        : '';
      const row = addRow('audio', ROW.audio, trackHeader(tr, refSubtitle(), select));
      tr.hdr = row.hdr; tr.lane = row.lane;
      tracks.push(tr);
      wireTrackHeader(tr);
      row.hdr.querySelector('.daw-ref-select')?.addEventListener('change', (e) => switchReference(e.target.value));
    }

    (data.tracks || []).forEach((t, i) => {
      const kind = t.drums ? 'drums' : 'melodic';
      const tr = { kind: 'midi', index: i, name: t.name, drums: t.drums, color: COLOR[kind], body: BODY[kind], icon: t.drums ? ICON_DRUM : ICON_NOTE, muted: false, solo: false, volume: 1 };
      const row = addRow('midi' + i, 96, trackHeader(tr, `${GM[t.program] || 'MIDI'} · score (MIDI)`));
      tr.hdr = row.hdr; tr.lane = row.lane;
      tracks.push(tr);
      wireTrackHeader(tr);
    });

    if (!refCat && !(data.tracks || []).length) {
      const row = addRow('empty', 64, '<span>Tracks</span>', { meta: true });
      row.lane.innerHTML = '<span class="daw-lane-empty">No reference recording and no score yet — add a recording from the Reference tab, or import a Guitar Pro file.</span>';
    }

    playheadEl = document.createElement('div');
    playheadEl.className = 'daw-playhead';
    contentEl.appendChild(playheadEl);
    rows.ruler.lane.insertAdjacentHTML('beforeend', HEAD_SVG);
    headEl = rows.ruler.lane.querySelector('.daw-playhead-head');

    wireLaneSeeking();
    layout();
    built = true;
  }

  function refSubtitle() {
    const meta = audioCats[refCat];
    const dur = audioDuration ? ' · ' + fmtTime(audioDuration).replace(/\.\d$/, '') : '';
    let sync = 'not aligned yet';
    if (meta && meta.barSyncPoints && meta.barSyncPoints.length) sync = `aligned · ${meta.barSyncPoints.length} bar sync points`;
    else if (meta && meta.markers && meta.markers.length) sync = `aligned · ${meta.markers.length} section marker${meta.markers.length > 1 ? 's' : ''}`;
    return `${CAT_LABEL[refCat] || refCat}${dur} · ${sync}`;
  }

  // ---- Layout (everything positioned from barTimes + pxPerSec) ---------
  function layout(skipNotation) {
    const width = Math.ceil(xOf(timelineEnd()) + PAD);
    contentEl.style.width = width + 'px';
    renderRuler();
    renderSections();
    renderChords();
    renderGrid();
    renderAudioLane();
    if (!skipNotation) renderNotationLanes(true);
    else {
      tracks.forEach(placeNotation);
      positionNotationClips();
    }
    updatePlayhead();
  }

  function renderRuler() {
    const lane = rows.ruler.lane;
    lane.querySelectorAll('.daw-tick, .daw-tick-label, .daw-loop-region').forEach((e) => e.remove());
    const n = N();
    const frag = document.createDocumentFragment();
    const avgBarPx = n ? (barTimes[n] - barTimes[0]) / n * pxPerSec : 100;
    const labelEvery = avgBarPx >= 34 ? 1 : avgBarPx >= 17 ? 2 : avgBarPx >= 8 ? 4 : 8;
    for (let b = 0; b <= n; b++) {
      const x = xOf(barTimes[b]);
      frag.appendChild(tick(x, 'is-bar'));
      if (b < n && b % labelEvery === 0 && !(b === 0 && pickup())) frag.appendChild(label(x, barLabel(b)));
      if (b < n && avgBarPx / beatsPerBar() >= 9) {
        for (let k = 1; k < beatsIn(b); k++) frag.appendChild(tick(xOf(barTimes[b] + k / beatsIn(b) * (barTimes[b + 1] - barTimes[b])), 'is-beat'));
      }
    }
    // Time labels across the lead-in before bar 1 (a recording's count-in,
    // talk, tuning…), where there are no bars to number.
    if (barTimes[0] > 0) {
      const step = pxPerSec >= 60 ? 5 : 10;
      for (let t = 0; t < barTimes[0] - 1; t += step) {
        frag.appendChild(tick(xOf(t), 'is-beat'));
        const l = label(xOf(t), fmtTime(t).replace(/\.\d$/, ''));
        l.classList.add('is-time');
        frag.appendChild(l);
      }
    }
    const span = loopSpan();
    if (span) {
      const r = document.createElement('div');
      r.className = 'daw-loop-region';
      r.style.left = xOf(span[0]) + 'px';
      r.style.width = Math.max(2, (span[1] - span[0]) * pxPerSec) + 'px';
      frag.appendChild(r);
    }
    lane.appendChild(frag);
  }

  function tick(x, cls) {
    const e = document.createElement('div');
    e.className = 'daw-tick ' + cls;
    e.style.left = x + 'px';
    return e;
  }

  function label(x, text) {
    const e = document.createElement('span');
    e.className = 'daw-tick-label';
    e.style.left = x + 'px';
    e.textContent = text;
    return e;
  }

  function renderSections() {
    const lane = rows.sections.lane;
    lane.innerHTML = '';
    data.sections.forEach((s, i) => {
      const [t0, t1] = sectionSpan(s);
      const el = document.createElement('div');
      el.className = 'daw-section' + (i === selected ? ' is-selected' : '');
      el.style.left = xOf(t0) + 'px';
      el.style.width = Math.max(4, (t1 - t0) * pxPerSec - 2) + 'px';
      el.style.background = sectionColor(s.label);
      el.title = `${s.label} — ${s.barCount} bars (${barLabel(s.startBar)}–${barLabel(s.startBar + s.barCount - 1)})`;
      el.innerHTML = `${escapeHTML(s.label)}<span class="daw-section-bars">${s.barCount}</span>`;
      el.addEventListener('click', (e) => {
        e.stopPropagation();
        selected = selected === i && !aligning ? -1 : i;
        renderSections();
        renderRuler();
        if (selected >= 0) seek(t0);
        updateAlignStatus();
      });
      lane.appendChild(el);
    });
  }

  // Chord regions in the style of Logic's chord track: one region per
  // chord, running until the next change, across bar and section lines.
  function renderChords() {
    const lane = rows.chords.lane;
    lane.innerHTML = '';
    const events = [];
    for (const s of data.sections) {
      s.chords.forEach((bar, bi) => {
        for (const c of bar || []) events.push({ bar: s.startBar + bi + c.pos, name: c.name });
      });
    }
    const merged = [];
    for (const e of events) {
      if (!merged.length || merged[merged.length - 1].name !== e.name) merged.push(e);
    }
    merged.forEach((e, i) => {
      const t0 = timeAt(e.bar);
      const t1 = i + 1 < merged.length ? timeAt(merged[i + 1].bar) : barTimes[N()];
      const el = document.createElement('div');
      el.className = 'daw-chord';
      el.style.left = xOf(t0) + 'px';
      el.style.width = Math.max(3, (t1 - t0) * pxPerSec - 1) + 'px';
      el.textContent = e.name;
      el.title = e.name;
      lane.appendChild(el);
    });
  }

  function topRowsHeight() {
    return ROW.ruler + ROW.sections + ROW.chords;
  }

  function renderGrid() {
    contentEl.querySelector('.daw-grid')?.remove();
    const grid = document.createElement('div');
    grid.className = 'daw-grid';
    grid.style.top = topRowsHeight() + 'px';
    const starts = new Set(data.sections.map((s) => s.startBar));
    const n = N();
    const beatPx = n ? (barTimes[n] - barTimes[0]) / n * pxPerSec / beatsPerBar() : 0;
    for (let b = 0; b <= n; b++) {
      const gl = document.createElement('div');
      gl.className = 'daw-gl ' + (starts.has(b) || b === n ? 'is-section' : 'is-bar');
      gl.style.left = xOf(barTimes[b]) + 'px';
      grid.appendChild(gl);
      if (b < n && beatPx >= 12) {
        for (let k = 1; k < beatsIn(b); k++) {
          const bl = document.createElement('div');
          bl.className = 'daw-gl is-beat';
          bl.style.left = xOf(barTimes[b] + k / beatsIn(b) * (barTimes[b + 1] - barTimes[b])) + 'px';
          grid.appendChild(bl);
        }
      }
    }
    contentEl.insertBefore(grid, contentEl.firstChild);
  }

  // ---- Reference recording lane ----------------------------------------
  function renderAudioLane() {
    if (!rows.audio) return;
    const lane = rows.audio.lane;
    lane.querySelectorAll('.daw-clip, .daw-anchor').forEach((e) => e.remove());
    if (audioDuration) {
      const clip = document.createElement('div');
      clip.className = 'daw-clip';
      clip.style.left = xOf(0) + 'px';
      clip.style.width = audioDuration * pxPerSec + 'px';
      clip.style.background = COLOR.audio;
      clip.innerHTML = `<div class="daw-clip-hdr" style="background:${COLOR.audio}"><span>${escapeHTML(document.title.replace(/ — Songbooks$/, ''))} — ${CAT_LABEL[refCat] || refCat}</span></div>`;
      lane.insertBefore(clip, lane.firstChild);
    }
    if (!waveCanvas || waveCanvas.parentNode !== lane) {
      waveCanvas = document.createElement('canvas');
      waveCanvas.className = 'daw-wave';
      lane.appendChild(waveCanvas);
    }
    if (aligning) renderAnchors();
    drawWave();
  }

  function drawWave() {
    if (!waveCanvas || !rows.audio) return;
    const w = scrollEl.clientWidth;
    const h = rows.audio.lane.clientHeight - 26;
    const dpr = window.devicePixelRatio || 1;
    waveCanvas.style.left = scrollEl.scrollLeft + 'px';
    waveCanvas.style.width = w + 'px';
    waveCanvas.width = Math.max(1, Math.round(w * dpr));
    waveCanvas.height = Math.max(1, Math.round(h * dpr));
    const ctx = waveCanvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    if (!peaks) return;
    const mid = h / 2;
    ctx.fillStyle = 'rgba(12, 32, 74, 0.82)';
    const left = scrollEl.scrollLeft;
    for (let px = 0; px < w; px++) {
      const t0 = tOf(left + px);
      const t1 = tOf(left + px + 1);
      if (t1 < 0 || t0 > audioDuration) continue;
      let i0 = Math.max(0, Math.floor(t0 * peaks.perSec));
      const i1 = Math.min(peaks.max.length - 1, Math.max(i0, Math.ceil(t1 * peaks.perSec) - 1));
      let lo = 0, hi = 0;
      for (; i0 <= i1; i0++) {
        if (peaks.max[i0] > hi) hi = peaks.max[i0];
        if (peaks.min[i0] < lo) lo = peaks.min[i0];
      }
      ctx.fillRect(px, mid - hi * mid, 1, Math.max(1, (hi - lo) * mid));
    }
  }

  async function decodePeaks(buf) {
    try {
      const ctx = new (window.AudioContext || window.webkitAudioContext)();
      const decoded = await ctx.decodeAudioData(buf);
      ctx.close();
      const ch = decoded.getChannelData(0);
      const perSec = 200;
      const n = Math.ceil(decoded.duration * perSec);
      const step = decoded.sampleRate / perSec;
      const min = new Float32Array(n), max = new Float32Array(n);
      for (let i = 0; i < n; i++) {
        let lo = 0, hi = 0;
        const end = Math.min(ch.length, Math.floor((i + 1) * step));
        for (let j = Math.floor(i * step); j < end; j++) {
          const v = ch[j];
          if (v > hi) hi = v; else if (v < lo) lo = v;
        }
        min[i] = lo; max[i] = hi;
      }
      peaks = { min, max, perSec };
      drawWave();
    } catch (e) {
      setStatus('Could not decode the recording for its waveform (playback still works).');
    }
  }

  // ---- Score lanes (alphaTab, one mount per track, fixed bar widths) ---
  function laneSettings(isPlayer) {
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.display.layoutMode = alphaTab.LayoutMode.Horizontal;
    s.display.systemsLayoutMode = alphaTab.SystemsLayoutMode.UseModelLayout;
    s.display.staveProfile = alphaTab.StaveProfile.Default;
    s.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
    const E = alphaTab.NotationElement;
    for (const el of [E.TrackNames, E.GuitarTuning, E.EffectTempo, E.BarNumber, E.EffectChordNames, E.ScoreTitle, E.ScoreSubTitle, E.ScoreArtist, E.ScoreAlbum, E.ScoreWords, E.ScoreMusic, E.ScoreWordsAndMusic, E.ScoreCopyright]) {
      if (el !== undefined) s.notation.elements.set(el, false);
    }
    s.player.enablePlayer = isPlayer;
    s.player.enableCursor = false;
    s.player.scrollMode = alphaTab.ScrollMode.Off;
    if (isPlayer) s.player.soundFont = '/static/js/vendor/alphatab/soundfont/sonivox.sf2';
    return s;
  }

  // Notation is engraved at a legible scale — never below NOTATION_MIN_PX
  // pixels per second — and squeezed horizontally to the current zoom
  // with a CSS transform. Zooming out (down to Fit) shows that engraving
  // as a faithful preview at once, at the same height, instead of
  // re-engraving cramped bars; zooming in stretches it until a sharper
  // engraving at the new scale replaces it.
  const NOTATION_MIN_PX = 110;
  const notationPx = () => Math.max(pxPerSec, NOTATION_MIN_PX);
  const mountWidth = (px) => Math.ceil(2 * PAD + timelineEnd() * px) + 'px';

  function applyBarWidths(score, px) {
    const n = Math.min(N(), score.masterBars.length);
    for (let b = 0; b < n; b++) {
      const w = Math.max(12, (barTimes[b + 1] - barTimes[b]) * px);
      score.masterBars[b].displayWidth = w;
      for (const t of score.tracks) for (const st of t.staves) if (st.bars[b]) st.bars[b].displayWidth = w;
    }
  }

  function hasNotes(score, trackIdx, from, to) {
    const st = score.tracks[trackIdx].staves[0];
    for (let b = from; b < to && b < st.bars.length; b++) {
      for (const v of st.bars[b].voices) for (const beat of v.beats) if (!beat.isRest && beat.notes.length) return true;
    }
    return false;
  }

  // force: bar times changed (alignment), so engrave again even at the
  // same scale.
  function renderNotationLanes(force) {
    if (typeof alphaTab === 'undefined' || !data.alphaTex) return;
    const target = notationPx();
    for (const tr of tracks) {
      if (tr.kind !== 'midi') continue;
      if (tr.api) {
        if (!force && tr.renderedPx === target) { placeNotation(tr); continue; }
        tr.renderedPx = target;
        applyBarWidths(tr.api.score, target);
        tr.mount.style.width = mountWidth(target);
        tr.api.render();
        continue;
      }
      const isPlayer = tr.index === 0;
      const settings = laneSettings(isPlayer);
      const imp = new alphaTab.importer.AlphaTexImporter();
      imp.initFromString(data.alphaTex, settings);
      const score = imp.readScore();
      score.tracks.forEach((st, i) => { const p = data.tracks[i] && data.tracks[i].program; if (p >= 0) st.playbackInfo.program = p; });
      applyBarWidths(score, target);
      tr.renderedPx = target;
      const wrap = document.createElement('div');
      wrap.className = 'daw-notation';
      const mount = document.createElement('div');
      mount.style.width = mountWidth(target);
      mount.style.transformOrigin = '0 0';
      wrap.appendChild(mount);
      tr.lane.appendChild(wrap);
      tr.wrap = wrap;
      tr.mount = mount;
      const api = new alphaTab.AlphaTabApi(mount, settings);
      tr.api = api;
      api.postRenderFinished.on(() => fitNotation(tr));
      if (isPlayer) {
        playerApi = api;
        api.playerReady.on(() => { synthReady = true; applyMix(); });
        api.playerStateChanged.on((e) => { synthPlaying = e.state === 1; });
        api.playerFinished.on(() => { if (!hasAudio()) pause(); });
      }
      api.renderScore(score, [tr.index]);
    }
  }

  // Line the rendered staff up with the lane: bar 0's own x inside the
  // SVG goes to barTimes[0] on the timeline; crop to the staff system
  // (which also drops alphaTab's tagline below it).
  function fitNotation(tr) {
    const bl = tr.api.renderer.boundsLookup;
    const sys = bl && bl.staffSystems && bl.staffSystems[0];
    if (!sys || !sys.bars.length) return;
    tr.bar0x = sys.bars[0].realBounds.x;
    tr.sysRight = sys.realBounds.x + sys.realBounds.w;
    // Heights only ever grow, so rows never jump between zoom levels.
    tr.notationH = Math.max(tr.notationH || 0, Math.ceil(sys.realBounds.h) + 6);
    tr.wrap.style.top = '24px';
    tr.wrap.style.height = tr.notationH + 'px';
    tr.mount.style.marginTop = (3 - sys.realBounds.y) + 'px';
    setRowHeight('midi' + tr.index, tr.notationH + 32);
    placeNotation(tr);
    positionNotationClips();
    renderGrid();
  }

  function placeNotation(tr) {
    if (tr.kind !== 'midi' || !tr.wrap || tr.bar0x === undefined) return;
    const k = pxPerSec / tr.renderedPx;
    tr.mount.style.transform = Math.abs(k - 1) < 1e-6 ? '' : `scaleX(${k})`;
    tr.wrap.style.left = (xOf(barTimes[0]) - tr.bar0x * k) + 'px';
    tr.wrap.style.width = Math.ceil(tr.sysRight * k + 4) + 'px';
  }

  function positionNotationClips() {
    for (const tr of tracks) {
      if (tr.kind !== 'midi' || !tr.api || !tr.api.score) continue;
      tr.lane.querySelectorAll('.daw-clip').forEach((e) => e.remove());
      for (const s of data.sections) {
        if (!hasNotes(tr.api.score, tr.index, s.startBar, s.startBar + s.barCount)) continue;
        const [t0, t1] = sectionSpan(s);
        const clip = document.createElement('div');
        clip.className = 'daw-clip';
        clip.style.left = xOf(t0) + 'px';
        clip.style.width = Math.max(4, (t1 - t0) * pxPerSec) + 'px';
        clip.style.background = tr.body;
        clip.innerHTML = `<div class="daw-clip-hdr" style="background:${tr.color}"><span>${escapeHTML(s.label)}</span></div>`;
        tr.lane.insertBefore(clip, tr.lane.firstChild);
      }
    }
  }

  // ---- Playhead, LCD, transport ----------------------------------------
  function updatePlayhead() {
    if (!playheadEl) return;
    const x = xOf(position);
    playheadEl.style.left = x + 'px';
    headEl.style.left = x + 'px';
    const b = barAt(position);
    if (b < 0) lcdBar.textContent = 'lead-in';
    else if (b >= N()) lcdBar.textContent = 'end';
    else {
      const bar = Math.floor(b);
      const beat = Math.floor((b - bar) * beatsIn(bar)) + 1 + (bar === 0 && pickup() ? beatsPerBar() - beatsIn(0) : 0);
      lcdBar.textContent = `${barLabel(bar)}.${beat}`;
    }
    lcdTime.textContent = fmtTime(position);
  }

  function followPlayhead() {
    const x = xOf(position);
    const w = scrollEl.clientWidth;
    if (x > scrollEl.scrollLeft + w - 40 || x < scrollEl.scrollLeft) scrollEl.scrollLeft = Math.max(0, x - 60);
  }

  function loopSpan() {
    if (selected >= 0) return sectionSpan(data.sections[selected]);
    if (loop && N()) return [barTimes[0], barTimes[N()]];
    return null;
  }

  function seek(t) {
    position = Math.max(0, Math.min(timelineEnd(), t));
    if (hasAudio()) audio.currentTime = Math.min(position, audioDuration);
    if (playerApi && synthReady) {
      const b = barAt(position);
      if (b >= 0 && b < N()) playerApi.tickPosition = tickAtBar(b);
      synthGuardUntil = performance.now() + 400;
    }
    updatePlayhead();
  }

  function play() {
    if (!hasAudio() && !(playerApi && synthReady)) return;
    document.dispatchEvent(new CustomEvent('songbooks:pause-reference'));
    playing = true;
    playStartPos = position;
    playBtn.innerHTML = ICON_PAUSE;
    if (hasAudio()) {
      audio.playbackRate = speed;
      audio.preservesPitch = true;
      if (position >= audioDuration) seek(0);
      audio.play().catch(() => {});
    } else {
      if (position >= barTimes[N()]) seek(barTimes[0]);
      playerApi.playbackSpeed = speed;
      playerApi.tickPosition = tickAtBar(Math.max(0, barAt(position)));
      playerApi.play();
    }
    clearInterval(ticker);
    ticker = setInterval(loopTick, 30);
  }

  function pause() {
    playing = false;
    clearInterval(ticker);
    playBtn.innerHTML = ICON_PLAY;
    if (audio) audio.pause();
    if (playerApi && synthPlaying) playerApi.pause();
  }

  // Keeps AlphaSynth on the recording's clock: each bar plays at the speed
  // that makes its nominal length match its real length in the recording,
  // and a drift beyond ~90ms snaps it back.
  function syncSynth(t) {
    if (!playerApi || !synthReady) return;
    const b = barAt(t);
    if (!synthWanted() || b < 0 || b >= N()) {
      if (synthPlaying) playerApi.pause();
      return;
    }
    const i = Math.floor(b);
    const barDur = barTimes[i + 1] - barTimes[i];
    const target = speed * barNomSec(i) / barDur;
    if (Math.abs(playerApi.playbackSpeed - target) > 0.004) playerApi.playbackSpeed = target;
    const expected = tickAtBar(b);
    const now = performance.now();
    if (!synthPlaying) {
      playerApi.tickPosition = expected;
      playerApi.play();
      synthGuardUntil = now + 400;
      return;
    }
    if (now < synthGuardUntil) return;
    const driftSec = (barAtTick(playerApi.tickPosition) - b) * barDur;
    if (Math.abs(driftSec) > 0.09) {
      playerApi.tickPosition = expected;
      synthGuardUntil = now + 400;
    }
  }

  function loopTick() {
    if (!playing) return;
    let t;
    if (hasAudio()) {
      t = audio.currentTime;
      if (audio.ended) { pause(); return; }
      syncSynth(t);
    } else {
      t = timeAt(barAtTick(playerApi.tickPosition));
    }
    const span = loop ? loopSpan() : null;
    if (span && t >= span[1]) {
      seek(span[0]);
      t = span[0];
    }
    position = t;
    updatePlayhead();
    followPlayhead();
  }

  playBtn.addEventListener('click', () => (playing ? pause() : play()));
  $('daw-stop').addEventListener('click', () => { pause(); seek(playStartPos); });
  $('daw-rewind').addEventListener('click', () => {
    seek(position > barTimes[0] + 0.5 ? barTimes[0] : 0);
    followPlayhead();
  });
  $('daw-loop').addEventListener('click', toggleLoop);
  function toggleLoop() {
    loop = !loop;
    $('daw-loop').setAttribute('aria-pressed', loop);
    dawEl.classList.toggle('is-looping', loop);
    renderRuler();
  }
  $('daw-speed').addEventListener('change', (e) => {
    speed = Number(e.target.value) || 1;
    if (audio) audio.playbackRate = speed;
    if (playerApi && !hasAudio()) playerApi.playbackSpeed = speed;
  });

  function wireLaneSeeking() {
    contentEl.addEventListener('click', (e) => {
      if (e.target.closest('.daw-section, .daw-anchor')) return;
      const rect = contentEl.getBoundingClientRect();
      seek(tOf(e.clientX - rect.left));
    });
  }

  // ---- Zoom --------------------------------------------------------------
  // The range runs from "the whole song fits the window" to "two bars fill
  // it" (or 400 px/s, whichever is closer), on a log scale so each slider
  // step feels the same at both ends. Zooming keeps a point fixed on
  // screen: the mouse for ⌘/Ctrl-wheel and pinch, the playhead while
  // playing, otherwise the middle of the view.
  let zoomTimer = null;
  const viewWidth = () => Math.max(100, scrollEl.clientWidth - 2 * PAD);
  const avgBarSec = () => (N() ? (barTimes[N()] - barTimes[0]) / N() : nominalBarSec());

  function zoomBounds() {
    const min = viewWidth() / timelineEnd();
    const max = Math.max(400, viewWidth() / (2 * avgBarSec()));
    return [min, Math.max(min * 1.05, max)];
  }

  function sliderFor(v) {
    const [lo, hi] = zoomBounds();
    return Math.round(1000 * Math.log(v / lo) / Math.log(hi / lo));
  }

  function zoomFromSlider(s) {
    const [lo, hi] = zoomBounds();
    return lo * Math.pow(hi / lo, s / 1000);
  }

  function setZoom(v, anchorT, anchorPx) {
    if (!built) return;
    const w = scrollEl.clientWidth;
    if (anchorT === undefined) {
      const playheadPx = xOf(position) - scrollEl.scrollLeft;
      if (playing && playheadPx >= 0 && playheadPx <= w) { anchorT = position; anchorPx = playheadPx; }
      else { anchorT = tOf(scrollEl.scrollLeft + w / 2); anchorPx = w / 2; }
    }
    const [lo, hi] = zoomBounds();
    pxPerSec = Math.max(lo, Math.min(hi, v));
    zoomInput.value = sliderFor(pxPerSec);
    layout(true);
    scrollEl.scrollLeft = Math.max(0, xOf(anchorT) - anchorPx);
    drawWave();
    updateZoomLabel();
    clearTimeout(zoomTimer);
    zoomTimer = setTimeout(() => {
      renderNotationLanes();
    }, 250);
  }

  function updateZoomLabel() {
    const label = $('daw-zoom-label');
    const [lo] = zoomBounds();
    if (pxPerSec <= lo * 1.001) { label.textContent = 'whole song'; return; }
    const bars = viewWidth() / pxPerSec / avgBarSec();
    label.textContent = bars < 10 ? `${bars.toFixed(1).replace(/\.0$/, '')} bars` : `${Math.round(bars)} bars`;
  }

  function zoomToFit() {
    setZoom(0);
    scrollEl.scrollLeft = 0;
    drawWave();
  }

  function zoomToTwoBars() {
    const b = Math.max(0, Math.min(N() - 1, Math.floor(barAt(position))));
    const span = N() ? barTimes[Math.min(N(), b + 2)] - barTimes[b] : 2 * nominalBarSec();
    setZoom(viewWidth() / span, N() ? barTimes[b] : position, PAD);
  }

  zoomInput.addEventListener('input', () => setZoom(zoomFromSlider(Number(zoomInput.value))));
  $('daw-zoom-in').addEventListener('click', () => setZoom(pxPerSec * 1.4));
  $('daw-zoom-out').addEventListener('click', () => setZoom(pxPerSec / 1.4));
  $('daw-zoom-fit').addEventListener('click', zoomToFit);
  $('daw-zoom-bars').addEventListener('click', zoomToTwoBars);
  scrollEl.addEventListener('wheel', (e) => {
    if (!(e.ctrlKey || e.metaKey)) return; // trackpad pinch arrives as ctrl+wheel too
    e.preventDefault();
    const px = e.clientX - scrollEl.getBoundingClientRect().left;
    setZoom(pxPerSec * Math.exp(-e.deltaY * 0.01), tOf(scrollEl.scrollLeft + px), px);
  }, { passive: false });
  scrollEl.addEventListener('scroll', drawWave, { passive: true });
  window.addEventListener('resize', () => { if (built) setZoom(pxPerSec); });

  // ---- Align recording ---------------------------------------------------
  // Flags on the recording at each section start. Dragging one stretches
  // the bars between it and its nearest AUTHORED neighbors (sync point or
  // marker) and shifts estimated ones; with nothing authored yet it moves
  // the whole grid (the "where does bar 1 start" case). Shift-drag always
  // moves the whole grid. GP recordings save per-bar sync points; any
  // other recording saves the section's marker.
  function anchorList() {
    return data.sections.map((s, i) => ({ i, bar: s.startBar, label: s.label, real: realBars.has(s.startBar) }));
  }

  function renderAnchors() {
    const lane = rows.audio.lane;
    lane.querySelectorAll('.daw-anchor').forEach((e) => e.remove());
    for (const a of anchorList()) {
      const el = document.createElement('div');
      el.className = 'daw-anchor' + (a.real ? '' : ' is-estimated');
      el.style.left = xOf(barTimes[a.bar]) + 'px';
      el.title = `${a.label} starts at ${fmtTime(barTimes[a.bar])}${a.real ? '' : ' (estimated from tempo)'} — drag to align`;
      el.innerHTML = `<span class="daw-anchor-flag">${escapeHTML(a.label)}</span>`;
      el.addEventListener('mousedown', (e) => startAnchorDrag(e, a));
      lane.appendChild(el);
    }
  }

  function movedTimes(orig, a, newT, whole) {
    const n = N();
    const delta = newT - orig[a.bar];
    if (whole) return orig.map((t) => t + delta);
    const realAnchors = anchorList().filter((x) => x.real && x.bar !== a.bar);
    const prev = realAnchors.filter((x) => x.bar < a.bar).pop();
    const next = realAnchors.find((x) => x.bar > a.bar);
    if (!prev && !next) return orig.map((t) => t + delta);
    const lo = prev ? orig[prev.bar] + 0.05 * (a.bar - prev.bar) : -Infinity;
    const hi = next ? orig[next.bar] - 0.05 * (next.bar - a.bar) : Infinity;
    newT = Math.max(lo, Math.min(hi, newT));
    const out = orig.slice();
    const remap = (b, s0, s1, d0, d1) => d0 + (orig[b] - s0) / (s1 - s0) * (d1 - d0);
    for (let b = 0; b <= n; b++) {
      if (b === a.bar) out[b] = newT;
      else if (b < a.bar) out[b] = prev ? (b > prev.bar ? remap(b, orig[prev.bar], orig[a.bar], orig[prev.bar], newT) : orig[b]) : orig[b] + (newT - orig[a.bar]);
      else out[b] = next ? (b < next.bar ? remap(b, orig[a.bar], orig[next.bar], newT, orig[next.bar]) : orig[b]) : orig[b] + (newT - orig[a.bar]);
    }
    return out;
  }

  function startAnchorDrag(e, a) {
    if (!canEdit) return;
    e.preventDefault();
    e.stopPropagation();
    const orig = barTimes.slice();
    const x0 = e.clientX;
    const whole = e.shiftKey;
    const el = e.currentTarget;
    el.classList.add('is-dragging');
    const onMove = (ev) => {
      barTimes = movedTimes(orig, a, orig[a.bar] + (ev.clientX - x0) / pxPerSec, whole || ev.shiftKey);
      layout(true);
      alignStatus.textContent = `${a.label} → ${fmtTime(barTimes[a.bar])}`;
    };
    const onUp = async () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      if (barTimes[a.bar] === orig[a.bar] && barTimes[0] === orig[0]) { renderAnchors(); return; }
      await saveAlignment(a, orig);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }

  async function saveAlignment(a, orig) {
    const meta = audioCats[refCat];
    alignStatus.textContent = 'saving…';
    try {
      if (meta.barSyncPoints && meta.barSyncPoints.length) {
        const points = [];
        for (let b = 0; b < N(); b++) points.push({ barIndex: b, timeSeconds: barTimes[b] });
        await put(`/api/songs/${variantId}/audio/gp/sync-points`, { points });
        meta.barSyncPoints = points;
      } else {
        const shifted = data.sections.filter((s) => realBars.has(s.startBar) || s.startBar === a.bar);
        meta.markers = meta.markers || [];
        for (const s of shifted) {
          const t = barTimes[s.startBar];
          if (Math.abs(t - orig[s.startBar]) < 1e-6 && s.startBar !== a.bar) continue;
          await put(`/api/songs/${variantId}/audio/${refCat}/markers/${s.index}`, { timeSeconds: t });
          const m = meta.markers.find((x) => x.sectionIndex === s.index);
          if (m) m.timeSeconds = t; else meta.markers.push({ sectionIndex: s.index, timeSeconds: t });
        }
      }
      alignStatus.textContent = `saved — ${a.label} at ${fmtTime(barTimes[a.bar])}`;
      document.dispatchEvent(new CustomEvent('songbooks:audio-changed'));
    } catch (err) {
      alignStatus.textContent = 'could not save: ' + err.message;
      barTimes = orig;
    }
    computeBarTimes();
    rows.audio.hdr.querySelector('.daw-hdr-sub').textContent = refSubtitle();
    layout();
  }

  async function put(url, body) {
    const res = await fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (!res.ok) throw new Error(await res.text());
  }

  function updateAlignStatus() {
    if (!aligning) return;
    alignStatus.textContent = selected >= 0 ? `selected: ${data.sections[selected].label}` : 'select a section to set its start at the playhead';
  }

  alignBtn?.addEventListener('click', () => {
    if (!hasAudio()) { setStatus('Add a reference recording first (Reference tab) — there is nothing to align yet.'); return; }
    aligning = !aligning;
    alignBtn.setAttribute('aria-pressed', aligning);
    alignBar.hidden = !aligning;
    renderAudioLane();
    updateAlignStatus();
  });
  $('daw-align-here')?.addEventListener('click', async () => {
    if (selected < 0) { alignStatus.textContent = 'select a section first (click it in the Sections row)'; return; }
    const s = data.sections[selected];
    const a = anchorList().find((x) => x.i === selected);
    const orig = barTimes.slice();
    barTimes = movedTimes(orig, a, position, false);
    layout(true);
    await saveAlignment({ ...a, label: s.label }, orig);
  });

  // ---- Keyboard ------------------------------------------------------------
  document.addEventListener('keydown', (e) => {
    if (!isActive() || e.target.closest('input, select, textarea, [contenteditable]')) return;
    if (e.code === 'Space') { e.preventDefault(); playing ? pause() : play(); }
    else if (e.key === 'Home') { e.preventDefault(); seek(barTimes[0] || 0); followPlayhead(); }
    else if (e.key === 'l' || e.key === 'L') toggleLoop();
    else if (e.metaKey || e.ctrlKey || e.altKey) return;
    else if (e.key === 'f' || e.key === 'F') zoomToFit();
    else if (e.key === 'b' || e.key === 'B') zoomToTwoBars();
    else if (e.key === '+' || e.key === '=') setZoom(pxPerSec * 1.4);
    else if (e.key === '-' || e.key === '_') setZoom(pxPerSec / 1.4);
  });

  // ---- Loading -------------------------------------------------------------
  function setStatus(msg) { statusEl.textContent = msg || ''; }

  async function loadAudio(cat) {
    refCat = cat;
    peaks = null;
    audioDuration = 0;
    if (audio) {
      audio.pause();
      if (audio.src.startsWith('blob:')) URL.revokeObjectURL(audio.src);
    }
    if (!cat) { audio = null; return; }
    setStatus('Loading the recording…');
    let bytes;
    try {
      bytes = window.songbooksAudioBytes
        ? await window.songbooksAudioBytes(variantId, cat, audioCats[cat])
        : await (await fetch(`/song/${variantId}/audio.mp3?category=${cat}`)).arrayBuffer();
    } catch (e) {
      audio = null;
      setStatus('Could not load the recording: ' + e.message);
      return;
    }
    audio = new Audio(URL.createObjectURL(new Blob([bytes], { type: 'audio/mpeg' })));
    audio.preload = 'auto';
    await new Promise((res) => {
      audio.addEventListener('loadedmetadata', res, { once: true });
      audio.addEventListener('error', res, { once: true });
    });
    audioDuration = isFinite(audio.duration) ? audio.duration : 0;
    audio.addEventListener('ended', () => pause());
    decodePeaks(bytes.slice(0));
  }

  async function switchReference(cat) {
    pause();
    await loadAudio(cat);
    computeBarTimes();
    build();
  }

  function pickReference() {
    const cats = Object.keys(audioCats);
    if (!cats.length) return null;
    if (audioCats.gp) return 'gp';
    let pref = null;
    try { pref = localStorage.getItem(`songbooks:audio-category-choice:${variantId}`); } catch (e) { /* ignore */ }
    return pref && audioCats[pref] ? pref : cats[0];
  }

  // A song imported from a Guitar Pro file keeps that file: it knows each
  // track's real instrument, which the score riffs don't store, so the
  // server's guess from the track name is only the fallback.
  async function applyGPInstruments() {
    if (typeof alphaTab === 'undefined' || !data.tracks.length) return;
    try {
      const res = await fetch(`/song/${variantId}/gp-file`);
      if (!res.ok) return;
      const gp = alphaTab.importer.ScoreLoader.loadScoreFromBytes(new Uint8Array(await res.arrayBuffer()));
      const byName = new Map(gp.tracks.map((t) => [String(t.name).trim().toLowerCase(), t]));
      for (const t of data.tracks) {
        const g = byName.get(String(t.name).trim().toLowerCase());
        if (g && !t.drums && !(g.staves[0] && g.staves[0].isPercussion)) t.program = g.playbackInfo.program;
      }
    } catch (e) { /* keep the name-based guess */ }
  }

  async function init() {
    setStatus('Loading…');
    try {
      const [mx, au] = await Promise.all([
        fetch(`/api/songs/${variantId}/mixer`).then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.statusText)))),
        fetch(`/api/songs/${variantId}/audio`).then((r) => (r.ok ? r.json() : { categories: [] })),
      ]);
      data = mx;
      await applyGPInstruments();
      audioCats = {};
      (au.categories || []).forEach((c) => { audioCats[c.category] = c; });
      await loadAudio(pickReference());
      computeBarTimes();
      const [zlo, zhi] = zoomBounds();
      pxPerSec = Math.max(zlo, Math.min(zhi, pxPerSec));
      build();
      zoomInput.value = sliderFor(pxPerSec);
      updateZoomLabel();
      if (hasAudio() && barTimes[0] > 3) scrollEl.scrollLeft = Math.max(0, xOf(barTimes[0]) - 80);
      seek(hasAudio() ? 0 : barTimes[0]);
      drawWave();
      setStatus(data.tracks.length && typeof alphaTab === 'undefined' ? 'Score tracks need the notation engine, which did not load.' : '');
    } catch (e) {
      setStatus('Could not load the mixer: ' + e.message);
    }
  }

  // Built lazily the first time the Mixer tab is shown (layout needs real
  // widths); playback pauses when leaving it.
  const sheet = document.getElementById('sheet');
  const isActive = () => sheet && sheet.dataset.renderMode === 'mixer';
  let started = false;
  function onModeChange() {
    if (isActive()) {
      if (!started) { started = true; setTimeout(init, 0); }
      else if (built) drawWave();
    } else if (playing) {
      pause();
    }
  }
  // Read-only snapshot for debugging playback sync from the console.
  page.mixerState = () => ({
    playing, position, refCat, audioTime: audio ? audio.currentTime : null,
    bar: barAt(position), synthBar: playerApi && synthReady ? barAtTick(playerApi.tickPosition) : null,
    synthPlaying, synthSpeed: playerApi ? playerApi.playbackSpeed : null,
  });
  if (sheet) new MutationObserver(onModeChange).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });
  onModeChange();
})();

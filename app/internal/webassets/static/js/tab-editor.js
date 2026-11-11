// Scores tab: a Guitar Pro–style tab editor for one section's
// "score[label]:" voice at a time (see docs/editor-plan.md, phase 1).
//
// The model is the voice's tab.Riff (internal/tab/tab.go), edited in place
// and written back into the drawer's in-memory song — the same
// structSections the Chart editor edits, reached through
// window.SongEditorBridge (song.js) — so Save, Discard, Undo and the
// unsaved-changes guard all work exactly as they do for chords. alphaTab is
// only the view: every edit re-emits alphaTeX (a port of internal/tab's
// writeBeat) and re-renders, and its boundsLookup places the cursor and
// answers clicks.
//
// A voice written in the LilyPond subset is decoded by the server
// (POST /api/score/riff) for display; the first edit stores it as Riff
// JSON, since text can't say which string a note is played on.
(function () {
  const DURATIONS = ['1', '2', '4', '8', '16', '32', '64'];
  const DURATION_LABEL = { 1: 'whole', 2: 'half', 4: 'quarter', 8: 'eighth', 16: '16th', 32: '32nd', 64: '64th' };
  const STANDARD_TUNING = {
    Guitar: [64, 59, 55, 50, 45, 40], // E4 B3 G3 D3 A2 E2, string 1 first
    Bass: [43, 38, 33, 28],           // G2 D2 A1 E1
  };
  const MAX_FRET = 24;
  const DIGIT_WINDOW_MS = 600; // a second digit this soon makes a two-digit fret, as in Guitar Pro

  const $ = (id) => document.getElementById(id);
  let bridge = null;
  let initialized = false;

  const st = {
    sec: -1, voice: -1,
    riff: null,        // the Riff being edited (decoded from the voice body)
    body: null,        // the voice body st.riff was decoded from
    source: '',        // 'json' | 'lilypond'
    error: '',
    bar: 0, beat: 0, string: 1,
    digit: null,       // { bar, beat, string, val, t } — the last fret digit typed
    clip: null,        // a copied bar
    api: null,
    tex: '',
    map: [],           // map[bar][alphaTab beat index] = riff beat index
    loadSeq: 0,
  };

  // ---- Durations -------------------------------------------------------------

  function durationBeats(d) {
    const m = /^(\d+)(\.*)$/.exec(d || '');
    if (!m) return 0;
    let v = 4 / Number(m[1]);
    if (m[2].length === 1) v *= 1.5;
    else if (m[2].length >= 2) v *= 1.75;
    return v;
  }
  function beatBeats(b) {
    if (b.Grace) return 0;
    let v = durationBeats(b.Duration);
    if (b.TupletNum > 0 && b.TupletDen > 0) v *= b.TupletDen / b.TupletNum;
    return v;
  }
  function barBeats(bar) { return bar.Beats.reduce((a, b) => a + beatBeats(b), 0); }

  // restBeats is internal/tab.RestBeats: rests filling exactly n quarter notes.
  function restBeats(n) {
    const values = [['1', 4], ['2.', 3], ['2', 2], ['4.', 1.5], ['4', 1], ['8.', 0.75], ['8', 0.5], ['16.', 0.375], ['16', 0.25], ['32', 0.125]];
    const out = [];
    for (const [dur, v] of values) {
      while (n >= v - 1e-9) { out.push({ Rest: true, Duration: dur }); n -= v; }
    }
    return out.length ? out : [{ Rest: true, Duration: '1' }];
  }

  function timeSig() {
    const sec = currentSection();
    const meta = bridge.meta().find((f) => f.key === 'time');
    const m = /^\s*(\d+)\s*\/\s*(\d+)/.exec((sec && sec.time) || (meta && meta.value) || '4/4');
    return m ? [Number(m[1]), Number(m[2])] : [4, 4];
  }
  function barCapacity() { const [n, d] = timeSig(); return n * 4 / d; }

  // ---- Model access ----------------------------------------------------------

  function currentSection() { return bridge.sections()[st.sec] || null; }
  function currentVoice() {
    const sec = currentSection();
    return sec && sec.scores ? sec.scores[st.voice] || null : null;
  }
  function bars() { return st.riff ? st.riff.Bars : []; }
  function curBar() { return bars()[st.bar]; }
  function curBeat() { const b = curBar(); return b ? b.Beats[st.beat] : null; }
  function numStrings() { return st.riff ? st.riff.Tuning.length : 6; }
  function noteOn(beat, s) { return beat && !beat.Rest ? (beat.Notes || []).find((n) => n.String === s) : null; }
  function editable() { return st.riff && !st.riff.Drums && !st.error; }

  function clampCursor() {
    const bs = bars();
    st.bar = Math.max(0, Math.min(st.bar, bs.length - 1));
    const n = bs[st.bar] ? bs[st.bar].Beats.length : 1;
    st.beat = Math.max(0, Math.min(st.beat, n - 1));
    st.string = Math.max(1, Math.min(st.string, numStrings()));
  }

  // The stored form: Go's own field names (internal/tab decodes it with
  // encoding/json), zero values left out to keep the line short.
  function encodeRiff(r) {
    const out = { Tuning: r.Tuning, MinimumFret: r.MinimumFret || 0 };
    if (r.Drums) out.Drums = true;
    out.Bars = r.Bars.map((bar) => ({
      Beats: bar.Beats.map((b) => {
        const o = {};
        if (b.Rest) o.Rest = true;
        o.Duration = b.Duration;
        if (!b.Rest) o.Notes = (b.Notes || []).map((n) => {
          const no = { MIDI: n.MIDI, String: n.String, Fret: n.Fret };
          if (n.Tied) no.Tied = true;
          return no;
        });
        for (const k of ['MinimumFret', 'HammerPull', 'HasSlideFrom', 'SlideFromFret', 'BendSemitones', 'BendRelease', 'PalmMute', 'TupletNum', 'TupletDen', 'Grace']) {
          if (b[k]) o[k] = b[k];
        }
        return o;
      }),
    }));
    return out;
  }

  // ---- Editing ---------------------------------------------------------------

  // edit runs one change against st.riff and writes the voice back: an
  // undo step first (unless this keystroke only completes the previous
  // one, a two-digit fret), then the song is marked unsaved.
  function edit(fn, { joinPrevious = false } = {}) {
    if (!editable()) return;
    if (joinPrevious) bridge.dirty(); else bridge.begin();
    fn();
    for (const bar of st.riff.Bars) if (!bar.Beats.length) bar.Beats.push({ Rest: true, Duration: '1' });
    const v = currentVoice();
    v.body = JSON.stringify(encodeRiff(st.riff));
    st.body = v.body;
    if (st.source !== 'json') { st.source = 'json'; renderNotice(); }
    clampCursor();
    render();
  }

  function setFret(fret, joinPrevious) {
    edit(() => {
      const beat = curBeat();
      const s = st.string;
      const midi = st.riff.Tuning[s - 1] + fret;
      if (beat.Rest) { beat.Rest = false; beat.Notes = []; }
      beat.Notes = beat.Notes || [];
      const n = noteOn(beat, s);
      if (n) { n.Fret = fret; n.MIDI = midi; n.Tied = false; }
      else beat.Notes.push({ MIDI: midi, String: s, Fret: fret });
      beat.Notes.sort((a, b) => a.String - b.String);
    }, { joinPrevious });
  }

  function typeDigit(d) {
    const now = Date.now();
    const p = st.digit;
    if (p && p.bar === st.bar && p.beat === st.beat && p.string === st.string && now - p.t < DIGIT_WINDOW_MS && p.val * 10 + d <= MAX_FRET && p.val > 0) {
      const val = p.val * 10 + d;
      st.digit = null;
      setFret(val, true);
      return;
    }
    st.digit = { bar: st.bar, beat: st.beat, string: st.string, val: d, t: now };
    setFret(d, false);
  }

  function deleteAtCursor() {
    const beat = curBeat();
    if (!beat) return;
    if (noteOn(beat, st.string)) {
      edit(() => {
        beat.Notes = beat.Notes.filter((n) => n.String !== st.string);
        if (!beat.Notes.length) clearBeat(beat);
      });
    } else if (curBar().Beats.length > 1) {
      edit(() => { curBar().Beats.splice(st.beat, 1); if (st.beat > 0 && st.beat >= curBar().Beats.length) st.beat--; });
    } else if (!beat.Rest) {
      edit(() => clearBeat(beat));
    }
  }

  function clearBeat(beat) {
    beat.Rest = true;
    beat.Notes = [];
    for (const k of ['HammerPull', 'HasSlideFrom', 'SlideFromFret', 'BendSemitones', 'BendRelease', 'PalmMute']) delete beat[k];
  }

  function changeDuration(step) { // +1 shorter, -1 longer
    const beat = curBeat();
    if (!beat) return;
    const dotted = beat.Duration.endsWith('.');
    const i = DURATIONS.indexOf(beat.Duration.replace(/\.+$/, ''));
    const j = Math.max(0, Math.min(DURATIONS.length - 1, (i < 0 ? 2 : i) + step));
    if (j === i) return;
    edit(() => { beat.Duration = DURATIONS[j] + (dotted ? '.' : ''); });
  }
  function setDuration(d) {
    const beat = curBeat();
    if (!beat) return;
    edit(() => { beat.Duration = d + (beat.Duration.endsWith('.') ? '.' : ''); });
  }
  function toggleDot() {
    const beat = curBeat();
    if (!beat) return;
    edit(() => { beat.Duration = beat.Duration.endsWith('.') ? beat.Duration.replace(/\.+$/, '') : beat.Duration + '.'; });
  }
  function toggleTriplet() {
    const beat = curBeat();
    if (!beat) return;
    edit(() => {
      if (beat.TupletNum) { delete beat.TupletNum; delete beat.TupletDen; }
      else { beat.TupletNum = 3; beat.TupletDen = 2; }
    });
  }
  function makeRest() {
    const beat = curBeat();
    if (beat && !beat.Rest) edit(() => clearBeat(beat));
  }

  // The same string's note on the beat before the cursor (across a
  // barline), for ties and slides.
  function previousNote() {
    let bi = st.bar, i = st.beat - 1;
    if (i < 0) { bi--; if (bi < 0) return null; i = bars()[bi].Beats.length - 1; }
    return noteOn(bars()[bi].Beats[i], st.string);
  }

  function toggleTie() {
    const beat = curBeat();
    const n = noteOn(beat, st.string);
    if (n) { edit(() => { n.Tied = !n.Tied; if (n.Tied) { const p = previousNote(); if (p) { n.Fret = p.Fret; n.MIDI = p.MIDI; } } }); return; }
    const p = previousNote();
    if (!p) return;
    edit(() => {
      if (beat.Rest) { beat.Rest = false; beat.Notes = []; }
      beat.Notes.push({ MIDI: p.MIDI, String: st.string, Fret: p.Fret, Tied: true });
      beat.Notes.sort((a, b) => a.String - b.String);
    });
  }
  function toggleFlag(key) {
    const beat = curBeat();
    if (!beat || beat.Rest) return;
    edit(() => { if (beat[key]) delete beat[key]; else beat[key] = true; });
  }
  function toggleSlide() {
    const beat = curBeat();
    const n = noteOn(beat, st.string) || (beat && !beat.Rest && beat.Notes[0]);
    if (!n) return;
    edit(() => {
      if (beat.HasSlideFrom) { delete beat.HasSlideFrom; delete beat.SlideFromFret; return; }
      const p = previousNote();
      beat.HasSlideFrom = true;
      beat.SlideFromFret = p && p.Fret !== n.Fret ? p.Fret : Math.max(0, n.Fret - 2);
    });
  }
  function cycleBend() {
    const beat = curBeat();
    if (!beat || beat.Rest) return;
    edit(() => {
      const next = { 0: 1, 1: 2, 2: 0 }[beat.BendSemitones || 0] ?? 0;
      if (next) beat.BendSemitones = next; else { delete beat.BendSemitones; delete beat.BendRelease; }
    });
  }

  function insertBeat() {
    const beat = curBeat();
    if (!beat) return;
    edit(() => {
      const nb = { Rest: true, Duration: beat.Duration };
      if (beat.TupletNum) { nb.TupletNum = beat.TupletNum; nb.TupletDen = beat.TupletDen; }
      curBar().Beats.splice(st.beat, 0, nb);
    });
  }

  // Moving right off a bar's last beat while the bar isn't full yet adds a
  // rest there, as Guitar Pro does, so a bar is written left to right.
  function moveRight() {
    const bar = curBar();
    if (!bar) return;
    if (st.beat < bar.Beats.length - 1) { st.beat++; return render(); }
    const left = barCapacity() - barBeats(bar);
    if (left > 1e-9 && editable()) {
      const last = bar.Beats[bar.Beats.length - 1];
      let nb;
      if (beatBeats(last) <= left + 1e-9 && !last.Grace) {
        nb = { Rest: true, Duration: last.Duration };
        if (last.TupletNum) { nb.TupletNum = last.TupletNum; nb.TupletDen = last.TupletDen; }
      } else {
        nb = restBeats(left)[0];
      }
      edit(() => { bar.Beats.push(nb); st.beat = bar.Beats.length - 1; });
      return;
    }
    if (st.bar < bars().length - 1) { st.bar++; st.beat = 0; render(); }
  }
  function moveLeft() {
    if (st.beat > 0) st.beat--;
    else if (st.bar > 0) { st.bar--; st.beat = curBar().Beats.length - 1; }
    render();
  }
  function moveBar(d) {
    st.bar = Math.max(0, Math.min(bars().length - 1, st.bar + d));
    st.beat = 0;
    render();
  }

  function addBar() { edit(() => { st.riff.Bars.splice(st.bar + 1, 0, { Beats: restBeats(barCapacity()) }); st.bar++; st.beat = 0; }); }
  function duplicateBar() { edit(() => { st.riff.Bars.splice(st.bar + 1, 0, JSON.parse(JSON.stringify(curBar()))); st.bar++; st.beat = 0; }); }
  function clearBar() { edit(() => { curBar().Beats = restBeats(barCapacity()); st.beat = 0; }); }
  function deleteBar() {
    if (bars().length <= 1) return;
    edit(() => { st.riff.Bars.splice(st.bar, 1); st.beat = 0; });
  }
  function copyBar() { if (curBar()) { st.clip = JSON.parse(JSON.stringify(curBar())); setStatusNote('bar copied'); } }
  function pasteBar() {
    if (!st.clip) return;
    const clip = JSON.parse(JSON.stringify(st.clip));
    // A bar copied from another track keeps its frets; its pitches follow this track's tuning.
    for (const b of clip.Beats) for (const n of b.Notes || []) {
      if (n.String > numStrings()) n.String = numStrings();
      n.MIDI = st.riff.Tuning[n.String - 1] + n.Fret;
    }
    edit(() => { st.riff.Bars[st.bar] = clip; st.beat = 0; });
  }

  // ---- alphaTeX --------------------------------------------------------------
  // A port of internal/tab's writeBeat/writeBeatEffects, recording for each
  // bar which riff beat every alphaTab beat comes from (a slide adds a
  // grace beat in front of its note).

  function midiToNote(midi) {
    const names = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B'];
    return names[midi % 12] + (Math.floor(midi / 12) - 1);
  }
  function esc(s) { return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"'); }

  function noteProps(beat, n) {
    const p = [];
    if (beat.HammerPull) p.push('h');
    if (beat.BendSemitones > 0) p.push(`b (0 ${Math.round(beat.BendSemitones * 2)})`);
    if (beat.PalmMute) p.push('pm');
    if (n.Tied) p.push('t');
    return p;
  }

  function beatTex(beat, chord) {
    const m = /^(\d+)(\.*)$/.exec(beat.Duration) || [null, '4', ''];
    const dur = m[1];
    let out = '';
    let extra = 0;
    if (beat.Rest || !(beat.Notes || []).length) {
      out = 'r.' + dur;
    } else {
      if (beat.HasSlideFrom && beat.Notes.length === 1) {
        out += `(${beat.SlideFromFret}.${beat.Notes[0].String}{sl}).32{gr} `;
        extra = 1;
      }
      const notes = beat.Notes.map((n) => {
        const p = noteProps(beat, n);
        return `${n.Fret}.${n.String}` + (p.length ? `{${p.join(' ')}}` : '');
      });
      out += `(${notes.join(' ')}).${dur}`;
    }
    const fx = [];
    if (m[2]) fx.push('d');
    if (beat.TupletNum > 1 && beat.TupletDen > 0) fx.push(beat.TupletNum === 3 && beat.TupletDen === 2 ? 'tu 3' : `tu (${beat.TupletNum} ${beat.TupletDen})`);
    if (beat.Grace) fx.push('gr');
    if (chord) fx.push(`ch "${esc(chord)}"`);
    if (fx.length) out += `{${fx.join(' ')}}`;
    return { tex: out, extra };
  }

  function chordLabels() {
    const sec = currentSection();
    if (!sec) return [];
    return bridge.measures(sec).map((m) => (m.tokens || []).find((t) => /^[A-G]/.test(t)) || '');
  }

  function buildTex() {
    const r = st.riff;
    const labels = chordLabels();
    const [tn, td] = timeSig();
    let tex = `\\tuning (${r.Tuning.map(midiToNote).join(' ')})\n`;
    if (r.Tuning.length <= 4) tex += '\\clef F4\n';
    tex += '.\n';
    st.map = [];
    r.Bars.forEach((bar, bi) => {
      if (bi > 0) tex += ' | ';
      else if (!(tn === 4 && td === 4)) tex += `\\ts ${tn} ${td} `;
      const map = [];
      bar.Beats.forEach((beat, i) => {
        const { tex: t, extra } = beatTex(beat, i === 0 ? labels[bi] : '');
        for (let k = 0; k <= extra; k++) map.push(i);
        tex += (i ? ' ' : '') + t;
      });
      st.map.push(map);
    });
    return tex + ' |';
  }

  // ---- Rendering -------------------------------------------------------------

  function color(name, fallback) {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    try { return v ? alphaTab.model.Color.fromJson(v) : fallback; } catch (e) { return fallback; }
  }

  function ensureApi() {
    if (st.api) return st.api;
    const host = $('te-canvas');
    const settings = new alphaTab.Settings();
    settings.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    settings.core.useWorkers = false;
    settings.core.enableLazyLoading = false;
    settings.core.includeNoteBounds = true;
    settings.player.enablePlayer = false;
    settings.display.staveProfile = alphaTab.StaveProfile.Tab;
    settings.display.layoutMode = alphaTab.LayoutMode.Page;
    settings.display.barsPerRow = 4;
    settings.display.justifyLastSystem = true;
    settings.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
    settings.notation.elements.set(alphaTab.NotationElement.GuitarTuning, false);
    settings.notation.elements.set(alphaTab.NotationElement.ScoreTitle, false);
    settings.notation.elements.set(alphaTab.NotationElement.EffectDynamics, false);
    const fg = color('--fg', new alphaTab.model.Color(0x24, 0x1e, 0x18));
    settings.display.resources.staffLineColor = color('--fg-muted', fg);
    settings.display.resources.mainGlyphColor = fg;
    settings.display.resources.secondaryGlyphColor = fg;
    settings.display.resources.scoreInfoColor = fg;
    settings.display.resources.barSeparatorColor = fg;
    settings.display.resources.barNumberColor = color('--fg-muted', fg);
    st.api = new alphaTab.AlphaTabApi(host, settings);
    st.api.postRenderFinished.on(drawCursor);
    return st.api;
  }

  function render() {
    renderToolbar();
    renderStatus();
    if (!st.riff || st.riff.Drums || st.error) {
      $('te-canvas-wrap').hidden = true;
      return;
    }
    $('te-canvas-wrap').hidden = false;
    clampCursor();
    const tex = buildTex();
    if (tex === st.tex && st.api) { drawCursor(); return; }
    st.tex = tex;
    try {
      ensureApi().tex(tex);
    } catch (e) {
      st.error = 'alphaTab could not draw this track: ' + e.message;
      renderStatus();
    }
  }

  // The bar's tab staff in the bounds lookup; string s's line sits at an
  // even spacing from its top (string 1) to its bottom (the last string).
  function barBounds(barIndex) {
    const lookup = st.api && st.api.renderer.boundsLookup;
    if (!lookup) return null;
    for (const sys of lookup.staffSystems) {
      for (const mb of sys.bars) {
        if (mb.index === barIndex && mb.bars[0]) return mb.bars[0];
      }
    }
    return null;
  }
  function stringY(bb, s) {
    const v = bb.visualBounds;
    const n = numStrings();
    return v.y + (n > 1 ? (s - 1) * v.h / (n - 1) : v.h / 2);
  }

  function drawCursor() {
    const cursor = $('te-cursor'), barHl = $('te-bar-hl');
    const bb = barBounds(st.bar);
    if (!bb) { cursor.hidden = true; barHl.hidden = true; return; }
    const map = st.map[st.bar] || [];
    let beatB = null;
    bb.beats.forEach((b) => { if (map[b.beat.index] === st.beat) beatB = b; });
    const v = bb.visualBounds;
    Object.assign(barHl.style, { left: v.x + 'px', top: (v.y - 8) + 'px', width: v.w + 'px', height: (v.h + 16) + 'px' });
    barHl.hidden = false;
    barHl.classList.toggle('over', barBeats(curBar()) > barCapacity() + 1e-9);
    if (!beatB) { cursor.hidden = true; return; }
    const bv = beatB.visualBounds;
    const w = Math.max(16, bv.w + 8);
    const h = Math.min(16, v.h / Math.max(1, numStrings() - 1) + 2);
    const y = stringY(bb, st.string);
    Object.assign(cursor.style, { left: (bv.x + bv.w / 2 - w / 2) + 'px', top: (y - h / 2) + 'px', width: w + 'px', height: h + 'px' });
    cursor.hidden = false;
    if (document.activeElement === $('te-canvas-wrap')) cursor.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  function pickAt(ev) {
    const lookup = st.api && st.api.renderer.boundsLookup;
    if (!lookup) return;
    const rect = $('te-canvas').getBoundingClientRect();
    const x = ev.clientX - rect.left, y = ev.clientY - rect.top;
    const beat = lookup.getBeatAtPos(x, y);
    if (!beat) return;
    const barIndex = beat.voice.bar.index;
    const map = st.map[barIndex];
    if (!map) return;
    st.bar = barIndex;
    st.beat = map[beat.index] ?? 0;
    const bb = barBounds(barIndex);
    if (bb) {
      let best = 1, bestD = Infinity;
      for (let s = 1; s <= numStrings(); s++) {
        const d = Math.abs(stringY(bb, s) - y);
        if (d < bestD) { bestD = d; best = s; }
      }
      st.string = best;
    }
    st.digit = null;
    render();
  }

  // ---- Toolbar, pickers, status ---------------------------------------------

  const TOOLS = [
    ...DURATIONS.slice(0, 6).map((d) => ({ act: 'dur', arg: d, label: d === '1' ? '1' : '1/' + d, title: `${DURATION_LABEL[d]} note` })),
    { act: 'dot', label: '•', title: 'Dotted (.)' },
    { act: 'triplet', label: '3', title: 'Triplet (T)' },
    { sep: true },
    { act: 'rest', label: 'Rest', title: 'Rest (R)' },
    { act: 'tie', label: 'Tie', title: 'Tie to the previous note (L)' },
    { act: 'hammer', label: 'H/P', title: 'Hammer-on / pull-off (H)' },
    { act: 'slide', label: 'Slide', title: 'Slide in from the previous fret (S)' },
    { act: 'bend', label: 'Bend', title: 'Bend: ½, full, off (B)' },
    { act: 'pm', label: 'P.M.', title: 'Palm mute (P)' },
    { sep: true },
    { act: 'insert', label: '+ beat', title: 'Insert a beat before the cursor (Insert or I)' },
    { act: 'addbar', label: '+ bar', title: 'Add an empty bar after this one' },
    { act: 'dupbar', label: 'Duplicate bar', title: 'Duplicate this bar' },
    { act: 'clearbar', label: 'Clear bar', title: 'Replace this bar with rests' },
    { act: 'delbar', label: 'Delete bar', title: 'Delete this bar' },
  ];

  function renderToolbar() {
    const tb = $('te-toolbar');
    if (!tb.childElementCount) {
      for (const t of TOOLS) {
        if (t.sep) { const s = document.createElement('span'); s.className = 'te-sep'; tb.appendChild(s); continue; }
        const b = document.createElement('button');
        b.type = 'button';
        b.dataset.act = t.act;
        if (t.arg) b.dataset.arg = t.arg;
        b.textContent = t.label;
        b.title = t.title;
        tb.appendChild(b);
      }
      tb.addEventListener('mousedown', (e) => e.preventDefault()); // keep the keyboard focus on the tab
      tb.addEventListener('click', (e) => {
        const b = e.target.closest('button[data-act]');
        if (b) { runAction(b.dataset.act, b.dataset.arg); $('te-canvas-wrap').focus({ preventScroll: true }); }
      });
    }
    const beat = curBeat();
    const ok = editable() && !!beat;
    const base = beat ? beat.Duration.replace(/\.+$/, '') : '';
    const n = noteOn(beat, st.string);
    const on = {
      dur: (b) => b.dataset.arg === base,
      dot: () => !!beat && beat.Duration.endsWith('.'),
      triplet: () => !!beat && !!beat.TupletNum,
      rest: () => !!beat && !!beat.Rest,
      tie: () => !!n && !!n.Tied,
      hammer: () => !!beat && !!beat.HammerPull,
      slide: () => !!beat && !!beat.HasSlideFrom,
      bend: () => !!beat && beat.BendSemitones > 0,
      pm: () => !!beat && !!beat.PalmMute,
    };
    tb.querySelectorAll('button[data-act]').forEach((b) => {
      b.disabled = !ok;
      b.classList.toggle('active', ok && !!on[b.dataset.act] && on[b.dataset.act](b));
    });
  }

  let noteTimer = null;
  function setStatusNote(msg) {
    const el = $('te-note');
    el.textContent = msg;
    clearTimeout(noteTimer);
    noteTimer = setTimeout(() => { el.textContent = ''; }, 1500);
  }

  function renderStatus() {
    const el = $('te-status');
    el.className = 'te-status';
    if (st.error) { el.textContent = st.error; el.classList.add('err'); return; }
    if (!st.riff) { el.textContent = ''; return; }
    if (st.riff.Drums) { el.textContent = 'Drum tracks can’t be edited here yet: use Source, or edit the Guitar Pro file.'; return; }
    const bar = curBar();
    const used = bar ? barBeats(bar) : 0, cap = barCapacity();
    const fmt = (x) => (Math.round(x * 100) / 100).toString();
    const secBars = bridge.measures(currentSection()).length;
    let msg = `Bar ${st.bar + 1} of ${bars().length} · beat ${st.beat + 1} · string ${st.string} · ${fmt(used)} of ${fmt(cap)} beats`;
    if (used > cap + 1e-9) { msg += ' — too long'; el.classList.add('err'); }
    else if (used < cap - 1e-9) { msg += ' — not full'; el.classList.add('warn'); }
    if (secBars && secBars !== bars().length) msg += ` · the section has ${secBars} bars`;
    el.textContent = msg;
  }

  function renderNotice() {
    const el = $('te-notice');
    el.hidden = st.source !== 'lilypond';
    el.textContent = 'This track is written as LilyPond text. Your first edit here stores it as tab data (a string and fret per note), which Source shows as JSON.';
  }

  function sectionTitle(sec, i) {
    const n = bridge.sections().slice(0, i + 1).filter((s) => s.label === sec.label).length;
    const total = bridge.sections().filter((s) => s.label === sec.label).length;
    return `${i + 1}. ${sec.label || 'Untitled'}${total > 1 ? ' ' + n : ''}` + (sec.scores && sec.scores.length ? '' : ' (no tab)');
  }

  function renderPickers() {
    const secSel = $('te-section'), voiceSel = $('te-voice');
    const sections = bridge.sections();
    secSel.innerHTML = '';
    sections.forEach((sec, i) => secSel.add(new Option(sectionTitle(sec, i), String(i))));
    secSel.value = String(st.sec);
    voiceSel.innerHTML = '';
    const sec = currentSection();
    const voices = (sec && sec.scores) || [];
    voices.forEach((v, i) => voiceSel.add(new Option(v.label || 'Track', String(i))));
    voiceSel.hidden = !voices.length;
    voiceSel.value = String(st.voice);
    $('te-delete-voice').hidden = !voices.length;
    $('te-empty').hidden = !!voices.length || !sections.length;
  }

  // ---- Loading a voice -------------------------------------------------------

  async function loadVoice() {
    const seq = ++st.loadSeq;
    const v = currentVoice();
    st.riff = null; st.error = ''; st.source = ''; st.tex = '';
    renderPickers();
    if (!v) { st.body = null; renderNotice(); render(); return; }
    st.body = v.body;
    const body = (v.body || '').trim();
    let riff = null;
    if (body.startsWith('{')) {
      try { riff = JSON.parse(body); st.source = 'json'; } catch (e) { /* braced LilyPond — the server decides */ }
    }
    if (!riff) {
      try {
        const res = await fetch('/api/score/riff', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ label: v.label, body: v.body }),
        });
        const data = await res.json();
        if (seq !== st.loadSeq) return;
        if (!res.ok) throw new Error(data.error || 'could not read this track');
        riff = data.riff;
        st.source = 'lilypond';
      } catch (e) {
        if (seq !== st.loadSeq) return;
        st.error = 'This track doesn’t parse: ' + e.message + '. Fix it in Source.';
      }
    }
    if (riff && !riff.Drums && !(riff.Tuning || []).length) { st.error = 'This track has no tuning.'; riff = null; }
    if (riff) for (const bar of riff.Bars || []) bar.Beats = bar.Beats || [];
    st.riff = riff;
    clampCursor();
    renderNotice();
    render();
  }

  function selectSection(i) {
    st.sec = i; st.voice = 0; st.bar = 0; st.beat = 0; st.string = 1;
    loadVoice();
  }

  function newVoice(kind) {
    const sec = currentSection();
    if (!sec) return;
    const labels = new Set((sec.scores || []).map((v) => (v.label || '').toLowerCase()));
    let label = kind, n = 2;
    while (labels.has(label.toLowerCase())) label = `${kind} ${n++}`;
    const count = Math.max(1, bridge.measures(sec).length);
    const riff = { Tuning: STANDARD_TUNING[kind].slice(), MinimumFret: 0, Bars: Array.from({ length: count }, () => ({ Beats: restBeats(barCapacity()) })) };
    bridge.begin();
    sec.scores = sec.scores || [];
    sec.scores.push({ label, body: JSON.stringify(encodeRiff(riff)) });
    st.voice = sec.scores.length - 1;
    st.bar = 0; st.beat = 0; st.string = kind === 'Bass' ? 4 : 6;
    loadVoice().then(() => $('te-canvas-wrap').focus());
  }

  function deleteVoice() {
    const sec = currentSection(), v = currentVoice();
    if (!v || !confirm(`Delete the “${v.label}” track from this section?`)) return;
    bridge.begin();
    sec.scores.splice(st.voice, 1);
    st.voice = 0;
    loadVoice();
  }

  // ---- Keyboard --------------------------------------------------------------

  function runAction(act, arg) {
    switch (act) {
      case 'dur': setDuration(arg); break;
      case 'dot': toggleDot(); break;
      case 'triplet': toggleTriplet(); break;
      case 'rest': makeRest(); break;
      case 'tie': toggleTie(); break;
      case 'hammer': toggleFlag('HammerPull'); break;
      case 'slide': toggleSlide(); break;
      case 'bend': cycleBend(); break;
      case 'pm': toggleFlag('PalmMute'); break;
      case 'insert': insertBeat(); break;
      case 'addbar': addBar(); break;
      case 'dupbar': duplicateBar(); break;
      case 'clearbar': clearBar(); break;
      case 'delbar': deleteBar(); break;
    }
  }

  function onKey(e) {
    if (!st.riff) return;
    const mod = e.metaKey || e.ctrlKey;
    const k = e.key;
    let handled = true;
    if (mod) {
      const lk = k.toLowerCase();
      if (lk === 'z' && !e.shiftKey) bridge.undo();
      else if ((lk === 'z' && e.shiftKey) || lk === 'y') bridge.redo();
      else if (lk === 'c') copyBar();
      else if (lk === 'v') pasteBar();
      else if (k === 'ArrowLeft') moveBar(-1);
      else if (k === 'ArrowRight') moveBar(1);
      else handled = false;
    } else if (e.altKey) {
      handled = false;
    } else if (/^[0-9]$/.test(k)) {
      if (editable()) typeDigit(Number(k));
    } else {
      switch (k) {
        case 'ArrowRight': moveRight(); break;
        case 'ArrowLeft': moveLeft(); break;
        case 'ArrowUp': st.string = Math.max(1, st.string - 1); st.digit = null; render(); break;
        case 'ArrowDown': st.string = Math.min(numStrings(), st.string + 1); st.digit = null; render(); break;
        case 'Home': st.beat = 0; render(); break;
        case 'End': st.beat = curBar().Beats.length - 1; render(); break;
        case 'Backspace': case 'Delete': deleteAtCursor(); break;
        case 'Insert': case 'i': case 'I': insertBeat(); break;
        case '+': case '=': changeDuration(1); break;
        case '-': case '_': changeDuration(-1); break;
        case '.': toggleDot(); break;
        case 'r': case 'R': makeRest(); break;
        case 't': case 'T': toggleTriplet(); break;
        case 'l': case 'L': toggleTie(); break;
        case 'h': case 'H': toggleFlag('HammerPull'); break;
        case 's': case 'S': toggleSlide(); break;
        case 'b': case 'B': cycleBend(); break;
        case 'p': case 'P': toggleFlag('PalmMute'); break;
        case 'Escape': $('te-canvas-wrap').blur(); break;
        default: handled = false;
      }
    }
    if (handled) { e.preventDefault(); e.stopPropagation(); }
  }

  // ---- Setup -----------------------------------------------------------------

  function init() {
    bridge = window.SongEditorBridge;
    if (!bridge || !$('tab-editor')) return;
    if (!initialized) {
      initialized = true;
      $('te-section').addEventListener('change', (e) => selectSection(Number(e.target.value)));
      $('te-voice').addEventListener('change', (e) => { st.voice = Number(e.target.value); st.bar = 0; st.beat = 0; loadVoice(); });
      $('te-new-voice').addEventListener('change', (e) => { const k = e.target.value; e.target.value = ''; if (k) newVoice(k); });
      $('te-delete-voice').addEventListener('click', deleteVoice);
      const wrap = $('te-canvas-wrap');
      wrap.addEventListener('keydown', onKey);
      wrap.addEventListener('mousedown', (e) => { pickAt(e); });
      wrap.addEventListener('focus', () => wrap.classList.add('focused'));
      wrap.addEventListener('blur', () => wrap.classList.remove('focused'));
    }
    refresh();
  }

  // refresh re-reads the song after anything outside this editor replaced
  // it (an undo, a reload of the structure): same section and track when
  // they still exist, the voice re-decoded only if its body changed.
  function refresh() {
    if (!initialized) return;
    const sections = bridge.sections();
    if (!sections.length) { st.sec = -1; renderPickers(); st.riff = null; render(); return; }
    if (st.sec < 0 || st.sec >= sections.length) {
      st.sec = Math.max(0, sections.findIndex((s) => s.scores && s.scores.length));
      st.voice = 0;
    }
    const sec = currentSection();
    if (!sec.scores || st.voice >= sec.scores.length) st.voice = 0;
    const v = currentVoice();
    if (!v || v.body !== st.body || !st.riff) { loadVoice(); return; }
    renderPickers();
    render();
  }

  window.TabEditor = { init, refresh };
})();

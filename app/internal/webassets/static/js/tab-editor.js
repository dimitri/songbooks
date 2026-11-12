// Scores tab: a Guitar Pro–style editor for one section's "score[label]:"
// voice at a time (see docs/editor-plan.md) — guitar as tab, bass as an
// F-clef staff over its tab (notes entered on either), drums on a
// percussion staff, one piece at a time.
//
// The model is the voice's tab.Riff (internal/tab/tab.go), edited in place
// and written back into the drawer's in-memory song — the same
// structSections the Chart editor edits, reached through
// window.SongEditorBridge (song.js) — so Save, Discard, Undo and the
// unsaved-changes guard all work exactly as they do for chords. alphaTab is
// only the view: every edit re-emits alphaTeX (a port of internal/tab's
// writeBeat and writeDrumBeat) and re-renders, and its boundsLookup places
// the cursor and answers clicks.
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

  // The drum kit, top of the staff to the bottom. line is alphaTab's staff
  // position for that General MIDI piece (0 = top line, one step = half a
  // line space), art its alphaTeX articulation name (the same names
  // internal/tab's drumArticulationName declares), key its shortcut.
  const DRUM_PIECES = [
    { midi: 49, name: 'Crash', art: 'Crash', key: 'c', line: -2 },
    { midi: 46, name: 'Hi-hat open', art: 'HiHatOpen', key: 'o', line: -1 },
    { midi: 42, name: 'Hi-hat', art: 'HiHatClosed', key: 'h', line: -1 },
    { midi: 51, name: 'Ride', art: 'Ride', key: 'y', line: 0 },
    { midi: 53, name: 'Ride bell', art: 'RideBell', key: '', line: 0 },
    { midi: 56, name: 'Cowbell', art: 'Cowbell', key: '', line: 0 },
    { midi: 50, name: 'High tom', art: 'TomHigh', key: '1', line: 1 },
    { midi: 38, name: 'Snare', art: 'Snare', key: 's', line: 3 },
    { midi: 37, name: 'Side stick', art: 'SideStick', key: 'x', line: 3 },
    { midi: 47, name: 'Mid tom', art: 'TomMid', key: '2', line: 4 },
    { midi: 45, name: 'Low tom', art: 'TomLow', key: '3', line: 5 },
    { midi: 43, name: 'Floor tom', art: 'TomFloorHigh', key: '4', line: 6 },
    { midi: 41, name: 'Low floor tom', art: 'TomFloorLow', key: '5', line: 5 },
    { midi: 36, name: 'Kick', art: 'Kick', key: 'k', line: 7 },
    { midi: 44, name: 'Hi-hat pedal', art: 'HiHatPedal', key: 'f', line: 9 },
  ];
  // Every piece internal/tab's drumArticulationName knows is in the list
  // above; a General MIDI piece outside it (from Guitar Pro) isn't drawn.
  const DRUM_ART_EXTRA = {};
  const drumArt = (midi) => (DRUM_PIECES.find((p) => p.midi === midi) || {}).art || DRUM_ART_EXTRA[midi] || '';

  // The staff: guitar and bass are written an octave above where they
  // sound. A clef's top line as a diatonic step (octave × 7 + letter):
  // the F clef's is A3 (26), the treble clef's F5 (38).
  const WRITTEN_OFFSET = 12;
  const topStep = () => (st.kind === 'bass' ? 26 : 38);
  const NOTE_NAMES = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B'];
  const PC_LETTER = [0, 0, 1, 1, 2, 3, 3, 4, 4, 5, 5, 6]; // pitch class → letter index (sharps)
  const LETTER_PC = [0, 2, 4, 5, 7, 9, 11];
  const LETTERS = 'CDEFGAB';

  const $ = (id) => document.getElementById(id);
  let bridge = null;
  let initialized = false;

  const st = {
    sec: -1, voice: -1,
    riff: null,        // the Riff being edited (decoded from the voice body)
    body: null,        // the voice body st.riff was decoded from
    source: '',        // 'json' | 'lilypond'
    kind: 'guitar',    // 'guitar' | 'bass' | 'drums'
    mode: 'tab',       // 'tab' or 'staff': where the cursor is when both are shown
    error: '',
    bar: 0, beat: 0, string: 1,
    piece: DRUM_PIECES.findIndex((p) => p.midi === 36), // drums: index into DRUM_PIECES (the kick)
    staffStep: 22,     // bass staff cursor on an empty beat (written D3, the middle line)
    digit: null,       // { bar, beat, string, val, t } — the last fret digit typed
    clip: null,        // a copied bar
    api: null, apiKind: '',
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

  // ---- Pitch -----------------------------------------------------------------

  function noteName(midi) { return NOTE_NAMES[midi % 12] + (Math.floor(midi / 12) - 1); }
  function diatonicStep(midi) { return (Math.floor(midi / 12) - 1) * 7 + PC_LETTER[midi % 12]; }
  function naturalMidi(step) { const o = Math.floor(step / 7), l = ((step % 7) + 7) % 7; return 12 * (o + 1) + LETTER_PC[l]; }

  // ---- Model access ----------------------------------------------------------

  function currentSection() { return bridge.sections()[st.sec] || null; }
  function currentVoice() {
    const sec = currentSection();
    return sec && sec.scores ? sec.scores[st.voice] || null : null;
  }
  function bars() { return st.riff ? st.riff.Bars : []; }
  function curBar() { return bars()[st.bar]; }
  function curBeat() { const b = curBar(); return b ? b.Beats[st.beat] : null; }
  function numStrings() { return st.riff && st.riff.Tuning ? st.riff.Tuning.length : 6; }
  function noteOn(beat, s) { return beat && !beat.Rest ? (beat.Notes || []).find((n) => n.String === s) : null; }
  // The note the bass staff cursor is on: the one on the cursor's string,
  // or else the beat's lowest (a bass line is mostly one note at a time).
  function staffNote(beat) {
    if (!beat || beat.Rest || !(beat.Notes || []).length) return null;
    return noteOn(beat, st.string) || beat.Notes.reduce((a, b) => (b.MIDI < a.MIDI ? b : a));
  }
  function drumNote(beat, midi) { return beat && !beat.Rest ? (beat.Notes || []).find((n) => n.MIDI === midi) : null; }
  function editable() { return !!st.riff && !st.error; }
  // shownAs is internal/tab's Riff.ShownAs: the track's own notation, or
  // the instrument's usual one (bass: staff and tab, guitar: tab).
  function shownAs() {
    if (!st.riff || st.riff.Drums) return 'staff';
    if (['tab', 'staff', 'both'].includes(st.riff.Notation)) return st.riff.Notation;
    return st.kind === 'bass' ? 'both' : 'tab';
  }
  function onStaff() { return st.kind !== 'drums' && cursorStaff() === 'staff'; }
  function kindOf(riff) { return riff.Drums ? 'drums' : (riff.Tuning || []).length <= 4 ? 'bass' : 'guitar'; }

  function clampCursor() {
    const bs = bars();
    st.bar = Math.max(0, Math.min(st.bar, bs.length - 1));
    const n = bs[st.bar] ? bs[st.bar].Beats.length : 1;
    st.beat = Math.max(0, Math.min(st.beat, n - 1));
    st.string = Math.max(1, Math.min(st.string, numStrings()));
    st.piece = Math.max(0, Math.min(st.piece, DRUM_PIECES.length - 1));
  }

  // The stored form: Go's own field names (internal/tab decodes it with
  // encoding/json), zero values left out to keep the line short.
  function encodeRiff(r) {
    const out = {};
    if (r.Drums) out.Drums = true;
    else { out.Tuning = r.Tuning; out.MinimumFret = r.MinimumFret || 0; }
    if (r.Pattern) out.Pattern = true;
    if (r.Notation) out.Notation = r.Notation;
    out.Bars = r.Bars.map((bar) => ({
      Beats: bar.Beats.map((b) => {
        const o = {};
        if (b.Rest) o.Rest = true;
        o.Duration = b.Duration;
        if (!b.Rest) o.Notes = (b.Notes || []).map((n) => {
          if (r.Drums) return { MIDI: n.MIDI };
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

  function sounding(beat) { if (beat.Rest) { beat.Rest = false; beat.Notes = []; } beat.Notes = beat.Notes || []; }

  function setFret(fret, joinPrevious) {
    if (fret > MAX_FRET) return;
    edit(() => {
      const beat = curBeat();
      const s = st.string;
      sounding(beat);
      const n = noteOn(beat, s);
      const midi = st.riff.Tuning[s - 1] + fret;
      if (n) { n.Fret = fret; n.MIDI = midi; n.Tied = false; }
      else beat.Notes.push({ MIDI: midi, String: s, Fret: fret });
      beat.Notes.sort((a, b) => a.String - b.String);
    }, { joinPrevious });
  }

  function typeDigit(d) {
    const now = Date.now();
    const p = st.digit;
    if (p && p.bar === st.bar && p.beat === st.beat && p.string === st.string && now - p.t < DIGIT_WINDOW_MS && p.val > 0 && p.val * 10 + d <= MAX_FRET) {
      st.digit = null;
      setFret(p.val * 10 + d, true);
      return;
    }
    st.digit = { bar: st.bar, beat: st.beat, string: st.string, val: d, t: now };
    setFret(d, false);
  }

  // placePitch puts a sounding pitch on the beat — the bass staff's way in:
  // it replaces the note under the cursor, on that string when the fret
  // fits, otherwise on the string whose fret is closest to where the hand
  // already is.
  function placePitch(midi, joinPrevious) {
    const beat = curBeat();
    if (!beat) return;
    const old = onStaff() ? staffNote(beat) : noteOn(beat, st.string);
    const prev = previousNote();
    const ref = old ? old.Fret : prev ? prev.Fret : 3;
    let best = null;
    st.riff.Tuning.forEach((open, i) => {
      const s = i + 1, fret = midi - open;
      if (fret < 0 || fret > MAX_FRET) return;
      const cost = (old && s === old.String ? -3 : 0) + Math.abs(fret - ref); // stay on the string unless the hand would have to move
      if (!best || cost < best.cost) best = { s, fret, cost };
    });
    if (!best) { setStatusNote(noteName(midi) + ' is out of this instrument’s range'); return; }
    edit(() => {
      sounding(beat);
      beat.Notes = beat.Notes.filter((n) => n !== old && n.String !== best.s);
      beat.Notes.push({ MIDI: midi, String: best.s, Fret: best.fret });
      beat.Notes.sort((a, b) => a.String - b.String);
      st.string = best.s;
      st.staffStep = diatonicStep(midi + WRITTEN_OFFSET);
    }, { joinPrevious });
  }

  // A letter on the bass staff: that note, in the octave nearest the note
  // being replaced (or the one before it, or the staff cursor).
  function typeLetter(letter) {
    const beat = curBeat();
    const cur = staffNote(beat) || previousNote();
    const ref = cur ? cur.MIDI : naturalMidi(st.staffStep) - WRITTEN_OFFSET;
    const pc = LETTER_PC[LETTERS.indexOf(letter)];
    let best = null;
    for (let o = 0; o <= 8; o++) {
      const m = 12 * o + pc;
      if (!best || Math.abs(m - ref) < Math.abs(best - ref)) best = m;
    }
    placePitch(best);
  }

  function stepPitch(d) {
    const n = staffNote(curBeat());
    if (n) { placePitch(n.MIDI + d); return; }
    st.staffStep += d;
    render();
  }

  function toggleDrum(midi) {
    const beat = curBeat();
    if (!beat) return;
    edit(() => {
      if (drumNote(beat, midi)) {
        beat.Notes = beat.Notes.filter((n) => n.MIDI !== midi);
        if (!beat.Notes.length) clearBeat(beat);
      } else {
        sounding(beat);
        beat.Notes.push({ MIDI: midi, String: 0, Fret: 0 });
      }
    });
  }

  function deleteAtCursor() {
    const beat = curBeat();
    if (!beat) return;
    const here = st.kind === 'drums' ? drumNote(beat, DRUM_PIECES[st.piece].midi) : noteOn(beat, st.string);
    if (here) {
      edit(() => {
        beat.Notes = beat.Notes.filter((n) => n !== here);
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
    const b = bars()[bi].Beats[i];
    return onStaff() ? staffNote(b) : noteOn(b, st.string);
  }

  function toggleTie() {
    const beat = curBeat();
    const n = noteOn(beat, st.string);
    if (n) { edit(() => { n.Tied = !n.Tied; if (n.Tied) { const p = previousNote(); if (p) { n.Fret = p.Fret; n.MIDI = p.MIDI; } } }); return; }
    const p = previousNote();
    if (!p) return;
    edit(() => {
      sounding(beat);
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
    st.digit = null;
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
    st.digit = null;
    if (st.beat > 0) st.beat--;
    else if (st.bar > 0) { st.bar--; st.beat = curBar().Beats.length - 1; }
    render();
  }
  function moveBar(d) {
    st.bar = Math.max(0, Math.min(bars().length - 1, st.bar + d));
    st.beat = 0;
    render();
  }
  function moveVertical(d) { // -1 up, +1 down
    st.digit = null;
    if (st.kind === 'drums') st.piece = Math.max(0, Math.min(DRUM_PIECES.length - 1, st.piece + d));
    else if (onStaff()) { stepPitch(-d); return; }
    else st.string = Math.max(1, Math.min(numStrings(), st.string + d));
    render();
  }

  function addBar() { edit(() => { st.riff.Bars.splice(st.bar + 1, 0, { Beats: restBeats(barCapacity()) }); st.bar++; st.beat = 0; }); }
  function duplicateBar() { edit(() => { st.riff.Bars.splice(st.bar + 1, 0, JSON.parse(JSON.stringify(curBar()))); st.bar++; st.beat = 0; }); }
  function clearBar() { edit(() => { curBar().Beats = restBeats(barCapacity()); st.beat = 0; }); }
  function deleteBar() {
    if (bars().length <= 1) return;
    edit(() => { st.riff.Bars.splice(st.bar, 1); st.beat = 0; });
  }
  function copyBar() { if (curBar()) { st.clip = { kind: st.kind, bar: JSON.parse(JSON.stringify(curBar())) }; setStatusNote('bar copied'); } }
  function pasteBar() {
    if (!st.clip) return;
    if ((st.clip.kind === 'drums') !== (st.kind === 'drums')) { setStatusNote('a drum bar only pastes into a drum track'); return; }
    const clip = JSON.parse(JSON.stringify(st.clip.bar));
    // A bar copied from another track keeps its frets; its pitches follow this track's tuning.
    if (st.kind !== 'drums') for (const b of clip.Beats) for (const n of b.Notes || []) {
      if (n.String > numStrings()) n.String = numStrings();
      n.MIDI = st.riff.Tuning[n.String - 1] + n.Fret;
    }
    edit(() => { st.riff.Bars[st.bar] = clip; st.beat = 0; });
  }
  function setNotation(v) {
    edit(() => { if (v) st.riff.Notation = v; else delete st.riff.Notation; });
    const n = shownAs();
    st.mode = n === 'tab' ? 'tab' : n === 'staff' ? 'staff' : st.mode;
  }
  function togglePattern(on) {
    edit(() => { if (on) st.riff.Pattern = true; else delete st.riff.Pattern; });
  }

  // ---- alphaTeX --------------------------------------------------------------
  // A port of internal/tab's writeBeat/writeDrumBeat, recording for each
  // bar which riff beat every alphaTab beat comes from (a slide adds a
  // grace beat in front of its note).

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
    const drumNames = st.kind === 'drums' ? (beat.Notes || []).map((n) => drumArt(n.MIDI)).filter(Boolean) : null;
    if (beat.Rest || !(beat.Notes || []).length || (drumNames && !drumNames.length)) {
      out = 'r.' + dur;
    } else if (drumNames) {
      out = drumNames.length === 1 ? `${drumNames[0]}.${dur}` : `(${drumNames.join(' ')}).${dur}`;
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
    let tex;
    if (st.kind === 'drums') {
      tex = '\\track "Drums" { instrument percussion }\n';
      const arts = new Map(DRUM_PIECES.map((p) => [p.art, p.midi]));
      for (const [m, a] of Object.entries(DRUM_ART_EXTRA)) arts.set(a, Number(m));
      for (const [a, m] of arts) tex += `\\articulation (${a} ${m})\n`;
      tex += '.\n';
    } else {
      tex = `\\tuning (${r.Tuning.map(noteName).join(' ')})\n`;
      if (st.kind === 'bass') tex += '\\clef F4\n';
      tex += '.\n';
    }
    st.map = [];
    r.Bars.forEach((bar, bi) => {
      if (bi > 0) tex += ' | ';
      else {
        if (!(tn === 4 && td === 4)) tex += `\\ts ${tn} ${td} `;
        if (st.kind === 'drums') tex += '\\clef neutral ';
      }
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

  // One alphaTab instance per kind of staff: guitar is tab only, bass an
  // F-clef staff over its tab, drums a percussion staff.
  function ensureApi() {
    const key = st.kind + ':' + shownAs() + ':' + narrow();
    if (st.api && st.apiKind === key) return st.api;
    if (st.api) { try { st.api.destroy(); } catch (e) { /* already gone */ } st.api = null; }
    const host = $('te-canvas');
    host.innerHTML = '';
    const settings = new alphaTab.Settings();
    settings.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    settings.core.useWorkers = false;
    settings.core.enableLazyLoading = false;
    settings.core.includeNoteBounds = true;
    settings.player.enablePlayer = false;
    const shown = shownAs();
    settings.display.staveProfile = shown === 'staff' ? alphaTab.StaveProfile.Score : shown === 'both' ? alphaTab.StaveProfile.ScoreTab : alphaTab.StaveProfile.Tab;
    settings.display.layoutMode = alphaTab.LayoutMode.Page;
    settings.display.barsPerRow = narrow() ? 2 : 4;
    settings.display.justifyLastSystem = true;
    settings.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
    settings.notation.elements.set(alphaTab.NotationElement.GuitarTuning, false);
    settings.notation.elements.set(alphaTab.NotationElement.ScoreTitle, false);
    settings.notation.elements.set(alphaTab.NotationElement.EffectDynamics, false);
    settings.notation.elements.set(alphaTab.NotationElement.TrackNames, false);
    const fg = color('--fg', new alphaTab.model.Color(0x24, 0x1e, 0x18));
    settings.display.resources.staffLineColor = color('--fg-muted', fg);
    settings.display.resources.mainGlyphColor = fg;
    settings.display.resources.secondaryGlyphColor = fg;
    settings.display.resources.scoreInfoColor = fg;
    settings.display.resources.barSeparatorColor = fg;
    settings.display.resources.barNumberColor = color('--fg-muted', fg);
    st.api = new alphaTab.AlphaTabApi(host, settings);
    st.apiKind = key;
    if (st.kind !== 'drums' && shown !== 'tab') {
      st.api.scoreLoaded.on((score) => { for (const t of score.tracks) for (const s of t.staves) s.displayTranspositionPitch = -WRITTEN_OFFSET; });
    }
    st.api.postRenderFinished.on(drawCursor);
    return st.api;
  }

  // A phone-width editor draws two bars per row, so notes stay big enough
  // to tap.
  function narrow() { return ($('te-canvas-wrap').clientWidth || 800) < 560; }

  // ---- Touch keypad ----------------------------------------------------------
  // The editor is keyboard-first; on a phone or tablet (or on request) a
  // keypad under the score sends the same keys: frets, note names on a
  // staff, the cursor moves, delete. The toolbar above already holds
  // durations, techniques and drum pieces.
  const coarse = window.matchMedia('(pointer: coarse)');
  function padWanted() {
    let saved = null;
    try { saved = localStorage.getItem('songbooks:tab-editor:pad'); } catch (e) { /* storage off */ }
    return saved === null ? coarse.matches : saved === '1';
  }
  function padKeys() {
    const move = [['ArrowLeft', '←'], ['ArrowRight', '→'], ['ArrowUp', '↑'], ['ArrowDown', '↓'], ['Backspace', '⌫']];
    if (st.kind === 'drums') return [...move, [' ', 'Hit']];
    if (onStaff()) return [...'CDEFGAB'.split('').map((l) => [l, l]), ...move];
    return [...'0123456789'.split('').map((d) => [d, d]), ...move];
  }
  function renderPad() {
    const pad = $('te-pad');
    $('te-pad-toggle').setAttribute('aria-pressed', padWanted() ? 'true' : 'false');
    pad.hidden = !padWanted() || !st.riff || !!st.error;
    if (pad.hidden) return;
    const keys = padKeys();
    const sig = keys.map((k) => k[0]).join('|');
    if (pad.dataset.sig === sig) return;
    pad.dataset.sig = sig;
    pad.innerHTML = '';
    for (const [key, label] of keys) {
      const b = document.createElement('button');
      b.type = 'button';
      b.dataset.key = key;
      b.textContent = label;
      if (key.startsWith('Arrow') || key === 'Backspace' || key === ' ') b.className = 'te-pad-move';
      pad.appendChild(b);
    }
  }
  function pressPad(key) {
    onKey({ key, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, preventDefault() {}, stopPropagation() {} });
  }

  function render() {
    renderPad();
    renderToolbar();
    renderStatus();
    if (!st.riff || st.error) {
      $('te-canvas-wrap').hidden = true;
      return;
    }
    $('te-canvas-wrap').hidden = false;
    clampCursor();
    const tex = buildTex();
    if (tex === st.tex && st.api && st.apiKind === st.kind + ':' + shownAs() + ':' + narrow()) { drawCursor(); return; }
    st.tex = tex;
    try {
      ensureApi().tex(tex);
    } catch (e) {
      st.error = 'alphaTab could not draw this track: ' + e.message;
      renderStatus();
    }
  }

  // The bar's bounds on one staff: 'tab', or 'staff' (the F-clef staff of a
  // bass, a drum track's only staff).
  function barBounds(barIndex, which) {
    const lookup = st.api && st.api.renderer.boundsLookup;
    if (!lookup) return null;
    for (const sys of lookup.staffSystems) {
      for (const mb of sys.bars) {
        if (mb.index !== barIndex || !mb.bars.length) continue;
        return which === 'tab' ? mb.bars[mb.bars.length - 1] : mb.bars[0];
      }
    }
    return null;
  }
  // Tab: string s's line, evenly spaced from string 1 (top) to the last.
  function stringY(bb, s) {
    const v = bb.visualBounds;
    const n = numStrings();
    return v.y + (n > 1 ? (s - 1) * v.h / (n - 1) : v.h / 2);
  }
  // Five-line staff: one step (line to space) is an eighth of its height.
  function staffStepPx(bb) { return bb.visualBounds.h / 8; }
  function drumY(bb, piece) { return bb.visualBounds.y + DRUM_PIECES[piece].line * staffStepPx(bb); }
  function bassStaffY(bb, step) { return bb.visualBounds.y + (topStep() - step) * staffStepPx(bb); }
  function cursorStaff() {
    if (st.kind === 'drums') return 'staff';
    const n = shownAs();
    return n === 'both' ? st.mode : n;
  }

  function drawCursor() {
    const cursor = $('te-cursor'), barHl = $('te-bar-hl');
    const which = cursorStaff();
    const bb = barBounds(st.bar, which);
    if (!bb || !curBar()) { cursor.hidden = true; barHl.hidden = true; return; }
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
    let y, h;
    if (which === 'tab') {
      y = stringY(bb, st.string);
      h = Math.min(16, v.h / Math.max(1, numStrings() - 1) + 2);
    } else {
      h = staffStepPx(bb) * 2 + 2;
      if (st.kind === 'drums') y = drumY(bb, st.piece);
      else {
        const n = staffNote(curBeat());
        y = bassStaffY(bb, n ? diatonicStep(n.MIDI + WRITTEN_OFFSET) : st.staffStep);
      }
    }
    Object.assign(cursor.style, { left: (bv.x + bv.w / 2 - w / 2) + 'px', top: (y - h / 2) + 'px', width: w + 'px', height: h + 'px' });
    cursor.hidden = false;
    if (document.activeElement === $('te-canvas-wrap')) cursor.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  function pickAt(ev) {
    const lookup = st.api && st.api.renderer.boundsLookup;
    if (!lookup || !st.riff) return;
    const rect = $('te-canvas').getBoundingClientRect();
    const x = ev.clientX - rect.left, y = ev.clientY - rect.top;
    const beat = lookup.getBeatAtPos(x, y);
    if (!beat) return;
    const barIndex = beat.voice.bar.index;
    const map = st.map[barIndex];
    if (!map) return;
    st.bar = barIndex;
    st.beat = map[beat.index] ?? 0;
    st.digit = null;
    if (st.kind !== 'drums' && shownAs() === 'both') {
      // Which staff was clicked: the nearer of the two.
      const sb = barBounds(barIndex, 'staff'), tb = barBounds(barIndex, 'tab');
      const dist = (bb) => { const v = bb.visualBounds; return y < v.y ? v.y - y : y > v.y + v.h ? y - v.y - v.h : 0; };
      if (sb && tb) st.mode = dist(sb) < dist(tb) ? 'staff' : 'tab';
    }
    const which = cursorStaff();
    const bb = barBounds(barIndex, which);
    if (bb) {
      if (which === 'tab') {
        let best = 1, bestD = Infinity;
        for (let s = 1; s <= numStrings(); s++) {
          const d = Math.abs(stringY(bb, s) - y);
          if (d < bestD) { bestD = d; best = s; }
        }
        st.string = best;
      } else if (st.kind === 'drums') {
        const b = curBeat();
        let best = st.piece, bestD = Infinity;
        DRUM_PIECES.forEach((p, i) => {
          const d = Math.abs(drumY(bb, i) - y) - (drumNote(b, p.midi) ? 0.5 : 0); // a piece already on the beat wins a tie
          if (d < bestD) { bestD = d; best = i; }
        });
        st.piece = best;
      } else {
        st.staffStep = topStep() - Math.round((y - bb.visualBounds.y) / staffStepPx(bb));
        // Clicking a note head selects that note (its string).
        const b = curBeat();
        let near = null;
        for (const n of (b && !b.Rest ? b.Notes : [])) {
          const d = Math.abs(bassStaffY(bb, diatonicStep(n.MIDI + WRITTEN_OFFSET)) - y);
          if (d <= staffStepPx(bb) * 1.5 && (!near || d < near.d)) near = { n, d };
        }
        if (near) st.string = near.n.String;
        else if (b && !b.Rest && b.Notes.length === 1) st.string = b.Notes[0].String;
      }
    }
    render();
  }

  // ---- Toolbar, pickers, status ---------------------------------------------

  const MELODIC = ['guitar', 'bass'], ALL = ['guitar', 'bass', 'drums'];
  const TOOLS = [
    { act: 'mode', arg: 'staff', label: 'Staff', title: 'Enter notes on the staff: A–G, ↑ ↓ by semitone', kinds: MELODIC, both: true },
    { act: 'mode', arg: 'tab', label: 'Tab', title: 'Enter notes on the tab: fret numbers', kinds: MELODIC, both: true },
    { sep: true, kinds: MELODIC, both: true },
    ...DURATIONS.slice(0, 6).map((d) => ({ act: 'dur', arg: d, label: d === '1' ? '1' : '1/' + d, title: `${DURATION_LABEL[d]} note`, kinds: ALL })),
    { act: 'dot', label: '•', title: 'Dotted (.)', kinds: ALL },
    { act: 'triplet', label: '3', title: 'Triplet (T)', kinds: ALL },
    { sep: true, kinds: ALL },
    { act: 'rest', label: 'Rest', title: 'Rest (R)', kinds: ALL },
    { act: 'tie', label: 'Tie', title: 'Tie to the previous note (L)', kinds: MELODIC },
    { act: 'hammer', label: 'H/P', title: 'Hammer-on / pull-off (H)', kinds: MELODIC },
    { act: 'slide', label: 'Slide', title: 'Slide in from the previous fret (S)', kinds: MELODIC },
    { act: 'bend', label: 'Bend', title: 'Bend: ½, full, off (B)', kinds: MELODIC },
    { act: 'pm', label: 'P.M.', title: 'Palm mute (P)', kinds: MELODIC },
    ...DRUM_PIECES.map((p, i) => ({ act: 'drum', arg: String(i), label: p.name, title: p.key ? `${p.name} (${p.key.toUpperCase()})` : p.name, kinds: ['drums'] })),
    { sep: true, kinds: ALL },
    { act: 'insert', label: '+ beat', title: 'Insert a beat before the cursor (Insert or I)', kinds: ALL },
    { act: 'addbar', label: '+ bar', title: 'Add an empty bar after this one', kinds: ALL },
    { act: 'dupbar', label: 'Duplicate bar', title: 'Duplicate this bar', kinds: ALL },
    { act: 'clearbar', label: 'Clear bar', title: 'Replace this bar with rests', kinds: ALL },
    { act: 'delbar', label: 'Delete bar', title: 'Delete this bar', kinds: ALL },
  ];

  function renderToolbar() {
    const tb = $('te-toolbar');
    if (!tb.childElementCount) {
      for (const t of TOOLS) {
        const el = document.createElement(t.sep ? 'span' : 'button');
        el.dataset.kinds = t.kinds.join(' ');
        if (t.both) el.dataset.both = '1';
        if (t.sep) { el.className = 'te-sep'; tb.appendChild(el); continue; }
        el.type = 'button';
        el.dataset.act = t.act;
        if (t.arg) el.dataset.arg = t.arg;
        el.textContent = t.label;
        el.title = t.title;
        tb.appendChild(el);
      }
      tb.addEventListener('mousedown', (e) => e.preventDefault()); // keep the keyboard focus on the score
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
      mode: (b) => b.dataset.arg === st.mode,
      dur: (b) => b.dataset.arg === base,
      dot: () => !!beat && beat.Duration.endsWith('.'),
      triplet: () => !!beat && !!beat.TupletNum,
      rest: () => !!beat && !!beat.Rest,
      tie: () => !!n && !!n.Tied,
      hammer: () => !!beat && !!beat.HammerPull,
      slide: () => !!beat && !!beat.HasSlideFrom,
      bend: () => !!beat && beat.BendSemitones > 0,
      pm: () => !!beat && !!beat.PalmMute,
      drum: (b) => !!drumNote(beat, DRUM_PIECES[Number(b.dataset.arg)].midi),
    };
    tb.querySelectorAll('[data-kinds]').forEach((el) => {
      el.hidden = !el.dataset.kinds.split(' ').includes(st.kind) || (!!el.dataset.both && shownAs() !== 'both');
    });
    tb.querySelectorAll('button[data-act]').forEach((b) => {
      b.disabled = !ok && b.dataset.act !== 'mode';
      b.classList.toggle('active', ok && !!on[b.dataset.act] && on[b.dataset.act](b));
      b.classList.toggle('te-selected', b.dataset.act === 'drum' && Number(b.dataset.arg) === st.piece);
    });
    const nsel = $('te-notation');
    nsel.hidden = !st.riff || st.kind === 'drums';
    nsel.value = (st.riff && st.riff.Notation) || '';
    nsel.disabled = !editable();
    const pat = $('te-pattern');
    pat.checked = !!(st.riff && st.riff.Pattern);
    pat.disabled = !editable();
    $('te-pattern-label').hidden = !st.riff;
  }

  let noteTimer = null;
  function setStatusNote(msg) {
    const el = $('te-note');
    el.textContent = msg;
    clearTimeout(noteTimer);
    noteTimer = setTimeout(() => { el.textContent = ''; }, 1800);
  }

  function renderStatus() {
    const el = $('te-status');
    el.className = 'te-status';
    if (st.error) { el.textContent = st.error; el.classList.add('err'); return; }
    if (!st.riff) { el.textContent = ''; return; }
    const bar = curBar();
    const used = bar ? barBeats(bar) : 0, cap = barCapacity();
    const fmt = (x) => (Math.round(x * 100) / 100).toString();
    const secBars = bridge.measures(currentSection()).length;
    let where;
    if (st.kind === 'drums') where = DRUM_PIECES[st.piece].name;
    else if (onStaff()) {
      const n = staffNote(curBeat());
      where = 'staff · ' + (n ? `${noteName(n.MIDI)} (string ${n.String}, fret ${n.Fret})` : noteName(naturalMidi(st.staffStep) - WRITTEN_OFFSET));
    } else where = `string ${st.string}`;
    let msg = `Bar ${st.bar + 1} of ${bars().length} · beat ${st.beat + 1} · ${where} · ${fmt(used)} of ${fmt(cap)} beats`;
    if (used > cap + 1e-9) { msg += ' — too long'; el.classList.add('err'); }
    else if (used < cap - 1e-9) { msg += ' — not full'; el.classList.add('warn'); }
    if (st.riff.Pattern) msg += secBars > bars().length ? ` · repeats through the section’s ${secBars} bars` : '';
    else if (secBars && secBars !== bars().length) msg += ` · the section has ${secBars} bars`;
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
    return `${i + 1}. ${sec.label || 'Untitled'}${total > 1 ? ' ' + n : ''}` + (sec.scores && sec.scores.length ? '' : ' (no score)');
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
    if (riff) {
      riff.Bars = riff.Bars || [];
      if (!riff.Bars.length) riff.Bars.push({ Beats: [{ Rest: true, Duration: '1' }] });
      for (const bar of riff.Bars) bar.Beats = bar.Beats || [];
      const kind = kindOf(riff);
      if (kind !== st.kind) { st.kind = kind; st.staffStep = topStep() - 4; } // the staff's middle line
    }
    st.riff = riff;
    if (riff && !riff.Drums) { const n = shownAs(); st.mode = n === 'tab' ? 'tab' : 'staff'; }
    clampCursor();
    renderNotice();
    render();
  }

  function selectSection(i) {
    st.sec = i; st.voice = 0; st.bar = 0; st.beat = 0;
    loadVoice();
  }

  // newVoice adds a track to the section: kind is Guitar, Bass, Drums, or
  // Pattern (one bar of drums that repeats through the section).
  function newVoice(kind) {
    const sec = currentSection();
    if (!sec) return;
    const base = kind === 'Pattern' ? 'Drums' : kind;
    const labels = new Set((sec.scores || []).map((v) => (v.label || '').toLowerCase()));
    let label = base, n = 2;
    while (labels.has(label.toLowerCase())) label = `${base} ${n++}`;
    const count = kind === 'Pattern' ? 1 : Math.max(1, bridge.measures(sec).length);
    const blank = () => Array.from({ length: count }, () => ({ Beats: restBeats(barCapacity()) }));
    const riff = base === 'Drums' ? { Drums: true, Bars: blank() } : { Tuning: STANDARD_TUNING[kind].slice(), MinimumFret: 0, Bars: blank() };
    if (kind === 'Pattern') riff.Pattern = true;
    bridge.begin();
    sec.scores = sec.scores || [];
    sec.scores.push({ label, body: JSON.stringify(encodeRiff(riff)) });
    st.voice = sec.scores.length - 1;
    st.bar = 0; st.beat = 0;
    st.string = kind === 'Bass' ? 4 : 6;
    st.piece = DRUM_PIECES.findIndex((p) => p.midi === 42);
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
      case 'mode': st.mode = arg; render(); break;
      case 'dur': setDuration(arg); break;
      case 'dot': toggleDot(); break;
      case 'triplet': toggleTriplet(); break;
      case 'rest': makeRest(); break;
      case 'tie': toggleTie(); break;
      case 'hammer': toggleFlag('HammerPull'); break;
      case 'slide': toggleSlide(); break;
      case 'bend': cycleBend(); break;
      case 'pm': toggleFlag('PalmMute'); break;
      case 'drum': st.piece = Number(arg); toggleDrum(DRUM_PIECES[st.piece].midi); break;
      case 'insert': insertBeat(); break;
      case 'addbar': addBar(); break;
      case 'dupbar': duplicateBar(); break;
      case 'clearbar': clearBar(); break;
      case 'delbar': deleteBar(); break;
    }
  }

  // Keys every kind of track shares; each kind's own keys come first.
  function commonKey(k) {
    switch (k) {
      case 'ArrowRight': moveRight(); return true;
      case 'ArrowLeft': moveLeft(); return true;
      case 'ArrowUp': moveVertical(-1); return true;
      case 'ArrowDown': moveVertical(1); return true;
      case 'Home': st.beat = 0; render(); return true;
      case 'End': st.beat = curBar().Beats.length - 1; render(); return true;
      case 'Backspace': case 'Delete': deleteAtCursor(); return true;
      case 'Insert': case 'i': case 'I': insertBeat(); return true;
      case '+': case '=': changeDuration(1); return true;
      case '-': case '_': changeDuration(-1); return true;
      case '.': toggleDot(); return true;
      case 'r': case 'R': makeRest(); return true;
      case 't': case 'T': toggleTriplet(); return true;
      case 'Escape': $('te-canvas-wrap').blur(); return true;
    }
    return false;
  }

  function drumKey(k) {
    const lk = k.toLowerCase();
    if (k === ' ' || k === 'Enter') { toggleDrum(DRUM_PIECES[st.piece].midi); return true; }
    const i = DRUM_PIECES.findIndex((p) => p.key && p.key === lk);
    if (i < 0) return false;
    st.piece = i;
    toggleDrum(DRUM_PIECES[i].midi);
    return true;
  }

  function melodicKey(k) {
    if (/^[0-9]$/.test(k)) { if (editable()) typeDigit(Number(k)); return true; }
    if (onStaff() && /^[a-gA-G]$/.test(k)) { typeLetter(k.toUpperCase()); return true; }
    switch (k) {
      case 'l': case 'L': toggleTie(); return true;
      case 'h': case 'H': toggleFlag('HammerPull'); return true;
      case 's': case 'S': toggleSlide(); return true;
      case 'b': case 'B': cycleBend(); return true;
      case 'p': case 'P': toggleFlag('PalmMute'); return true;
    }
    return false;
  }

  function onKey(e) {
    if (!st.riff) return;
    const k = e.key;
    let handled = true;
    if (e.metaKey || e.ctrlKey) {
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
    } else {
      handled = (st.kind === 'drums' ? drumKey(k) : melodicKey(k)) || commonKey(k);
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
      $('te-pattern').addEventListener('change', (e) => togglePattern(e.target.checked));
      $('te-notation').addEventListener('change', (e) => setNotation(e.target.value));
      const wrap = $('te-canvas-wrap');
      wrap.addEventListener('keydown', onKey);
      wrap.addEventListener('mousedown', (e) => { pickAt(e); });
      const pad = $('te-pad');
      pad.addEventListener('pointerdown', (e) => e.preventDefault()); // keep the focus (and no on-screen keyboard)
      pad.addEventListener('click', (e) => { const b = e.target.closest('button[data-key]'); if (b) pressPad(b.dataset.key); });
      $('te-pad-toggle').addEventListener('click', () => {
        try { localStorage.setItem('songbooks:tab-editor:pad', padWanted() ? '0' : '1'); } catch (e) { /* storage off */ }
        render();
      });
      let wasNarrow = null;
      window.addEventListener('resize', () => {
        if (!st.riff || $('te-canvas-wrap').hidden) return;
        const n = narrow();
        if (wasNarrow !== null && n !== wasNarrow) render();
        wasNarrow = n;
      });
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

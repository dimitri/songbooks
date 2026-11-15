// The Guitar Pro file editor (/song/{id}/gp-edit, gp_edit.html): edits the
// song's attached file itself and saves each change as a new file version
// (docs/gp-file-model.md §7), so nothing the file holds is lost on the
// way — every track, voice and effect stays as Guitar Pro wrote it.
//
// The file is held as alphaTab's own JSON form of its model (lower-case
// keys: masterbars, tracks[].staves[].bars[].voices[].beats[].notes[]).
// Every edit changes that JSON, and the score is rebuilt from it with
// JsonConverter.jsonToScore, which links ties, slides and hammer-ons and
// times every beat again — the same path a file being opened takes — then
// redrawn. The JSON text is also the undo history.
//
// Keys follow Guitar Pro's where it has them: arrows move, digits type a
// fret on the current string (two quick digits make 10–24), + and − make
// a note shorter or longer, Insert adds a beat, R rests, and letters
// toggle techniques. Strings are numbered as alphaTab and Guitar Pro do:
// 1 is the lowest.
(function () {
  const root = document.getElementById('gp-editor');
  if (!root || typeof alphaTab === 'undefined' || !window.GPFile) return;
  const variantId = root.dataset.variantId;
  let fileName = root.dataset.gpName || '';
  const $ = (id) => document.getElementById(id);
  const JC = alphaTab.model.JsonConverter;

  const DURATIONS = [1, 2, 4, 8, 16, 32, 64];
  const DURATION_LABEL = { 1: 'Whole', 2: 'Half', 4: 'Quarter', 8: 'Eighth', 16: '16th', 32: '32nd', 64: '64th' };
  const MAX_FRET = 24;
  const DIGIT_WINDOW_MS = 600;
  const UNDO_DEPTH = 100;
  // The drum kit, top of the staff to the bottom: General MIDI number,
  // name, alphaTeX articulation name, key.
  const DRUM_PIECES = [
    { midi: 49, name: 'Crash', art: 'Crash', key: 'c' },
    { midi: 46, name: 'Hi-hat open', art: 'HiHatOpen', key: 'o' },
    { midi: 42, name: 'Hi-hat', art: 'HiHatClosed', key: 'h' },
    { midi: 51, name: 'Ride', art: 'Ride', key: 'y' },
    { midi: 53, name: 'Ride bell', art: 'RideBell', key: '' },
    { midi: 56, name: 'Cowbell', art: 'Cowbell', key: '' },
    { midi: 50, name: 'High tom', art: 'TomHigh', key: '1' },
    { midi: 38, name: 'Snare', art: 'Snare', key: 's' },
    { midi: 37, name: 'Side stick', art: 'SideStick', key: 'x' },
    { midi: 47, name: 'Mid tom', art: 'TomMid', key: '2' },
    { midi: 45, name: 'Low tom', art: 'TomLow', key: '3' },
    { midi: 43, name: 'Floor tom', art: 'TomFloorHigh', key: '4' },
    { midi: 41, name: 'Low floor tom', art: 'TomFloorLow', key: '5' },
    { midi: 36, name: 'Kick', art: 'Kick', key: 'k' },
    { midi: 44, name: 'Hi-hat pedal', art: 'HiHatPedal', key: 'f' },
  ];

  const st = {
    doc: null, score: null, api: null,
    track: 0, voice: 0, bar: 0, beat: 0, string: 1, piece: 13,
    dur: 8, dirty: false, undo: [], redo: [], digit: null, clip: null,
    notation: '', playing: false,
  };

  // ---- The JSON model --------------------------------------------------------

  const tracksJ = () => st.doc.tracks;
  const staffJ = (t = st.track) => st.doc.tracks[t].staves[0];
  const masterJ = (b = st.bar) => st.doc.masterbars[b];
  const barJ = (b = st.bar) => staffJ().bars[b];
  const isDrums = () => !!staffJ().ispercussion;
  const tuning = () => (staffJ().stringtuning && staffJ().stringtuning.tunings) || [];
  const numStrings = () => tuning().length;
  const voiceJ = (b = st.bar) => barJ(b).voices[st.voice];
  const beatsJ = (b = st.bar) => { const v = voiceJ(b); return v ? v.beats : []; };
  const beatJ = () => beatsJ()[st.beat];

  // The model as alphaTab built it, for timing and drawing.
  const barM = (b = st.bar) => st.score.tracks[st.track].staves[0].bars[b];
  const beatM = () => { const v = barM().voices[st.voice]; return v && v.beats[st.beat]; };

  function restBeat(duration) {
    return { duration: duration || st.dur, dots: 0, notes: [], isempty: false, tupletnumerator: -1, tupletdenominator: -1 };
  }
  // A bar of rests filling time signature num/den.
  function restBeats(num, den) {
    if (num === 4 && den === 4) return [restBeat(1)];
    return Array.from({ length: num }, () => restBeat(den));
  }
  // The voice the cursor is on, created (empty, then rests) when the bar
  // has fewer voices.
  function ensureVoice(b = st.bar) {
    const bar = barJ(b);
    const mb = masterJ(b);
    while (bar.voices.length <= st.voice) bar.voices.push({ beats: [] });
    const v = bar.voices[st.voice];
    if (!v.beats.length || v.beats.every((x) => x.isempty)) v.beats = restBeats(mb.timesignaturenumerator, mb.timesignaturedenominator);
    return v;
  }

  function noteOnString(beat, s) { return beat && beat.notes.find((n) => n.string === s); }
  function drumArticulations() {
    const t = tracksJ()[st.track];
    if (!t.percussionarticulations) t.percussionarticulations = [];
    return t.percussionarticulations;
  }
  // The articulation index for a kit piece, added to the track when it
  // isn't there yet (its definition comes from alphaTab's own table, read
  // through a one-line alphaTeX document).
  const artTemplate = {};
  function articulationFor(piece) {
    const arts = drumArticulations();
    let i = arts.findIndex((a) => a.outputmidinumber === piece.midi);
    if (i >= 0) return i;
    if (!artTemplate[piece.midi]) {
      const imp = new alphaTab.importer.AlphaTexImporter();
      imp.initFromString(`\\track "D" { instrument percussion }\n\\staff {score}\n\\articulation (${piece.art} ${piece.midi})\n.\n(${piece.midi}).4 |`, new alphaTab.Settings());
      const doc = JSON.parse(JC.scoreToJson(imp.readScore()));
      artTemplate[piece.midi] = doc.tracks[0].percussionarticulations[0];
    }
    arts.push(JSON.parse(JSON.stringify(artTemplate[piece.midi])));
    return arts.length - 1;
  }
  function drumNote(beat, piece) {
    const arts = drumArticulations();
    return beat && beat.notes.find((n) => arts[n.percussionarticulation] && arts[n.percussionarticulation].outputmidinumber === piece.midi);
  }

  // ---- Edits, undo -------------------------------------------------------------

  const snapshot = () => JSON.stringify(st.doc);
  function edit(fn) {
    const before = snapshot();
    let ok;
    try { ok = fn(); } catch (e) { setStatus(e.message, true); st.doc = JSON.parse(before); return; }
    if (ok === false) { st.doc = JSON.parse(before); return; }
    st.undo.push(before);
    if (st.undo.length > UNDO_DEPTH) st.undo.shift();
    st.redo = [];
    markDirty(true);
    rebuild();
  }
  function undo() {
    if (!st.undo.length) return;
    st.redo.push(snapshot());
    st.doc = JSON.parse(st.undo.pop());
    markDirty(true);
    rebuild();
  }
  function redo() {
    if (!st.redo.length) return;
    st.undo.push(snapshot());
    st.doc = JSON.parse(st.redo.pop());
    markDirty(true);
    rebuild();
  }
  function markDirty(d) {
    st.dirty = d;
    $('gpe-save').disabled = !d;
    $('gpe-undo').disabled = !st.undo.length;
    $('gpe-redo').disabled = !st.redo.length;
    document.title = (d ? '• ' : '') + document.title.replace(/^• /, '');
  }

  // ---- Notes -------------------------------------------------------------------

  function setFret(fret) {
    if (isDrums()) return;
    edit(() => {
      ensureVoice();
      const beat = beatJ();
      if (!beat) return false;
      beat.isempty = false;
      let n = noteOnString(beat, st.string);
      if (!n) { n = { string: st.string, fret }; beat.notes.push(n); }
      n.fret = fret;
    });
  }
  function toggleDrum(piece) {
    edit(() => {
      ensureVoice();
      const beat = beatJ();
      if (!beat) return false;
      beat.isempty = false;
      const have = drumNote(beat, piece);
      if (have) beat.notes.splice(beat.notes.indexOf(have), 1);
      else beat.notes.push({ percussionarticulation: articulationFor(piece) });
    });
    st.piece = DRUM_PIECES.indexOf(piece);
  }
  function deleteAtCursor() {
    edit(() => {
      const beats = beatsJ();
      const beat = beatJ();
      if (!beat) return false;
      const n = isDrums() ? drumNote(beat, DRUM_PIECES[st.piece]) : noteOnString(beat, st.string);
      if (n) { beat.notes.splice(beat.notes.indexOf(n), 1); return; }
      if (beat.notes.length) return false;
      if (beats.length > 1) {
        beats.splice(st.beat, 1);
        st.beat = Math.min(st.beat, beats.length - 1);
      } else return false;
    });
  }
  function insertBeat() {
    edit(() => {
      const v = ensureVoice();
      v.beats.splice(st.beat, 0, restBeat(beatJ() ? beatJ().duration : st.dur));
    });
  }

  function setDuration(d) {
    st.dur = d;
    edit(() => { ensureVoice(); const b = beatJ(); if (!b) return false; b.duration = d; });
  }
  function stepDuration(dir) {
    const b = beatJ();
    const cur = b ? b.duration : st.dur;
    const i = DURATIONS.indexOf(cur);
    const next = DURATIONS[Math.max(0, Math.min(DURATIONS.length - 1, (i < 0 ? 2 : i) + dir))];
    setDuration(next);
  }
  // The note on the current string (the current kit piece on drums).
  function curNote() {
    const b = beatJ();
    return b && (isDrums() ? drumNote(b, DRUM_PIECES[st.piece]) : noteOnString(b, st.string));
  }

  // ---- Bars ----------------------------------------------------------------------

  function newBarAt(b, from) {
    const src = masterJ(from);
    const mb = { timesignaturenumerator: src.timesignaturenumerator, timesignaturedenominator: src.timesignaturedenominator, timesignaturecommon: src.timesignaturecommon, tripletfeel: src.tripletfeel };
    st.doc.masterbars.splice(b, 0, mb);
    for (const t of tracksJ()) {
      for (const s of t.staves) {
        const ref = s.bars[from] || s.bars[0];
        const bar = { clef: ref.clef, clefottava: ref.clefottava, keysignature: ref.keysignature, keysignaturetype: ref.keysignaturetype, voices: [] };
        bar.voices.push({ beats: restBeats(mb.timesignaturenumerator, mb.timesignaturedenominator) });
        s.bars.splice(b, 0, bar);
      }
    }
  }
  const addBarAfter = () => edit(() => { newBarAt(st.bar + 1, st.bar); st.bar++; st.beat = 0; });
  const addBarBefore = () => edit(() => { newBarAt(st.bar, st.bar); st.beat = 0; });
  const deleteBar = () => edit(() => {
    if (st.doc.masterbars.length < 2) return false;
    if (!confirm(`Delete bar ${st.bar + 1} in every track?`)) return false;
    st.doc.masterbars.splice(st.bar, 1);
    for (const t of tracksJ()) for (const s of t.staves) s.bars.splice(st.bar, 1);
    st.bar = Math.min(st.bar, st.doc.masterbars.length - 1);
    st.beat = 0;
  });
  const copyBar = () => { st.clip = JSON.parse(JSON.stringify(barJ())); setStatus(`Bar ${st.bar + 1} copied.`); };
  const pasteBar = () => {
    if (!st.clip) return;
    edit(() => {
      const strip = (x) => { if (Array.isArray(x)) x.forEach(strip); else if (x && typeof x === 'object') { delete x.id; for (const k of ['tiedestinationnoteid', 'tieoriginnoteid', 'hammerpulldestinationnoteid', 'hammerpulloriginnoteid', 'slidetargetnoteid', 'slideoriginnoteid']) delete x[k]; for (const k in x) strip(x[k]); } return x; };
      staffJ().bars[st.bar] = strip(JSON.parse(JSON.stringify(st.clip)));
    });
  };
  function askTimeSignature() {
    const mb = masterJ();
    const v = prompt('Time signature from this bar on (like 3/4, 6/8, 12/8):', `${mb.timesignaturenumerator}/${mb.timesignaturedenominator}`);
    const m = v && v.trim().match(/^(\d{1,2})\s*\/\s*(1|2|4|8|16|32)$/);
    if (!m) return;
    edit(() => {
      const on = mb.timesignaturenumerator + '/' + mb.timesignaturedenominator;
      for (let b = st.bar; b < st.doc.masterbars.length; b++) {
        const x = st.doc.masterbars[b];
        if (b > st.bar && x.timesignaturenumerator + '/' + x.timesignaturedenominator !== on) break;
        x.timesignaturenumerator = +m[1];
        x.timesignaturedenominator = +m[2];
      }
    });
  }
  // Key signatures by name: sharps (> 0) or flats (< 0).
  const KEYS = { C: 0, G: 1, D: 2, A: 3, E: 4, B: 5, 'F#': 6, 'C#': 7, F: -1, Bb: -2, Eb: -3, Ab: -4, Db: -5, Gb: -6, Cb: -7 };
  const MINOR = { A: 0, E: 1, B: 2, 'F#': 3, 'C#': 4, 'G#': 5, 'D#': 6, 'A#': 7, D: -1, G: -2, C: -3, F: -4, Bb: -5, Eb: -6, Ab: -7 };
  function keyName(sig, minor) {
    const t = minor ? MINOR : KEYS;
    return (Object.keys(t).find((k) => t[k] === sig) || '?') + (minor ? 'm' : '');
  }
  function askKey() {
    const bar = barJ();
    const v = prompt('Key signature from this bar on (like E, Bb, F#m):', keyName(bar.keysignature || 0, bar.keysignaturetype === 1));
    if (v === null) return;
    const m = v.trim().match(/^([A-G][b#]?)(m)?$/);
    const sig = m ? (m[2] ? MINOR : KEYS)[m[1]] : undefined;
    if (sig === undefined) { setStatus('A key like E, Bb or F#m, please.', true); return; }
    edit(() => {
      for (const t of tracksJ()) {
        for (const sf of t.staves) {
          if (sf.ispercussion) continue;
          const old = sf.bars[st.bar] && (sf.bars[st.bar].keysignature + '/' + sf.bars[st.bar].keysignaturetype);
          for (let b = st.bar; b < sf.bars.length; b++) {
            if (b > st.bar && sf.bars[b].keysignature + '/' + sf.bars[b].keysignaturetype !== old) break;
            sf.bars[b].keysignature = sig;
            sf.bars[b].keysignaturetype = m[2] ? 1 : 0;
          }
        }
      }
    });
  }
  function askTempo() {
    const mb = masterJ();
    const cur = (mb.tempoautomations && mb.tempoautomations[0] && mb.tempoautomations[0].value) || '';
    const v = prompt('Tempo from this bar on, in beats per minute (empty: none here):', cur);
    if (v === null) return;
    const n = parseInt(v, 10);
    edit(() => {
      if (!v.trim()) { mb.tempoautomations = []; return; }
      if (!(n >= 20 && n <= 400)) return false;
      mb.tempoautomations = [{ islinear: false, type: 0, value: n, ratioposition: 0, text: '', isvisible: true }];
    });
  }
  function askSection() {
    const mb = masterJ();
    const v = prompt('Section name on this bar (empty: none):', (mb.section && mb.section.text) || '');
    if (v === null) return;
    edit(() => { if (v.trim()) mb.section = { marker: '', text: v.trim() }; else delete mb.section; });
  }
  function askRepeatEnd() {
    const mb = masterJ();
    const v = prompt('Repeat this bar back to the opening repeat — how many times in all? (empty: no repeat)', mb.repeatcount || 2);
    if (v === null) return;
    edit(() => { mb.repeatcount = v.trim() ? Math.max(2, parseInt(v, 10) || 2) : 0; });
  }
  function askEnding() {
    const mb = masterJ();
    const cur = [];
    for (let i = 0; i < 8; i++) if (mb.alternateendings & (1 << i)) cur.push(i + 1);
    const v = prompt('Alternate ending: which passes play this bar (like 1 or 1,2; empty: every pass)?', cur.join(','));
    if (v === null) return;
    edit(() => {
      let mask = 0;
      for (const p of v.split(',').map((x) => parseInt(x, 10)).filter((x) => x >= 1 && x <= 8)) mask |= 1 << (p - 1);
      mb.alternateendings = mask;
    });
  }

  // ---- Tracks ----------------------------------------------------------------------

  function addTrack(part) {
    edit(() => {
      const mbs = st.doc.masterbars;
      const s = GPFile.blank({ title: 'x', tempo: 120, num: 4, den: 4, bars: mbs.length, parts: [part] });
      const t = JSON.parse(JC.scoreToJson(s)).tracks[0];
      t.staves[0].bars.forEach((bar, i) => { bar.voices = [{ beats: restBeats(mbs[i].timesignaturenumerator, mbs[i].timesignaturedenominator) }]; });
      // A MIDI channel no other track uses (9 is the drums').
      const used = new Set();
      for (const x of tracksJ()) { used.add(x.playbackinfo.primarychannel); used.add(x.playbackinfo.secondarychannel); }
      const free = () => { for (let c = 0; c < 16; c++) if (c !== 9 && !used.has(c)) { used.add(c); return c; } return 0; };
      if (part === 'drums') { t.playbackinfo.primarychannel = 9; t.playbackinfo.secondarychannel = 9; } else { t.playbackinfo.primarychannel = free(); t.playbackinfo.secondarychannel = free(); }
      const same = tracksJ().filter((x) => (x.name || '').startsWith(t.name)).length;
      if (same) t.name += ' ' + (same + 1);
      tracksJ().push(t);
      st.track = tracksJ().length - 1;
      st.bar = 0; st.beat = 0; st.voice = 0;
    });
    fillTracks();
  }
  function renameTrack() {
    const t = tracksJ()[st.track];
    const v = prompt('Track name (what the song’s gp[…] lines call it):', t.name);
    if (!v || !v.trim() || v.trim() === t.name) return;
    edit(() => { t.name = v.trim(); t.shortname = v.trim().slice(0, 10); });
    fillTracks();
  }
  function deleteTrack() {
    if (tracksJ().length < 2) return;
    if (!confirm(`Delete the track “${tracksJ()[st.track].name}”?`)) return;
    edit(() => { tracksJ().splice(st.track, 1); st.track = Math.max(0, st.track - 1); });
    fillTracks();
  }

  // ---- Drawing ---------------------------------------------------------------------

  function settings() {
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.core.includeNoteBounds = true;
    s.display.layoutMode = alphaTab.LayoutMode.Page;
    s.display.staveProfile = alphaTab.StaveProfile.Default;
    s.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
    for (const n of ['ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright', 'TrackNames']) {
      if (alphaTab.NotationElement[n] !== undefined) s.notation.elements.set(alphaTab.NotationElement[n], false);
    }
    s.player.enablePlayer = true;
    s.player.enableCursor = true;
    s.player.enableUserInteraction = false;
    s.player.soundFont = '/static/js/vendor/alphatab/soundfont/sonivox.sf2';
    s.player.scrollMode = alphaTab.ScrollMode.Off;
    if (alphaTab.PlayerMode) s.player.playerMode = alphaTab.PlayerMode.EnabledSynthesizer;
    // The vendored bundle's AudioWorklet can't be fetched (see mixer.js).
    s.player.outputMode = alphaTab.PlayerOutputMode.WebAudioScriptProcessor;
    const fg = readColor('--fg', new alphaTab.model.Color(0x24, 0x1e, 0x18));
    const r = s.display.resources;
    r.staffLineColor = readColor('--fg-muted', fg);
    r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = fg;
    return s;
  }
  function readColor(name, fallback) {
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(getComputedStyle(document.body).getPropertyValue(name).trim());
    return m ? new alphaTab.model.Color(parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)) : fallback;
  }

  function rebuild() {
    st.score = JC.jsonToScore(JSON.stringify(st.doc), new alphaTab.Settings());
    // Chords marked "show diagram" with no fingering draw as empty boxes.
    for (const t of st.score.tracks) for (const sf of t.staves) if (sf.chords) for (const c of sf.chords.values()) if (!(c.strings || []).some((f) => f >= 0)) c.showDiagram = false;
    clampCursor();
    applyNotation(st.score);
    draw();
    renderPanels();
  }
  // One drawing at a time: alphaTab draws in parts, and a second
  // renderScore started before the first is done lays its parts over the
  // first one's. The latest score is drawn once the current drawing ends.
  function draw() {
    if (st.drawing) { st.drawAgain = true; return; }
    st.drawing = true;
    st.drawAgain = false;
    // alphaTab adds a new drawing's parts next to the previous ones
    // rather than replacing them: clear the surface first.
    const surface = $('gpe-canvas').querySelector('.at-surface');
    if (surface) surface.replaceChildren();
    st.api.renderScore(st.score, [st.track]);
  }
  function drawn() {
    st.drawing = false;
    if (st.drawAgain) { draw(); return; }
    drawCursor();
  }
  // How the current track is drawn: the person's choice for this editor.
  function applyNotation(score) {
    const t = score.tracks[st.track];
    if (!t || !st.notation) return;
    for (const s of t.staves) {
      if (s.isPercussion) continue;
      s.showTablature = st.notation !== 'staff';
      s.showStandardNotation = st.notation !== 'tab';
    }
  }

  function clampCursor() {
    const bars = staffJ().bars.length;
    st.bar = Math.max(0, Math.min(st.bar, bars - 1));
    const n = Math.max(1, beatsJ().length);
    st.beat = Math.max(0, Math.min(st.beat, n - 1));
    if (!isDrums()) st.string = Math.max(1, Math.min(st.string, numStrings() || 1));
  }

  function barBounds(b, which) {
    const lookup = st.api && st.api.renderer.boundsLookup;
    if (!lookup) return null;
    for (const sys of lookup.staffSystems) {
      for (const mb of sys.bars) {
        if (mb.index !== b || !mb.bars.length) continue;
        return which === 'tab' ? mb.bars[mb.bars.length - 1] : mb.bars[0];
      }
    }
    return null;
  }
  // Tab: the highest string is the top line; alphaTab numbers it n.
  function stringY(bb, s) {
    const v = bb.visualBounds;
    const n = numStrings();
    return v.y + (n > 1 ? (n - s) * v.h / (n - 1) : v.h / 2);
  }

  function drawCursor() {
    const cursor = $('gpe-cursor'), hl = $('gpe-bar-hl');
    const bb = barBounds(st.bar, isDrums() ? 'staff' : 'tab');
    if (!bb) { cursor.hidden = true; hl.hidden = true; return; }
    // Bounds are the canvas's; the boxes sit in its padded wrapper.
    const canvas = $('gpe-canvas');
    const ox = canvas.offsetLeft, oy = canvas.offsetTop;
    const v = bb.visualBounds;
    Object.assign(hl.style, { left: (ox + v.x) + 'px', top: (oy + v.y - 8) + 'px', width: v.w + 'px', height: (v.h + 16) + 'px' });
    hl.hidden = false;
    hl.classList.toggle('over', barFill() > 1 + 1e-9);
    hl.classList.toggle('under', barFill() < 1 - 1e-9);
    let beatB = null;
    for (const b of bb.beats) if (b.beat.index === st.beat && b.beat.voice.index === st.voice) beatB = b;
    if (!beatB) { cursor.hidden = true; return; }
    const bv = beatB.visualBounds;
    const w = Math.max(18, bv.w + 8);
    let y, h;
    if (isDrums()) {
      y = v.y + v.h / 2; h = v.h + 8;
    } else {
      y = stringY(bb, st.string);
      h = Math.min(18, v.h / Math.max(1, numStrings() - 1) + 3);
    }
    Object.assign(cursor.style, { left: (ox + bv.x + bv.w / 2 - w / 2) + 'px', top: (oy + y - h / 2) + 'px', width: w + 'px', height: h + 'px' });
    cursor.hidden = false;
    cursor.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  // How full the bar is in this voice: 1 when it holds its time signature.
  function barFill() {
    const bm = barM();
    const v = bm && bm.voices[st.voice];
    if (!v || v.isEmpty) return 1;
    return v.calculateDuration() / bm.masterBar.calculateDuration();
  }

  function pickAt(ev) {
    const lookup = st.api && st.api.renderer.boundsLookup;
    if (!lookup) return;
    const rect = $('gpe-canvas').getBoundingClientRect();
    const x = ev.clientX - rect.left, y = ev.clientY - rect.top;
    const beat = lookup.getBeatAtPos(x, y);
    if (!beat) return;
    st.bar = beat.voice.bar.index;
    st.voice = beat.voice.index;
    st.beat = beat.index;
    st.digit = null;
    const bb = barBounds(st.bar, 'tab');
    if (bb && !isDrums()) {
      let best = 1, bestD = Infinity;
      for (let s = 1; s <= numStrings(); s++) {
        const d = Math.abs(stringY(bb, s) - y);
        if (d < bestD) { bestD = d; best = s; }
      }
      st.string = best;
    }
    renderPanels();
    drawCursor();
    $('gpe-canvas-wrap').focus();
  }

  // ---- Panels --------------------------------------------------------------------

  function setStatus(msg, bad) {
    const el = $('gpe-status');
    el.textContent = msg || '';
    el.classList.toggle('bad', !!bad);
  }

  function fillTracks() {
    const sel = $('gpe-track');
    sel.innerHTML = '';
    tracksJ().forEach((t, i) => sel.add(new Option(t.name || `Track ${i + 1}`, i)));
    sel.value = st.track;
  }

  function renderPanels() {
    const beat = beatJ();
    const mb = masterJ();
    if (palette) palette.refresh();
    $('gpe-drums').hidden = !isDrums();
    document.querySelectorAll('#gpe-drums button').forEach((b, i) => {
      b.setAttribute('aria-pressed', beat && drumNote(beat, DRUM_PIECES[i]) ? 'true' : 'false');
      b.classList.toggle('current', i === st.piece);
    });
    $('gpe-delete-track').disabled = tracksJ().length < 2;
    // Where the cursor is.
    const fill = barFill();
    const where = [`Bar ${st.bar + 1} of ${st.doc.masterbars.length}`, `${mb.timesignaturenumerator}/${mb.timesignaturedenominator}`, `voice ${st.voice + 1}`];
    if (mb.section && mb.section.text) where.push(`“${mb.section.text}”`);
    if (fill > 1 + 1e-9) where.push('too full');
    else if (fill < 1 - 1e-9) where.push('not full yet');
    if (beat) {
      where.push(`beat ${st.beat + 1}: ${DURATION_LABEL[beat.duration] || beat.duration}${beat.dots ? ' dotted' : ''}${beat.tupletnumerator > 0 ? ` (${beat.tupletnumerator}:${beat.tupletdenominator})` : ''}`);
      if (isDrums()) where.push(DRUM_PIECES[st.piece].name);
      else {
        where.push(`string ${numStrings() - st.string + 1}`);
        const n = curNote();
        if (n) where.push(`fret ${n.fret}`);
      }
    }
    $('gpe-where').textContent = where.join(' · ');
  }

  let palette = null;
  function buildToolbar() {
    palette = GPPalette({
      st, edit, beatJ, beatM, curNote, ensureVoice, staffJ, masterJ, barJ, tracksJ, isDrums, tuning, numStrings,
      setDuration, setStatus, askKey, askTimeSignature, askTempo, askSection, askRepeatEnd, askEnding,
      setVoice: (v) => { st.voice = v; st.beat = 0; renderPanels(); drawCursor(); },
      focus: () => $('gpe-canvas-wrap').focus(),
    });
    palette.build($('gpe-palette'));
    const keys = $('gpe-palette-keys');
    for (const [k, name] of palette.shortcuts()) {
      const dt = document.createElement('dt');
      dt.textContent = k;
      const dd = document.createElement('dd');
      dd.textContent = name;
      keys.append(dt, dd);
    }
    const drums = $('gpe-drums');
    for (const p of DRUM_PIECES) {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = p.name;
      b.title = p.key ? `${p.name} (${p.key.toUpperCase()})` : p.name;
      b.addEventListener('click', () => toggleDrum(p));
      drums.appendChild(b);
    }
    const on = (id, fn) => $(id).addEventListener('click', fn);
    on('gpe-insert-beat', insertBeat);
    on('gpe-delete', deleteAtCursor);
    on('gpe-bar-before', addBarBefore);
    on('gpe-bar-after', addBarAfter);
    on('gpe-bar-delete', deleteBar);
    on('gpe-bar-copy', copyBar);
    on('gpe-bar-paste', pasteBar);
    on('gpe-undo', undo);
    on('gpe-redo', redo);
    on('gpe-save', save);
    on('gpe-play', playPause);
    on('gpe-stop', () => st.api.stop());
    on('gpe-rename-track', renameTrack);
    on('gpe-delete-track', deleteTrack);
    $('gpe-add-track').addEventListener('change', (e) => { const v = e.target.value; e.target.value = ''; if (v) addTrack(v); });
    $('gpe-track').addEventListener('change', (e) => { st.track = +e.target.value; st.voice = 0; st.beat = 0; rebuild(); });
    $('gpe-notation').addEventListener('change', (e) => { st.notation = e.target.value; rebuild(); });
  }

  // ---- Playback ------------------------------------------------------------------

  function playPause() {
    const api = st.api;
    if (!api.isReadyForPlayback) { setStatus('The sounds are still loading…'); return; }
    if (st.playing) { api.pause(); return; }
    const b = beatM();
    if (b) api.tickPosition = b.absolutePlaybackStart;
    api.play();
  }

  // ---- Keys ------------------------------------------------------------------------

  function moveBeat(dir) {
    const n = beatsJ().length;
    if (dir > 0 && st.beat >= n - 1) {
      // Past the last beat: a bar that isn't full gets a beat, a full
      // one hands over to the next bar (a new one at the end).
      if (barFill() < 1 - 1e-9) { edit(() => { ensureVoice().beats.push(restBeat(st.dur)); st.beat = beatsJ().length - 1; }); return; }
      if (st.bar >= staffJ().bars.length - 1) { addBarAfter(); return; }
      st.bar++; st.beat = 0;
    } else if (dir < 0 && st.beat === 0) {
      if (st.bar === 0) return;
      st.bar--; st.beat = Math.max(0, beatsJ().length - 1);
    } else st.beat += dir;
    st.digit = null;
    renderPanels();
    drawCursor();
  }
  function moveBar(dir) {
    st.bar = Math.max(0, Math.min(staffJ().bars.length - 1, st.bar + dir));
    st.beat = 0;
    renderPanels();
    drawCursor();
  }

  // Keys: moving and typing frets first, then the drum kit on a drum
  // track, then the palette's Guitar Pro shortcuts.
  function onKey(e) {
    const mod = e.metaKey || e.ctrlKey;
    const k = e.key;
    if (mod && !e.altKey && (k === 'z' || k === 'Z')) { e.preventDefault(); if (e.shiftKey) redo(); else undo(); return; }
    if (mod && !e.altKey && k === 'y') { e.preventDefault(); redo(); return; }
    if (mod && !e.altKey && k === 's') { e.preventDefault(); save(); return; }
    if (mod && !e.altKey && !e.shiftKey && k === 'c') { e.preventDefault(); copyBar(); return; }
    if (mod && !e.altKey && !e.shiftKey && k === 'v') { e.preventDefault(); pasteBar(); return; }
    if (mod && k === 'ArrowRight') { e.preventDefault(); moveBar(1); return; }
    if (mod && k === 'ArrowLeft') { e.preventDefault(); moveBar(-1); return; }
    if (!mod && !e.altKey) {
      if (k === 'ArrowRight') { e.preventDefault(); moveBeat(1); return; }
      if (k === 'ArrowLeft') { e.preventDefault(); moveBeat(-1); return; }
      if ((k === 'ArrowUp' || k === 'ArrowDown') && !e.shiftKey) {
        e.preventDefault();
        const up = k === 'ArrowUp';
        if (isDrums()) st.piece = Math.max(0, Math.min(DRUM_PIECES.length - 1, st.piece + (up ? -1 : 1)));
        else st.string = Math.max(1, Math.min(numStrings(), st.string + (up ? 1 : -1)));
        st.digit = null;
        renderPanels();
        drawCursor();
        return;
      }
      if (k === 'Backspace' || k === 'Delete') { e.preventDefault(); deleteAtCursor(); return; }
      if (k === 'Insert') { e.preventDefault(); insertBeat(); return; }
      if (k === '+' || k === '=') { e.preventDefault(); stepDuration(1); return; }
      if (k === '-' || k === '_') { e.preventDefault(); stepDuration(-1); return; }
      if (k === ' ') { e.preventDefault(); if (isDrums()) toggleDrum(DRUM_PIECES[st.piece]); else playPause(); return; }
      if (isDrums()) {
        const p = DRUM_PIECES.find((x) => x.key && x.key === k.toLowerCase() && !e.shiftKey);
        if (p) { e.preventDefault(); toggleDrum(p); return; }
      } else if (/^[0-9]$/.test(k)) {
        e.preventDefault();
        const now = Date.now();
        let fret = +k;
        if (st.digit && now - st.digit.at < DIGIT_WINDOW_MS && st.digit.bar === st.bar && st.digit.beat === st.beat) {
          const two = st.digit.value * 10 + fret;
          if (two <= MAX_FRET) fret = two;
        }
        st.digit = { value: fret, at: now, bar: st.bar, beat: st.beat };
        setFret(fret);
        return;
      }
    }
    if (palette) palette.onKey(e);
  }

  // ---- Load, save --------------------------------------------------------------------

  async function save() {
    if (!st.dirty) return;
    const btn = $('gpe-save');
    btn.disabled = true;
    setStatus('Saving a new version of the file…');
    try {
      const out = await GPFile.save(variantId, JC.jsonToScore(JSON.stringify(st.doc), new alphaTab.Settings()), fileName);
      fileName = out.name;
      markDirty(false);
      setStatus('Saved. The earlier version is on the song’s History page.');
    } catch (e) {
      btn.disabled = false;
      setStatus(e.message, true);
    }
  }

  async function start() {
    let score;
    try {
      if (root.dataset.blank) {
        const d = root.dataset;
        score = GPFile.blank({ title: d.title, artist: d.artist, tempo: +d.tempo, num: +d.num, den: +d.den, bars: +d.bars, parts: (d.parts || 'guitar').split(',') });
        st.dirty = true;
      } else {
        score = GPFile.parse(await GPFile.loadBytes(variantId));
      }
    } catch (e) {
      setStatus(e.message, true);
      return;
    }
    st.doc = JSON.parse(JC.scoreToJson(score));
    const want = root.dataset.track ? GPFacts.findTrack(score.tracks, root.dataset.track) : -1;
    if (want >= 0) st.track = want;
    if (root.dataset.bar) st.bar = Math.max(0, +root.dataset.bar - 1);
    st.string = Math.min(3, numStrings()) || 1;
    st.api = new alphaTab.AlphaTabApi($('gpe-canvas'), settings());
    st.api.postRenderFinished.on(drawn);
    st.api.playerReady.on(() => { $('gpe-play').disabled = false; });
    st.api.playerStateChanged.on((e) => {
      st.playing = e.state === 1;
      $('gpe-play').textContent = st.playing ? '❚❚' : '▶';
    });
    buildToolbar();
    fillTracks();
    markDirty(st.dirty);
    rebuild();
    const wrap = $('gpe-canvas-wrap');
    wrap.addEventListener('keydown', onKey);
    $('gpe-canvas').addEventListener('click', pickAt);
    wrap.focus();
    window.addEventListener('beforeunload', (e) => { if (st.dirty) { e.preventDefault(); e.returnValue = ''; } });
    let t = null;
    window.addEventListener('resize', () => { clearTimeout(t); t = setTimeout(draw, 200); });
  }

  start();
})();

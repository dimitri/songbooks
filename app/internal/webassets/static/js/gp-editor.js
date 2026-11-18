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
    { midi: 49, name: t('Crash'), short: 'Cr', art: 'Crash', key: 'c' },
    { midi: 46, name: t('Hi-hat open'), short: 'OH', art: 'HiHatOpen', key: 'o' },
    { midi: 42, name: t('Hi-hat'), short: 'HH', art: 'HiHatClosed', key: 'h' },
    { midi: 51, name: t('Ride'), short: 'Rd', art: 'Ride', key: 'y' },
    { midi: 53, name: t('Ride bell'), short: 'Bl', art: 'RideBell', key: '' },
    { midi: 56, name: t('Cowbell'), short: 'Cb', art: 'Cowbell', key: '' },
    { midi: 50, name: t('High tom'), short: 'T1', art: 'TomHigh', key: '1' },
    { midi: 38, name: t('Snare'), short: 'SD', art: 'Snare', key: 's' },
    { midi: 37, name: t('Side stick'), short: 'SS', art: 'SideStick', key: 'x' },
    { midi: 47, name: t('Mid tom'), short: 'T2', art: 'TomMid', key: '2' },
    { midi: 45, name: t('Low tom'), short: 'T3', art: 'TomLow', key: '3' },
    { midi: 43, name: t('Floor tom'), short: 'FT', art: 'TomFloorHigh', key: '4' },
    { midi: 41, name: t('Low floor tom'), short: 'FT2', art: 'TomFloorLow', key: '5' },
    { midi: 36, name: t('Kick'), short: 'BD', art: 'Kick', key: 'k' },
    { midi: 44, name: t('Hi-hat pedal'), short: 'HP', art: 'HiHatPedal', key: 'f' },
  ];

  const st = {
    doc: null, score: null, api: null,
    track: 0, voice: 0, bar: 0, beat: 0, string: 1, piece: 13,
    dur: 8, dirty: false, undo: [], redo: [], digit: null, clip: null,
    notation: '', playing: false,
    sel: null, // bars selected for the song (0-based, inclusive): { from, to }
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
    $('gpe-dirty').hidden = !d;
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
  async function deleteBar(anchor) {
    if (st.doc.masterbars.length < 2) { setStatus(t('A file keeps at least one bar.'), 'hint'); return; }
    if (!(await GPUI.confirm(anchor || $('gpe-cursor'), { title: t('Delete bar %d?', st.bar + 1), body: t('It goes from every track. Undo brings it back.') }))) return;
    edit(() => {
      st.doc.masterbars.splice(st.bar, 1);
      for (const t of tracksJ()) for (const s of t.staves) s.bars.splice(st.bar, 1);
      st.bar = Math.min(st.bar, st.doc.masterbars.length - 1);
      st.beat = 0;
    });
  }
  const copyBar = () => { st.clip = JSON.parse(JSON.stringify(barJ())); setStatus(t('Bar %d copied: Ctrl/⌘ V pastes it over another bar.', st.bar + 1), 'ok'); };
  const pasteBar = () => {
    if (!st.clip) return;
    edit(() => {
      const strip = (x) => { if (Array.isArray(x)) x.forEach(strip); else if (x && typeof x === 'object') { delete x.id; for (const k of ['tiedestinationnoteid', 'tieoriginnoteid', 'hammerpulldestinationnoteid', 'hammerpulloriginnoteid', 'slidetargetnoteid', 'slideoriginnoteid']) delete x[k]; for (const k in x) strip(x[k]); } return x; };
      staffJ().bars[st.bar] = strip(JSON.parse(JSON.stringify(st.clip)));
    });
  };
  const ask = (anchor, spec) => GPUI.ask(anchor || $('gpe-cursor'), spec);
  async function askTimeSignature(anchor) {
    const mb = masterJ();
    const v = await ask(anchor, { title: t('Time signature'), hint: t('From this bar until the next change.'), fields: [
      { name: 'num', label: t('Beats'), type: 'number', min: 1, max: 32, value: mb.timesignaturenumerator },
      { name: 'den', label: t('Beat value'), type: 'select', options: [['1', '1'], ['2', '2'], ['4', '4'], ['8', '8'], ['16', '16'], ['32', '32']], value: String(mb.timesignaturedenominator) }],
      check: (x) => (x.num >= 1 && x.num <= 32 ? '' : t('From 1 to 32 beats.')) });
    if (!v) return;
    edit(() => {
      const on = mb.timesignaturenumerator + '/' + mb.timesignaturedenominator;
      for (let b = st.bar; b < st.doc.masterbars.length; b++) {
        const x = st.doc.masterbars[b];
        if (b > st.bar && x.timesignaturenumerator + '/' + x.timesignaturedenominator !== on) break;
        x.timesignaturenumerator = v.num;
        x.timesignaturedenominator = +v.den;
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
  async function askKey(anchor) {
    const bar = barJ();
    const minor = bar.keysignaturetype === 1;
    const v = await ask(anchor, { title: t('Key signature'), hint: t('From this bar until the next change, in every track.'), fields: [
      { name: 'root', label: t('Key'), type: 'select', options: Object.keys(minor ? MINOR : KEYS).map((k) => [k, k]).concat(Object.keys(minor ? KEYS : MINOR).filter((k) => !(k in (minor ? MINOR : KEYS))).map((k) => [k, k])), value: keyName(bar.keysignature || 0, minor).replace(/m$/, '') },
      { name: 'mode', label: t('Mode'), type: 'select', options: [['major', t('Major')], ['minor', t('Minor')]], value: minor ? 'minor' : 'major' }],
      check: (x) => ((x.mode === 'minor' ? MINOR : KEYS)[x.root] === undefined ? t(x.mode === 'minor' ? '%s minor isn’t a written key; try its other spelling.' : '%s major isn’t a written key; try its other spelling.', x.root) : '') });
    if (!v) return;
    const sig = (v.mode === 'minor' ? MINOR : KEYS)[v.root];
    edit(() => {
      for (const t of tracksJ()) {
        for (const sf of t.staves) {
          if (sf.ispercussion) continue;
          const old = sf.bars[st.bar] && (sf.bars[st.bar].keysignature + '/' + sf.bars[st.bar].keysignaturetype);
          for (let b = st.bar; b < sf.bars.length; b++) {
            if (b > st.bar && sf.bars[b].keysignature + '/' + sf.bars[b].keysignaturetype !== old) break;
            sf.bars[b].keysignature = sig;
            sf.bars[b].keysignaturetype = v.mode === 'minor' ? 1 : 0;
          }
        }
      }
    });
  }
  async function askTempo(anchor) {
    const mb = masterJ();
    const cur = mb.tempoautomations && mb.tempoautomations[0] && mb.tempoautomations[0].value;
    const v = await ask(anchor, { title: t('Tempo'), hint: t('From this bar on.'), clearable: !!cur, fields: [{ name: 'bpm', label: t('Beats per minute'), type: 'number', min: 20, max: 400, value: cur || tempoAt(st.bar) }],
      check: (x) => (x.bpm >= 20 && x.bpm <= 400 ? '' : t('From 20 to 400.')) });
    if (!v) return;
    edit(() => { mb.tempoautomations = v.cleared ? [] : [{ islinear: false, type: 0, value: v.bpm, ratioposition: 0, text: '', isvisible: true }]; });
  }
  async function askSection(anchor) {
    const mb = masterJ();
    const cur = (mb.section && mb.section.text) || '';
    const v = await ask(anchor, { title: t('Section'), clearable: !!cur, fields: [{ name: 'text', label: t('Name'), value: cur || songSectionAt(st.bar) || '', placeholder: t('Verse') }] });
    if (!v) return;
    edit(() => { if (!v.cleared && v.text) mb.section = { marker: '', text: v.text }; else delete mb.section; });
  }
  async function askRepeatEnd(anchor) {
    const mb = masterJ();
    const v = await ask(anchor, { title: t('Repeat close'), hint: t('Back to the repeat start; the number of times played in all.'), clearable: !!mb.repeatcount,
      fields: [{ name: 'n', label: t('Times'), type: 'number', min: 2, max: 16, value: mb.repeatcount || 2 }], check: (x) => (x.n >= 2 ? '' : t('At least 2.')) });
    if (!v) return;
    edit(() => { mb.repeatcount = v.cleared ? 0 : v.n; });
  }
  async function askEnding(anchor) {
    const mb = masterJ();
    const cur = [];
    for (let i = 0; i < 8; i++) if (mb.alternateendings & (1 << i)) cur.push(i + 1);
    const v = await ask(anchor, { title: t('Alternate ending'), clearable: !!cur.length, fields: [{ name: 'passes', label: t('Played on passes'), value: cur.join(', ') || '1', placeholder: '1, 2' }],
      check: (x) => (/^\s*\d(\s*,\s*\d)*\s*$/.test(x.passes) ? '' : t('Pass numbers, like 1 or 1, 2.')) });
    if (!v) return;
    edit(() => {
      let mask = 0;
      if (!v.cleared) for (const p of v.passes.split(',').map((x) => parseInt(x, 10)).filter((x) => x >= 1 && x <= 8)) mask |= 1 << (p - 1);
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
  }
  async function renameTrack(anchor) {
    const tr = tracksJ()[st.track];
    const v = await ask(anchor, { title: t('Rename the track'), hint: t('The song’s gp[…] lines call it by this name.'), fields: [{ name: 'name', label: t('Name'), value: tr.name }],
      check: (x) => (x.name ? '' : t('A track needs a name.')) });
    if (!v || v.name === tr.name) return;
    edit(() => { tr.name = v.name; tr.shortname = v.name.slice(0, 10); });
  }
  async function deleteTrack(anchor) {
    if (tracksJ().length < 2) { setStatus(t('A file keeps at least one track.'), 'hint'); return; }
    if (!(await GPUI.confirm(anchor, { title: t('Delete “%s”?', tracksJ()[st.track].name), body: t('Its notes go with it. Undo brings it back.') }))) return;
    edit(() => { tracksJ().splice(st.track, 1); st.track = Math.max(0, st.track - 1); st.voice = 0; });
  }
  async function trackMenu(anchor) {
    const v = await GPUI.choose(anchor, { title: tracksJ()[st.track].name, columns: 1, options: [
      { value: 'rename', label: t('Rename') }, { value: 'delete', label: t('Delete') }] });
    if (v === 'rename') renameTrack(anchor);
    if (v === 'delete') deleteTrack(anchor);
  }
  async function addTrackMenu(anchor) {
    const v = await GPUI.choose(anchor, { title: t('Add a track'), columns: 3, options: [
      { value: 'guitar', label: t('Guitar') }, { value: 'bass', label: t('Bass') }, { value: 'drums', label: t('Drums') }] });
    if (v) addTrack(v);
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
    hl.classList.remove('over', 'under');
    const bb = barBounds(st.bar, isDrums() ? 'staff' : 'tab');
    if (!bb) { cursor.hidden = true; hl.hidden = true; return; }
    // Bounds are the canvas's; the boxes sit in its padded wrapper.
    const canvas = $('gpe-canvas');
    const ox = canvas.offsetLeft, oy = canvas.offsetTop;
    const v = bb.visualBounds;
    Object.assign(hl.style, { left: (ox + v.x) + 'px', top: (oy + v.y - 8) + 'px', width: v.w + 'px', height: (v.h + 16) + 'px' });
    hl.hidden = false;
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
    drawSelection();
  }

  // Bars selected to show in the song: a band over each, across systems.
  function drawSelection() {
    const layer = $('gpe-sel-layer');
    layer.replaceChildren();
    const box = $('gpe-selbar');
    if (!st.sel) { box.hidden = true; return; }
    const canvas = $('gpe-canvas');
    const ox = canvas.offsetLeft, oy = canvas.offsetTop;
    for (let b = st.sel.from; b <= st.sel.to; b++) {
      const bb = barBounds(b, isDrums() ? 'staff' : 'tab');
      if (!bb) continue;
      const v = bb.visualBounds;
      const d = document.createElement('div');
      d.className = 'gpe-sel';
      Object.assign(d.style, { left: (ox + v.x) + 'px', top: (oy + v.y - 10) + 'px', width: v.w + 'px', height: (v.h + 20) + 'px' });
      layer.appendChild(d);
    }
    box.hidden = false;
    const a = st.sel.from + 1, z = st.sel.to + 1;
    $('gpe-seltxt').textContent = a === z ? t('Bar %d selected', a) : t('Bars %d–%d selected', a, z);
  }
  function extendSelection(toBar) {
    const anchor = st.sel ? (st.sel.anchor ?? st.sel.from) : st.bar;
    st.sel = { anchor, from: Math.min(anchor, toBar), to: Math.max(anchor, toBar) };
  }
  // The selected bars, for a section of the song: the bar picker opens on
  // that section with them chosen, to check their length and apply.
  async function useSelectionFor(anchor) {
    if (!st.sel || !window.GPBarPicker) return;
    let sections = [];
    try { sections = JSON.parse(root.dataset.pickerSections || '[]'); } catch (e) { /* none */ }
    if (!sections.length) { setStatus(t('The song has no sections yet: add them in its Chart tab.'), 'hint'); return; }
    const pickIdx = await GPUI.choose(anchor, { title: t('Use these bars for'), columns: 1, options: sections.map((x, i) => ({
      value: i, label: x.label + (x.gp.length ? ' · ' + x.gp.map((g) => (g.track || t('all')) + ' ' + g.spec).join(', ') : ''),
    })) });
    if (pickIdx === null) return;
    if (st.dirty) setStatus(t('Save first if these bars changed: the song shows the saved file.'), 'hint');
    const tr = tracksJ()[st.track];
    const same = tracksJ().filter((x) => (x.name || '').trim().toLowerCase() === (tr.name || '').trim().toLowerCase()).length;
    const label = tr.name && same === 1 ? tr.name : `#${st.track + 1}`;
    const req = (method, index, body, track) => fetch(`/api/songs/${variantId}/sections/${index}/gp${method === 'DELETE' ? '?track=' + encodeURIComponent(track || '') : ''}`, {
      method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined,
    }).then(async (r) => { if (!r.ok) throw new Error((await r.text()) || t('Saving failed')); });
    GPBarPicker.open({
      variantId, sections, current: pickIdx, track: label, ranges: [[st.sel.from + 1, st.sel.to + 1]],
      onApply: async ({ index, track, spec }) => {
        await req('PUT', index, { track, spec });
        const x = sections.find((y) => y.index === index);
        const g = x.gp.find((y) => (y.track || '').toLowerCase() === (track || '').toLowerCase());
        if (g) g.spec = spec; else x.gp.push({ track, spec });
        root.dataset.pickerSections = JSON.stringify(sections);
      },
      onRemove: async ({ index, track }) => {
        await req('DELETE', index, null, track);
        const x = sections.find((y) => y.index === index);
        x.gp = x.gp.filter((y) => (y.track || '').toLowerCase() !== (track || '').toLowerCase());
        root.dataset.pickerSections = JSON.stringify(sections);
      },
      onClose: (changed) => { if (changed) setStatus(t('The song shows the new bars.'), 'ok'); },
    });
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
    if (ev.shiftKey) extendSelection(beat.voice.bar.index);
    else st.sel = null;
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

  // The status line's message: kind 'hint' and 'ok' fade after a while,
  // true (a problem) stays until the next message.
  let statusTimer = null;
  function setStatus(msg, kind) {
    const el = $('gpe-status');
    clearTimeout(statusTimer);
    el.replaceChildren();
    if (msg instanceof Node) el.appendChild(msg); else el.textContent = msg || '';
    el.className = 'gpe-msg' + (kind === true ? ' bad' : kind ? ' ' + kind : '');
    if (msg && (kind === 'hint' || kind === 'ok')) statusTimer = setTimeout(() => { el.textContent = ''; }, 6000);
  }

  // The tempo in effect at bar b.
  function tempoAt(b) {
    let t = st.score ? Math.round(st.score.tempo) : 120;
    for (let i = 0; i <= b && i < st.doc.masterbars.length; i++) {
      const a = st.doc.masterbars[i].tempoautomations;
      if (a && a.length) t = Math.round(a[0].value);
    }
    return t;
  }
  // The song's sections that show bar b (1-based) of this track, from its
  // gp[…] lines (gpview.go puts them on the page).
  const songSections = (() => { try { return JSON.parse(root.dataset.sections || '[]'); } catch (e) { return []; } })();
  function songSectionAt(b) {
    const name = (tracksJ()[st.track].name || '').toLowerCase();
    const hit = songSections.find((x) => (!x.track || x.track.toLowerCase() === name || x.track === '#' + (st.track + 1)) && x.bars.some(([lo, hi]) => b + 1 >= lo && b + 1 <= hi));
    return hit ? hit.label : '';
  }
  function songSectionIndexAt(b) {
    const name = (tracksJ()[st.track].name || '').toLowerCase();
    const hit = songSections.find((x) => (!x.track || x.track.toLowerCase() === name) && x.bars.some(([lo, hi]) => b + 1 >= lo && b + 1 <= hi));
    return hit ? hit.index : -1;
  }

  // Beats as a musician says them: 3½, not 3.5.
  function beats(x) {
    const q = Math.round(x * 4) / 4;
    const whole = Math.floor(q), frac = q - whole;
    const f = { 0: '', 0.25: '¼', 0.5: '½', 0.75: '¾' }[frac];
    return f === undefined ? x.toFixed(2) : (whole || !f ? String(whole) : '') + f;
  }

  // Track colours: the file's own, unless several tracks share one; then a
  // colour each, in the app's palette.
  const TRACK_COLOURS = ['#c2553a', '#1d8a6a', '#5b4fb5', '#b07418', '#2f6fb5', '#b0446e'];
  function trackColour(i) {
    const t = tracksJ()[i];
    const own = typeof t.color === 'number' ? '#' + ((t.color >>> 0) & 0xffffff).toString(16).padStart(6, '0') : '';
    const shared = own && tracksJ().some((x, j) => j !== i && x.color === t.color);
    return own && !shared ? own : TRACK_COLOURS[i % TRACK_COLOURS.length];
  }
  function trackKind(i) {
    const sf = tracksJ()[i].staves[0];
    if (sf.ispercussion) return 'drums';
    const tun = (sf.stringtuning && sf.stringtuning.tunings) || [];
    return tun.length && tun.length <= 5 && Math.min(...tun) < 40 ? 'bass' : 'guitar';
  }

  function renderTabs() {
    const box = $('gpe-tabs');
    box.replaceChildren();
    tracksJ().forEach((tr, i) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'gpe-tab' + (i === st.track ? ' on' : '');
      b.setAttribute('role', 'tab');
      b.setAttribute('aria-selected', i === st.track ? 'true' : 'false');
      b.style.setProperty('--tc', trackColour(i));
      b.innerHTML = `<span class="sw"></span>${GPUI.icon(trackKind(i))}<span class="nm"></span>`;
      b.querySelector('.nm').textContent = tr.name || t('Track %d', i + 1);
      b.addEventListener('click', () => { if (i === st.track) { trackMenu(b); return; } st.track = i; st.voice = 0; st.beat = 0; rebuild(); });
      if (i === st.track) {
        b.dataset.tip = t('Rename or delete this track');
        const more = document.createElement('span');
        more.className = 'more';
        more.innerHTML = GPUI.icon('more');
        b.appendChild(more);
      }
      box.appendChild(b);
    });
    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'gpe-tab add';
    add.innerHTML = GPUI.icon('plus');
    add.dataset.tip = t('Add a track');
    add.setAttribute('aria-label', t('Add a track'));
    add.addEventListener('click', () => addTrackMenu(add));
    box.appendChild(add);
  }

  function renderPanels() {
    const beat = beatJ();
    const mb = masterJ();
    if (palette) palette.refresh();
    renderTabs();
    root.dataset.voice = st.voice + 1;
    // Where the cursor is.
    const pos = $('gpe-pos');
    pos.replaceChildren();
    const barTxt = document.createElement('span');
    barTxt.textContent = t('Bar %d/%d', st.bar + 1, st.doc.masterbars.length);
    const more = document.createElement('span');
    more.className = 'gpe-pos-more';
    more.textContent = ` · ${mb.timesignaturenumerator}/${mb.timesignaturedenominator} · ♩ = ${tempoAt(st.bar)}`;
    pos.append(barTxt, more);
    $('gpe-voice').textContent = t('voice %d', st.voice + 1);
    const what = [];
    if (beat) {
      let d = DURATION_LABEL[beat.duration] ? t(DURATION_LABEL[beat.duration]) : String(beat.duration);
      if (beat.dots) d = t(beat.dots === 2 ? '%s double-dotted' : '%s dotted', d);
      if (beat.tupletnumerator > 0) d += ` ${beat.tupletnumerator}:${beat.tupletdenominator}`;
      what.push(beat.notes.length ? d : t('%s rest', d));
      if (isDrums()) what.push(DRUM_PIECES[st.piece].name);
      else {
        what.push(t('string %d', numStrings() - st.string + 1));
        const n = curNote();
        if (n) what.push(t('fret %d', n.fret));
      }
    }
    $('gpe-what').textContent = what.join(' · ');
    const sec = [songSectionAt(st.bar), mb.section && mb.section.text].filter(Boolean);
    $('gpe-section').textContent = sec.length ? [...new Set(sec)].join(' / ') : '';
    $('gpe-section').hidden = !sec.length;
    // How full the bar is.
    const bm = barM(), v = bm && bm.voices[st.voice];
    const cap = bm ? bm.masterBar.calculateDuration() : 3840;
    const has = v && !v.isEmpty ? v.calculateDuration() : 0;
    const unit = 3840 / (mb.timesignaturedenominator || 4);
    const meter = $('gpe-fill');
    meter.querySelector('.txt').textContent = t('%s of %s', beats(has / unit), beats(cap / unit));
    meter.querySelector('.bar > span').style.width = Math.min(100, (has / cap) * 100) + '%';
    meter.dataset.state = has > cap + 1 ? 'over' : has < cap - 1 ? 'under' : 'full';
    meter.dataset.tip = has > cap + 1 ? t('This bar holds more than its time signature') : has < cap - 1 ? t('This bar isn’t full yet') : t('This bar is full');
  }

  let palette = null;
  function buildToolbar() {
    document.querySelectorAll('.gpe-shell [data-icon]').forEach((el) => { el.insertAdjacentHTML('afterbegin', GPUI.icon(el.dataset.icon)); });
    palette = GPPalette({
      st, edit, beatJ, beatM, curNote, ensureVoice, staffJ, masterJ, barJ, tracksJ, isDrums, tuning, numStrings, DRUM_PIECES, drumNote, toggleDrum,
      setDuration, setStatus, askKey, askTimeSignature, askTempo, askSection, askRepeatEnd, askEnding,
      insertBeat, deleteAtCursor, addBarBefore, addBarAfter, deleteBar, copyBar, pasteBar,
      setVoice: (v) => { st.voice = v; st.beat = 0; renderPanels(); drawCursor(); },
      focus: () => $('gpe-canvas-wrap').focus({ preventScroll: true }),
    });
    palette.build($('gpe-palette'));
    // Phone keypad: frets, moves and delete, the keyboard's keys.
    const pad = $('gpe-keypad');
    for (const [key, label] of [...'1234567890'.split('').map((d) => [d, d]), ['ArrowLeft', '←'], ['ArrowUp', '↑'], ['ArrowDown', '↓'], ['ArrowRight', '→'], ['Backspace', '⌫']]) {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = label;
      b.addEventListener('click', () => onKey({ key, code: '', metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, preventDefault() {} }));
      pad.appendChild(b);
    }
    const on = (id, fn) => $(id).addEventListener('click', (e) => fn(e.currentTarget));
    on('gpe-undo', undo);
    on('gpe-redo', redo);
    on('gpe-save', save);
    on('gpe-play', playPause);
    on('gpe-stop', () => st.api.stop());
    on('gpe-loop', (b) => { st.api.isLooping = !st.api.isLooping; b.setAttribute('aria-pressed', st.api.isLooping ? 'true' : 'false'); });
    on('gpe-countin', (b) => { const on2 = b.getAttribute('aria-pressed') !== 'true'; st.api.countInVolume = on2 ? 1 : 0; b.setAttribute('aria-pressed', on2 ? 'true' : 'false'); });
    on('gpe-metronome', (b) => { const on2 = b.getAttribute('aria-pressed') !== 'true'; st.api.metronomeVolume = on2 ? 1 : 0; b.setAttribute('aria-pressed', on2 ? 'true' : 'false'); });
    on('gpe-help', showKeys);
    on('gpe-usefor', useSelectionFor);
    on('gpe-keypad-toggle', (b) => { const show = $('gpe-keypad').hidden; $('gpe-keypad').hidden = !show; b.setAttribute('aria-pressed', show ? 'true' : 'false'); });
    $('gpe-speed').addEventListener('change', (e) => { st.api.playbackSpeed = Number(e.target.value) || 1; });
    $('gpe-notation').addEventListener('change', (e) => { st.notation = e.target.value; rebuild(); });
    GPUI.tooltips(root.closest('.gpe-shell') || document.body);
  }

  // The keyboard help, in a popover: the editor's own keys, then the
  // palette's (Guitar Pro's).
  function showKeys(anchor) {
    GPUI.popover(anchor, (el) => {
      const dl = document.createElement('dl');
      dl.className = 'gpe-keys-list';
      const rows = [
        ['← →', t('Beat; past the end of a bar that isn’t full adds a beat')], ['↑ ↓', t('String; on drums, the kit piece')], ['Ctrl/⌘ ← →', t('Previous or next bar')],
        ['0–24', t('Fret on the string (two quick digits for 10–24)')], ['+ / −', t('Shorter or longer')], ['Space', t('Play from the cursor; on drums, add the piece')],
        [t('⇧ ← →, ⇧-click'), t('Select bars, to show them in a section of the song')], ['Ctrl/⌘ Z, ⇧Z, S', t('Undo, redo, save')], ...palette.shortcuts(),
      ];
      for (const [k, n] of rows) {
        const dt = document.createElement('dt');
        const kb = document.createElement('kbd');
        kb.textContent = k;
        dt.appendChild(kb);
        const dd = document.createElement('dd');
        dd.textContent = n;
        dl.append(dt, dd);
      }
      el.appendChild(dl);
      const p = document.createElement('p');
      p.className = 'gpe-form-hint';
      p.textContent = t('Shortcuts are Guitar Pro’s. A few, like Ctrl/⌘ T, stay with the browser: use the palette for those.');
      el.appendChild(p);
    }, { title: t('Keyboard'), className: 'gpe-pop-keys' });
  }

  // ---- Playback ------------------------------------------------------------------

  function playPause() {
    const api = st.api;
    if (!api.isReadyForPlayback) { setStatus(t('The sounds are still loading…'), 'hint'); return; }
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
    if (!mod && !e.altKey && e.shiftKey && (k === 'ArrowRight' || k === 'ArrowLeft')) {
      e.preventDefault();
      const to = Math.max(0, Math.min(staffJ().bars.length - 1, (st.sel ? (st.bar) : st.bar) + (k === 'ArrowRight' ? 1 : -1)));
      if (!st.sel) st.sel = { anchor: st.bar, from: st.bar, to: st.bar };
      extendSelection(to);
      st.bar = to; st.beat = 0;
      renderPanels();
      drawCursor();
      return;
    }
    if (k === 'Escape' && st.sel) { e.preventDefault(); st.sel = null; drawSelection(); return; }
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
    if (k === '?' && !mod) { e.preventDefault(); showKeys($('gpe-help')); return; }
    if (palette) palette.onKey(e);
  }

  // ---- Load, save --------------------------------------------------------------------

  async function save() {
    if (!st.dirty) return;
    const btn = $('gpe-save');
    btn.disabled = true;
    setStatus(t('Saving a new version of the file…'));
    try {
      const out = await GPFile.save(variantId, JC.jsonToScore(JSON.stringify(st.doc), new alphaTab.Settings()), fileName);
      fileName = out.name;
      markDirty(false);
      const msg = document.createElement('span');
      msg.append(t('Saved as a new version.') + ' ');
      const back = document.createElement('a');
      const si = songSectionIndexAt(st.bar);
      back.href = `/song/${variantId}${si >= 0 ? '#section-' + si : ''}`;
      back.textContent = t('Back to the song');
      msg.appendChild(back);
      setStatus(msg, 'ok');
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
    st.api.playerReady.on(() => { $('gpe-play').classList.remove('loading'); });
    st.api.playerStateChanged.on((e) => {
      st.playing = e.state === 1;
      $('gpe-play').innerHTML = GPUI.icon(st.playing ? 'pause' : 'play');
      $('gpe-play').dataset.tip = st.playing ? t('Pause') : t('Play from the cursor');
    });
    buildToolbar();
    markDirty(st.dirty);
    rebuild();
    const wrap = $('gpe-canvas-wrap');
    wrap.addEventListener('keydown', onKey);
    $('gpe-canvas').addEventListener('click', pickAt);
    wrap.focus();
    window.addEventListener('beforeunload', (e) => { if (st.dirty) { e.preventDefault(); e.returnValue = ''; } });
    let rt = null;
    window.addEventListener('resize', () => { clearTimeout(rt); rt = setTimeout(draw, 200); });
  }

  start();
})();

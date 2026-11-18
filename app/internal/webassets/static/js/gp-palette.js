// The editing palette of the Guitar Pro file editor (gp-editor.js), laid
// out like Guitar Pro 8's: a rail down the left of the score with its
// groups in its order (Voice, Lyrics, Bar, Note, Effect, Notation,
// Automation — guitar-pro.com/docs/gp8/score/note/symbols) and its
// keyboard shortcuts. A drum track trades the string effects for its kit.
// The rail folds to its group icons, each opening its group beside it; on
// a phone it is a sheet at the bottom, one group at a time.
//
// Every tile changes alphaTab's JSON form of the file through the editor
// (ed.edit), so it is undoable and saved like any other edit. A tile with
// variants (bend amounts, harmonic kinds, tuplets…) offers them on a right
// click or a long press; values are asked in a small form beside the tile.
// Musical symbols are Bravura glyphs, the SMuFL font alphaTab ships.
//
// Left out, because alphaTab's model of a file can't hold them: master
// track volume and pan automations, pick scrapes, nested tuplets and
// sustain pedal marks; and Guitar Pro's design mode (page layout).
(function () {
  const G = (cp) => String.fromCodePoint(...[].concat(cp)); // Bravura glyph(s)
  const DYN = { p: 0xE520, m: 0xE521, f: 0xE522 };
  const dyn = (s) => G([...s].map((c) => DYN[c]));

  // Key specs: { key } is e.key without Ctrl/⌘ or Alt (letters compared
  // in lower case, Shift as given); { code, mod, alt, shift } for chords
  // with modifiers (e.code, so Alt on a Mac doesn't change the letter).
  function matches(spec, e) {
    const mod = e.metaKey || e.ctrlKey;
    if (spec.code) {
      return e.code === spec.code && mod === !!spec.mod && e.altKey === !!spec.alt && (spec.shift === undefined || e.shiftKey === spec.shift);
    }
    if (mod || e.altKey) return false;
    if (/^[a-z]$/.test(spec.key)) return e.key.toLowerCase() === spec.key && e.shiftKey === !!spec.shift;
    return e.key === spec.key;
  }
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const CODE_LABEL = { Period: '.', Slash: '/', Comma: '<', ArrowUp: '↑', ArrowDown: '↓' };
  function keyLabel(spec) {
    if (!spec) return '';
    if (spec.key) return (spec.shift ? '⇧' : '') + (spec.key.length === 1 ? spec.key.toUpperCase() : spec.key);
    const k = CODE_LABEL[spec.code] || spec.code.replace(/^Key|^Digit/, '');
    return (spec.mod ? (isMac ? '⌘' : 'Ctrl ') : '') + (spec.alt ? (isMac ? '⌥' : 'Alt ') : '') + (spec.shift ? (isMac ? '⇧' : 'Shift ') : '') + k;
  }

  window.GPPalette = function (ed) {
    const { st } = ed;
    const beat = () => ed.beatJ();
    const note = () => ed.curNote();
    const mb = () => ed.masterJ();
    const bar = () => ed.barJ();
    const ask = (anchor, spec) => GPUI.ask(anchor, spec);

    // Toggles on the beat, or on the note under the cursor.
    const onBeat = (field, value, off) => ({
      on: () => { const b = beat(); return !!b && b[field] === value; },
      run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); if (!b) return false; b[field] = b[field] === value ? off : value; }),
    });
    const onNote = (field, value, off) => ({
      needsNote: true,
      on: () => { const n = note(); return !!n && n[field] === value; },
      run: () => ed.edit(() => { const n = note(); if (!n) return false; n[field] = n[field] === value ? off : value; }),
    });
    const setNote = (fn) => ed.edit(() => { const n = note(); if (!n) return false; return fn(n); });
    const setBeat = (fn) => ed.edit(() => { ed.ensureVoice(); const b = beat(); if (!b) return false; return fn(b); });
    // A value from the cursor on, kept until the next change (dynamics,
    // triplet feel, clef): applied to every following beat or bar that
    // still had the old one.
    function fromHere(list, start, get, set, value) {
      const old = get(list[start]);
      for (let i = start; i < list.length; i++) {
        if (i > start && get(list[i]) !== old) break;
        set(list[i], value);
      }
    }
    const beatsFromCursor = () => {
      const out = [];
      const bars = ed.staffJ().bars;
      for (let b = st.bar; b < bars.length; b++) {
        const v = bars[b].voices[st.voice];
        if (!v) continue;
        v.beats.forEach((x, i) => { if (b > st.bar || i >= st.beat) out.push(x); });
      }
      return out;
    };

    // Pitches and accidentals.
    const LETTER_PC = [0, 2, 4, 5, 7, 9, 11];
    const pitchOf = (n) => ed.tuning()[ed.numStrings() - n.string] + n.fret;
    function setPitch(n, midi) {
      const fret = midi - ed.tuning()[ed.numStrings() - n.string];
      if (fret < 0 || fret > 24) throw new Error(t('That note is off this string.'));
      n.fret = fret;
    }
    // Writes the note with accidental acc (-2..2) on its letter; the pitch
    // follows, as Guitar Pro's accidental buttons do.
    function accidental(acc, mode) {
      return {
        needsNote: true, strings: true,
        on: () => { const n = note(); return !!n && n.accidentalmode === mode; },
        run: () => setNote((n) => {
          const midi = pitchOf(n);
          const pc = ((midi % 12) + 12) % 12;
          const flat = n.accidentalmode === 5 || n.accidentalmode === 6;
          let letter = LETTER_PC.lastIndexOf(LETTER_PC.filter((x) => x <= pc).pop());
          if (flat && !LETTER_PC.includes(pc)) letter = (letter + 1) % 7;
          let natural = midi - pc + LETTER_PC[letter];
          if (natural - midi > 6) natural -= 12;
          if (midi - natural > 6) natural += 12;
          setPitch(n, natural + acc);
          n.accidentalmode = mode;
        }),
      };
    }

    // Bends: the amount in quarter tones, and the shape.
    function bendPoints(kind, v) {
      if (kind === 'release') return [{ offset: 0, value: 0 }, { offset: 15, value: v }, { offset: 30, value: v }, { offset: 45, value: 0 }, { offset: 60, value: 0 }];
      if (kind === 'pre') return [{ offset: 0, value: v }, { offset: 60, value: v }];
      return [{ offset: 0, value: 0 }, { offset: 15, value: v }, { offset: 60, value: v }];
    }
    const BEND_TYPE = { bend: 2, release: 4, pre: 6 };
    const bendTop = (n) => (n.bendpoints && n.bendpoints.length ? Math.max(...n.bendpoints.map((p) => p.value)) : 0);
    const setBend = (kind, v) => setNote((n) => {
      if (!v) { n.bendtype = 0; n.bendpoints = []; return; }
      n.bendtype = BEND_TYPE[kind];
      n.bendpoints = bendPoints(kind, v);
    });

    const groups = [
      {
        id: 'voice', name: t('Voice'), icon: 'voice', items: [0, 1, 2, 3].map((v) => ({
          id: 'voice' + v, voice: v, text: String(v + 1), name: t('Voice %d', v + 1),
          on: () => st.voice === v, run: () => ed.setVoice(v),
        })),
      },
      {
        id: 'lyrics', name: t('Lyrics'), icon: 'lyrics', items: [{
          id: 'lyrics', text: 'la', perf: true, name: t('Lyrics: the syllable sung on this beat'),
          on: () => { const b = beat(); return !!(b && b.lyrics && b.lyrics.length && b.lyrics[0]); },
          run: async (a) => {
            const b = beat();
            const v = await ask(a, { title: t('Syllable on this beat'), clearable: true, fields: [{ name: 'syl', label: t('Sung'), value: (b && b.lyrics && b.lyrics[0]) || '', placeholder: 'la' }] });
            if (v) setBeat((x) => { x.lyrics = v.cleared || !v.syl ? null : [v.syl]; });
          },
        }],
      },
      {
        id: 'bar', name: t('Bar'), icon: 'bar', items: [
          { id: 'beatadd', svg: 'beatAdd', name: t('Insert a beat'), keys: [{ key: 'Insert' }], run: () => ed.insertBeat() },
          { id: 'beatdel', svg: 'beatDel', name: t('Delete the note, or an empty beat'), keys: [{ key: 'Backspace' }], run: () => ed.deleteAtCursor() },
          { id: 'barbefore', svg: 'barBefore', name: t('Insert a bar before this one'), run: () => ed.addBarBefore() },
          { id: 'barafter', svg: 'barAfter', name: t('Insert a bar after this one'), run: () => ed.addBarAfter() },
          { id: 'bardel', svg: 'barDel', name: t('Delete this bar, in every track'), run: (a) => ed.deleteBar(a) },
          { id: 'barcopy', svg: 'copy', name: t('Copy this bar'), keys: [{ code: 'KeyC', mod: true, shift: false }], run: () => ed.copyBar() },
          { id: 'barpaste', svg: 'paste', name: t('Paste over this bar'), keys: [{ code: 'KeyV', mod: true, shift: false }], run: () => ed.pasteBar() },
          { id: 'clef', glyph: G(0xE050), name: t('Clef'), on: () => bar().clef !== 4,
            variants: () => [[4, 'Treble clef', 0xE050], [3, 'Bass clef', 0xE062], [1, 'Alto clef', 0xE05C], [2, 'Tenor clef', 0xE05C], [0, 'None', null]].map(([v, n, g]) => ({ value: v, name: t(n), glyph: g ? G(g) : undefined, label: t(n), on: bar().clef === v })),
            pick: (v) => ed.edit(() => fromHere(ed.staffJ().bars, st.bar, (x) => x.clef, (x, val) => { x.clef = val; }, v)),
            run: (a, it) => it.openVariants(a) },
          { id: 'key', glyph: G([0xE262, 0xE260]), name: t('Key signature'), keys: [{ code: 'KeyK', mod: true }], run: (a) => ed.askKey(a) },
          { id: 'time', glyph: G(0xE08A), name: t('Time signature'), keys: [{ code: 'KeyT', mod: true }], run: (a) => ed.askTimeSignature(a) },
          { id: 'feel', text: '♫³', name: t('Rhythmic interpretation (triplet feel)'), keys: [{ code: 'Slash', mod: true }],
            on: () => !!mb().tripletfeel,
            variants: () => [[0, 'Straight'], [2, 'Swung eighths'], [1, 'Swung sixteenths'], [4, 'Dotted eighths'], [3, 'Dotted sixteenths']].map(([v, n]) => ({ value: v, label: t(n), on: (mb().tripletfeel || 0) === v })),
            pick: (v) => ed.edit(() => fromHere(st.doc.masterbars, st.bar, (x) => x.tripletfeel || 0, (x, val) => { x.tripletfeel = val; }, v)),
            run: () => ed.edit(() => { const order = [0, 2, 1]; const next = order[(order.indexOf(mb().tripletfeel || 0) + 1) % 3]; fromHere(st.doc.masterbars, st.bar, (x) => x.tripletfeel || 0, (x, v) => { x.tripletfeel = v; }, next); }) },
          { id: 'free', text: 'free', perf: true, name: t('Free time'), keys: [{ key: '|' }], on: () => !!mb().isfreetime, run: () => ed.edit(() => { mb().isfreetime = !mb().isfreetime; }) },
          { id: 'double', glyph: G(0xE031), name: t('Double bar line'), on: () => !!mb().isdoublebar, run: () => ed.edit(() => { mb().isdoublebar = !mb().isdoublebar; }) },
          { id: 'simile1', glyph: G(0xE500), name: t('Repeat the previous bar'), keys: [{ key: '%' }], on: () => bar().similemark === 1,
            run: () => ed.edit(() => { bar().similemark = bar().similemark === 1 ? 0 : 1; }) },
          { id: 'simile2', glyph: G(0xE501), name: t('Repeat the previous two bars'), keys: [{ code: 'Digit5', mod: true, shift: true }], on: () => bar().similemark === 2,
            run: () => ed.edit(() => {
              const bars = ed.staffJ().bars;
              if (st.bar + 1 >= bars.length) throw new Error(t('This needs a bar after this one.'));
              const on = bar().similemark === 2;
              bars[st.bar].similemark = on ? 0 : 2;
              bars[st.bar + 1].similemark = on ? 0 : 3;
            }) },
          { id: 'ropen', glyph: G(0xE040), name: t('Repeat start'), keys: [{ key: '[' }], on: () => !!mb().isrepeatstart, run: () => ed.edit(() => { mb().isrepeatstart = !mb().isrepeatstart; }) },
          { id: 'rclose', glyph: G(0xE041), name: t('Repeat close'), keys: [{ key: ']' }], on: () => !!mb().repeatcount, run: (a) => ed.askRepeatEnd(a) },
          { id: 'ending', text: '1.', perf: true, name: t('Alternate endings'), on: () => !!mb().alternateendings, run: (a) => ed.askEnding(a) },
          { id: 'directions', glyph: G([0xE047]), name: t('Directions: segno, coda, fine, D.C., D.S.'), keys: [{ key: 'd' }], on: () => !!(mb().directions && mb().directions.length), run: (a) => askDirections(a) },
          ...[['8va', 1], ['8vb', 3], ['15ma', 0], ['15mb', 4]].map(([label, v]) => ({
            id: label, text: label, perf: true, name: t(label.startsWith('8') ? (label.endsWith('a') ? '%s: played an octave higher' : '%s: played an octave lower') : (label.endsWith('a') ? '%s: played two octaves higher' : '%s: played two octaves lower'), label),
            on: () => { const b = beat(); return !!b && b.ottava === v; },
            run: () => ed.edit(() => { const v0 = ed.ensureVoice(); const on = beat() && beat().ottava === v; for (const b of v0.beats) b.ottava = on ? 2 : v; }),
          })),
        ],
      },
      {
        id: 'note', name: t('Note'), icon: 'note', items: [
          ...[[1, 0xE1D2], [2, 0xE1D3], [4, 0xE1D5], [8, 0xE1D7], [16, 0xE1D9], [32, 0xE1DB], [64, 0xE1DD]].map(([d, cp]) => ({
            id: 'dur' + d, glyph: G(cp), name: t({ 1: 'Whole note', 2: 'Half note', 4: 'Quarter note', 8: 'Eighth note', 16: 'Sixteenth note', 32: 'Thirty-second note', 64: 'Sixty-fourth note' }[d]),
            more: d === 4 ? t('+ shorter, − longer') : '',
            on: () => { const b = beat(); return !!b && b.duration === d; }, run: () => ed.setDuration(d),
          })),
          { id: 'rest', glyph: G(0xE4E5), name: t('Rest'), keys: [{ key: 'r' }], on: () => { const b = beat(); return !!b && !b.notes.length; },
            run: () => ed.edit(() => { const b = beat(); if (!b || !b.notes.length) return false; b.notes = []; }) },
          { id: 'dot', glyph: G(0xE1E7), name: t('Dotted note'), keys: [{ key: '.' }], on: () => { const b = beat(); return !!b && b.dots === 1; },
            run: () => setBeat((b) => { b.dots = b.dots === 1 ? 0 : 1; }) },
          { id: 'ddot', glyph: G([0xE1E7, 0xE1E7]), name: t('Double-dotted note'), keys: [{ code: 'Period', mod: true }], on: () => { const b = beat(); return !!b && b.dots === 2; },
            run: () => setBeat((b) => { b.dots = b.dots === 2 ? 0 : 2; }) },
          { id: 'triplet', glyph: G(0xE883), name: t('Triplet'), keys: [{ key: '/' }], on: () => { const b = beat(); return !!b && b.tupletnumerator > 0; },
            variants: () => { const b = beat() || {}; return [[3, 2], [5, 4], [6, 4], [7, 4], [9, 8], [0, 0]].map(([n, d]) => ({ value: [n, d], label: n ? `${n}:${d}` : t('None'), on: b.tupletnumerator === n && b.tupletdenominator === d })); },
            pick: ([n, d]) => setBeat((b) => { b.tupletnumerator = n || -1; b.tupletdenominator = d || -1; }),
            run: () => setBeat((b) => { const on = b.tupletnumerator === 3 && b.tupletdenominator === 2; b.tupletnumerator = on ? -1 : 3; b.tupletdenominator = on ? -1 : 2; }) },
          { id: 'tuplet', text: 'n:m', perf: true, name: t('Custom n-tuplet'), on: () => { const b = beat(); return !!b && b.tupletnumerator > 0 && !(b.tupletnumerator === 3 && b.tupletdenominator === 2); },
            run: async (a) => {
              const b = beat() || {};
              const v = await ask(a, { title: t('Tuplet'), clearable: true, fields: [
                { name: 'n', label: t('Notes'), type: 'number', min: 2, max: 13, value: b.tupletnumerator > 0 ? b.tupletnumerator : 5 },
                { name: 'd', label: t('In the time of'), type: 'number', min: 1, max: 16, value: b.tupletdenominator > 0 ? b.tupletdenominator : 4 }],
                check: (x) => (x.n >= 2 && x.d >= 1 ? '' : t('Two numbers, like 5 in the time of 4.')) });
              if (v) setBeat((x) => { x.tupletnumerator = v.cleared ? -1 : v.n; x.tupletdenominator = v.cleared ? -1 : v.d; });
            } },
          { id: 'tie', text: '⁀', name: t('Tie'), keys: [{ key: 'l' }], ...onNote('istiedestination', true, false) },
          { id: 'ties', text: '⁀⁀', name: t('Tie every note of the beat'), keys: [{ key: 'l', shift: true }], needsNote: true,
            on: () => { const b = beat(); return !!b && b.notes.length > 0 && b.notes.every((n) => n.istiedestination); },
            run: () => ed.edit(() => { const b = beat(); if (!b || !b.notes.length) return false; const on = b.notes.every((n) => n.istiedestination); for (const n of b.notes) n.istiedestination = !on; }) },
          { id: 'fermata', glyph: G(0xE4C0), name: t('Fermata'), keys: [{ key: 'f' }], on: () => !!fermataAt(),
            variants: () => [[0, 'Short', 0xE4C4], [1, 'Medium', 0xE4C0], [2, 'Long', 0xE4C6], [-1, 'None', null]].map(([v, n, g]) => ({ value: v, name: t(n), label: t(n), glyph: g ? G(g) : undefined, on: (fermataAt() || {}).type === v })),
            pick: (v) => ed.edit(() => { const m = mb(); const at = fermataKey(); m.fermata = m.fermata || {}; if (v < 0) delete m.fermata[at]; else m.fermata[at] = { type: v, length: 1 }; }),
            run: () => ed.edit(() => { const m = mb(); const at = fermataKey(); m.fermata = m.fermata || {}; if (m.fermata[at]) delete m.fermata[at]; else m.fermata[at] = { type: 1, length: 1 }; }) },
          { id: 'dflat', glyph: G(0xE264), name: t('Double flat'), keys: [{ code: 'Digit7', mod: true, alt: true }], ...accidental(-2, 6) },
          { id: 'flat', glyph: G(0xE260), name: t('Flat'), keys: [{ code: 'Digit7', mod: true, alt: false }], ...accidental(-1, 5) },
          { id: 'natural', glyph: G(0xE261), name: t('Natural'), keys: [{ code: 'Digit8', mod: true, alt: false }], ...accidental(0, 2) },
          { id: 'sharp', glyph: G(0xE262), name: t('Sharp'), keys: [{ code: 'Digit9', mod: true, alt: false }], ...accidental(1, 3) },
          { id: 'dsharp', glyph: G(0xE263), name: t('Double sharp'), keys: [{ code: 'Digit9', mod: true, alt: true }], ...accidental(2, 4) },
          { id: 'enh', glyph: G([0xE262, 0xE260]), name: t('Change accidental: the same pitch, spelled the other way'), keys: [{ code: 'Digit8', mod: true, alt: true }], needsNote: true, strings: true,
            run: () => setNote((n) => { n.accidentalmode = n.accidentalmode === 5 ? 3 : 5; }) },
          { id: 'down', text: '−½', name: t('Lower by a semitone'), keys: [{ code: 'ArrowDown', alt: true, shift: true }], needsNote: true, strings: true, run: () => setNote((n) => { setPitch(n, pitchOf(n) - 1); }) },
          { id: 'up', text: '+½', name: t('Raise by a semitone'), keys: [{ code: 'ArrowUp', alt: true, shift: true }], needsNote: true, strings: true, run: () => setNote((n) => { setPitch(n, pitchOf(n) + 1); }) },
          ...['ppp', 'pp', 'p', 'mp', 'mf', 'f', 'ff', 'fff'].map((d, v) => ({
            id: 'dyn' + d, glyph: dyn(d), name: { ppp: 'Pianississimo', pp: 'Pianissimo', p: 'Piano', mp: 'Mezzo piano', mf: 'Mezzo forte', f: 'Forte', ff: 'Fortissimo', fff: 'Fortississimo' }[d], more: t('From here until the next change'),
            on: () => { const b = beat(); return !!b && b.notes.length > 0 && b.dynamics === v; },
            run: () => ed.edit(() => { ed.ensureVoice(); fromHere(beatsFromCursor(), 0, (x) => x.dynamics, (x, val) => { x.dynamics = val; }, v); }),
          })),
          { id: 'cresc', glyph: G(0xE53E), name: t('Crescendo'), ...onBeat('crescendo', 1, 0) },
          { id: 'decresc', glyph: G(0xE53F), name: t('Decrescendo'), ...onBeat('crescendo', 2, 0) },
        ],
      },
      {
        id: 'effect', name: t('Effect'), icon: 'effect', strings: true, items: [
          { id: 'ghost', glyph: G([0xE0F5, 0xE0A4, 0xE0F6]), name: t('Ghost note'), keys: [{ key: 'o' }], ...onNote('isghost', true, false) },
          { id: 'accent', glyph: G(0xE4A0), name: t('Accented note'), keys: [{ key: ';' }], ...onNote('accentuated', 1, 0) },
          { id: 'heavy', glyph: G(0xE4AC), name: t('Heavily accented note'), keys: [{ key: ':' }], ...onNote('accentuated', 2, 0) },
          { id: 'staccato', glyph: G(0xE4A2), name: t('Staccato'), keys: [{ key: '!' }], ...onNote('isstaccato', true, false) },
          { id: 'legato', text: '⌒', name: t('Legato'), keys: [{ key: 'h', shift: true }], ...onBeat('islegatoorigin', true, false) },
          { id: 'letring', text: 'L.R.', perf: true, name: t('Let ring'), keys: [{ key: 'i' }], ...onNote('isletring', true, false) },
          { id: 'palm', text: 'P.M.', perf: true, name: t('Palm mute'), keys: [{ key: 'p' }], ...onNote('ispalmmute', true, false) },
          { id: 'dead', glyph: G(0xE0A9), name: t('Dead note'), keys: [{ key: 'x' }], ...onNote('isdead', true, false) },
          { id: 'nharm', glyph: G(0xE0D9), name: t('Natural harmonic'), keys: [{ key: 'y' }], needsNote: true,
            on: () => { const n = note(); return !!n && n.harmonictype === 1; },
            run: () => setNote((n) => { const on = n.harmonictype === 1; n.harmonictype = on ? 0 : 1; n.harmonicvalue = on ? 0 : n.fret; }) },
          { id: 'aharm', glyph: G(0xE0DB), name: t('Artificial harmonic'), keys: [{ code: 'KeyY', alt: true }], needsNote: true,
            on: () => { const n = note(); return !!n && n.harmonictype >= 2; },
            variants: () => [[2, 'A.H.', 'Artificial'], [3, 'P.H.', 'Pinch'], [4, 'T.H.', 'Tapped'], [5, 'S.H.', 'Semi'], [6, 'Fdbk', 'Feedback'], [0, 'None', 'None']].map(([v, l, n]) => ({ value: v, label: t(l), name: t(n), on: (note() || {}).harmonictype === v })),
            pick: (v) => setNote((n) => { n.harmonictype = v; n.harmonicvalue = v ? n.fret + 12 : 0; }),
            run: () => setNote((n) => { const order = [0, 2, 3, 4, 5]; const next = order[(order.indexOf(n.harmonictype >= 2 ? n.harmonictype : 0) + 1) % order.length]; n.harmonictype = next; n.harmonicvalue = next ? n.fret + 12 : 0; }) },
          { id: 'bend', text: '⤴', name: t('Bend'), keys: [{ key: 'b' }], needsNote: true, more: t('Right-click for the amount and shape'),
            on: () => { const n = note(); return !!n && !!n.bendtype; },
            variants: () => {
              const n = note() || {};
              const cur = (k, v) => n.bendtype === BEND_TYPE[k] && bendTop(n) === v;
              return [['bend', 1, '¼'], ['bend', 2, '½'], ['bend', 4, 'full'], ['bend', 6, '1½'], ['bend', 8, '2'], ['release', 4, 'full ↓'], ['release', 2, '½ ↓'], ['pre', 4, 'pre'], ['bend', 0, 'None']]
                .map(([k, v, l]) => ({ value: [k, v], label: t(l), name: k === 'release' ? t('Bend and release') : k === 'pre' ? t('Pre-bend') : t('Bend'), on: v ? cur(k, v) : !n.bendtype }));
            },
            pick: ([k, v]) => setBend(k, v),
            run: () => { const n = note(); if (!n) return; const order = [0, 4, 2, 1, 6]; const next = order[(order.indexOf(n.bendtype ? bendTop(n) : 0) + 1) % order.length]; setBend('bend', next); } },
          { id: 'fadein', text: '<', name: t('Fade-in'), keys: [{ key: '<' }], ...onBeat('fade', 1, 0) },
          { id: 'fadeout', text: '>', name: t('Fade-out'), keys: [{ key: '>' }], ...onBeat('fade', 2, 0) },
          { id: 'swell', text: '<>', name: t('Volume swell'), keys: [{ code: 'Comma', alt: true }], ...onBeat('fade', 3, 0) },
          { id: 'gbefore', glyph: G(0xE560), name: t('Grace note before the beat'), keys: [{ key: 'g' }], ...onBeat('gracetype', 2, 0) },
          { id: 'gon', glyph: G(0xE562), name: t('Grace note on the beat'), keys: [{ code: 'KeyG', alt: true }], ...onBeat('gracetype', 1, 0) },
          { id: 'hammer', text: 'H/P', perf: true, name: t('Hammer-on / pull-off'), keys: [{ key: 'h' }], ...onNote('ishammerpullorigin', true, false) },
          { id: 'tremolo', glyph: G(0xE222), name: t('Tremolo picking'), keys: [{ key: '"' }],
            on: () => { const b = beat(); return !!(b && b.tremolopicking); },
            variants: () => [[1, 0xE220, 'Eighths'], [2, 0xE221, 'Sixteenths'], [3, 0xE222, 'Thirty-seconds'], [0, null, 'None']].map(([m, g, n]) => ({ value: m, glyph: g ? G(g) : undefined, label: t(n), name: t(n), on: ((beat() || {}).tremolopicking || {}).marks === m })),
            pick: (m) => setBeat((b) => { b.tremolopicking = m ? { marks: m, style: 0 } : null; }),
            run: () => setBeat((b) => { b.tremolopicking = b.tremolopicking ? null : { marks: 3, style: 0 }; }) },
          { id: 'umordent', glyph: G(0xE56C), name: t('Upper mordent'), ...onNote('ornament', 3, 0) },
          { id: 'lmordent', glyph: G(0xE56D), name: t('Lower mordent'), ...onNote('ornament', 4, 0) },
          { id: 'turn', glyph: G(0xE567), name: t('Turn'), ...onNote('ornament', 2, 0) },
          { id: 'iturn', glyph: G(0xE568), name: t('Inverted turn'), ...onNote('ornament', 1, 0) },
          { id: 'lhtap', text: '(T)', perf: true, name: t('Left-hand tapping'), keys: [{ key: '(' }], ...onNote('islefthandtapped', true, false) },
          { id: 'lslide', text: '⟋', name: t('Legato slide'), keys: [{ key: 's' }], ...onNote('slideouttype', 2, 0) },
          { id: 'sslide', text: '⟋s', name: t('Shift slide'), ...onNote('slideouttype', 1, 0) },
          { id: 'inbelow', text: '⟋•', name: t('Slide in from below'), ...onNote('slideintype', 1, 0) },
          { id: 'inabove', text: '⟍•', name: t('Slide in from above'), ...onNote('slideintype', 2, 0) },
          { id: 'outdown', text: '•⟍', name: t('Slide out downwards'), ...onNote('slideouttype', 4, 0) },
          { id: 'outup', text: '•⟋', name: t('Slide out upwards'), ...onNote('slideouttype', 3, 0) },
          { id: 'whammy', text: 'w/b', perf: true, name: t('Whammy bar'), keys: [{ code: 'KeyV', alt: true }],
            on: () => { const b = beat(); return !!(b && b.whammybartype); },
            variants: () => [[3, 'Dip'], [2, 'Dive'], [5, 'Pre-dive'], [0, 'None']].map(([v, l]) => ({ value: v, label: t(l), on: (beat() || {}).whammybartype === v })),
            pick: (v) => setBeat((b) => { b.whammybartype = v; b.whammybarpoints = v === 3 ? [{ offset: 0, value: 0 }, { offset: 30, value: -4 }, { offset: 60, value: 0 }] : v === 2 ? [{ offset: 0, value: 0 }, { offset: 60, value: -8 }] : v === 5 ? [{ offset: 0, value: -4 }, { offset: 60, value: -4 }] : []; }),
            run: () => setBeat((b) => { if (b.whammybartype) { b.whammybartype = 0; b.whammybarpoints = []; } else { b.whammybartype = 3; b.whammybarpoints = [{ offset: 0, value: 0 }, { offset: 30, value: -4 }, { offset: 60, value: 0 }]; } }) },
          { id: 'trill', glyph: G(0xE566), name: t('Trill'), keys: [{ key: 'n' }], needsNote: true,
            on: () => { const n = note(); return !!n && n.trillvalue >= 0; },
            run: async (a) => {
              const n = note(); if (!n) return;
              const v = await ask(a, { title: t('Trill'), clearable: n.trillvalue >= 0, fields: [{ name: 'fret', label: t('With fret'), type: 'number', min: 0, max: 24, value: n.trillvalue >= 0 ? n.trillvalue - ed.tuning()[ed.numStrings() - n.string] : n.fret + 2 }],
                check: (x) => (x.fret >= 0 && x.fret <= 24 ? '' : t('A fret from 0 to 24.')) });
              if (v) setNote((x) => { if (v.cleared) { x.trillvalue = -1; return; } x.trillvalue = ed.tuning()[ed.numStrings() - x.string] + v.fret; x.trillspeed = 16; });
            } },
          { id: 'vib', text: '∿', name: t('Slight left-hand vibrato'), keys: [{ key: 'v' }], ...onNote('vibrato', 1, 0) },
          { id: 'wvib', text: '≋', name: t('Wide left-hand vibrato'), ...onNote('vibrato', 2, 0) },
          { id: 'bvib', text: 'w∿', perf: true, name: t('Slight bridge vibrato'), keys: [{ key: 'w' }], ...onBeat('vibrato', 1, 0) },
          { id: 'wbvib', text: 'w≋', perf: true, name: t('Wide bridge vibrato'), keys: [{ code: 'KeyW', alt: true }], ...onBeat('vibrato', 2, 0) },
          { id: 'wahc', glyph: G(0xE5E5), name: t('Closed wah-wah'), keys: [{ code: 'KeyC', alt: true }], ...onBeat('wahpedal', 2, 0) },
          { id: 'waho', glyph: G(0xE5E7), name: t('Open wah-wah'), keys: [{ code: 'KeyO', alt: true }], ...onBeat('wahpedal', 1, 0) },
          { id: 'arpdown', glyph: G(0xE635), name: t('Downward arpeggio'), keys: [{ code: 'KeyD', mod: true, shift: true }], ...onBeat('brushtype', 4, 0) },
          { id: 'arpup', glyph: G(0xE634), name: t('Upward arpeggio'), keys: [{ code: 'KeyU', mod: true, shift: true }], ...onBeat('brushtype', 3, 0) },
          { id: 'brushdown', text: '↡', name: t('Downward brush'), keys: [{ code: 'KeyD', mod: true, shift: false }], ...onBeat('brushtype', 2, 0) },
          { id: 'brushup', text: '↟', name: t('Upward brush'), keys: [{ code: 'KeyU', mod: true, shift: false }], ...onBeat('brushtype', 1, 0) },
          { id: 'rasg', text: 'rasg.', perf: true, name: t('Rasgueado'), keys: [{ key: 'r', shift: true }], ...onBeat('rasgueado', 1, 0) },
          { id: 'lhf', text: 'LH', perf: true, name: t('Left-hand fingering'), needsNote: true, on: () => { const n = note(); return !!n && n.lefthandfinger >= 0; },
            variants: () => ['T', '1', '2', '3', '4', 'None'].map((l, i) => ({ value: i < 5 ? i : -1, label: t(l), on: (note() || {}).lefthandfinger === (i < 5 ? i : -1) })),
            pick: (v) => setNote((n) => { n.lefthandfinger = v; }), run: (a, it) => it.openVariants(a) },
          { id: 'rhf', text: 'RH', perf: true, name: t('Right-hand fingering'), needsNote: true, on: () => { const n = note(); return !!n && n.righthandfinger >= 0; },
            variants: () => ['p', 'i', 'm', 'a', 'c', 'None'].map((l, i) => ({ value: i < 5 ? i : -1, label: t(l), on: (note() || {}).righthandfinger === (i < 5 ? i : -1) })),
            pick: (v) => setNote((n) => { n.righthandfinger = v; }), run: (a, it) => it.openVariants(a) },
          { id: 'stringnum', text: '③', name: t('String number'), ...onNote('showstringnumber', true, false) },
          { id: 'pickdown', glyph: G(0xE610), name: t('Downward pickstroke'), keys: [{ key: 'd', shift: true }], ...onBeat('pickstroke', 2, 0) },
          { id: 'pickup', glyph: G(0xE612), name: t('Upward pickstroke'), keys: [{ key: 'u', shift: true }], ...onBeat('pickstroke', 1, 0) },
          { id: 'tap', text: 'T', perf: true, name: t('Tapping'), keys: [{ key: ')' }], ...onBeat('tap', true, false) },
          { id: 'slap', text: 'S', perf: true, name: t('Slap'), keys: [{ key: '$' }], ...onBeat('slap', true, false) },
          { id: 'pop', text: 'P', perf: true, name: t('Pop'), ...onBeat('pop', true, false) },
          { id: 'fgolpe', text: 'g.f', perf: true, name: t('Finger golpe'), ...onBeat('golpe', 2, 0) },
          { id: 'tgolpe', text: 'g.t', perf: true, name: t('Thumb golpe'), ...onBeat('golpe', 1, 0) },
          { id: 'deadslap', glyph: G(0xE0A9), text: '', name: t('Dead slap'), ...onBeat('deadslapped', true, false) },
        ],
      },
      {
        id: 'kit', name: t('Kit'), icon: 'kit', drums: true, items: ed.DRUM_PIECES.map((p, i) => ({
          id: 'kit' + p.midi, text: p.short, name: p.name, keys: p.key ? [{ key: p.key }] : undefined, kit: true,
          on: () => !!ed.drumNote(beat(), p), current: () => st.piece === i, run: () => ed.toggleDrum(p),
        })),
      },
      {
        id: 'notation', name: t('Notation'), icon: 'notation', items: [
          { id: 'chord', text: 'C7', chord: true, name: t('Chord name on this beat'), keys: [{ key: 'a' }], on: () => { const b = beat(); return !!b && b.chordid != null; }, run: (a) => askChord(a) },
          { id: 'slash', glyph: G(0xE101), name: t('Slash notation'), ...onBeat('slashed', true, false) },
          { id: 'barre', text: 'C.V', perf: true, name: t('Barre'), keys: [{ key: 'i', shift: true }], on: () => { const b = beat(); return !!b && b.barrefret > 0; },
            run: async (a) => {
              const b = beat() || {};
              const v = await ask(a, { title: t('Barre'), clearable: b.barrefret > 0, fields: [
                { name: 'fret', label: t('Fret'), type: 'number', min: 1, max: 24, value: b.barrefret > 0 ? b.barrefret : 5 },
                { name: 'shape', label: t('Shape'), type: 'select', options: [['1', t('Full barre')], ['2', t('Half barre')]], value: String(b.barreshape === 2 ? 2 : 1) }],
                check: (x) => (x.fret >= 1 && x.fret <= 24 ? '' : t('A fret from 1 to 24.')) });
              if (v) setBeat((x) => { x.barrefret = v.cleared ? -1 : v.fret; x.barreshape = v.cleared ? 0 : +v.shape; });
            } },
          { id: 'timer', text: '0:00', perf: true, name: t('Timer'), keys: [{ key: '@' }], ...onBeat('showtimer', true, false) },
          { id: 'text', text: 'Txt', perf: true, name: t('Text'), keys: [{ key: 't' }], on: () => { const b = beat(); return !!(b && b.text); },
            run: async (a) => {
              const b = beat();
              const v = await ask(a, { title: t('Text over this beat'), clearable: !!(b && b.text), fields: [{ name: 'text', label: t('Text'), value: (b && b.text) || '', placeholder: t('let it ring') }] });
              if (v) setBeat((x) => { x.text = v.cleared || !v.text ? null : v.text; });
            } },
          { id: 'section', text: 'A', rehearsal: true, name: t('Section'), on: () => !!(mb().section && mb().section.text), run: (a) => ed.askSection(a) },
          { id: 'stemauto', text: 'auto', perf: true, name: t('Automatic stem direction'), on: () => { const b = beat(); return !!b && b.preferredbeamdirection == null; },
            run: () => setBeat((b) => { b.preferredbeamdirection = null; }) },
          { id: 'steminv', text: '⇅', name: t('Inverted stem direction'), on: () => { const b = beat(); return !!b && b.preferredbeamdirection != null; },
            run: () => setBeat((b) => { b.preferredbeamdirection = b.preferredbeamdirection === 1 ? 0 : 1; }) },
          { id: 'beamauto', text: '♫', name: t('Automatic beaming'), ...onBeat('beamingmode', 0, 0) },
          { id: 'beamforce', text: '♫+', name: t('Force beaming with the next note'), ...onBeat('beamingmode', 2, 0) },
          { id: 'beambreak', text: '♪♪', name: t('Break beaming'), ...onBeat('beamingmode', 1, 0) },
          { id: 'beambreak2', text: '♬|', name: t('Break secondary beaming'), ...onBeat('beamingmode', 3, 0) },
        ],
      },
      {
        id: 'automation', name: t('Automation'), icon: 'automation', items: [
          { id: 'tempo', glyph: G([0xE1D5]), text: '', suffix: '=', name: t('Tempo from this bar on'), on: () => !!(mb().tempoautomations && mb().tempoautomations.length), run: (a) => ed.askTempo(a) },
          { id: 'volume', text: 'Vol', perf: true, name: t('Track volume from this beat on'), on: () => !!automation(1), run: (a) => askAutomation(a, 1, t('Track volume'), t('Volume (0–16)'), 0, 16, (v) => v, (v) => v) },
          { id: 'pan', text: 'Pan', perf: true, name: t('Track pan from this beat on'), on: () => !!automation(3), run: (a) => askAutomation(a, 3, t('Track pan'), t('Pan (−8 left … 8 right)'), -8, 8, (v) => v + 8, (v) => v - 8) },
        ],
      },
    ];

    const fermataKey = () => String(ed.beatM() ? ed.beatM().playbackStart : 0);
    function fermataAt() { const m = mb(); return m.fermata && m.fermata[fermataKey()]; }
    function automation(type) { const b = beat(); return b && (b.automations || []).find((x) => x.type === type); }
    async function askAutomation(a, type, title, label, lo, hi, store, show) {
      const cur = automation(type);
      const v = await ask(a, { title, clearable: !!cur, fields: [{ name: 'v', label, type: 'number', min: lo, max: hi, value: cur ? show(cur.value) : (type === 3 ? 0 : 12) }],
        check: (x) => (x.v >= lo && x.v <= hi ? '' : t('From %d to %d.', lo, hi)) });
      if (!v) return;
      setBeat((b) => {
        b.automations = (b.automations || []).filter((x) => x.type !== type);
        if (!v.cleared) b.automations.push({ islinear: false, type, value: store(v.v), ratioposition: 0, text: '', isvisible: true });
      });
    }
    const DIRECTIONS = ['Fine', 'Segno', 'Segno Segno', 'Coda', 'Double Coda', 'Da Capo', 'Da Capo al Coda', 'Da Capo al Double Coda', 'Da Capo al Fine', 'Dal Segno', 'Dal Segno al Coda', 'Dal Segno al Double Coda', 'Dal Segno al Fine', 'Dal Segno Segno', 'Dal Segno Segno al Coda', 'Dal Segno Segno al Double Coda', 'Dal Segno Segno al Fine', 'Da Coda', 'Da Double Coda'];
    async function askDirections(a) {
      const cur = new Set(mb().directions || []);
      const v = await GPUI.choose(a, { title: t('Directions on this bar'), columns: 2, options: [
        ...DIRECTIONS.map((d, i) => ({ value: i, label: d, on: cur.has(i) })),
        { value: -1, label: t('None'), on: !cur.size }] });
      if (v === null) return;
      ed.edit(() => {
        const m = mb();
        const set = new Set(m.directions || []);
        if (v < 0) set.clear(); else if (set.has(v)) set.delete(v); else set.add(v);
        m.directions = [...set];
      });
    }
    async function askChord(a) {
      const b = beat();
      const chords = ed.staffJ().chords || {};
      const cur = b && b.chordid != null && chords[b.chordid] ? chords[b.chordid].name : '';
      const v = await ask(a, { title: t('Chord on this beat'), clearable: !!cur, fields: [{ name: 'name', label: t('Chord'), value: cur, placeholder: 'A7' }] });
      if (!v) return;
      setBeat((x) => {
        const name = v.cleared ? '' : v.name;
        if (!name) { x.chordid = null; return; }
        const staff = ed.staffJ();
        staff.chords = staff.chords || {};
        let id = Object.keys(staff.chords).find((k) => staff.chords[k].name === name);
        if (!id) {
          let n = 0;
          while (staff.chords[String(n)]) n++;
          id = String(n);
          staff.chords[id] = { name, firstfret: 1, strings: ed.tuning().map(() => -1), barrefrets: [], showname: true, showdiagram: false, showfingering: false };
        }
        x.chordid = id;
      });
    }

    // ---- Drawing the rail ----------------------------------------------------------
    const all = groups.flatMap((g) => g.items.map((it) => Object.assign(it, { group: g })));
    const visible = (g) => (g.strings ? !ed.isDrums() : g.drums ? ed.isDrums() : true);
    const itemVisible = (it) => visible(it.group) && !(it.strings && ed.isDrums());

    function anchorOf(it) {
      return it.el && it.el.offsetParent ? it.el : (document.getElementById('gpe-cursor') || document.body);
    }
    async function runItem(it, anchor) {
      if (it.needsNote && !note()) { ed.setStatus(ed.isDrums() ? t('Put a hit on this beat first.') : t('Put a note on this beat first: type a fret.'), 'hint'); return; }
      try { await it.run(anchor || anchorOf(it), it); } catch (e) { ed.setStatus(e.message, true); }
      ed.focus();
    }
    async function openVariants(it, anchor) {
      if (!it.variants) return runItem(it, anchor);
      if (it.needsNote && !note()) { ed.setStatus(t('Put a note on this beat first: type a fret.'), 'hint'); return; }
      const k = it.keys && it.keys[0] ? keyLabel(it.keys[0]) : '';
      const v = await GPUI.choose(anchor || anchorOf(it), { title: it.name, kbd: k, options: it.variants(), columns: 3 });
      if (v !== null) { try { await it.pick(v); } catch (e) { ed.setStatus(e.message, true); } }
      ed.focus();
    }
    for (const it of all) it.openVariants = (a) => openVariants(it, a);

    function tile(it) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'gpe-tile';
      b.dataset.id = it.id;
      if (it.svg) b.innerHTML = GPUI.icon(it.svg);
      else if (it.glyph) { b.textContent = it.glyph + (it.suffix || ''); b.classList.add('smufl'); }
      else b.textContent = it.text;
      if (it.perf) b.classList.add('perf');
      if (it.chord) b.classList.add('chordname');
      if (it.rehearsal) b.classList.add('rehearsal');
      if (it.kit) b.classList.add('kit');
      if (it.voice !== undefined) { b.classList.add('voice'); b.style.setProperty('--v', `var(--gpe-v${it.voice + 1})`); }
      if (it.variants) b.classList.add('has-more');
      b.dataset.tip = it.name;
      if (it.keys && it.keys[0]) b.dataset.kbd = keyLabel(it.keys[0]);
      const more = [it.more, it.variants ? t('Right-click or hold for more') : ''].filter(Boolean).join(' · ');
      if (more) b.dataset.more = more;
      b.setAttribute('aria-label', it.name);
      b.addEventListener('click', () => runItem(it, b));
      if (it.variants) {
        b.addEventListener('contextmenu', (e) => { e.preventDefault(); openVariants(it, b); });
        let hold = null;
        b.addEventListener('pointerdown', (e) => { if (e.pointerType !== 'mouse') hold = setTimeout(() => { hold = 'fired'; openVariants(it, b); }, 450); });
        const cancel = () => { if (hold && hold !== 'fired') clearTimeout(hold); };
        b.addEventListener('pointerup', cancel);
        b.addEventListener('pointerleave', cancel);
        b.addEventListener('click', (e) => { if (hold === 'fired') { e.stopImmediatePropagation(); hold = null; } }, true);
      }
      return b;
    }

    function build(rail) {
      rail.innerHTML = '';
      const head = document.createElement('div');
      head.className = 'gpe-rail-head';
      const fold = document.createElement('button');
      fold.type = 'button';
      fold.className = 'gpe-rail-fold';
      fold.innerHTML = GPUI.icon('rail');
      fold.dataset.tip = t('Fold the palette to its group icons');
      fold.setAttribute('aria-label', t('Fold the palette'));
      fold.addEventListener('click', () => {
        rail.classList.toggle('folded');
        try { localStorage.setItem('gpe-rail-folded', rail.classList.contains('folded') ? '1' : '0'); } catch (e) { /* storage off */ }
        fold.dataset.tip = rail.classList.contains('folded') ? t('Unfold the palette') : t('Fold the palette to its group icons');
      });
      const title = document.createElement('span');
      title.textContent = t('Palette');
      head.append(title, fold);
      rail.appendChild(head);
      try { if (localStorage.getItem('gpe-rail-folded') === '1') rail.classList.add('folded'); } catch (e) { /* storage off */ }

      // Phone: the groups as tabs over one grid.
      const tabs = document.createElement('div');
      tabs.className = 'gpe-rail-tabs';
      tabs.setAttribute('role', 'tablist');
      rail.appendChild(tabs);

      for (const g of groups) {
        const sec = document.createElement('section');
        sec.className = 'gpe-pal-group';
        sec.dataset.group = g.id;
        const h = document.createElement('button');
        h.type = 'button';
        h.className = 'gpe-pal-head';
        h.innerHTML = GPUI.icon(g.icon) + `<span>${g.name}</span>`;
        h.dataset.tip = g.name;
        h.setAttribute('aria-expanded', 'true');
        let closed = false;
        try { closed = localStorage.getItem('gpe-pal-closed:' + g.id) === '1'; } catch (e) { /* storage off */ }
        if (closed) { sec.classList.add('closed'); h.setAttribute('aria-expanded', 'false'); }
        h.addEventListener('click', () => {
          if (rail.classList.contains('folded')) { flyout(g, h); return; }
          sec.classList.toggle('closed');
          h.setAttribute('aria-expanded', sec.classList.contains('closed') ? 'false' : 'true');
          try { localStorage.setItem('gpe-pal-closed:' + g.id, sec.classList.contains('closed') ? '1' : '0'); } catch (e) { /* storage off */ }
        });
        const grid = document.createElement('div');
        grid.className = 'gpe-pal-grid';
        for (const it of g.items) { it.el = tile(it); grid.appendChild(it.el); }
        sec.append(h, grid);
        g.el = sec;
        rail.appendChild(sec);

        const tab = document.createElement('button');
        tab.type = 'button';
        tab.setAttribute('role', 'tab');
        tab.innerHTML = GPUI.icon(g.icon) + `<span>${g.name}</span>`;
        tab.addEventListener('click', () => { rail.dataset.group = g.id; syncTabs(); });
        g.tab = tab;
        tabs.appendChild(tab);
      }
      if (!rail.dataset.group) rail.dataset.group = 'note';
      syncTabs();
    }
    function syncTabs() {
      const rail = groups[0].el.parentElement;
      let cur = groups.find((g) => g.id === rail.dataset.group && visible(g));
      if (!cur) { cur = groups.find((g) => visible(g) && g.id !== 'voice'); rail.dataset.group = cur.id; }
      for (const g of groups) {
        g.el.classList.toggle('active', g === cur);
        g.tab.setAttribute('aria-selected', g === cur ? 'true' : 'false');
      }
    }
    // The folded rail: a group opens beside its icon.
    function flyout(g, anchor) {
      GPUI.popover(anchor, (el) => {
        const grid = document.createElement('div');
        grid.className = 'gpe-pal-grid';
        for (const it of g.items) {
          if (!itemVisible(it)) continue;
          const b = tile(it);
          refreshTile(it, b);
          grid.appendChild(b);
        }
        el.appendChild(grid);
      }, { title: g.name, className: 'gpe-pop-flyout' });
    }

    function refreshTile(it, el) {
      let on = false;
      try { on = it.on ? !!it.on() : false; } catch (e) { on = false; }
      el.setAttribute('aria-pressed', on ? 'true' : 'false');
      el.classList.toggle('current', !!(it.current && it.current()));
      el.hidden = !itemVisible(it);
    }
    function refresh() {
      for (const g of groups) {
        const show = visible(g);
        g.el.hidden = !show;
        g.tab.hidden = !show;
      }
      syncTabs();
      for (const it of all) if (it.el) refreshTile(it, it.el);
    }
    function onKey(e) {
      for (const it of all) {
        if (!it.keys || !itemVisible(it) || !it.keys.some((k) => matches(k, e))) continue;
        e.preventDefault();
        runItem(it, null);
        return true;
      }
      return false;
    }
    // Rows for the keyboard help: shortcut and name.
    const shortcuts = () => all.filter((it) => it.keys && it.keys.length && !it.kit).map((it) => [keyLabel(it.keys[0]), it.name]);
    return { build, refresh, onKey, shortcuts };
  };
})();

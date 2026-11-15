// The editing palette of the Guitar Pro file editor (gp-editor.js), laid
// out like Guitar Pro 8's: a column down the left of the score, in its
// groups and their order (Voice, Lyrics, Bar, Note, Effect, Notation,
// Automation symbols — guitar-pro.com/docs/gp8/score/note/symbols), with
// its keyboard shortcuts. Every button changes alphaTab's JSON form of the
// file through the editor (ed.edit), so it is undoable and saved like any
// other edit. Glyphs come from Bravura, the SMuFL music font alphaTab
// ships.
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
  function keyLabel(spec) {
    if (!spec) return '';
    if (spec.key) return (spec.shift ? '⇧' : '') + (spec.key.length === 1 ? spec.key.toUpperCase() : spec.key);
    const k = spec.code.replace(/^Key|^Digit/, '').replace('Period', '.').replace('Slash', '/').replace('Digit5', '%').replace('Comma', '<').replace('ArrowUp', '↑').replace('ArrowDown', '↓');
    return (spec.mod ? (isMac ? '⌘' : 'Ctrl+') : '') + (spec.alt ? (isMac ? '⌥' : 'Alt+') : '') + (spec.shift ? (isMac ? '⇧' : 'Shift+') : '') + k;
  }

  window.GPPalette = function (ed) {
    const { st } = ed;
    const beat = () => ed.beatJ();
    const note = () => ed.curNote();
    const mb = () => ed.masterJ();
    const bar = () => ed.barJ();

    // Toggles on the beat, or on the note under the cursor.
    const onBeat = (field, value, off) => ({
      on: () => { const b = beat(); return !!b && b[field] === value; },
      run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); if (!b) return false; b[field] = b[field] === value ? off : value; }),
    });
    const onNote = (field, value, off) => ({
      on: () => { const n = note(); return !!n && n[field] === value; },
      run: () => ed.edit(() => { const n = note(); if (!n) return false; n[field] = n[field] === value ? off : value; }),
      needsNote: true,
    });
    // A value from the cursor on, kept until the next change (dynamics,
    // triplet feel, clef, key): applied to every following beat or bar
    // that still had the old one.
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

    // A note's pitch class and the letter it's written as, for accidentals.
    const LETTER_PC = [0, 2, 4, 5, 7, 9, 11];
    function pitchOf(n) { return ed.tuning()[ed.numStrings() - n.string] + n.fret; }
    function setPitch(n, midi) {
      const fret = midi - ed.tuning()[ed.numStrings() - n.string];
      if (fret < 0 || fret > 24) throw new Error('That note is off this string.');
      n.fret = fret;
    }
    // Writes the note with accidental acc (-2..2) on its letter: the pitch
    // follows, as Guitar Pro's accidental buttons do.
    function accidental(acc, mode) {
      return {
        on: () => { const n = note(); return !!n && n.accidentalmode === mode; },
        needsNote: true,
        run: () => ed.edit(() => {
          const n = note();
          if (!n) return false;
          const midi = pitchOf(n);
          const pc = ((midi % 12) + 12) % 12;
          // The letter it's written on now: a sharp spelling unless forced flat.
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

    function promptNumber(msg, cur, lo, hi) {
      const v = prompt(msg, cur);
      if (v === null || v.trim() === '') return null;
      const n = Number(v);
      if (!(n >= lo && n <= hi)) throw new Error(`Between ${lo} and ${hi}, please.`);
      return n;
    }

    const groups = [
      {
        name: 'Voice', items: [0, 1, 2, 3].map((v) => ({
          id: 'voice' + v, text: String(v + 1), name: `Voice ${v + 1}`,
          on: () => st.voice === v, run: () => ed.setVoice(v),
        })),
      },
      {
        name: 'Lyrics', items: [{
          id: 'lyrics', text: 'Ly', name: 'Lyrics: the syllable sung on this beat',
          on: () => { const b = beat(); return !!(b && b.lyrics && b.lyrics.length && b.lyrics[0]); },
          run: () => { const b = beat(); const v = prompt('Syllable sung on this beat (empty: none):', (b && b.lyrics && b.lyrics[0]) || ''); if (v === null) return; ed.edit(() => { ed.ensureVoice(); beat().lyrics = v.trim() ? [v.trim()] : null; }); },
        }],
      },
      {
        name: 'Bar', items: [
          { id: 'clef', glyph: G(0xE050), name: 'Clef: treble, bass, alto, tenor, none', run: () => ed.edit(() => {
            const order = [4, 3, 1, 2, 0];
            const next = order[(order.indexOf(bar().clef) + 1) % order.length];
            fromHere(ed.staffJ().bars, st.bar, (x) => x.clef, (x, v) => { x.clef = v; }, next);
          }) },
          { id: 'key', glyph: G([0xE262, 0xE260]), name: 'Key signature', keys: [{ code: 'KeyK', mod: true }], run: () => ed.askKey() },
          { id: 'time', text: '4/4', name: 'Time signature', keys: [{ code: 'KeyT', mod: true }], run: () => ed.askTimeSignature() },
          { id: 'feel', text: '♪♪=♪³', name: 'Rhythmic interpretation (triplet feel): straight, swung 8ths, swung 16ths', keys: [{ code: 'Slash', mod: true }],
            on: () => !!mb().tripletfeel,
            run: () => ed.edit(() => { const order = [0, 2, 1]; const next = order[(order.indexOf(mb().tripletfeel || 0) + 1) % 3]; fromHere(ed.st.doc.masterbars, st.bar, (x) => x.tripletfeel || 0, (x, v) => { x.tripletfeel = v; }, next); }) },
          { id: 'free', text: 'free', name: 'Free time', keys: [{ key: '|' }], on: () => !!mb().isfreetime, run: () => ed.edit(() => { mb().isfreetime = !mb().isfreetime; }) },
          { id: 'double', text: '‖', name: 'Double bar line', on: () => !!mb().isdoublebar, run: () => ed.edit(() => { mb().isdoublebar = !mb().isdoublebar; }) },
          { id: 'simile1', glyph: G(0xE500), name: 'Repeat the previous bar', keys: [{ key: '%' }], on: () => bar().similemark === 1,
            run: () => ed.edit(() => { bar().similemark = bar().similemark === 1 ? 0 : 1; }) },
          { id: 'simile2', glyph: G(0xE501), name: 'Repeat the previous two bars', keys: [{ code: 'Digit5', mod: true, shift: true }], on: () => bar().similemark === 2,
            run: () => ed.edit(() => {
              const bars = ed.staffJ().bars;
              if (st.bar + 1 >= bars.length) throw new Error('Needs a bar after this one.');
              const on = bar().similemark === 2;
              bars[st.bar].similemark = on ? 0 : 2;
              bars[st.bar + 1].similemark = on ? 0 : 3;
            }) },
          { id: 'ropen', glyph: G(0xE040), name: 'Repeat start', keys: [{ key: '[' }], on: () => !!mb().isrepeatstart, run: () => ed.edit(() => { mb().isrepeatstart = !mb().isrepeatstart; }) },
          { id: 'rclose', glyph: G(0xE041), name: 'Repeat close', keys: [{ key: ']' }], on: () => !!mb().repeatcount, run: () => ed.askRepeatEnd() },
          { id: 'ending', text: '1.', name: 'Alternate endings', on: () => !!mb().alternateendings, run: () => ed.askEnding() },
          { id: 'directions', glyph: G([0xE047, 0xE048]), name: 'Directions: segno, coda, fine, D.C., D.S.…', keys: [{ key: 'd' }], on: () => !!(mb().directions && mb().directions.length), run: () => askDirections() },
          ...[['8va', 1], ['8vb', 3], ['15ma', 0], ['15mb', 4]].map(([label, v]) => ({
            id: label, text: label, name: `${label}: the bar played ${label.startsWith('8') ? 'an octave' : 'two octaves'} ${label.endsWith('a') ? 'higher' : 'lower'}`,
            on: () => { const b = beat(); return !!b && b.ottava === v; },
            run: () => ed.edit(() => { const v0 = ed.ensureVoice(); const on = beat() && beat().ottava === v; for (const b of v0.beats) b.ottava = on ? 2 : v; }),
          })),
        ],
      },
      {
        name: 'Note', items: [
          ...[[1, 0xE1D2], [2, 0xE1D3], [4, 0xE1D5], [8, 0xE1D7], [16, 0xE1D9], [32, 0xE1DB], [64, 0xE1DD]].map(([d, cp]) => ({
            id: 'dur' + d, glyph: G(cp), name: { 1: 'Whole note', 2: 'Half note', 4: 'Quarter note', 8: 'Eighth note', 16: 'Sixteenth note', 32: 'Thirty-second note', 64: 'Sixty-fourth note' }[d],
            on: () => { const b = beat(); return !!b && b.duration === d; }, run: () => ed.setDuration(d),
          })),
          { id: 'rest', glyph: G(0xE4E5), name: 'Rest', keys: [{ key: 'r' }], on: () => { const b = beat(); return !!b && !b.notes.length; },
            run: () => ed.edit(() => { const b = beat(); if (!b || !b.notes.length) return false; b.notes = []; }) },
          { id: 'dot', glyph: G(0xE1E7), name: 'Dotted note', keys: [{ key: '.' }], on: () => { const b = beat(); return !!b && b.dots === 1; },
            run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); b.dots = b.dots === 1 ? 0 : 1; }) },
          { id: 'ddot', glyph: G([0xE1E7, 0xE1E7]), name: 'Double-dotted note', keys: [{ code: 'Period', mod: true }], on: () => { const b = beat(); return !!b && b.dots === 2; },
            run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); b.dots = b.dots === 2 ? 0 : 2; }) },
          { id: 'triplet', text: '3', name: 'Triplet', keys: [{ key: '/' }], on: () => { const b = beat(); return !!b && b.tupletnumerator === 3 && b.tupletdenominator === 2; },
            run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); const on = b.tupletnumerator === 3 && b.tupletdenominator === 2; b.tupletnumerator = on ? -1 : 3; b.tupletdenominator = on ? -1 : 2; }) },
          { id: 'tuplet', text: 'n:m', name: 'Custom n-tuplet (5:4, 6:4, 7:8…)', on: () => { const b = beat(); return !!b && b.tupletnumerator > 0 && !(b.tupletnumerator === 3 && b.tupletdenominator === 2); },
            run: () => {
              const b = beat();
              const v = prompt('Tuplet, notes:in the time of (like 5:4; empty: none):', b && b.tupletnumerator > 0 ? `${b.tupletnumerator}:${b.tupletdenominator}` : '5:4');
              if (v === null) return;
              const m = v.trim().match(/^(\d{1,2})\s*:\s*(\d{1,2})$/);
              ed.edit(() => { ed.ensureVoice(); const x = beat(); x.tupletnumerator = m ? +m[1] : -1; x.tupletdenominator = m ? +m[2] : -1; });
            } },
          { id: 'tie', text: '⁀', name: 'Tie', keys: [{ key: 'l' }], ...onNote('istiedestination', true, false) },
          { id: 'ties', text: '⁀⁀', name: 'Tie every note of the beat', keys: [{ key: 'l', shift: true }], on: () => { const b = beat(); return !!b && b.notes.length > 0 && b.notes.every((n) => n.istiedestination); },
            run: () => ed.edit(() => { const b = beat(); if (!b || !b.notes.length) return false; const on = b.notes.every((n) => n.istiedestination); for (const n of b.notes) n.istiedestination = !on; }) },
          { id: 'fermata', glyph: G(0xE4C0), name: 'Fermata', keys: [{ key: 'f' }], on: () => !!fermataAt(),
            run: () => ed.edit(() => { const m = mb(); const at = String(ed.beatM() ? ed.beatM().playbackStart : 0); m.fermata = m.fermata || {}; if (m.fermata[at]) delete m.fermata[at]; else m.fermata[at] = { type: 1, length: 1 }; }) },
          { id: 'dflat', glyph: G(0xE264), name: 'Double flat', keys: [{ code: 'Digit7', mod: true, alt: true }], ...accidental(-2, 6) },
          { id: 'flat', glyph: G(0xE260), name: 'Flat', keys: [{ code: 'Digit7', mod: true, alt: false }], ...accidental(-1, 5) },
          { id: 'natural', glyph: G(0xE261), name: 'Natural', keys: [{ code: 'Digit8', mod: true, alt: false }], ...accidental(0, 2) },
          { id: 'sharp', glyph: G(0xE262), name: 'Sharp', keys: [{ code: 'Digit9', mod: true, alt: false }], ...accidental(1, 3) },
          { id: 'dsharp', glyph: G(0xE263), name: 'Double sharp', keys: [{ code: 'Digit9', mod: true, alt: true }], ...accidental(2, 4) },
          { id: 'enh', glyph: G([0xE262, 0xE260]), name: 'Change accidental: the same pitch spelled the other way', keys: [{ code: 'Digit8', mod: true, alt: true }], needsNote: true,
            run: () => ed.edit(() => { const n = note(); if (!n) return false; n.accidentalmode = n.accidentalmode === 5 ? 3 : 5; }) },
          { id: 'down', text: '−½', name: 'Lower by a semitone', keys: [{ code: 'ArrowDown', alt: true, shift: true }], needsNote: true,
            run: () => ed.edit(() => { const n = note(); if (!n) return false; setPitch(n, pitchOf(n) - 1); }) },
          { id: 'up', text: '+½', name: 'Raise by a semitone', keys: [{ code: 'ArrowUp', alt: true, shift: true }], needsNote: true,
            run: () => ed.edit(() => { const n = note(); if (!n) return false; setPitch(n, pitchOf(n) + 1); }) },
          ...['ppp', 'pp', 'p', 'mp', 'mf', 'f', 'ff', 'fff'].map((d, v) => ({
            id: 'dyn' + d, glyph: dyn(d), name: { ppp: 'Pianississimo', pp: 'Pianissimo', p: 'Piano', mp: 'Mezzo piano', mf: 'Mezzo forte', f: 'Forte', ff: 'Fortissimo', fff: 'Fortississimo' }[d] + ', from here on',
            on: () => { const b = beat(); return !!b && b.notes.length > 0 && b.dynamics === v; },
            run: () => ed.edit(() => { ed.ensureVoice(); const list = beatsFromCursor(); fromHere(list, 0, (x) => x.dynamics, (x, val) => { x.dynamics = val; }, v); }),
          })),
          { id: 'cresc', text: '<', name: 'Crescendo', ...onBeat('crescendo', 1, 0) },
          { id: 'decresc', text: '>', name: 'Decrescendo', ...onBeat('crescendo', 2, 0) },
        ],
      },
      {
        name: 'Effect', items: [
          { id: 'ghost', text: '( )', name: 'Ghost note', keys: [{ key: 'o' }], ...onNote('isghost', true, false) },
          { id: 'accent', glyph: G(0xE4A0), name: 'Accented note', keys: [{ key: ';' }], ...onNote('accentuated', 1, 0) },
          { id: 'heavy', glyph: G(0xE4AC), name: 'Heavily accented note', keys: [{ key: ':' }], ...onNote('accentuated', 2, 0) },
          { id: 'staccato', glyph: G(0xE4A2), name: 'Staccato', keys: [{ key: '!' }], ...onNote('isstaccato', true, false) },
          { id: 'legato', text: '⌒', name: 'Legato', keys: [{ key: 'h', shift: true }], ...onBeat('islegatoorigin', true, false) },
          { id: 'letring', text: 'L.R.', name: 'Let ring', keys: [{ key: 'i' }], ...onNote('isletring', true, false) },
          { id: 'palm', text: 'P.M.', name: 'Palm mute', keys: [{ key: 'p' }], ...onNote('ispalmmute', true, false) },
          { id: 'dead', text: '×', name: 'Dead note', keys: [{ key: 'x' }], ...onNote('isdead', true, false) },
          { id: 'nharm', text: '◇', name: 'Natural harmonic', keys: [{ key: 'y' }], needsNote: true,
            on: () => { const n = note(); return !!n && n.harmonictype === 1; },
            run: () => ed.edit(() => { const n = note(); if (!n) return false; const on = n.harmonictype === 1; n.harmonictype = on ? 0 : 1; n.harmonicvalue = on ? 0 : n.fret; }) },
          { id: 'aharm', text: 'A.H.', name: 'Artificial harmonic: artificial, pinch, tapped, semi, off', keys: [{ code: 'KeyY', alt: true }], needsNote: true,
            on: () => { const n = note(); return !!n && n.harmonictype >= 2; },
            run: () => ed.edit(() => { const n = note(); if (!n) return false; const order = [0, 2, 3, 4, 5]; const next = order[(order.indexOf(n.harmonictype >= 2 ? n.harmonictype : 0) + 1) % order.length]; n.harmonictype = next; n.harmonicvalue = next ? n.fret + 12 : 0; }) },
          { id: 'bend', text: '⤴', name: 'Bend: full, ½, ¼, 1½, off', keys: [{ key: 'b' }], needsNote: true,
            on: () => { const n = note(); return !!n && !!n.bendtype; },
            run: () => ed.edit(() => {
              const n = note(); if (!n) return false;
              const top = n.bendpoints && n.bendpoints.length ? Math.max(...n.bendpoints.map((p) => p.value)) : 0;
              const order = [0, 4, 2, 1, 6];
              const next = order[(order.indexOf(n.bendtype ? top : 0) + 1) % order.length];
              if (!next) { n.bendtype = 0; n.bendpoints = []; return; }
              n.bendtype = 2;
              n.bendpoints = [{ offset: 0, value: 0 }, { offset: 15, value: next }, { offset: 60, value: next }];
            }) },
          { id: 'fadein', text: '◁', name: 'Fade-in', keys: [{ key: '<' }], ...onBeat('fade', 1, 0) },
          { id: 'fadeout', text: '▷', name: 'Fade-out', keys: [{ key: '>' }], ...onBeat('fade', 2, 0) },
          { id: 'swell', text: '◇', name: 'Volume swell', keys: [{ code: 'Comma', alt: true }], ...onBeat('fade', 3, 0) },
          { id: 'gbefore', text: 'gr.', name: 'Grace note before the beat', keys: [{ key: 'g' }], ...onBeat('gracetype', 2, 0) },
          { id: 'gon', text: 'gr.↓', name: 'Grace note on the beat', keys: [{ code: 'KeyG', alt: true }], ...onBeat('gracetype', 1, 0) },
          { id: 'hammer', text: 'H/P', name: 'Hammer-on / pull-off', keys: [{ key: 'h' }], ...onNote('ishammerpullorigin', true, false) },
          { id: 'tremolo', glyph: G(0xE222), name: 'Tremolo picking', keys: [{ key: '"' }],
            on: () => { const b = beat(); return !!(b && b.tremolopicking); },
            run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); b.tremolopicking = b.tremolopicking ? null : { marks: 3, style: 0 }; }) },
          { id: 'umordent', glyph: G(0xE56C), name: 'Upper mordent', ...onNote('ornament', 3, 0) },
          { id: 'lmordent', glyph: G(0xE56D), name: 'Lower mordent', ...onNote('ornament', 4, 0) },
          { id: 'turn', glyph: G(0xE567), name: 'Turn', ...onNote('ornament', 2, 0) },
          { id: 'iturn', glyph: G(0xE568), name: 'Inverted turn', ...onNote('ornament', 1, 0) },
          { id: 'lhtap', text: '(T)', name: 'Left-hand tapping', keys: [{ key: '(' }], ...onNote('islefthandtapped', true, false) },
          { id: 'lslide', text: '⟋', name: 'Legato slide', keys: [{ key: 's' }], ...onNote('slideouttype', 2, 0) },
          { id: 'sslide', text: '⟋s', name: 'Shift slide', ...onNote('slideouttype', 1, 0) },
          { id: 'inbelow', text: '↗•', name: 'Slide in from below', ...onNote('slideintype', 1, 0) },
          { id: 'inabove', text: '↘•', name: 'Slide in from above', ...onNote('slideintype', 2, 0) },
          { id: 'outdown', text: '•↘', name: 'Slide out downwards', ...onNote('slideouttype', 4, 0) },
          { id: 'outup', text: '•↗', name: 'Slide out upwards', ...onNote('slideouttype', 3, 0) },
          { id: 'whammy', text: 'w/bar', name: 'Whammy bar dip', keys: [{ code: 'KeyV', alt: true }],
            on: () => { const b = beat(); return !!(b && b.whammybartype); },
            run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); if (b.whammybartype) { b.whammybartype = 0; b.whammybarpoints = []; } else { b.whammybartype = 3; b.whammybarpoints = [{ offset: 0, value: 0 }, { offset: 30, value: -4 }, { offset: 60, value: 0 }]; } }) },
          { id: 'trill', glyph: G(0xE566), name: 'Trill', keys: [{ key: 'n' }], needsNote: true,
            on: () => { const n = note(); return !!n && n.trillvalue >= 0 && n.trillvalue !== undefined && n.trillvalue !== -1; },
            run: () => {
              const n = note(); if (!n) return;
              if (n.trillvalue >= 0) { ed.edit(() => { note().trillvalue = -1; }); return; }
              const f = promptNumber('Trill with which fret?', n.fret + 2, 0, 24);
              if (f === null) return;
              ed.edit(() => { const x = note(); x.trillvalue = ed.tuning()[ed.numStrings() - x.string] + f; x.trillspeed = 16; });
            } },
          { id: 'vib', text: '∿', name: 'Slight left-hand vibrato', keys: [{ key: 'v' }], ...onNote('vibrato', 1, 0) },
          { id: 'wvib', text: '≈', name: 'Wide left-hand vibrato', ...onNote('vibrato', 2, 0) },
          { id: 'bvib', text: 'w∿', name: 'Slight bridge vibrato', keys: [{ key: 'w' }], ...onBeat('vibrato', 1, 0) },
          { id: 'wbvib', text: 'w≈', name: 'Wide bridge vibrato', keys: [{ code: 'KeyW', alt: true }], ...onBeat('vibrato', 2, 0) },
          { id: 'wahc', text: '+', name: 'Closed wah-wah', keys: [{ code: 'KeyC', alt: true }], ...onBeat('wahpedal', 2, 0) },
          { id: 'waho', text: 'o', name: 'Open wah-wah', keys: [{ code: 'KeyO', alt: true }], ...onBeat('wahpedal', 1, 0) },
          { id: 'arpdown', glyph: G(0xE635), name: 'Downward arpeggio', keys: [{ code: 'KeyD', mod: true, shift: true }], ...onBeat('brushtype', 4, 0) },
          { id: 'arpup', glyph: G(0xE634), name: 'Upward arpeggio', keys: [{ code: 'KeyU', mod: true, shift: true }], ...onBeat('brushtype', 3, 0) },
          { id: 'brushdown', text: '↓', name: 'Downward brush', keys: [{ code: 'KeyD', mod: true, shift: false }], ...onBeat('brushtype', 2, 0) },
          { id: 'brushup', text: '↑', name: 'Upward brush', keys: [{ code: 'KeyU', mod: true, shift: false }], ...onBeat('brushtype', 1, 0) },
          { id: 'rasg', text: 'rasg.', name: 'Rasgueado', keys: [{ key: 'r', shift: true }], ...onBeat('rasgueado', 1, 0) },
          { id: 'lhf', text: 'LH', name: 'Left-hand fingering (T 1 2 3 4)', needsNote: true, on: () => { const n = note(); return !!n && n.lefthandfinger >= 0; },
            run: () => fingering('lefthandfinger', 'Left-hand finger: T, 1, 2, 3, 4 (empty: none)', ['t', '1', '2', '3', '4']) },
          { id: 'rhf', text: 'RH', name: 'Right-hand fingering (p i m a c)', needsNote: true, on: () => { const n = note(); return !!n && n.righthandfinger >= 0; },
            run: () => fingering('righthandfinger', 'Right-hand finger: p, i, m, a, c (empty: none)', ['p', 'i', 'm', 'a', 'c']) },
          { id: 'stringnum', text: '③', name: 'String number', ...onNote('showstringnumber', true, false) },
          { id: 'pickdown', glyph: G(0xE610), name: 'Downward pickstroke', keys: [{ key: 'd', shift: true }], ...onBeat('pickstroke', 2, 0) },
          { id: 'pickup', glyph: G(0xE612), name: 'Upward pickstroke', keys: [{ key: 'u', shift: true }], ...onBeat('pickstroke', 1, 0) },
          { id: 'tap', text: 'T', name: 'Tapping', keys: [{ key: ')' }], ...onBeat('tap', true, false) },
          { id: 'slap', text: 'S', name: 'Slap', keys: [{ key: '$' }], ...onBeat('slap', true, false) },
          { id: 'pop', text: 'P', name: 'Pop', ...onBeat('pop', true, false) },
          { id: 'fgolpe', text: 'g.f', name: 'Finger golpe', ...onBeat('golpe', 2, 0) },
          { id: 'tgolpe', text: 'g.t', name: 'Thumb golpe', ...onBeat('golpe', 1, 0) },
          { id: 'deadslap', text: '×S', name: 'Dead slap', ...onBeat('deadslapped', true, false) },
        ],
      },
      {
        name: 'Notation', items: [
          { id: 'chord', text: 'C7', name: 'Chord name on this beat', keys: [{ key: 'a' }], on: () => { const b = beat(); return !!b && b.chordid != null; }, run: () => askChord() },
          { id: 'slash', text: '⁄', name: 'Slash notation', ...onBeat('slashed', true, false) },
          { id: 'barre', text: 'C.V', name: 'Barre', keys: [{ key: 'i', shift: true }], on: () => { const b = beat(); return !!b && b.barrefret > 0; },
            run: () => {
              const b = beat();
              const v = prompt('Barre at fret (add ½ for a half barre, like "5½"; empty: none):', b && b.barrefret > 0 ? b.barrefret + (b.barreshape === 2 ? '½' : '') : '');
              if (v === null) return;
              const m = v.trim().match(/^(\d{1,2})(½|\/2)?$/);
              ed.edit(() => { ed.ensureVoice(); const x = beat(); x.barrefret = m ? +m[1] : -1; x.barreshape = m ? (m[2] ? 2 : 1) : 0; });
            } },
          { id: 'timer', text: '⏱', name: 'Timer', keys: [{ key: '@' }], ...onBeat('showtimer', true, false) },
          { id: 'text', text: 'Txt', name: 'Text', keys: [{ key: 't' }], on: () => { const b = beat(); return !!(b && b.text); },
            run: () => { const b = beat(); const v = prompt('Text over this beat (empty: none):', (b && b.text) || ''); if (v === null) return; ed.edit(() => { ed.ensureVoice(); beat().text = v.trim() || null; }); } },
          { id: 'section', text: '[A]', name: 'Section', on: () => !!(mb().section && mb().section.text), run: () => ed.askSection() },
          { id: 'stemauto', text: 'stem', name: 'Automatic stem direction', on: () => { const b = beat(); return !!b && b.preferredbeamdirection == null; },
            run: () => ed.edit(() => { ed.ensureVoice(); beat().preferredbeamdirection = null; }) },
          { id: 'steminv', text: 'stem⇅', name: 'Inverted stem direction', on: () => { const b = beat(); return !!b && b.preferredbeamdirection != null; },
            run: () => ed.edit(() => { ed.ensureVoice(); const b = beat(); b.preferredbeamdirection = b.preferredbeamdirection === 1 ? 0 : 1; }) },
          { id: 'beamauto', text: '♫', name: 'Automatic beaming', ...onBeat('beamingmode', 0, 0) },
          { id: 'beamforce', text: '♫+', name: 'Force beaming with the next note', ...onBeat('beamingmode', 2, 0) },
          { id: 'beambreak', text: '♪|♪', name: 'Break beaming', ...onBeat('beamingmode', 1, 0) },
          { id: 'beambreak2', text: '♬|', name: 'Break secondary beaming', ...onBeat('beamingmode', 3, 0) },
        ],
      },
      {
        name: 'Automation', items: [
          { id: 'tempo', text: '♩=', name: 'Tempo from this bar on', on: () => !!(mb().tempoautomations && mb().tempoautomations.length), run: () => ed.askTempo() },
          { id: 'volume', text: 'Vol', name: 'Track volume from this beat on (0–16)', on: () => !!automation(1), run: () => askAutomation(1, 'Track volume from here, 0 to 16 (empty: none here):', 0, 16, (v) => v) },
          { id: 'pan', text: 'Pan', name: 'Track pan from this beat on (−8 left … 8 right)', on: () => !!automation(3), run: () => askAutomation(3, 'Pan from here, −8 (left) to 8 (right) (empty: none here):', -8, 8, (v) => v + 8) },
        ],
      },
    ];

    function fermataAt() {
      const m = mb();
      const b = ed.beatM();
      return m.fermata && b && m.fermata[String(b.playbackStart)];
    }
    function automation(type) {
      const b = beat();
      return b && (b.automations || []).find((a) => a.type === type);
    }
    function askAutomation(type, msg, lo, hi, store) {
      const cur = automation(type);
      const v = promptNumber(msg, cur ? (type === 3 ? cur.value - 8 : cur.value) : '', lo, hi);
      ed.edit(() => {
        ed.ensureVoice();
        const b = beat();
        b.automations = (b.automations || []).filter((a) => a.type !== type);
        if (v !== null) b.automations.push({ islinear: false, type, value: store(v), ratioposition: 0, text: '', isvisible: true });
      });
    }
    function fingering(field, msg, names) {
      const n = note();
      const v = prompt(msg, n && n[field] >= 0 ? names[n[field]] : '');
      if (v === null) return;
      const i = names.indexOf(v.trim().toLowerCase());
      ed.edit(() => { note()[field] = i >= 0 ? i : -1; });
    }
    const DIRECTIONS = ['Fine', 'Segno', 'Segno Segno', 'Coda', 'Double Coda', 'Da Capo', 'Da Capo al Coda', 'Da Capo al Double Coda', 'Da Capo al Fine', 'Dal Segno', 'Dal Segno al Coda', 'Dal Segno al Double Coda', 'Dal Segno al Fine', 'Dal Segno Segno', 'Dal Segno Segno al Coda', 'Dal Segno Segno al Double Coda', 'Dal Segno Segno al Fine', 'Da Coda', 'Da Double Coda'];
    function askDirections() {
      const cur = (mb().directions || []).map((d) => DIRECTIONS[d]).join(', ');
      const v = prompt('Directions on this bar, comma-separated (empty: none):\n' + DIRECTIONS.join(' · '), cur);
      if (v === null) return;
      const want = v.split(',').map((x) => x.trim().toLowerCase()).filter(Boolean);
      const ids = want.map((w) => DIRECTIONS.findIndex((d) => d.toLowerCase() === w));
      if (ids.some((i) => i < 0)) { ed.setStatus('Unknown direction: ' + want[ids.findIndex((i) => i < 0)], true); return; }
      ed.edit(() => { mb().directions = ids; });
    }
    function askChord() {
      const b = beat();
      const chords = ed.staffJ().chords || {};
      const cur = b && b.chordid != null && chords[b.chordid] ? chords[b.chordid].name : '';
      const v = prompt('Chord name on this beat (empty: none):', cur);
      if (v === null) return;
      ed.edit(() => {
        ed.ensureVoice();
        const x = beat();
        const name = v.trim();
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

    // ---- The column ----------------------------------------------------------
    const all = groups.flatMap((g) => g.items);
    function build(host) {
      host.innerHTML = '';
      for (const g of groups) {
        const sec = document.createElement('details');
        sec.className = 'gpe-pal-group';
        sec.open = true;
        try { if (localStorage.getItem('gpe-pal-closed:' + g.name) === '1') sec.open = false; } catch (e) { /* storage off */ }
        sec.addEventListener('toggle', () => { try { localStorage.setItem('gpe-pal-closed:' + g.name, sec.open ? '0' : '1'); } catch (e) { /* storage off */ } });
        const sum = document.createElement('summary');
        sum.textContent = g.name;
        sec.appendChild(sum);
        const grid = document.createElement('div');
        grid.className = 'gpe-pal-grid';
        for (const it of g.items) {
          const b = document.createElement('button');
          b.type = 'button';
          b.dataset.id = it.id;
          if (it.glyph) { b.textContent = it.glyph; b.classList.add('smufl'); } else b.textContent = it.text;
          const k = it.keys && it.keys[0] ? ` (${keyLabel(it.keys[0])})` : '';
          b.title = it.name + k;
          b.setAttribute('aria-label', it.name);
          b.addEventListener('click', () => { try { it.run(); } catch (e) { ed.setStatus(e.message, true); } ed.focus(); });
          it.el = b;
          grid.appendChild(b);
        }
        sec.appendChild(grid);
        host.appendChild(sec);
      }
    }
    function refresh() {
      const hasNote = !!note();
      for (const it of all) {
        if (!it.el) continue;
        let on = false;
        try { on = it.on ? !!it.on() : false; } catch (e) { on = false; }
        it.el.setAttribute('aria-pressed', on ? 'true' : 'false');
        it.el.disabled = !!it.needsNote && !hasNote;
      }
    }
    function onKey(e) {
      for (const it of all) {
        if (!it.keys || !it.keys.some((k) => matches(k, e))) continue;
        if (it.needsNote && !note()) return true;
        e.preventDefault();
        try { it.run(); } catch (err) { ed.setStatus(err.message, true); }
        return true;
      }
      return false;
    }
    // Rows for the keyboard help: name and shortcut.
    const shortcuts = () => all.filter((it) => it.keys && it.keys.length).map((it) => [keyLabel(it.keys[0]), it.name]);
    return { build, refresh, onKey, shortcuts };
  };
})();

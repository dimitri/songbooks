// warmup.js — warm-up exercises (docs/coaching.md, "Warm-ups"): a pattern
// the coach writes once, written out by the app in every key of a
// student's path, as a MusicXML score of two parts (the line to sing,
// with its syllables, and the piano) that alphaTab draws and plays; and
// the exact time and pitch of every note to sing, for the checker.
//
// A pattern (exercises.pattern, JSON):
//   tonic      the written key's tonic, "C4"
//   scale      "major", "minor" or "chromatic"
//   notes      scale degrees, "1 2 3 4 5 4 3 2 1" (8: the octave; for
//              chromatic, semitones from the tonic, 0 up); "r" a rest
//   lengths    per note, 1 2 4 8 16 (whole to sixteenth), "4." dotted;
//              fewer than the notes: the last one goes on
//   syllables  sung, cycled over the notes: "ma me mi mo mu", "a"
//   piano      "double" (the line), "chords" (the key's chord), "none"
//   prep       "I", "V7-I" or "none": the piano's chord before each repeat
//   tempo      beats a minute
//   breath     beats of rest after each repeat
//   step       semitones between repeats (1, or 2)
(function () {
  const DIV = 4; // divisions a quarter: a sixteenth is 1
  const SCALES = {
    major: [0, 2, 4, 5, 7, 9, 11],
    minor: [0, 2, 3, 5, 7, 8, 10],
  };
  const NAMES_SHARP = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B'];
  const NAMES_FLAT = ['C', 'Db', 'D', 'Eb', 'E', 'F', 'Gb', 'G', 'Ab', 'A', 'Bb', 'B'];

  // "C4" → 60, "Bb3" → 58.
  function midiOf(name) {
    const m = /^([A-Ga-g])([#b]?)(-?\d)$/.exec(String(name || '').trim());
    if (!m) return 60;
    const base = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 }[m[1].toUpperCase()];
    return 12 * (Number(m[3]) + 1) + base + (m[2] === '#' ? 1 : m[2] === 'b' ? -1 : 0);
  }
  const flatKey = (pc) => [1, 3, 5, 6, 8, 10].includes(pc);
  function nameOf(midi, flats) {
    const pc = ((midi % 12) + 12) % 12;
    return (flats ? NAMES_FLAT : NAMES_SHARP)[pc] + (Math.floor(midi / 12) - 1);
  }

  // A degree to semitones from the tonic.
  function semis(deg, scale) {
    if (scale === 'chromatic') return deg;
    const s = SCALES[scale] || SCALES.major;
    const d = deg - 1;
    return 12 * Math.floor(d / 7) + s[((d % 7) + 7) % 7];
  }

  // A length token to divisions: "8" an eighth (2), "4." a dotted quarter (6).
  function divsOf(tok) {
    const m = /^(1|2|4|8|16)(\.?)$/.exec(String(tok).trim());
    if (!m) return DIV;
    const d = (4 * DIV) / Number(m[1]);
    return m[2] ? d * 1.5 : d;
  }

  // The pattern's notes: [{semis | null (a rest), divs, syllable}].
  function parse(p) {
    const toks = String(p.notes || '').trim().split(/\s+/).filter(Boolean);
    const lens = String(p.lengths || '4').trim().split(/\s+/).filter(Boolean);
    const syl = String(p.syllables || '').trim().split(/\s+/).filter(Boolean);
    let si = 0;
    return toks.map((tk, i) => {
      const divs = divsOf(lens[Math.min(i, lens.length - 1)] || '4');
      if (/^r$/i.test(tk)) return { semis: null, divs, syllable: '' };
      const n = Number(tk);
      return { semis: Number.isFinite(n) ? semis(n, p.scale) : 0, divs, syllable: syl.length ? syl[si++ % syl.length] : '' };
    });
  }

  // The lowest and highest notes, in semitones from the tonic.
  function span(p) {
    const s = parse(p).filter((n) => n.semis !== null).map((n) => n.semis);
    return s.length ? [Math.min(...s), Math.max(...s)] : [0, 0];
  }

  // Waypoints to keys, a step at a time: [0, 4, 0] → 0 1 2 3 4 3 2 1 0.
  function expandPath(waypoints, step) {
    const w = (waypoints && waypoints.length ? waypoints : [0]).map(Number);
    step = Math.max(1, Math.min(2, Number(step) || 1));
    const out = [w[0]];
    for (let i = 1; i < w.length; i++) {
      let k = out[out.length - 1];
      while (k !== w[i]) {
        k += Math.sign(w[i] - k) * Math.min(step, Math.abs(w[i] - k));
        out.push(k);
      }
    }
    return out;
  }

  // The app's path for a student: the pattern within their range (low,
  // high: MIDI, 0 when not noted), up to its top, then back: at most
  // eight steps up.
  function defaultPath(p, low, high) {
    if (!low || !high) return [0, 4, 0];
    const tonic = midiOf(p.tonic);
    const [lo, hi] = span(p);
    const kLo = low - (tonic + lo), kHi = high - (tonic + hi);
    if (kHi < kLo) return [Math.round((kLo + kHi) / 2)];
    const start = Math.max(kLo, kHi - 8);
    return start === kHi ? [start] : [start, kHi, start];
  }

  // Divisions to note types, longest first, a dotted one where it fits.
  const TYPES = [[16, 'whole', false], [12, 'half', true], [8, 'half', false], [6, 'quarter', true], [4, 'quarter', false], [3, 'eighth', true], [2, 'eighth', false], [1, '16th', false]];
  function pieces(divs) {
    const out = [];
    while (divs > 0) {
      const t = TYPES.find(([d]) => d <= divs) || TYPES[TYPES.length - 1];
      out.push(t);
      divs -= t[0];
    }
    return out;
  }

  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

  // One part's events ([{midis: [] (a rest), divs, syllable}]) into 4/4
  // measures, a note over a bar line tied across it.
  function measures(events) {
    const BAR = 4 * DIV;
    const bars = [[]];
    let room = BAR;
    for (const ev of events) {
      let left = ev.divs, first = true;
      while (left > 0) {
        const take = Math.min(left, room);
        const split = pieces(take);
        split.forEach(([d, type, dot], i) => {
          const tieIn = !first || i > 0, tieOut = left - take > 0 || i < split.length - 1;
          bars[bars.length - 1].push({ midis: ev.midis, divs: d, type, dot, syllable: first && i === 0 ? ev.syllable : '', tieIn: ev.midis.length && tieIn, tieOut: ev.midis.length && tieOut });
        });
        first = false;
        left -= take;
        room -= take;
        if (room === 0) { bars.push([]); room = BAR; }
      }
    }
    if (bars[bars.length - 1].length === 0) bars.pop();
    else if (room < BAR) bars[bars.length - 1].push(...pieces(room).map(([d, type, dot]) => ({ midis: [], divs: d, type, dot, syllable: '' })));
    return bars;
  }

  function noteXML(n, flats, voiceLine) {
    if (!n.midis.length) return `<note><rest/><duration>${n.divs}</duration><voice>1</voice><type>${n.type}</type>${n.dot ? '<dot/>' : ''}</note>`;
    return n.midis.map((m, i) => {
      const name = nameOf(m, flats);
      const step = name[0], alter = name[1] === '#' ? 1 : name[1] === 'b' ? -1 : 0, oct = Math.floor(m / 12) - 1;
      const ties = (n.tieIn ? '<tie type="stop"/>' : '') + (n.tieOut ? '<tie type="start"/>' : '');
      const notations = (n.tieIn ? '<tied type="stop"/>' : '') + (n.tieOut ? '<tied type="start"/>' : '');
      return `<note>${i ? '<chord/>' : ''}<pitch><step>${step}</step>${alter ? `<alter>${alter}</alter>` : ''}<octave>${oct}</octave></pitch>`
        + `<duration>${n.divs}</duration>${ties}<voice>1</voice><type>${n.type}</type>${n.dot ? '<dot/>' : ''}`
        + (notations ? `<notations>${notations}</notations>` : '')
        + (voiceLine && i === 0 && n.syllable ? `<lyric number="1"><syllabic>single</syllabic><text>${esc(n.syllable)}</text></lyric>` : '')
        + '</note>';
    }).join('');
  }

  // The score, every repeat in its key: {xml, notes (the line's, in
  // seconds from the start), repeats ({key, start, end}), duration}.
  function build(p, keys, opts) {
    opts = opts || {};
    const tonic = midiOf(p.tonic) + (opts.octave || 0);
    const tempo = Math.max(30, Math.min(240, Number(p.tempo) || 90));
    const sec = (divs) => (divs / DIV) * (60 / tempo);
    const pat = parse(p);
    const minor = p.scale === 'minor';
    const triad = minor ? [0, 3, 7] : [0, 4, 7];
    const v7 = [-5, -1, 2, 5];
    const voice = [], piano = [], notes = [], repeats = [];
    let t = 0;
    const push = (ev, pianoMidis) => { voice.push(ev); piano.push({ midis: pianoMidis, divs: ev.divs, syllable: '' }); t += ev.divs; };
    for (const key of keys) {
      const root = tonic + key;
      const start = t;
      const chordAt = (offs) => offs.map((o) => root - 12 + o);
      if (p.prep === 'V7-I') { push({ midis: [], divs: 2 * DIV }, chordAt(v7)); push({ midis: [], divs: 2 * DIV }, chordAt(triad)); }
      else if (p.prep === 'I') push({ midis: [], divs: 2 * DIV }, chordAt(triad));
      const chord = chordAt(triad);
      for (const n of pat) {
        if (n.semis === null) { push({ midis: [], divs: n.divs }, []); continue; }
        const m = root + n.semis;
        notes.push({ start: sec(t), end: sec(t + n.divs), midi: m, repeat: repeats.length, syllable: n.syllable });
        const pm = p.piano === 'double' ? [m] : p.piano === 'chords' ? chord : [];
        push({ midis: [m], divs: n.divs, syllable: n.syllable }, pm);
      }
      const breath = Math.round(Math.max(0, Number(p.breath) || 0) * DIV);
      if (breath) push({ midis: [], divs: breath }, []);
      // the next repeat starts a bar, with its own key signature
      const BAR = 4 * DIV;
      if (t % BAR) push({ midis: [], divs: BAR - (t % BAR) }, []);
      repeats.push({ key, start: sec(start), end: sec(t), startDivs: start, root, tonic: nameOf(root, flatKey(((root % 12) + 12) % 12)).replace(/-?\d+$/, '') });
    }
    // A repeat's key signature: its tonic's (a minor one, its relative
    // major's), flats or sharps as the tonic is spelled.
    const FIFTHS = { 0: 0, 1: -5, 2: 2, 3: -3, 4: 4, 5: -1, 6: -6, 7: 1, 8: -4, 9: 3, 10: -2, 11: 5 };
    const fifthsOf = (root) => FIFTHS[(((root + (minor ? 3 : 0)) % 12) + 12) % 12];
    const startsAt = new Map(repeats.map((r) => [r.startDivs, r]));
    const partXML = (id, events, isVoice) => {
      const bars = measures(events);
      let at = 0, rep = 0;
      return bars.map((bar, i) => {
        const r = startsAt.get(i * 4 * DIV);
        const fifths = r ? fifthsOf(tonic + r.key) : null;
        const attrs = i === 0 ? `<attributes><divisions>${DIV}</divisions><key><fifths>${fifths || 0}</fifths></key><time><beats>4</beats><beat-type>4</beat-type></time><clef><sign>${isVoice ? 'G' : 'F'}</sign><line>${isVoice ? 2 : 4}</line></clef></attributes>`
          + (isVoice ? `<direction placement="above"><direction-type><metronome><beat-unit>quarter</beat-unit><per-minute>${tempo}</per-minute></metronome></direction-type><sound tempo="${tempo}"/></direction>` : '')
          : r ? `<attributes><key><fifths>${fifths}</fifths></key></attributes>` : '';
        const body = bar.map((n) => {
          while (rep < repeats.length - 1 && sec(at) >= repeats[rep].end - 1e-6) rep++;
          const root = tonic + repeats[rep].key;
          at += n.divs;
          return noteXML(n, flatKey(((root % 12) + 12) % 12), isVoice);
        }).join('');
        return `<measure number="${i + 1}">${attrs}${body}</measure>`;
      }).join('');
    };
    const xml = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1"><work><work-title>${esc(opts.title || '')}</work-title></work>
<part-list>
<score-part id="P1"><part-name>${esc(opts.voiceName || 'Voice')}</part-name><score-instrument id="P1-I1"><instrument-name>Voice</instrument-name></score-instrument><midi-instrument id="P1-I1"><midi-channel>1</midi-channel><midi-program>54</midi-program></midi-instrument></score-part>
<score-part id="P2"><part-name>${esc(opts.pianoName || 'Piano')}</part-name><score-instrument id="P2-I1"><instrument-name>Piano</instrument-name></score-instrument><midi-instrument id="P2-I1"><midi-channel>2</midi-channel><midi-program>1</midi-program></midi-instrument></score-part>
</part-list>
<part id="P1">${partXML('P1', voice, true)}</part>
<part id="P2">${partXML('P2', piano, false)}</part>
</score-partwise>`;
    return { xml, notes, repeats, duration: sec(t), tempo };
  }

  // Exercises to start from.
  const PRESETS = [
    { id: 'five', name: () => t('Five-note scale'), pattern: { tonic: 'C4', scale: 'major', notes: '1 2 3 4 5 4 3 2 1', lengths: '8 8 8 8 8 8 8 8 4', syllables: 'ma', piano: 'double', prep: 'I', tempo: 100, breath: 1, step: 1 } },
    { id: 'arpeggio', name: () => t('Arpeggio 1-3-5-8-5-3-1'), pattern: { tonic: 'C4', scale: 'major', notes: '1 3 5 8 5 3 1', lengths: '8 8 8 4 8 8 4', syllables: 'a', piano: 'chords', prep: 'I', tempo: 90, breath: 1, step: 1 } },
    { id: 'staccato', name: () => t('Staccato arpeggio'), pattern: { tonic: 'C4', scale: 'major', notes: '1 r 3 r 5 r 8 r 5 r 3 r 1', lengths: '16 16 16 16 16 16 16 16 16 16 16 16 4', syllables: 'ha', piano: 'chords', prep: 'I', tempo: 80, breath: 1, step: 1 } },
    { id: 'octave', name: () => t('Octave scale'), pattern: { tonic: 'C4', scale: 'major', notes: '1 2 3 4 5 6 7 8 7 6 5 4 3 2 1', lengths: '8', syllables: 'ou', piano: 'double', prep: 'I', tempo: 110, breath: 1, step: 1 } },
    { id: 'siren', name: () => t('Siren on a fifth'), pattern: { tonic: 'C4', scale: 'major', notes: '1 5 1', lengths: '2 2 2', syllables: 'ng', piano: 'chords', prep: 'I', tempo: 80, breath: 2, step: 1 } },
    { id: 'lip-trill', name: () => t('Lip trill, ninth'), pattern: { tonic: 'C4', scale: 'major', notes: '1 3 5 8 9 8 5 3 1', lengths: '8 8 8 8 4 8 8 8 4', syllables: 'brr', piano: 'double', prep: 'V7-I', tempo: 100, breath: 1, step: 1 } },
    { id: 'rhythm', name: () => t('Rhythm: syncopation'), kind: 'rhythm', pattern: { tonic: 'C4', scale: 'major', notes: '1 1 1 1 1 1', lengths: '4 8 4 8 4 4', syllables: 'ta', piano: 'none', prep: 'none', tempo: 90, breath: 2, step: 0 } },
  ];

  // A score on the page, drawn and played by alphaTab with the soundfont's
  // instruments: load(xml) shows it; clock() is where playback is
  // (seconds, between alphaTab's position events), playing or not.
  function player(container, opts) {
    opts = opts || {};
    const cs = getComputedStyle(document.body);
    const color = (name, fb) => {
      const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(cs.getPropertyValue(name).trim());
      return m ? new alphaTab.model.Color(parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)) : fb;
    };
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.display.layoutMode = alphaTab.LayoutMode.Page;
    s.display.scale = opts.scale || (matchMedia('(max-width: 720px)').matches ? 0.8 : 1);
    s.player.enablePlayer = true;
    s.player.enableCursor = true;
    s.player.soundFont = '/static/js/vendor/alphatab/soundfont/sonivox.sf2';
    s.player.scrollElement = opts.scrollElement || 'html,body';
    if (opts.scrollOffsetY) s.player.scrollOffsetY = opts.scrollOffsetY();
    if (alphaTab.PlayerMode) s.player.playerMode = alphaTab.PlayerMode.EnabledSynthesizer;
    for (const n of ['ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright']) {
      if (alphaTab.NotationElement[n] !== undefined) s.notation.elements.set(alphaTab.NotationElement[n], false);
    }
    const fg = color('--fg', new alphaTab.model.Color(0x24, 0x1e, 0x18));
    const r = s.display.resources;
    r.staffLineColor = color('--fg-muted', fg);
    r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = fg;
    const api = new alphaTab.AlphaTabApi(container, s);
    let pos = 0, stamp = 0, playing = false;
    api.playerPositionChanged.on((e) => { pos = e.currentTime / 1000; stamp = performance.now(); });
    api.playerStateChanged.on((e) => { playing = e.state === 1; stamp = performance.now(); });
    // the offset follows the page (the player card's height) as it starts
    if (opts.scrollOffsetY) api.playerStateChanged.on((e) => { if (e.state === 1) { api.settings.player.scrollOffsetY = opts.scrollOffsetY(); api.updateSettings(); } });
    return {
      api,
      load: (xml) => api.load(new TextEncoder().encode(xml)),
      clock: () => ({ time: playing ? pos + ((performance.now() - stamp) / 1000) * (api.playbackSpeed || 1) : pos, playing }),
    };
  }

  // A note as the page's language says it, with its octave: do ré mi in
  // French, where middle C is do3 (as the tuner, internal/i18n.NoteName).
  function label(midi) {
    const fr = window.SB_LANG === 'fr';
    const names = fr ? ['do', 'do♯', 'ré', 'mi♭', 'mi', 'fa', 'fa♯', 'sol', 'la♭', 'la', 'si♭', 'si'] : ['C', 'C♯', 'D', 'E♭', 'E', 'F', 'F♯', 'G', 'A♭', 'A', 'B♭', 'B'];
    return names[((midi % 12) + 12) % 12] + (Math.floor(midi / 12) - (fr ? 2 : 1));
  }

  // On an iPhone, web audio goes quiet with the ring switch on silent;
  // a "playback" session plays it anyway (Safari 16.4+), and a
  // "play-and-record" one keeps playing while the microphone listens.
  function audioSession(type) {
    try { if (navigator.audioSession) navigator.audioSession.type = type; } catch (e) { /* not this browser */ }
  }

  window.songbooksWarmup = { midiOf, nameOf, label, audioSession, parse, span, expandPath, defaultPath, build, player, PRESETS };
})();

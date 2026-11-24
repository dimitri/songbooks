// The instrument each track of a song's Guitar Pro file plays with.
//
// A file names a General MIDI program per track, and the app plays it
// (alphaTab's synthesizer, with GeneralUser GS cut down to the programs
// the score plays: gp-soundfont.js, docs/soundfont.md; Sonivox, the
// soundfont shipped with alphaTab, without it: its nylon guitar, three
// short samples, is easily heard as a piano). That program is often not
// the one wanted: GProTab files carry a nylon guitar or a piano for
// anything. So the
// person who plays the score picks one, track by track, from a short
// list grouped by family (pickerFor below), where the score plays: the
// Score view (gp-view.js) and the full Mixer (mixer.js).
//
// Whose choice: the song's editors choose for everyone (stored with the
// song's file: PUT /api/songs/{id}/gp-file/instruments, migration 0118);
// anyone else's choice is their own, in this browser (localStorage),
// over the song's. An editor can also write the choice into the file
// itself (a new version of it, gp-view.js) — then the song's choice goes.
//
// alphaTab plays a track's program from an Instrument automation on its
// first beat, which it adds on loading the file (Beat.finish) when the
// file has none: changing playbackInfo.program alone changes nothing you
// hear. setProgram changes both.
(function () {
  const FAMILIES = [
    ['instruments|Guitars', [[24, 'Nylon-string guitar'], [25, 'Steel-string guitar'], [26, 'Jazz guitar'], [27, 'Clean electric guitar'], [28, 'Muted electric guitar'], [29, 'Overdriven guitar'], [30, 'Distortion guitar'], [105, 'Banjo']]],
    ['instruments|Basses', [[32, 'Acoustic bass'], [33, 'Electric bass (finger)'], [34, 'Electric bass (pick)'], [35, 'Fretless bass'], [36, 'Slap bass'], [38, 'Synth bass']]],
    ['instruments|Keys', [[0, 'Piano'], [1, 'Bright piano'], [4, 'Electric piano'], [6, 'Harpsichord'], [11, 'Vibraphone']]],
    ['instruments|Organs', [[16, 'Drawbar organ'], [17, 'Percussive organ'], [19, 'Church organ'], [21, 'Accordion'], [22, 'Harmonica']]],
    ['instruments|Strings', [[40, 'Violin'], [42, 'Cello'], [48, 'String ensemble'], [45, 'Pizzicato strings'], [46, 'Harp']]],
    ['instruments|Voices', [[52, 'Choir (aah)'], [53, 'Voice (ooh)'], [54, 'Synth voice']]],
    ['instruments|Brass and winds', [[56, 'Trumpet'], [57, 'Trombone'], [61, 'Brass section'], [65, 'Alto sax'], [66, 'Tenor sax'], [71, 'Clarinet'], [73, 'Flute']]],
    ['instruments|Synths', [[80, 'Square lead'], [81, 'Sawtooth lead'], [89, 'Warm pad']]],
  ];
  // Any other program, by its GM family (programs 8 by 8).
  const GM_FAMILY = ['Piano', 'Chromatic percussion', 'Organ', 'Guitar', 'Bass', 'Strings', 'Ensemble', 'Brass', 'Reed', 'Pipe', 'Synth lead', 'Synth pad', 'Synth effects', 'Ethnic', 'Percussive', 'Sound effects'];
  const NAMES = new Map(FAMILIES.flatMap(([, list]) => list));
  const root = typeof window !== 'undefined' ? window : globalThis;
  const tr = (s) => (root.t ? root.t(s) : s.replace(/^[a-z]+\|/, ''));

  function name(p) {
    if (!(p >= 0 && p <= 127)) return tr('MIDI');
    return NAMES.has(p) ? tr(NAMES.get(p)) : `${tr(GM_FAMILY[Math.floor(p / 8)])} (${p + 1})`;
  }

  // ---- Changing what alphaTab plays --------------------------------------
  const INSTRUMENT = () => (root.alphaTab && root.alphaTab.model.AutomationType ? root.alphaTab.model.AutomationType.Instrument : 2);
  const isDrums = (track) => !!(track.staves[0] && track.staves[0].isPercussion);

  // setProgram makes a track play program: its playback program, and the
  // Instrument automations that set the track's own program (the first
  // beat's, and any later one going back to it); a change written later
  // in the file to another instrument stays. Returns whether anything
  // changed.
  function setProgram(track, program) {
    if (!track || isDrums(track) || !(program >= 0 && program <= 127)) return false;
    const old = track.playbackInfo.program;
    if (old === program) return false;
    track.playbackInfo.program = program;
    const kind = INSTRUMENT();
    for (const st of track.staves) {
      for (const bar of st.bars) {
        for (const v of bar.voices) {
          for (const beat of v.beats) {
            for (const a of beat.automations || []) if (a.type === kind && a.value === old) a.value = program;
          }
        }
      }
    }
    return true;
  }

  // ---- Whose choice ---------------------------------------------------------
  // The song's choice and whether this person makes it for everyone: on
  // the Score view's Instruments tool (play.html).
  const dataEl = () => (typeof document !== 'undefined' ? document.querySelector('[data-gp-instruments]') : null);
  const localKey = (variantId) => `songbooks:gp-instruments:${variantId}`;
  function readLocal(variantId) {
    try { return JSON.parse(localStorage.getItem(localKey(variantId)) || '{}') || {}; } catch (e) { return {}; }
  }
  function writeLocal(variantId, mine) {
    try {
      if (Object.keys(mine).length) localStorage.setItem(localKey(variantId), JSON.stringify(mine));
      else localStorage.removeItem(localKey(variantId));
    } catch (e) { /* this page only */ }
  }

  // One per song on a page: the file's own programs (as each view loads
  // the file), the song's choice, this person's.
  const songs = new Map();
  function state(variantId) {
    let s = songs.get(String(variantId));
    if (s) return s;
    const el = dataEl();
    let stored = [];
    try { stored = JSON.parse((el && el.dataset.gpInstruments) || '[]') || []; } catch (e) { stored = []; }
    s = {
      variantId: String(variantId),
      canEdit: !!(el && el.dataset.gpInstrumentsEdit),
      stored, // [{track, name, program}]
      mine: readLocal(variantId), // {track: {name, program}}
      file: null, // [{name, program, drums}], the file's own
    };
    songs.set(s.variantId, s);
    return s;
  }
  // The Mixer's data (gpInstruments), when the page didn't say (the page's
  // is kept: a choice made since is in it).
  function setStored(variantId, list) {
    const el = dataEl();
    if (el && el.dataset.gpInstruments !== undefined) return;
    if (Array.isArray(list)) state(variantId).stored = list;
  }

  // tracksOf: the file's own tracks, remembered once (a view that changed
  // programs since must not report its changes as the file's).
  function knowFile(variantId, score) {
    const s = state(variantId);
    if (!s.file) s.file = score.tracks.map((t) => ({ name: t.name || '', program: t.playbackInfo.program, drums: isDrums(t) }));
    return s.file;
  }

  // chosen: track i's program as chosen, and by whom ('mine', 'song'), or
  // null for the file's own. A choice made for a track of another name
  // (the file replaced since) doesn't count.
  function chosen(variantId, i) {
    const s = state(variantId);
    const f = s.file && s.file[i];
    const same = (n) => !f || n === undefined || n === f.name;
    const m = s.mine[i];
    if (m && same(m.name) && m.program >= 0) return { program: m.program, by: 'mine' };
    const st = s.stored.find((x) => x.track === i);
    if (st && same(st.name) && st.program >= 0) return { program: st.program, by: 'song' };
    return null;
  }
  function programFor(variantId, i) {
    const c = chosen(variantId, i);
    const s = state(variantId);
    return c ? c.program : (s.file && s.file[i] ? s.file[i].program : 0);
  }

  // apply gives score's tracks the programs chosen for this song; true
  // when anything changed.
  function apply(variantId, score) {
    const file = knowFile(variantId, score);
    let changed = false;
    score.tracks.forEach((t, i) => { if (file[i] && setProgram(t, programFor(variantId, i))) changed = true; });
    return changed;
  }

  // choose: track i plays program (null: the file's own again). An editor
  // chooses for everyone (and their own choice goes); anyone else, for
  // themselves. Everything that plays the score on the page hears it
  // ("songbooks:gp-instrument").
  async function choose(variantId, i, program) {
    const s = state(variantId);
    const f = s.file && s.file[i];
    const pick = program === null || program === undefined || program === '' ? null : Number(program);
    if (s.canEdit) {
      const list = s.stored.filter((x) => x.track !== i);
      if (pick !== null && !(f && pick === f.program)) list.push({ track: i, name: f ? f.name : '', program: pick });
      list.sort((a, b) => a.track - b.track);
      const res = await fetch(`/api/songs/${s.variantId}/gp-file/instruments`, {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tracks: list }),
      });
      if (!res.ok) throw new Error(t('Saving failed'));
      s.stored = (await res.json()).tracks || list;
      if (s.mine[i]) { delete s.mine[i]; writeLocal(s.variantId, s.mine); }
    } else {
      if (pick === null) delete s.mine[i];
      else s.mine[i] = { name: f ? f.name : '', program: pick };
      writeLocal(s.variantId, s.mine);
    }
    document.dispatchEvent(new CustomEvent('songbooks:gp-instrument', { detail: { variantId: s.variantId, track: i } }));
  }

  // The file now plays what was chosen (an editor wrote it in): the song's
  // choice and this person's go.
  async function savedInFile(variantId, score) {
    const s = state(variantId);
    s.file = null;
    knowFile(variantId, score);
    s.mine = {};
    writeLocal(s.variantId, s.mine);
    if (s.canEdit && s.stored.length) {
      await fetch(`/api/songs/${s.variantId}/gp-file/instruments`, {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: '{"tracks":[]}',
      });
    }
    s.stored = [];
    document.dispatchEvent(new CustomEvent('songbooks:gp-instrument', { detail: { variantId: s.variantId, track: -1 } }));
  }

  // ---- The picker ------------------------------------------------------------
  // pickerFor(variantId, i): a <select> for track i — the file's own
  // first, then the families. It shows what plays now; a change goes
  // through choose().
  function pickerFor(variantId, i, opts = {}) {
    const s = state(variantId);
    const sel = document.createElement('select');
    sel.className = 'gp-instrument' + (opts.className ? ' ' + opts.className : '');
    const fileTrack = () => s.file && s.file[i];
    const f = fileTrack();
    sel.setAttribute('aria-label', opts.label || t('Instrument for %s', (f && f.name) || t('Track %d', i + 1)));
    sel.title = s.canEdit ? t('the instrument this track plays with, for everyone who plays the song') : t('the instrument this track plays with (just for you)');
    const own = document.createElement('option');
    own.value = '';
    sel.appendChild(own);
    for (const [fam, list] of FAMILIES) {
      const g = document.createElement('optgroup');
      g.label = tr(fam);
      for (const [p, n] of list) {
        const o = document.createElement('option');
        o.value = String(p);
        o.textContent = tr(n);
        g.appendChild(o);
      }
      sel.appendChild(g);
    }
    const show = () => {
      // The file's own: written into it since, it may be another.
      own.textContent = t('As in the file: %s', name(fileTrack() ? fileTrack().program : 0));
      const c = chosen(variantId, i);
      const p = c ? c.program : null;
      if (p !== null && !NAMES.has(p) && !sel.querySelector(`option[value="${p}"]`)) {
        const o = document.createElement('option');
        o.value = String(p);
        o.textContent = name(p);
        sel.insertBefore(o, own.nextSibling);
      }
      sel.value = p === null ? '' : String(p);
      sel.dataset.by = c ? c.by : 'file';
    };
    show();
    sel.addEventListener('change', async () => {
      sel.disabled = true;
      try { await choose(variantId, i, sel.value); } catch (e) { show(); if (opts.onError) opts.onError(e); } finally { sel.disabled = false; }
    });
    document.addEventListener('songbooks:gp-instrument', (e) => { if (e.detail.variantId === String(variantId)) show(); });
    return sel;
  }

  const api = { FAMILIES, name, setProgram, apply, knowFile, chosen, programFor, choose, savedInFile, pickerFor, setStored, state, isDrums };
  root.GPInstruments = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})();

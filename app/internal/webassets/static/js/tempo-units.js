// tempo-units.js — what a chart's "tempo:" counts (internal/song/tempo.go):
// its time signature's beat. A simple meter's beat is its denominator's
// note (4/4 quarters, 2/2 halves, 7/8 eighths); a compound meter's, 6/8,
// 9/8, 12/8, is a dotted quarter, the musicians' convention: "tempo: 46"
// in 6/8 is 46 dotted quarters, 138 eighths, a minute. The player's bar
// grid, click and count-in, the metronome, the click track and the
// recording's analysis all count it so; Guitar Pro, alphaTab and the
// Mixer's auto-align count quarter notes (beatQuarters converts).
//
// Wherever a compound chart's bpm is shown it says which note it counts
// ("♩. = 46 (♪ = 138)"), and where a tempo is typed or tapped the note can
// be picked (picker below): a dotted quarter, a quarter or an eighth,
// stored in the chart's beat. Tested by jstest/tempo-units.test.mjs.
(function (root) {
  const tr = (s, ...a) => (typeof root.t === 'function' ? root.t(s, ...a) : s.replace(/%[sd]/g, () => String(a.shift())));

  // A time signature as numbers: (6, 8), ("6/8"), or the player's
  // transport element's dataset; null when unreadable.
  function sig(num, den) {
    if (typeof num === 'string' && den === undefined) {
      const m = /^\s*(\d+)\s*\/\s*(\d+)\s*$/.exec(num);
      if (!m) return null;
      num = m[1];
      den = m[2];
    }
    const n = parseInt(num, 10), d = parseInt(den, 10);
    if (!(n > 0) || !(d > 0)) return null;
    return { n, d };
  }
  function compound(num, den) {
    const s = sig(num, den);
    return !!s && s.d === 8 && s.n >= 6 && s.n % 3 === 0;
  }
  // How many quarter notes one beat of "tempo:" lasts.
  function beatQuarters(num, den) {
    const s = sig(num, den);
    if (!s) return 1;
    return compound(s.n, s.d) ? 1.5 : 4 / s.d;
  }

  // The notes a compound chart's tempo can be typed or tapped in, each
  // with how many of it make one beat; null for a simple meter (its
  // tempo counts its one obvious beat).
  const UNITS = [
    { id: 'beat', glyph: '♩.', per: 1, name: 'dotted quarters' },
    { id: 'quarter', glyph: '♩', per: 1.5, name: 'quarter notes' },
    { id: 'eighth', glyph: '♪', per: 3, name: 'eighth notes' },
  ];
  function units(num, den) {
    return compound(num, den) ? UNITS : null;
  }
  const unitOf = (id) => UNITS.find((u) => u.id === id) || UNITS[0];
  // A tempo in unit id, in the chart's beat (and back), unrounded.
  function toBeat(v, id, num, den) {
    return compound(num, den) ? v / unitOf(id).per : v;
  }
  function fromBeat(v, id, num, den) {
    return compound(num, den) ? v * unitOf(id).per : v;
  }

  // The bpm, said so it can't be read in another note: "46 bpm" in 4/4,
  // "♩. = 46 (♪ = 138)" in 6/8 ({short: true}: "♩. = 46").
  function text(bpm, num, den, opts) {
    const v = Math.round(bpm);
    if (!compound(num, den)) return tr('%d bpm', v);
    const main = `♩. = ${v}`;
    if (opts && opts.short) return main;
    return `${main} (♪ = ${Math.round(bpm * 3)})`;
  }

  // The note last picked, remembered in this browser.
  const KEY = 'songbooks:tempo-unit';
  function savedUnit() {
    try { return unitOf(localStorage.getItem(KEY) || 'beat').id; } catch (e) { return 'beat'; }
  }
  function saveUnit(id) {
    try { localStorage.setItem(KEY, id); } catch (e) { /* this time only */ }
  }

  // picker: the <select> of the notes a compound chart's tempo is typed
  // in, or null in a simple meter. onChange(id, previousId).
  function picker(num, den, onChange) {
    const list = units(num, den);
    if (!list || typeof document === 'undefined') return null;
    const sel = document.createElement('select');
    sel.className = 'tempo-unit';
    sel.setAttribute('aria-label', tr('the note the tempo counts'));
    sel.title = tr('the note the tempo counts: the chart keeps dotted quarters, the beat of %s', `${sig(num, den).n}/${sig(num, den).d}`);
    for (const u of list) {
      const o = document.createElement('option');
      o.value = u.id;
      o.textContent = `${u.glyph} ${tr(u.name)}`;
      sel.append(o);
    }
    sel.value = savedUnit();
    let was = sel.value;
    sel.addEventListener('change', () => {
      saveUnit(sel.value);
      const prev = was;
      was = sel.value;
      if (onChange) onChange(sel.value, prev);
    });
    return sel;
  }

  const api = { sig, compound, beatQuarters, units, toBeat, fromBeat, text, picker, UNITS };
  if (typeof module !== 'undefined' && module.exports) { module.exports = api; return; }
  root.SongbooksTempoUnits = api;
})(typeof window !== 'undefined' ? window : globalThis);

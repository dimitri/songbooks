// The chords over a Guitar Pro file's bars, as alphaTab draws them (its
// chord names, over the beats that carry them): the file's own chord
// symbols when it has any; else the song's chart's, each chart bar's
// chords put on the bar of the file it plays — never made up.
//
// The chart's bar for each bar of the file comes from the song's bar
// mapping (the Mixer's gpOrder: its gp[...] lines, or gp_start, a "lead:"
// left out as bars the file doesn't play), through GPAlign's
// chartBarsForFile. alphaTab's model holds a chord per written bar, so a
// bar the file plays twice (a repeat) shows the chords of its first pass.
//
//   GPChords.hasOwn(score)               the file has chord symbols
//   GPChords.byChartBar(sections)        the Mixer's sections → chords by
//                                        chart bar: [[{pos, name}], …]
//   GPChords.forWrittenBars(map, played, chartChords)
//                                        → Map(written bar index (0-based)
//                                          → [{pos, name}]); map[p] is the
//                                          chart bar of played bar p (-1
//                                          for none), played[p] its written
//                                          bar (1-based)
//   GPChords.put(score, trackIndex, chords, makeChord)
//                                        the chords on that track's first
//                                        staff (and none on the others it
//                                        put them on before); how many
//   GPChords.style(settings, alphaTab, size)
//                                        chord names that read: bold, not
//                                        alphaTab's small italic
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.GPChords = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  // Ids of the chords put here: told apart from the file's own.
  const PREFIX = 'songbooks-chart-';

  function staves(score) {
    const out = [];
    for (const tr of score.tracks || []) for (const st of tr.staves || []) out.push(st);
    return out;
  }
  function beatsOf(st) {
    const out = [];
    for (const bar of st.bars || []) for (const v of bar.voices || []) for (const b of v.beats || []) out.push(b);
    return out;
  }

  function hasOwn(score) {
    return staves(score).some((st) => beatsOf(st).some((b) => b.chordId != null && b.chordId !== '' && !String(b.chordId).startsWith(PREFIX)));
  }

  function byChartBar(sections) {
    const out = [];
    for (const s of sections || []) {
      (s.chords || []).forEach((bar, i) => {
        const named = (bar || []).filter((c) => c && c.name);
        if (named.length) out[s.startBar + i] = named.map((c) => ({ pos: Math.max(0, Math.min(1, +c.pos || 0)), name: String(c.name) }));
      });
    }
    return out;
  }

  function forWrittenBars(map, played, chartChords) {
    const out = new Map();
    (played || []).forEach((w, p) => {
      const b = map ? map[p] : -1;
      const w0 = w - 1;
      if (!(b >= 0) || w0 < 0 || out.has(w0)) return;
      const chords = chartChords[b];
      if (chords && chords.length) out.set(w0, chords);
    });
    return out;
  }

  // The beat a chord at pos (a fraction of the bar) sits on: the last one
  // starting at or before it (a little after counts), else the first.
  function beatAt(bar, pos, barTicks) {
    const beats = ((bar.voices || [])[0] || {}).beats || [];
    if (!beats.length) return null;
    const at = pos * barTicks + barTicks / 64;
    let pick = beats[0];
    for (const b of beats) if ((b.playbackStart || 0) <= at) pick = b;
    return pick;
  }

  function clear(score) {
    for (const st of staves(score)) {
      for (const b of beatsOf(st)) if (b.chordId != null && String(b.chordId).startsWith(PREFIX)) b.chordId = null;
      if (st.chords && typeof st.chords.delete === 'function') {
        for (const k of [...st.chords.keys()]) if (String(k).startsWith(PREFIX)) st.chords.delete(k);
      }
    }
  }

  function put(score, trackIndex, chords, makeChord) {
    clear(score);
    const tr = (score.tracks || [])[trackIndex];
    const st = tr && tr.staves && tr.staves[0];
    if (!st || !chords || !chords.size) return 0;
    let n = 0;
    for (const [w, list] of chords) {
      const bar = st.bars[w];
      const mb = (score.masterBars || [])[w];
      if (!bar || !mb) continue;
      const ticks = typeof mb.calculateDuration === 'function' ? mb.calculateDuration() : 3840;
      const onBeat = new Map();
      for (const c of list) {
        const beat = beatAt(bar, c.pos, ticks);
        if (!beat) continue;
        // Two chords on one beat (a long note under a change): both names.
        onBeat.set(beat, onBeat.has(beat) ? `${onBeat.get(beat)} · ${c.name}` : c.name);
      }
      for (const [beat, name] of onBeat) {
        const id = `${PREFIX}${w}-${beat.index ?? n}`;
        const chord = makeChord(name);
        if (typeof st.addChord === 'function') st.addChord(id, chord);
        else (st.chords || (st.chords = new Map())).set(id, chord);
        beat.chordId = id;
        n++;
      }
    }
    return n;
  }

  function style(settings, at, size) {
    const M = at && at.model, E = at && at.NotationElement;
    if (!M || !M.Font || !E || E.EffectChordNames === undefined) return;
    settings.display.resources.elementFonts.set(E.EffectChordNames, new M.Font('Arial', size || 15, M.FontStyle.Plain, M.FontWeight.Bold));
  }

  return { hasOwn, byChartBar, forWrittenBars, put, clear, style, PREFIX };
});

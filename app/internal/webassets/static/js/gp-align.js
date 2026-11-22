// The plain arithmetic behind a Guitar Pro file "next to the chart"
// (docs/gp-file-model.md §3.4), with no DOM, so node can test it
// (jstest/gp-align.test.mjs):
//
// - the GP import's check step: where the chart and the file's sections
//   sit on one bar axis, the one-tap fixes ("start the chart at the
//   file's bar N"), and how many differences are left;
// - the Score view's simplified mixer: which bar of the chart each bar the
//   file plays is, and where the song's recording is at that moment, so
//   the recording plays along with the score.
//
// The comparison itself (bar counts, the file's sections, chords) is the
// server's (gpalign.go): the check step and drift say the same thing.
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.GPAlign = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  // ---- The check step -----------------------------------------------------

  // A start the file can have: 1 (its first bar) to its last bar.
  function clampStart(n, fileBars) {
    n = Math.round(Number(n) || 1);
    return Math.max(1, Math.min(Math.max(1, fileBars || 1), n));
  }

  // Lining a section up with the file's section it's paired with moves the
  // whole chart by that section's shift.
  function startFor(start, row, fileBars) {
    if (!row || !row.match) return clampStart(start, fileBars);
    return clampStart(start + (row.shift || 0), fileBars);
  }

  // The fixes the app can make in one tap, best first, none twice: lining
  // the first section up with the file's section marker, where the chords
  // line up best (a file with no markers), past the file's silent bars.
  // Each is { start, why, bars }.
  function fixes(al) {
    const out = [];
    const add = (start, why) => {
      start = clampStart(start, al.fileBars);
      if (start !== al.start && !out.some((f) => f.start === start)) out.push({ start, why, bars: start - 1 });
    };
    const first = (al.sections || [])[0];
    if (first && first.match && first.shift && first.own) add(al.start + first.shift, 'marker');
    // The file's own section markers say more than chords matching: where
    // they pair the chart's first section, chords aren't offered.
    if (al.bestStart && !(first && first.match)) add(al.bestStart, 'chords');
    if (al.fileEmpty > 0) add(al.fileEmpty + 1, 'empty');
    if (al.start > 1 && !out.length && al.start !== (al.fileEmpty || 0) + 1 && (al.sections || []).every((s) => !s.match)) {
      // Nothing to go by: offer the file's first bar back.
      add(1, 'first');
    }
    return out;
  }

  // What's left to look at: sections differing on their own (a shift that
  // follows from an earlier section's bar count is that section's), and
  // header facts the import won't take from the file.
  function remaining(al, header, taken) {
    const own = (al.sections || []).filter((s) => s.status !== 'ok' && !(s.status === 'shifted' && !s.own));
    const facts = (header || []).filter((h) => !(taken && taken.has(h.fact)));
    return { sections: own.length, header: facts.length, total: own.length + facts.length };
  }

  // The two rows of the timeline on one bar axis, in played bars of the
  // file (0-based): the chart's sections above, the file's below, its
  // lead-in before its first section. x and w are fractions of the whole.
  function layout(al) {
    const lead = Math.max(0, (al.start || 1) - 1);
    const total = Math.max(1, al.fileBars || 0, lead + (al.chartBars || 0));
    const chart = (al.sections || []).map((s, i) => ({
      i, label: s.label, status: s.status, own: s.own,
      x: (lead + s.start) / total, w: s.bars / total,
    }));
    const file = [];
    const markers = al.markers || [];
    const firstFrom = markers.length ? markers[0].from : (al.fileBars || 0) + 1;
    if (firstFrom > 1) file.push({ label: '', lead: true, x: 0, w: (firstFrom - 1) / total, bars: firstFrom - 1 });
    for (const m of markers) file.push({ label: m.text, x: (m.from - 1) / total, w: m.bars / total, bars: m.bars, from: m.from, bar: m.bar });
    if (!markers.length && al.fileBars) file.push({ label: '', x: 0, w: al.fileBars / total, bars: al.fileBars, from: 1 });
    return { total, lead, chart, file };
  }

  // A key signature's name, as gpfacts.KeyName writes it: 1 flat major is
  // "F", 3 sharps minor "F#m". A file still in C major says nothing about
  // the song's key: "".
  function keyName(k) {
    if (!k || (!k.sig && !k.minor)) return '';
    let pc = (((k.sig * 7) % 12) + 12) % 12;
    if (k.minor) pc = (pc + 9) % 12;
    const sharps = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B'];
    const flats = ['C', 'Db', 'D', 'Eb', 'E', 'F', 'Gb', 'G', 'Ab', 'A', 'Bb', 'B'];
    return (k.sig < 0 ? flats : sharps)[pc] + (k.minor ? 'm' : '');
  }

  // ---- The Score view: the recording along with the score ----------------

  // For each bar the file plays (its played order: GPFacts.played, 1-based
  // written bars), the chart's bar it is, or -1. order is the Mixer's
  // gpOrder: the written bar each chart bar plays (0 for none). Taken in
  // order: the bar after the last one found when it fits, else the first
  // chart bar playing that written bar not taken yet.
  function chartBarsForFile(order, played) {
    const at = new Map(); // written bar -> chart bars playing it, in order
    (order || []).forEach((w, b) => { if (w > 0) { if (!at.has(w)) at.set(w, []); at.get(w).push(b); } });
    const used = new Set();
    let last = -1;
    return (played || []).map((w) => {
      const cands = at.get(w) || [];
      let b = cands.find((c) => c === last + 1 && !used.has(c));
      if (b === undefined) b = cands.find((c) => c > last && !used.has(c));
      if (b === undefined) b = cands.find((c) => !used.has(c));
      if (b === undefined) return -1;
      used.add(b);
      last = b;
      return b;
    });
  }

  // Seconds into the recording where each chart bar starts (n + 1 entries)
  // from what's known: [{bar, t}] (bar sync points, or the sections' start
  // markers). Between two known bars they keep their nominal proportions;
  // outside them, the nominal bar length. null when nothing is known: the
  // recording isn't lined up with the song's bars.
  function barTimes(known, n, nominalBarSec) {
    known = (known || []).filter((k) => k.bar >= 0 && k.bar <= n && isFinite(k.t)).sort((a, b) => a.bar - b.bar);
    if (!known.length || !(n > 0)) return null;
    const nom = nominalBarSec > 0 ? nominalBarSec : 2;
    const out = new Array(n + 1);
    for (let b = 0; b <= n; b++) {
      let a = null, c = null;
      for (const k of known) { if (k.bar <= b) a = k; else if (!c) c = k; }
      if (a && a.bar === b) out[b] = a.t;
      else if (a && c) out[b] = a.t + (b - a.bar) / (c.bar - a.bar) * (c.t - a.t);
      else if (a) out[b] = a.t + (b - a.bar) * nom;
      else out[b] = c.t - (c.bar - b) * nom;
    }
    return out;
  }

  // The known bars of a recording: its bar sync points, else each
  // section's start marker (sections: [{index, startBar}]).
  function knownBars(meta, sections) {
    if (!meta) return [];
    if (meta.barSyncPoints && meta.barSyncPoints.length) return meta.barSyncPoints.map((p) => ({ bar: p.barIndex, t: p.timeSeconds }));
    const out = [];
    for (const m of meta.markers || []) {
      const s = (sections || []).find((x) => x.index === m.sectionIndex);
      if (s) out.push({ bar: s.startBar, t: m.timeSeconds });
    }
    return out;
  }

  // Where the recording should be while the score plays position pos (a
  // played bar of the file, 0-based) at frac of its way through: its time,
  // and the speed the score plays that bar at so both last as long (the
  // recording's bar length against the score's own, synthBarSec). null
  // where the chart has no bar for it (the file's lead-in, a bar the
  // chart leaves out).
  function follow(map, times, pos, frac, synthBarSec) {
    if (!times || pos < 0 || pos >= map.length) return null;
    const b = map[pos];
    if (b < 0 || b >= times.length - 1) return null;
    const dur = times[b + 1] - times[b];
    const t = times[b] + Math.max(0, Math.min(1, frac || 0)) * dur;
    if (t < 0 || !(dur > 0)) return null;
    return { bar: b, t, ratio: synthBarSec > 0 ? synthBarSec / dur : 1 };
  }

  return { clampStart, startFor, keyName, fixes, remaining, layout, chartBarsForFile, barTimes, knownBars, follow };
});

// What a Guitar Pro file says about the song it holds — its "facts":
// tempo, time and key signatures, swing, section markers, tracks, the
// chord names over each bar, chord diagrams, and the order bars are played
// in. Read from alphaTab's model of the file (the browser at upload, and
// tools/gp in Node for files already stored), sent to the server with the
// file, stored next to it, and compared there with the song's text (see
// internal/gpfacts and docs/gp-file-model.md §4).
//
// Bars are numbered from 1, as Guitar Pro shows them. Strings run from the
// highest (index 0), as alphaTab and Guitar Pro's chord diagrams do.
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.GPFacts = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  const VERSION = 1;

  // played lists the bars in the order they're played: a "|: :|" bracket
  // plays repeatCount times (2 if unmarked), from its opening bar or the
  // bar after the previous bracket; an alternate ending plays only on the
  // passes its bit mask names. D.S. and coda jumps are not followed.
  function played(masterBars) {
    const out = [];
    let start = 0, pass = 1, guard = 0;
    for (let i = 0; i < masterBars.length && guard < 10000; guard++) {
      const mb = masterBars[i];
      if (mb.isRepeatStart && i !== start) { start = i; pass = 1; }
      const plays = !mb.alternateEndings || (mb.alternateEndings & (1 << (pass - 1))) !== 0;
      if (plays) out.push(i + 1);
      if (plays && mb.repeatCount > 0) {
        if (pass < mb.repeatCount) { pass++; i = start; continue; }
        pass = 1;
        start = i + 1;
      }
      i++;
    }
    return out;
  }

  function swingOf(feel) {
    // alphaTab's TripletFeel: 1 Triplet16th, 2 Triplet8th.
    if (feel === 2) return '8';
    if (feel === 1) return '16';
    return '';
  }

  function tempoOf(mb) {
    const a = (mb.tempoAutomations && mb.tempoAutomations[0]) || mb.tempoAutomation;
    return a ? Math.round(a.value) : 0;
  }

  // The chord names over one bar of a staff, in beat order, each with
  // where it falls in the bar (pos, 0 = the downbeat, as the Mixer's
  // chords are given).
  function barChords(staff, bar) {
    const out = [];
    const len = (bar.masterBar && bar.masterBar.calculateDuration && bar.masterBar.calculateDuration()) || 3840;
    for (const v of bar.voices) {
      for (const b of v.beats) {
        if (b.chordId == null) continue;
        const c = b.chord || (staff.chords && staff.chords.get(b.chordId));
        const name = c && c.name ? c.name.trim() : '';
        if (!name || (out.length && out[out.length - 1].name === name)) continue;
        out.push({ pos: Math.max(0, Math.round((b.playbackStart / len) * 100) / 100), name });
      }
      if (out.length) break;
    }
    return out;
  }

  function extract(score) {
    const mbs = score.masterBars;
    const facts = {
      v: VERSION,
      title: score.title || '',
      artist: score.artist || '',
      tempo: Math.round(score.tempo || tempoOf(mbs[0] || {}) || 0),
      swing: swingOf(mbs[0] ? mbs[0].tripletFeel : 0),
      bars: mbs.length,
      played: played(mbs),
      time: [], tempos: [], keys: [], markers: [], repeats: [],
      tracks: [], chords: [], diagrams: [],
    };
    let lastTime = '', lastTempo = 0;
    mbs.forEach((mb, i) => {
      const n = i + 1;
      const t = mb.timeSignatureNumerator + '/' + mb.timeSignatureDenominator;
      if (t !== lastTime) { facts.time.push({ bar: n, num: mb.timeSignatureNumerator, den: mb.timeSignatureDenominator }); lastTime = t; }
      const bpm = tempoOf(mb);
      if (bpm && bpm !== lastTempo) { facts.tempos.push({ bar: n, bpm }); lastTempo = bpm; }
      if (mb.section && (mb.section.text || mb.section.marker)) facts.markers.push({ bar: n, text: (mb.section.text || mb.section.marker).trim() });
      if (mb.isRepeatStart || mb.repeatCount > 0 || mb.alternateEndings) {
        facts.repeats.push({ bar: n, start: !!mb.isRepeatStart, count: mb.repeatCount || 0, endings: mb.alternateEndings || 0 });
      }
    });

    let chordTrack = -1, keyStaff = null;
    score.tracks.forEach((tr, ti) => {
      const st = tr.staves[0];
      facts.tracks.push({
        n: ti + 1,
        name: (tr.name || '').trim(),
        short: (tr.shortName || '').trim(),
        program: tr.playbackInfo ? tr.playbackInfo.program : 0,
        drums: !!st.isPercussion,
        tuning: st.isPercussion ? [] : Array.from(st.tuning || []),
        capo: st.capo || 0,
        tab: !!st.showTablature,
        staff: !!st.showStandardNotation,
      });
      if (!st.isPercussion && !keyStaff) keyStaff = st;
      for (const staff of tr.staves) {
        if (chordTrack < 0 && staff.bars.some(b => barChords(staff, b).length)) {
          chordTrack = ti;
          facts.chords = staff.bars.map(b => barChords(staff, b));
        }
        if (!staff.chords) continue;
        for (const c of staff.chords.values()) {
          const frets = Array.from(c.strings || []);
          if (!c.showDiagram || !frets.some(f => f >= 0)) continue;
          const d = { name: (c.name || '').trim(), track: ti + 1, base: c.firstFret || 1, frets, barres: Array.from(c.barreFrets || []) };
          if (!facts.diagrams.some(x => x.name === d.name && x.frets.join() === d.frets.join())) facts.diagrams.push(d);
        }
      }
    });
    if (chordTrack >= 0) facts.chordTrack = chordTrack + 1;

    let lastKey = null;
    if (keyStaff) {
      keyStaff.bars.forEach((b, i) => {
        const k = (b.keySignature ?? mbs[i].keySignature) + '/' + (b.keySignatureType ?? mbs[i].keySignatureType);
        if (k !== lastKey) {
          facts.keys.push({ bar: i + 1, sig: b.keySignature ?? mbs[i].keySignature, minor: (b.keySignatureType ?? mbs[i].keySignatureType) === 1 });
          lastKey = k;
        }
      });
    }
    return facts;
  }

  // parseRef reads the value of a "gp[Track]:" line — "9-12 staff",
  // "1-4, 9-12", "9" — into { ranges: [[from, to]], notation }. A bad
  // range comes back as { error }.
  function parseRef(value) {
    const out = { ranges: [], notation: '' };
    let v = (value || '').trim();
    const m = v.match(/\s+(tab\+staff|staff\+tab|tab|staff|slash)$/i) || v.match(/^(tab\+staff|staff\+tab|tab|staff|slash)$/i);
    if (m) {
      out.notation = m[1].toLowerCase() === 'staff+tab' ? 'tab+staff' : m[1].toLowerCase();
      v = v.slice(0, v.length - m[0].length).trim();
    }
    v = v.replace(/^bars?\s+/i, '');
    if (!v) return out;
    for (const part of v.split(',')) {
      const r = part.trim().match(/^(\d+)\s*(?:[-–]\s*(\d+))?$/);
      if (!r) return { error: 'bars must look like 9-12 or 1-4, 9-12' };
      const a = +r[1], b = r[2] ? +r[2] : a;
      if (a < 1 || b < a) return { error: 'bars ' + part.trim() + ' are not a range' };
      out.ranges.push([a, b]);
    }
    return out;
  }

  // findTrack returns the 0-based index of the track a "gp[...]" label
  // names: "#2" by number, otherwise by name or short name, ignoring case
  // and accents. -1 when there's none.
  function findTrack(tracks, label) {
    label = (label || '').trim();
    if (/^#\d+$/.test(label)) {
      const n = +label.slice(1);
      return n >= 1 && n <= tracks.length ? n - 1 : -1;
    }
    const fold = s => (s || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().trim();
    const want = fold(label);
    let i = tracks.findIndex(t => fold(t.name) === want);
    if (i < 0) i = tracks.findIndex(t => fold(t.shortName ?? t.short) === want);
    return i;
  }

  return { VERSION, extract, played, parseRef, findTrack };
});

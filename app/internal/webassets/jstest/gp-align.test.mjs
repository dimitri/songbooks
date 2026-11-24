// A Guitar Pro file next to the chart (static/js/gp-align.js): the GP
// import check step's fixes and timeline, and how the Score view keeps the
// song's recording with the score. Run by webassets' TestScripts (go
// test), or alone, from internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const A = require('../static/js/gp-align.js');
const GPFacts = require('../static/js/gp-facts.js');

// What POST /api/songs/{id}/gp-check answers for a file with two empty
// lead-in bars, read from its first bar (gpalign.go, its tests).
const fromBar1 = {
  start: 1, chartBars: 12, fileBars: 16, fileEmpty: 2, bestStart: 3,
  markers: [{ text: 'Verse 1', from: 3, bars: 4, bar: 3 }, { text: 'Chorus', from: 7, bars: 6, bar: 7 }, { text: 'Verse 2', from: 13, bars: 4, bar: 10 }],
  sections: [
    { label: 'Verse', start: 0, bars: 4, status: 'shifted', match: { text: 'Verse 1', from: 3 }, shift: 2, own: true },
    { label: 'Chorus', start: 4, bars: 4, status: 'bars', match: { text: 'Chorus', from: 7, bars: 6 }, shift: 2, own: false },
    { label: 'Verse 2', start: 8, bars: 4, status: 'shifted', match: { text: 'Verse 2', from: 13 }, shift: 4, own: false },
  ],
};

test('one-tap fixes: line the first section up, the chords, past the silence, none twice', () => {
  const f = A.fixes(fromBar1);
  assert.deepEqual(f.map((x) => [x.start, x.why, x.bars]), [[3, 'marker', 2]]);
  // Markers pair the first section: the chords' best guess isn't offered.
  assert.deepEqual(A.fixes({ ...fromBar1, bestStart: 5 }).map((x) => x.why), ['marker']);
  const noMarkers = { ...fromBar1, markers: [], sections: fromBar1.sections.map((s) => ({ ...s, match: null, shift: 0 })), bestStart: 4 };
  assert.deepEqual(A.fixes(noMarkers).map((x) => x.start), [4, 3]);
  // Already there: nothing to offer.
  assert.deepEqual(A.fixes({ ...noMarkers, start: 3, bestStart: 0 }), []);
  // Back to the file's first bar when the chart was moved for no reason.
  assert.deepEqual(A.fixes({ start: 5, fileBars: 16, sections: [{ status: 'chords' }], markers: [] }).map((x) => x.start), [1]);
});

test('lining a section up moves the whole chart by its shift, within the file', () => {
  assert.equal(A.startFor(1, fromBar1.sections[0], 16), 3);
  assert.equal(A.startFor(3, { match: {}, shift: -5 }, 16), 1);
  assert.equal(A.startFor(3, { match: {}, shift: 40 }, 16), 16);
  assert.equal(A.startFor(3, { shift: 2 }, 16), 3); // no file section paired: no move
  assert.equal(A.clampStart('7', 16), 7);
  assert.equal(A.clampStart(0, 16), 1);
});

test('what is left: a shift that follows from an earlier bar count is not counted twice', () => {
  assert.deepEqual(A.remaining(fromBar1, [{ fact: 'tempo' }, { fact: 'key' }], new Set(['tempo'])), { sections: 2, header: 1, total: 3 });
  assert.deepEqual(A.remaining({ sections: [{ status: 'ok' }] }, [], null), { sections: 0, header: 0, total: 0 });
  // The server's items count as drift will: where the chart starts is one,
  // on top of the first section's own bar count.
  assert.deepEqual(A.remaining(fromBar1, [], null, [{ fact: 'start' }, { fact: 'bars:1' }]), { sections: 2, header: 0, total: 2 });
});

test('the timeline: the chart above, the file below with its lead-in, on one bar axis', () => {
  const at3 = { ...fromBar1, start: 3 };
  const l = A.layout(at3);
  assert.equal(l.total, 16);
  assert.equal(l.lead, 2);
  assert.deepEqual(l.chart.map((c) => [c.label, c.x * 16, c.w * 16]), [['Verse', 2, 4], ['Chorus', 6, 4], ['Verse 2', 10, 4]]);
  assert.deepEqual(l.file.map((c) => [c.label, !!c.lead, c.x * 16, c.bars]), [['', true, 0, 2], ['Verse 1', false, 2, 4], ['Chorus', false, 6, 6], ['Verse 2', false, 12, 4]]);
  // A chart longer than the file widens the axis.
  assert.equal(A.layout({ start: 1, chartBars: 20, fileBars: 8, sections: [], markers: [] }).total, 20);
});

test('each bar the file plays is a bar of the chart: from gp_start, repeats played out', () => {
  // Written bars 1-2 lead-in, |: 3 4 :| twice, 5; the chart starts at the
  // file's bar 3 (gp_start: 3), so its gpOrder is the file from there.
  const masterBars = [{}, {}, { isRepeatStart: true }, { repeatCount: 2 }, {}];
  const played = GPFacts.played(masterBars);
  assert.deepEqual(played, [1, 2, 3, 4, 3, 4, 5]);
  const order = played.slice(2);
  assert.deepEqual(A.chartBarsForFile(order, played), [-1, -1, 0, 1, 2, 3, 4]);
});

test('a chart whose sections point into the file out of order still finds its bars', () => {
  // Chorus (written 9-10) played twice in the chart, once in the file.
  const order = [1, 2, 9, 10, 3, 4, 9, 10];
  const played = [1, 2, 3, 4, 9, 10];
  assert.deepEqual(A.chartBarsForFile(order, played), [0, 1, 4, 5, 6, 7]);
  assert.deepEqual(A.chartBarsForFile([], played), [-1, -1, -1, -1, -1, -1]);
});

test('the recording\'s bar times: from its sync points or section markers, else none', () => {
  assert.equal(A.barTimes([], 4, 2), null);
  // Markers at bars 0 and 4 (8.8 s): bars in between share the time.
  assert.deepEqual(A.barTimes([{ bar: 0, t: 1 }, { bar: 4, t: 9.8 }], 6, 2).map((x) => +x.toFixed(2)), [1, 3.2, 5.4, 7.6, 9.8, 11.8, 13.8]);
  // Before the first known bar: the nominal bar length.
  assert.deepEqual(A.barTimes([{ bar: 2, t: 6 }], 3, 2.5), [1, 3.5, 6, 8.5]);
  const sections = [{ index: 0, startBar: 0 }, { index: 1, startBar: 4 }];
  assert.deepEqual(A.knownBars({ markers: [{ sectionIndex: 1, timeSeconds: 10 }, { sectionIndex: 7, timeSeconds: 99 }] }, sections), [{ bar: 4, t: 10 }]);
  assert.deepEqual(A.knownBars({ barSyncPoints: [{ barIndex: 0, timeSeconds: 0.5 }], markers: [{ sectionIndex: 1, timeSeconds: 10 }] }, sections), [{ bar: 0, t: 0.5 }]);
  assert.deepEqual(A.knownBars(null, sections), []);
});

test('following the score: the recording\'s time, and the score\'s speed to keep with it', () => {
  const map = [-1, -1, 0, 1, 2];
  const times = [1, 3, 5.5, 8];
  assert.equal(A.follow(map, times, 0, 0.5, 2), null); // the file's lead-in
  assert.equal(A.follow(map, [1, 3, 5.5], 4, 0, 2), null); // past the chart's last timed bar
  const f = A.follow(map, times, 3, 0.5, 2);
  assert.equal(f.bar, 1);
  assert.equal(f.t, 4.25);
  assert.equal(f.ratio, 0.8); // the recording's bar lasts 2.5 s, the score's 2 s
  assert.equal(A.follow(map, null, 3, 0, 2), null);
});

test('key names, as the server writes them; a file left in C says nothing', () => {
  assert.equal(A.keyName({ sig: -1, minor: false }), 'F');
  assert.equal(A.keyName({ sig: 3, minor: true }), 'F#m');
  assert.equal(A.keyName({ sig: -3, minor: true }), 'Cm');
  assert.equal(A.keyName({ sig: 0, minor: true }), 'Am');
  assert.equal(A.keyName({ sig: 0, minor: false }), '');
  assert.equal(A.keyName(null), '');
});

// The dock playing a recording (its bars the chart's) and the Score view's
// cursor and the MIDI on the file's played bars: one place to the other.
test('the chart’s bars and the file’s, both ways', () => {
  // gp_start 3: the chart's bar 0 is the file's played bar 2.
  const map = A.chartBarsForFile([3, 4, 5], [1, 2, 3, 4, 5]);
  assert.deepEqual(map, [-1, -1, 0, 1, 2]);
  const inv = A.fileBarsForChart(map, 3);
  assert.deepEqual(inv, [2, 3, 4]);
  assert.equal(A.toFile(inv, 1.5), 3.5);
  assert.equal(A.toChart(map, 3.5), 1.5);
  assert.equal(A.toChart(map, 0.5), null); // the file's lead-in: no chart bar
  assert.equal(A.toFile(inv, 3), null); // past the chart
  // A repeat the chart writes out: each chart bar its own pass.
  const rep = A.chartBarsForFile([1, 2, 1, 2], [1, 2, 1, 2]);
  assert.deepEqual(A.fileBarsForChart(rep, 4), [0, 1, 2, 3]);
  // A "lead:" the file doesn't play (gpOrder 0): no file bar for it.
  const lead = A.chartBarsForFile([0, 1, 2], [1, 2]);
  assert.deepEqual(A.fileBarsForChart(lead, 3), [-1, 0, 1]);
  assert.equal(A.toFile(A.fileBarsForChart(lead, 3), 0.5), null);
});

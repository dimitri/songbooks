// lead-time.js — where the song's "lead:" bars fall in a recording, for
// the player (audio-player.js), as plain functions its tests can call
// (jstest/lead-time.test.mjs).
//
// The lead is the song's, before its first section: exactly N bars ending
// where the first section starts (bar N of the grid), each as long as the
// first section's bars are played (its measured pace, else the chart's
// tempo). The recording's time before the lead is no bar at all: nothing
// numbered, ticked or clicked there.
(function (root) {
  // grid: [time of bar 0, bar 1, …] in the recording; lead: N; pace: the
  // seconds of one lead bar. Returns the grid with its first N times
  // placed back from bar N. A lead longer than the time before bar N is
  // squeezed into it, from 0:00; with bar N at 0:00 or before (no time
  // at all), the lead stays before the recording, counted in.
  function placeLead(grid, lead, pace) {
    const n = Math.floor(lead) || 0;
    if (n <= 0 || !grid || grid.length <= n || !(pace > 0)) return grid;
    const out = grid.slice();
    const first = out[n];
    const start = first - n * pace;
    for (let b = 0; b < n; b++) {
      out[b] = start >= 0 || first <= 0 ? first - (n - b) * pace : (first * b) / n;
    }
    return out;
  }

  // The pace of the first section's bars: the seconds per bar from bar
  // lead to the end of its count bars (or as far as the grid goes); 0
  // when the grid can't tell.
  function firstPace(grid, lead, count) {
    const n = Math.floor(lead) || 0;
    const k = Math.min(Math.max(1, count || 1), (grid ? grid.length : 0) - 1 - n);
    if (k < 1) return 0;
    const p = (grid[n + k] - grid[n]) / k;
    return p > 0 ? p : 0;
  }

  // The bars a stretch of the recording counts: none before bar 0 (the
  // lead's first, or the first section's), whatever the grid says there.
  const firstCounted = (bar) => Math.max(0, Math.floor(bar));

  const api = { placeLead, firstPace, firstCounted };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.SongbooksLeadTime = api;
})(typeof window !== 'undefined' ? window : globalThis);

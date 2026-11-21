// bar-rows.js — how many bars each row of the Song view's chart holds,
// as plain functions, for song.js (reflowStaffLines) and its tests
// (internal/webassets/jstest/bar-rows.test.mjs, run by go test).
//
// The lead-sheet convention: rows of 4-bar phrases (perRow), aligned from
// the section's first bar. A pickup bar rides on the first row on top of
// its count. What's left over at the end:
//   - 1 bar joins the row before it (5 → one row of 5, 9 → 4 + 5), as long
//     as that row can still hold it at the bars' legibility floor (fit);
//   - 2 or 3 bars make a short last row (10 → 4 + 4 + 2) whose bars are as
//     wide as a full row's, its staff ending at its last bar.
// A section whose author wrote "bars:" keeps the author's rows, each one
// split the same way only when it holds more than a screen row does.
(function (root) {
  // The bar count of each row for n bars, perRow to a row. fitFirst/fit:
  // the most bars the first row / any other row can hold.
  function sizes(n, perRow, fitFirst, fit) {
    const P = Math.max(1, Math.floor(perRow) || 1);
    const out = [];
    for (let rest = n; rest > 0; rest -= P) out.push(Math.min(P, rest));
    const k = out.length;
    if (k > 1 && P > 1 && out[k - 1] === 1) {
      const room = k === 2 ? fitFirst : fit;
      if (P + 1 <= room) {
        out[k - 2] += 1;
        out.pop();
      }
    }
    return out;
  }

  // The rows of one section's chart: [{bars, cap}], in order. bars is the
  // row's bar count, its pickup included; cap is how many bars wide each of
  // its (regular) bars is drawn (--cap): a full row's own count, so it
  // fills the line, and perRow for a short row, so its bars keep a full
  // row's width.
  //   rows: the bar counts of the server's rows (a pickup counted in the first)
  //   o.perRow:   bars to a row (4)
  //   o.pickup:   true when the section opens with the song's pickup bar
  //   o.explicit: true when the author wrote "bars:"
  //   o.fit, o.fitFirst: the most regular bars a row / the first row (less
  //     the clef and the pickup) can hold at the legibility floor
  function plan(rows, o) {
    const counts = (rows || []).filter((r) => r > 0);
    if (!counts.length) return [];
    const P = Math.max(1, Math.floor(o.perRow) || 1);
    const pickup = o.pickup ? 1 : 0;
    const fit = o.fit == null ? Infinity : o.fit;
    const fitFirst = o.fitFirst == null ? fit : o.fitFirst;
    if (!o.explicit) {
      const n = counts.reduce((a, b) => a + b, 0) - pickup;
      const s = sizes(n, P, fitFirst, fit);
      if (!s.length) return [{ bars: pickup, cap: 1 }];
      return s.map((b, i) => ({ bars: b + (i === 0 ? pickup : 0), cap: b >= P ? b : P }));
    }
    // The author's rows, the pickup counted as one of the first row's bars
    // the way "bars:" counts it. A row only gets split when it holds more
    // than perRow; its bars are as wide as the author's longest row's.
    const Pe = Math.min(P, Math.max(...counts));
    const out = [];
    counts.forEach((r, i) => {
      sizes(r, Pe, i === 0 ? fitFirst + pickup : fit, fit).forEach((b, j) => {
        const pk = i === 0 && j === 0 ? pickup : 0;
        out.push({ bars: b, cap: b >= Pe ? Math.max(1, b - pk) : Pe });
      });
    });
    return out;
  }

  const api = { sizes, plan };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.SongbooksBarRows = api;
})(typeof window !== 'undefined' ? window : globalThis);

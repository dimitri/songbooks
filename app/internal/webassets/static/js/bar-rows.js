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
// split the same way only when it holds more than a screen row does; a
// lone bar such a split leaves where it can't join its row shares the row
// before it instead (bars: 5 on a phone: 3 + 2, not 4 + 1).
//
// pdfnative/bar_rows.go is this file's Go twin, for the PDF: both are
// checked against one table, jstest/bar-rows-cases.json.
(function (root) {
  // The bar count of each row for n bars, perRow to a row. fitFirst/fit:
  // the most bars the first row / any other row can hold. balance: a lone
  // last bar that can't join its row takes one from it (4 + 1 → 3 + 2).
  function sizes(n, perRow, fitFirst, fit, balance) {
    const P = Math.max(1, Math.floor(perRow) || 1);
    const out = [];
    for (let rest = n; rest > 0; rest -= P) out.push(Math.min(P, rest));
    const k = out.length;
    if (k > 1 && P > 1 && out[k - 1] === 1) {
      const room = k === 2 ? fitFirst : fit;
      if (P + 1 <= room) {
        out[k - 2] += 1;
        out.pop();
      } else if (balance && out[k - 2] >= 3) {
        out[k - 2] -= 1;
        out[k - 1] = 2;
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
  //     (or how many pickups it opens with)
  //   o.leads:    how many pickups it opens with — a "lead:" line's bars,
  //     which "bars:" doesn't count: they ride on the first row on top of it
  //   o.explicit: true when the author wrote "bars:"
  //   o.fit, o.fitFirst: the most regular bars a row / the first row (less
  //     the clef and the pickup) can hold at the legibility floor
  function plan(rows, o) {
    const counts = (rows || []).filter((r) => r > 0);
    if (!counts.length) return [];
    const P = Math.max(1, Math.floor(o.perRow) || 1);
    const pickup = o.leads > 0 ? Math.floor(o.leads) : typeof o.pickup === 'number' ? Math.max(0, o.pickup) : (o.pickup ? 1 : 0);
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
    // Several pickups (a "lead:" line) aren't in the author's count: the
    // first row is split without them, then they ride on its first piece.
    if (pickup > 1) {
      const own = counts.map((r, i) => (i === 0 ? Math.max(0, r - pickup) : r));
      const Pl = Math.min(P, Math.max(1, ...own));
      const res = [];
      own.forEach((r, i) => {
        const s = r > 0 ? sizes(r, Pl, i === 0 ? fitFirst : fit, fit, true) : [0];
        s.forEach((b, j) => {
          const pk = i === 0 && j === 0 ? pickup : 0;
          res.push({ bars: b + pk, cap: b >= Pl ? Math.max(1, b) : Pl });
        });
      });
      return res;
    }
    const Pe = Math.min(P, Math.max(...counts));
    const out = [];
    counts.forEach((r, i) => {
      sizes(r, Pe, i === 0 ? fitFirst + pickup : fit, fit, true).forEach((b, j) => {
        const pk = i === 0 && j === 0 ? pickup : 0;
        out.push({ bars: b, cap: b >= Pe ? Math.max(1, b - pk) : Pe });
      });
    });
    return out;
  }

  // A row whose chord labels collide (song.js' resolveLabelCollisions)
  // splits in halves, the first the longer: [{bars, cap}], both rows'
  // bars as wide as the longer one's. A 4-bar phrase reads as 2 + 2, a
  // 5-bar row as 3 + 2 — never 3 + 1, a bar alone on its row; only a
  // 3-bar row can't avoid it (2 + 1), and 1 bar can't be split.
  function split(n) {
    if (n <= 1) return [{ bars: n, cap: n }];
    const a = Math.ceil(n / 2);
    return [{ bars: a, cap: a }, { bars: n - a, cap: a }];
  }

  const api = { sizes, plan, split };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.SongbooksBarRows = api;
})(typeof window !== 'undefined' ? window : globalThis);

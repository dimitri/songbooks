// Unified song page: rendering/playback (formerly play.js) + structure
// editor + raw editor/importers (formerly prep's scripts), merged behind a
// single "Edit" toggle so editing happens in place, not on a separate page.
// Same in-house icon set as templates/icons.html, duplicated here because
// these particular buttons are built client-side (structure/matrix editor
// rows) rather than server-rendered — kept visually identical on purpose.
const ICONS = {
  up: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12l5-5 5 5"/></svg>',
  down: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 8l5 5 5-5"/></svg>',
  duplicate: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"><rect x="3.5" y="5.5" width="9" height="9" rx="1.2"/><path d="M7 5.5V4a1.2 1.2 0 0 1 1.2-1.2h6.3A1.2 1.2 0 0 1 15.7 4v6.3A1.2 1.2 0 0 1 14.5 11.5H13"/></svg>',
  close: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M5 5l10 10M15 5L5 15"/></svg>',
  grip: '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><circle cx="7" cy="5" r="1.3"/><circle cx="13" cy="5" r="1.3"/><circle cx="7" cy="10" r="1.3"/><circle cx="13" cy="10" r="1.3"/><circle cx="7" cy="15" r="1.3"/><circle cx="13" cy="15" r="1.3"/></svg>',
  plus: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M10 4v12M4 10h12"/></svg>',
};

(function () {
  const editorElForFork = document.getElementById('editor');
  const pageVariantId = Number(editorElForFork?.dataset.variantId) || 0;
  const pageCanEdit = editorElForFork?.dataset.canEdit !== 'false';

  // ---- Clock / metronome ---------------------------------------------------
  const transportEl = document.getElementById('transport');
  const bpm = parseFloat(transportEl.dataset.bpm) || 120;
  const beatsPerBar = parseInt(transportEl.dataset.beatsPerBar, 10) || 4;
  const clock = new Clock(bpm, beatsPerBar);
  const metronome = new Metronome(clock);

  const sheet = document.getElementById('sheet');
  let bars = [];
  let barsByPlayed = new Map(); // played bar -> its bar elements (refreshBarsCache)
  let sections = [];

  // ---- Chart reflow: real per-viewport line wrapping ------------------
  // Each .staff-line is server-rendered at the section's own declared
  // bars-per-line ("bars: N", default 4 — the same hint the PDF/print
  // pipeline uses to decide its line breaks). That's a fine width for a
  // print page or a wide screen, but it's a FIXED bar count, chosen with
  // no idea what viewport will actually render it — on a phone it's
  // routinely too wide. The row itself is a plain flex (nowrap) of
  // fixed-width bars (each bar's width floored at a real minimum
  // readable size, see .bar's own CSS comment), so once that floor pushes
  // the row past the viewport it used to just run off the right edge —
  // the "D bar clipped" bug. CSS alone can't reflow it: each bar needs
  // its own 5-line staff drawn behind it (one shared ::before per ROW,
  // not per bar — see .staff-line::before), so a plain flex-wrap would
  // detach whichever bars wrapped onto a second visual line from any
  // staff at all. Splitting the row into more, narrower *rows* — real
  // siblings, each a proper .staff-line with its own staff background —
  // is what actually reflows it, the same way the PDF pipeline already
  // picks a bars-per-line that fits ITS page width, just recomputed here
  // for the actual viewport instead of a print page.
  //
  // staffLineOriginals holds one pristine, NEVER-mutated clone per
  // server-rendered .staff-line, captured fresh every time the sheet's
  // own HTML changes (rebindSheet, below). Every reflow pass rebuilds
  // from these clones rather than re-splitting whatever the previous
  // pass left behind — so repeated resizes (narrower, then wider again,
  // e.g. a phone rotation) never compound drift or lose bars, and always
  // converge on the same layout a fresh load at that width would produce.
  let staffLineOriginals = null;
  function captureStaffLineOriginals() {
    // A TabOnly grid's real content is an .alphatab-row, not a
    // .staff-line — this system has no model of alphaTab's own live SVG
    // and nothing useful to reflow it into. Excluding it here (not just
    // giving it an empty `lines` array) keeps reflowStaffLines()'s loop
    // below from ever reaching it, so it never calls replaceChildren()
    // on this grid and never wipes the real alphaTab row — confirmed
    // live as the cause of both the Jazz-chart-empty regression and the
    // disconnected-duplicate-grid bug: every reflow (any mode switch or
    // resize) rebuilt this grid's children from whatever it had
    // captured as `.staff-line`, which never included the alphaTab row
    // itself. CSS alone handles showing/hiding .alphatab-row vs
    // .tab-only-chords-fallback per mode, and the fallback's own
    // flex-wrap handles its jazz-mode wrapping — neither needs this JS
    // reflow pass at all.
    staffLineOriginals = [...sheet.querySelectorAll('.grid.spans-full')]
      .filter(grid => !grid.querySelector(':scope > .alphatab-row'))
      .map(grid => ({
      grid,
      lines: [...grid.querySelectorAll(':scope > .staff-line')].map(line => {
        const barEls = [...line.querySelectorAll(':scope > .bar')];
        // The densest bar in this row sets its own needed min-width — see
        // the matching CSS comment on .bar's min-width formula, which this
        // mirrors exactly (max(2.6em, slots * 0.9em)) so a busier bar (more
        // mid-bar chord changes, more --slots) never gets squeezed by a
        // budget sized for a plainer one.
        const maxSlots = barEls.reduce((m, b) => Math.max(m, parseInt(b.style.getPropertyValue('--slots'), 10) || 3), 3);
        return {
          cap: parseInt(line.style.getPropertyValue('--cap'), 10) || 4,
          hasClef: !!line.querySelector(':scope > .staff-timesig-cell'),
          maxSlots,
          template: line,
        };
      }),
    }));
  }
  // Song Map's own pristine original — captured the same way as
  // staffLineOriginals, but flattened across every server-rendered
  // .staff-line into ONE ordered stream (clef cell, if any, first;
  // every real .bar and .bar-recall cell in document order after it).
  // Song Map's whole point is bars flowing continuously across section
  // boundaries on the same row, so there's no per-section grid to key
  // this off the way captureStaffLineOriginals does — one flat list for
  // the whole song is the right shape to rebuild rows from.
  let mapScoreOriginal = null;
  function captureMapScoreOriginal() {
    const mapScore = sheet.querySelector('.map-score');
    mapScoreOriginal = mapScore ? [...mapScore.querySelectorAll(':scope > .staff-line > *')] : null;
  }
  // Song Map's reflow pass — same capacity math as reflowStaffLines below
  // (chords+lyrics), reused rather than reimplemented, just applied to one
  // continuous cell stream instead of independent per-section grids. The
  // server pre-groups .MapLines at a fixed 8-cells/line (buildMapLines,
  // handlers_pages.go) purely as a reasonable initial HTML shape; this
  // always rebuilds fresh from mapScoreOriginal against the REAL viewport
  // width, the same "never trust what a previous pass left behind"
  // approach reflowStaffLines uses for staffLineOriginals. Previously Song
  // Map had NO per-viewport reflow at all — just a fixed server row count
  // plus a CSS flex-wrap fallback on narrow screens, which is what
  // produced rows of visibly different lengths (no justification) and,
  // once a recall-only row didn't fit, one cell per wrapped line instead
  // of a packed continuous sheet.
  function reflowMapLines() {
    if (!mapScoreOriginal || sheet.dataset.renderMode !== 'map') return;
    const mapScore = sheet.querySelector('.map-score');
    if (!mapScore) return;
    const availW = mapScore.clientWidth;
    if (!availW) return;
    const fontPx = parseFloat(getComputedStyle(sheet).fontSize) || 16;
    const clefPx = 2.5 * fontPx;
    const flatFloorEm = 7.2; // same floor reflowStaffLines uses for normal mode
    const maxSlots = mapScoreOriginal.reduce((m, el) => {
      if (!el.classList.contains('bar')) return m;
      return Math.max(m, parseInt(el.style.getPropertyValue('--slots'), 10) || 3);
    }, 3);
    const minBarPx = Math.max(flatFloorEm * fontPx, maxSlots * 0.9 * fontPx);
    const hasClef = mapScoreOriginal[0]?.classList.contains('staff-timesig-cell');
    const budget = hasClef ? availW - clefPx : availW;
    let cap = Math.max(1, Math.floor(budget / minBarPx));
    // Same 4-bars/line floor Chords+lyrics falls back to once bars would
    // otherwise shrink past a comfortable width on a narrow phone — bars
    // shrink (a row-specific --bar-w) rather than a row holding fewer of
    // them below this floor.
    let lineBarWpx = null;
    if (cap < 4) {
      cap = 4;
      lineBarWpx = Math.max(2.6 * fontPx, budget / cap);
    }
    const cells = mapScoreOriginal.filter(el => !el.classList.contains('staff-timesig-cell'));
    const newLines = [];
    let current = [];
    let isFirstRow = true;
    const flushRow = (isLast) => {
      if (!current.length) return;
      const line = document.createElement('div');
      line.className = 'staff-line';
      // A short final row keeps its OWN (smaller) count as --cap so its
      // bars stretch to fill the row's full width, same justified
      // "every system fills the page" look as every other row, instead
      // of leaving blank staff behind a handful of flat-width bars.
      line.style.setProperty('--cap', String(isLast && current.length < cap ? current.length : cap));
      if (lineBarWpx) line.style.setProperty('--bar-w', lineBarWpx + 'px');
      if (isFirstRow && hasClef) line.appendChild(mapScoreOriginal[0].cloneNode(true));
      for (const cell of current) line.appendChild(cell.cloneNode(true));
      newLines.push(line);
      isFirstRow = false;
      current = [];
    };
    for (let i = 0; i < cells.length; i++) {
      current.push(cells[i]);
      if (current.length >= cap) flushRow(false);
    }
    flushRow(true);
    mapScore.replaceChildren(...newLines);
    resolveLabelCollisions(mapScore);
  }
  function reflowStaffLines() {
    if (!staffLineOriginals) return;
    // Resolved from the live em value at #sheet's CURRENT font-size —
    // jazz mode's own fit search (applyPageBox, below) can still shrink
    // that font further; this just runs first, at whatever size is
    // current when it's called, which is good enough to keep every row
    // on-screen even before that search adjusts anything. The clef/
    // time-sig cell (only ever on the song's very first row) has a fixed
    // 2.1em + 0.4em-margin footprint — see the matching CSS comment on
    // .staff-line:has(.staff-timesig-cell) .bar — subtracted from that
    // one row's own budget so it's never squeezed out by its neighbors.
    const fontPx = parseFloat(getComputedStyle(sheet).fontSize) || 16;
    const clefPx = 2.5 * fontPx;
    // The server groups every mode's rows at ONE shared target (8 bars/
    // line, iReal Pro's denser convention, for jazz mode) because all
    // modes render from the same server-sent Lines; each mode regroups
    // them here, against the real width: jazz by its bars' real widths,
    // the others in 4-bar phrases (below). Song Map doesn't go through this
    // per-section pass at all — it renders its own continuous score
    // (.map-score, reflowMapLines above); the per-section grids stay in
    // the DOM there but hidden (display:none), so their clientWidth is 0
    // and the loop below skips them on its own.
    for (const { grid, lines: originalLines } of staffLineOriginals) {
      if (!grid.isConnected) continue;
      const availW = grid.clientWidth;
      if (!availW) continue; // hidden in this mode (e.g. a compact section's unused grid) — nothing to reflow
      const newLines = [];
      // Jazz mode draws its own chart (.jazz-chart): these grids are hidden
      // there (clientWidth 0, skipped above).
      // Every other mode: rows of 4-bar phrases, decided per SECTION (its
      // server rows pooled back into one run of bars) by bar-rows.js — the
      // server's own 8-bars/row chunks are only the page's first shape,
      // and splitting each of them on its own broke a section's phrasing
      // wherever its bar count wasn't a multiple of 8 (10 bars → 8 + 2 →
      // 4 + 4 + 2 was fine, but 9 → 8 + 1 → 4 + 4 + 1, and a 5-bar Intro
      // came out as 4 bars and then 1 bar stretched across the line). A
      // section whose author wrote "bars:" (data-bars="author") keeps the
      // author's rows; bar-rows.js only splits one that holds more than a
      // screen row. A lone last bar joins the row before it (5 → 5,
      // 9 → 4 + 5) where that row can still hold it at the bars' legibility
      // floor; a last row of 2 or 3 stays short, its bars as wide as a full
      // row's (--cap 4) and its staff ending at its last bar (the staff is
      // drawn per bar, .bar-body::before, so no blank staff follows it).
      //
      // 4 to a row whatever the width (a phone squeezes its bars, a
      // per-row --bar-w, rather than holding fewer of them): the
      // lead-sheet phrase, the PDF's own default too. The flat 7.2em is the
      // typeset bar width lyrics-only mode draws (.bar's --bar-w, app.css):
      // a row whose bars don't fit at it gets a narrower --bar-w, floored
      // at 2.6em (.bar's min-width). Whether a lone last bar still fits on
      // the row before it is decided against what a bar's content really
      // needs: its beat-slots' 0.9em each plus its own 1em of padding
      // (.bar's min-width leaves the padding out, so a row squeezed to it
      // spills its slashes into the next bar); where it doesn't — a phone,
      // or the first row beside the clef and a pickup — the bar stays a
      // short row of its own.
      const PHRASE = 4;
      const allBars = originalLines.flatMap(l => [...l.template.querySelectorAll(':scope > .bar')]);
      if (!allBars.length) { grid.replaceChildren(...originalLines.map(l => l.template.cloneNode(true))); continue; }
      const hasClef = originalLines[0].hasClef;
      const clefEl = hasClef ? originalLines[0].template.querySelector(':scope > .staff-timesig-cell') : null;
      const maxSlots = originalLines.reduce((m, l) => Math.max(m, l.maxSlots), 3);
      // The song's pickup bar, or its "lead:" bars (as many as it has).
      let pickup = 0;
      while (pickup < allBars.length && allBars[pickup].classList.contains('bar-pickup')) pickup++;
      const minBarPx = Math.max(7.2 * fontPx, maxSlots * 0.9 * fontPx);
      const floorPx = Math.max(2.6, maxSlots * 0.9 + 1) * fontPx;
      const firstBudget = availW - (hasClef ? clefPx : 0) - pickup * 3.4 * fontPx;
      const plan = window.SongbooksBarRows.plan(
        originalLines.map(l => l.template.querySelectorAll(':scope > .bar').length),
        {
          perRow: PHRASE,
          pickup,
          explicit: grid.dataset.bars === 'author',
          fit: Math.floor(availW / floorPx),
          fitFirst: Math.floor(firstBudget / floorPx),
        },
      );
      let i = 0;
      plan.forEach(({ bars: size, cap }, row) => {
        const chunk = allBars.slice(i, i + size);
        i += size;
        const line = document.createElement('div');
        line.className = 'staff-line';
        // --cap counts the row's regular bars: a pickup has its own narrow
        // width (.bar-pickup, app.css), so the bars next to it stay as wide
        // as every other row's.
        // A short first row beside the clef and pickups (bars: 4,5 on a
        // phone: the pickups + 2 bars) keeps the other rows' bar width when
        // it has the room, rather than squeezing its bars into what a full
        // row's would get beside them: its --cap is scaled so that
        // (row - clef - pickups) / --cap is the line's width / cap.
        let capCss = cap;
        const head = availW - firstBudget;
        if (row === 0 && pickup > 0 && size - pickup < cap && (size - pickup) * (availW / cap) + head <= availW) {
          capCss = Math.round((cap * (availW - head) / availW) * 1000) / 1000;
        }
        line.style.setProperty('--cap', String(capCss));
        if (row === 0 && pickup > 1) line.style.setProperty('--pickups', String(pickup));
        const budget = row === 0 ? firstBudget : availW;
        if (capCss * minBarPx > budget) line.style.setProperty('--bar-w', Math.max(2.6 * fontPx, budget / capCss) + 'px');
        if (row === 0 && clefEl) line.appendChild(clefEl.cloneNode(true));
        for (const bar of chunk) line.appendChild(bar.cloneNode(true));
        newLines.push(line);
      });
      grid.replaceChildren(...newLines);
      resolveLabelCollisions(grid);
    }
  }
  // A .bar's own min-width (above) is a GEOMETRIC floor — wide enough for
  // its beat-slots' slash marks — not a guarantee the actual CHORD TEXT
  // sitting above them (position:absolute, so it never contributes to
  // flex sizing — see .bar's own CSS comment) fits without touching its
  // neighbor: a couple of two-letter labels ("Cm", "F#") on adjacent
  // slots can still collide at a bar width the slot-count math alone says
  // is fine. jazz mode already has to solve exactly this (applyPageBox's
  // own labelsCollide, measuring real rendered label rects) — same check,
  // reused here as a correctness backstop for every mode: find the first
  // row whose labels actually touch, give it one fewer bar than it has
  // now, and recheck, same "detect → shrink → recheck" shape as jazz
  // mode's own search, until nothing collides (or a row is down to a
  // single bar, the same place jazz mode gives up and falls back to
  // letting content be what it is rather than looping forever).
  function lineLabelsCollide(line) {
    let prevRight = null;
    for (const el of line.querySelectorAll('.chord-label')) {
      const r = el.getBoundingClientRect();
      if (prevRight != null && r.left < prevRight - 0.5) return true;
      prevRight = r.right;
    }
    return false;
  }
  function resolveLabelCollisions(grid) {
    let guard = 0;
    while (guard++ < 40) {
      const line = [...grid.querySelectorAll(':scope > .staff-line')].find(lineLabelsCollide);
      if (!line) return;
      const barEls = [...line.querySelectorAll(':scope > .bar')];
      const curCap = parseInt(line.style.getPropertyValue('--cap'), 10) || barEls.length;
      // A pickup rides on the first half: it has its own narrow width and
      // isn't counted in --cap.
      let pickup = 0;
      while (pickup < barEls.length && barEls[pickup].classList.contains('bar-pickup')) pickup++;
      if (barEls.length - pickup <= 1) {
        // A lone bar (already the shortest possible row — typically a
        // short trailing/volta row) can't be split further, but it may
        // still be rendered NARROWER than the row it's in: --cap usually
        // still carries the section's full per-line bar count here (kept
        // deliberately, elsewhere, so a short row's bar doesn't stretch
        // wider than every other bar and read as a different-length
        // measure — see .bar's own CSS comment). That tradeoff stops
        // applying once this is the collision-of-last-resort case: a
        // single bar genuinely needs its own full row width before
        // there's truly nothing left to try. Only once --cap already
        // matches its own bar count (full width, no narrowing left to
        // undo) is this really the end of the line.
        const own = barEls.length - pickup;
        if (curCap > own) { line.style.setProperty('--cap', String(Math.max(1, own))); continue; }
        return;
      }
      const clefEl = line.querySelector(':scope > .staff-timesig-cell');
      // Split in halves (bar-rows.js' split): a 4-bar phrase as 2 + 2, a
      // 5-bar row as 3 + 2 — taking one bar off the end (3 + 1) left a bar
      // alone on its row, the very thing the rows avoid elsewhere. Each
      // half recollides on its own if it still has to.
      let at = 0;
      // Moves the live bar nodes (not clones) into the replacement rows —
      // `line` itself is discarded right after, so there's no need to
      // leave its children intact.
      const replacement = window.SongbooksBarRows.split(barEls.length - pickup).map(({ bars: size, cap }, gi) => {
        const n = size + (gi === 0 ? pickup : 0);
        const g = barEls.slice(at, at + n);
        at += n;
        const l = document.createElement('div');
        l.className = 'staff-line';
        // The shorter half (5 → 3 + 2) keeps the longer one's cap, its
        // bars as wide as its sibling's, the same as reflowStaffLines' own
        // short last row — the staff is drawn per bar (.bar-body::before),
        // so the row simply ends at its last bar instead of stretching
        // that bar across the line.
        l.style.setProperty('--cap', String(cap));
        if (gi === 0 && pickup > 1) l.style.setProperty('--pickups', String(pickup));
        if (gi === 0 && clefEl) l.appendChild(clefEl);
        for (const b of g) l.appendChild(b);
        return l;
      });
      line.replaceWith(...replacement);
    }
  }

  // reflowStaffLines() clones and replaces every .bar element it touches
  // (see its own comment), which leaves `bars` — the cache highlightTick()
  // walks every tick to find/mark the currently-playing one — pointing at
  // now-detached nodes. A plain resize (same content, new viewport) only
  // needs this refreshed, not a full rebindSheet(): `sections` and
  // everything rebindSheet() otherwise rebuilds live outside the bar grid
  // itself (the .sheet-section wrapper, not its contents) and stay valid.
  function refreshBarsCache() {
    bars = [...sheet.querySelectorAll('.bar, .jz-bar[data-global-bar]')];
    barsByPlayed = new Map();
    for (const b of bars) {
      // A drawn bar is read on every pass through a repeat (or a D.S.,
      // in the Jazz chart), an ending on the pass that takes it, and on
      // those of every later section replaying its grid without drawing
      // it (a bare "Verse 2"); a Map recall cell on each of its section's
      // bars: data-played lists them (playedbars.go). A bar without one
      // is read at its own.
      const list = b.dataset.played ? b.dataset.played.split(' ') : [b.dataset.globalBar];
      for (const v of list) {
        const n = parseInt(v, 10);
        if (Number.isNaN(n)) continue;
        if (!barsByPlayed.has(n)) barsByPlayed.set(n, []);
        barsByPlayed.get(n).push(b);
      }
    }
  }

  // Rebind DOM references after #sheet's innerHTML changes (live preview
  // while editing, or restoring the saved view when closing the editor) —
  // a fresh innerHTML means fresh nodes, so cached references go stale.
  function rebindSheet() {
    captureStaffLineOriginals();
    reflowStaffLines();
    captureMapScoreOriginal();
    reflowMapLines();
    refreshBarsCache();
    // startBar/barCount come from the server (every section's real
    // position in the performed bar sequence — see StartBar's Go-side doc
    // comment), not from "does this section happen to have a rendered
    // .bar element" — a compact/reference section never has one (it's
    // just a chip + multi-rest bar) but still really takes barCount bars
    // to play, and used to be entirely invisible to the clock because of
    // that: no jump target, and the metronome silently drifted out of
    // sync with the chart the first time it played through one.
    sections = [...sheet.querySelectorAll('.sheet-section')].map(el => {
      const startBar = parseInt(el.dataset.startBar, 10);
      const barCount = parseInt(el.dataset.barCount, 10) || 0;
      return {
        el, index: parseInt(el.dataset.sectionIndex, 10),
        startBar: Number.isNaN(startBar) ? null : startBar,
        barCount,
      };
    });
    metronome.sectionStartBeats = new Set(
      sections.filter(s => s.startBar != null).map(s => s.startBar * beatsPerBar)
    );
    sheet.querySelectorAll('.section-jump').forEach(btn => {
      const sec = sections[parseInt(btn.dataset.sectionIndex, 10)];
      btn.disabled = !sec || sec.startBar == null || sec.barCount === 0;
      if (btn.disabled) btn.title = t('no chord grid in this section yet — jump unavailable');
      btn.onclick = () => jumpToSection(parseInt(btn.dataset.sectionIndex, 10));
    });
    document.querySelectorAll('.structure-seg').forEach(btn => {
      btn.onclick = () => jumpToSection(parseInt(btn.dataset.sectionIndex, 10));
    });
    // The Jazz chart's rehearsal marks: play from that section's bar.
    sheet.querySelectorAll('.jz-rm[data-jump-bar]').forEach(btn => {
      btn.onclick = (e) => { e.stopPropagation(); jumpToBar(parseInt(btn.dataset.jumpBar, 10)); };
    });
    fitJazzChords();
    // New DOM from a raw-text save/preview starts back at the untransposed
    // pitch classes the server rendered — reapply whatever transpose was
    // already dialed in (see the Transpose section below, which sets this
    // hook once it's wired up).
    reapplyTranspose();
  }
  let reapplyTranspose = () => {};

  function jumpToSection(i) {
    const sec = sections[i];
    if (!sec || sec.startBar == null) return;
    jumpToBar(sec.startBar);
  }
  function jumpToBar(bar) {
    if (Number.isNaN(bar)) return;
    // Going somewhere on purpose: the page follows the music again.
    followHeld = false;
    // With a reference recording, play it from there (audio-player.js)
    // rather than starting the silent follow-along clock.
    if (window.songbooksReferenceSeekBar && window.songbooksReferenceSeekBar(bar)) return;
    // No count-in, no click — the in-page quick player is always "no
    // click" now (see metronome.enabled = false above), so a reader
    // clicking a section to follow along moves there immediately instead
    // of waiting through an un-asked-for count-in.
    clock.seekToBar(bar);
    clock.start();
    metronome.start();
    playPauseBtn.innerHTML = iconPause;
    playPauseBtn.title = t('pause');
    playPauseBtn.classList.add('active');
  }

  // Scroll only when el leaves the comfortable middle of the viewport,
  // bringing it to a quarter from the top: the next rows stay readable.
  window.songbooksKeepInView = function keepInView(el) {
    const r = el.getBoundingClientRect();
    if (r.top >= 0 && r.bottom <= window.innerHeight * 0.8) return;
    window.scrollBy({ top: r.top - window.innerHeight * 0.25, behavior: 'smooth' });
  };
  const keepInView = window.songbooksKeepInView;

  // ---- Follow the music ----------------------------------------------------
  // While the recording (or the follow-along clock) plays, the chart marks
  // where it is and scrolls along with it; in Lyrics only, line by line,
  // karaoke style. That is only as good as the sync behind it: a detected
  // downbeat that's a bar off, or section markers set by hand a little
  // early, light the wrong line and pull the page away from where the
  // singer is. So:
  // - Lyrics only has a "Follow the music" switch (#follow-bar, play.html),
  //   remembered per song in this browser: off, no line or section is lit
  //   and nothing scrolls; the singer scrolls, the recording still plays.
  // - In every view, scrolling by hand while it plays takes the page back
  //   (as Apple Music's and Spotify's lyrics do): following holds until
  //   "Back to the current line" or the next time playback starts.
  const lyricsFollowKey = `songbooks:lyrics-follow:${sheet.dataset.variantId || pageVariantId}`;
  let lyricsFollow = true;
  try { lyricsFollow = localStorage.getItem(lyricsFollowKey) !== 'off'; } catch (e) { /* on */ }
  let followHeld = false; // the reader scrolled away while it played
  let wasPlaying = false;
  let playingNow = false;
  const followBar = document.getElementById('follow-bar');
  const followToggle = document.getElementById('follow-toggle');
  const followLabel = document.getElementById('follow-label');
  const followHint = document.getElementById('follow-hint');
  const followBack = document.getElementById('follow-back');
  const followBackLabel = document.getElementById('follow-back-label');
  const lyricsMode = () => sheet.dataset.renderMode === 'lyrics';
  // The chart lights and follows the music: always, except Lyrics only
  // with its switch off.
  const chartFollows = () => !(lyricsMode() && !lyricsFollow);
  function renderFollow() {
    const on = lyricsFollow;
    if (followToggle) followToggle.checked = on;
    if (followLabel) followLabel.textContent = t('Follow the music');
    if (followHint) {
      followHint.textContent = on
        ? t('The line being sung lights up and the page scrolls with the recording.')
        : t('Off: no line lit, you scroll yourself. The recording still plays.');
    }
    followBar?.classList.toggle('is-off', !on);
  }
  // The switch is Lyrics only's.
  function showFollowBar() {
    if (followBar) followBar.hidden = !lyricsMode();
    renderFollow();
  }
  showFollowBar();
  new MutationObserver(showFollowBar).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });
  followToggle?.addEventListener('change', () => {
    const on = followToggle.checked;
    lyricsFollow = on;
    followHeld = false;
    try {
      if (on) localStorage.removeItem(lyricsFollowKey);
      else localStorage.setItem(lyricsFollowKey, 'off');
    } catch (e) { /* this page only */ }
    renderFollow();
    // Back on mid-song: go to where the music is now.
    if (on && playingNow) requestAnimationFrame(() => requestAnimationFrame(scrollToNow));
  });
  // Where the music is, brought into view at once.
  function scrollToNow() {
    if (currentLineEl) { currentLineEl.scrollIntoView({ block: 'center', behavior: 'smooth' }); return; }
    const cursor = document.querySelector('.tab-cursor-bar:not([hidden])');
    if (cursor && cursor.offsetParent) { keepInView(cursor); return; }
    if (currentBarEl && currentBarEl.offsetParent) { keepInView(currentBarEl); return; }
    currentSectionEl?.scrollIntoView({ block: 'start', behavior: 'smooth' });
  }
  function resumeFollow() {
    followHeld = false;
    if (followBack) followBack.hidden = true;
    scrollToNow();
  }
  followBack?.addEventListener('click', resumeFollow);
  // A scroll by hand: the wheel, a finger, or a scrolling key nobody else
  // took. Not in the player, its sheet or the Mixer (they scroll or drag
  // themselves), nor in a field.
  const notThePage = 'input, textarea, select, [contenteditable], .audio-dock, .audio-dock-backdrop, .audio-waveform-wrap, .stack-sheet, .daw-scroll, .edit-drawer, .follow-back';
  function scrolledByHand(e) {
    if (!playingNow || followHeld || !chartFollows()) return;
    if (e.target instanceof Element && e.target.closest(notThePage)) return;
    if (sheet.dataset.renderMode === 'mixer' || sheet.dataset.renderMode === 'reference') return;
    followHeld = true;
  }
  window.addEventListener('wheel', (e) => { if (!e.ctrlKey && Math.abs(e.deltaY) > Math.abs(e.deltaX)) scrolledByHand(e); }, { passive: true });
  window.addEventListener('touchmove', scrolledByHand, { passive: true });
  window.addEventListener('keydown', (e) => {
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
    if (['PageUp', 'PageDown', 'ArrowUp', 'ArrowDown', 'End'].includes(e.key)) scrolledByHand(e);
  });
  // Dragging the page's own scrollbar.
  window.addEventListener('pointerdown', (e) => {
    if (e.target === document.documentElement && e.clientX >= document.documentElement.clientWidth) scrolledByHand(e);
  });

  let currentBarEl = null;
  let currentPlayedBar = 0; // the played bar currentBarEl lights (a Jazz chart bar has several)
  let currentSectionEl = null; // where the music is (↑/↓ step from it), lit or not
  let litSectionEl = null;
  let currentLineEl = null;
  const structureFillEl = document.getElementById('structure-fill');
  function highlightTick() {
    // The reference recording (audio-player.js), once started, leads the
    // highlight; the metronome clock otherwise.
    const ref = !clock.running && window.songbooksReferencePosition ? window.songbooksReferencePosition() : null;
    const position = ref ? ref.bar : clock.currentBeat() / clock.beatsPerBar;
    const playing = clock.running || !!(ref && ref.playing);
    // Starting to play again takes the page back to the music.
    if (playing && !wasPlaying) followHeld = false;
    wasPlaying = playing;
    playingNow = playing;
    const lit = chartFollows();
    // Scroll along only while it plays, the chart follows, and the reader
    // hasn't scrolled away by hand.
    const following = playing && lit && !followHeld;
    if (followBack) {
      const show = playing && lit && followHeld;
      const label = lyricsMode() ? t('Back to the current line') : t('Back to where the music is');
      if (followBack.hidden === show) followBack.hidden = !show;
      if (show && followBackLabel && followBackLabel.textContent !== label) followBackLabel.textContent = label;
    }
    const bar = Math.floor(position);
    // Tab rows (alphatab-tab.js) draw their own Guitar Pro style cursor.
    const started = playing || !!ref || clock.currentBeat() > 0;
    const tabFollowed = window.songbooksTabCursor ? window.songbooksTabCursor(started && lit ? position : null, following) : false;
    // Recomputed every tick, not cached at load — rebindSheet() (which
    // populates `sections`) can run after this point (e.g. on first
    // load, it's called at the very end of the script), so a one-time
    // total captured before that would always be 0.
    if (structureFillEl) {
      const totalSongBars = sections.reduce((m, s) => s.startBar != null ? Math.max(m, s.startBar + s.barCount) : m, 0);
      if (totalSongBars > 0) {
        const pct = Math.max(0, Math.min(1, bar / totalSongBars));
        structureFillEl.style.width = `calc((100% - 0.4rem) * ${pct})`;
      }
    }
    // A reflowed row keeps its hidden original next to the visible copy:
    // highlight the copy on screen. Nothing lit when the chart doesn't
    // follow (Lyrics only, switched off).
    // A Map recall cell stands for a section replaying an earlier grid:
    // the grid's bars light rather than the cell, when they're drawn.
    const matches = lit ? (barsByPlayed.get(bar) || []) : [];
    const barEl = matches.find(b => b.offsetParent && !b.classList.contains('bar-recall')) || matches.find(b => b.offsetParent) || matches[0] || null;
    currentPlayedBar = bar;
    if (barEl && started) barEl.style.setProperty('--playhead', String(Math.min(1, position - bar)));
    if (barEl !== currentBarEl) {
      currentBarEl?.classList.remove('current', 'playhead');
      barEl?.classList.add('current');
      currentBarEl = barEl;
      // Keep the playing row on screen, turning the page a row early.
      if (barEl && following && !tabFollowed && barEl.offsetParent) keepInView(barEl);
    }
    // Smooth (Guitar Pro style) line only for bars engraved at even
    // beat spacing: the pickup cell is a compact rest glyph, a Map
    // recall cell a whole section's rest.
    currentBarEl?.classList.toggle('playhead', started && !currentBarEl.classList.contains('bar-pickup') && !currentBarEl.classList.contains('bar-recall'));
    // Section lookup no longer rides on barEl existing — a compact/
    // reference section has no .bar of its own (just a chip + multi-rest
    // bar), so deriving "current section" from "current bar's closest
    // .sheet-section" went blank for its whole duration: no highlight, no
    // auto-scroll, no timeline progress, for however many bars it's
    // actually being played. Found directly from each section's own
    // [startBar, startBar+barCount) range instead, which every section
    // has now regardless of whether it rendered any bars.
    // A zero-bar section (no grid at all — not a compact/recall section,
    // genuinely empty, e.g. an Intro nobody's filled in yet) has a
    // degenerate [startBar, startBar) range that bar < startBar+barCount
    // can never satisfy, even at bar === startBar — so it could never
    // become "current," and since whatever section follows it starts at
    // that SAME bar offset (it consumed zero bars), that next section's
    // real range matched instead: the indicator jumped straight past an
    // empty first section to whatever came after it, even while still
    // scrolled to the top of the page. Zero-bar sections get their own
    // single-instant match at exactly their own startBar instead; sections
    // are checked in song order, so on a tie (two sections sharing a
    // startBar) the earlier, zero-bar one wins until bar actually advances
    // past it.
    const curSec = sections.find(s => s.startBar != null && (
      s.barCount > 0 ? (bar >= s.startBar && bar < s.startBar + s.barCount) : bar === s.startBar
    ));
    const sectionEl = curSec ? curSec.el : null;
    if (sectionEl !== currentSectionEl) {
      const idx = sectionEl ? parseInt(sectionEl.dataset.sectionIndex, 10) : -1;
      // Dim sections already played, not just mark the current one — the
      // strip then reads as a progress bar through the song's structure,
      // not just a static roadmap with one segment lit up. (The strip is
      // the player's progress, so it moves even when the chart doesn't.)
      document.querySelectorAll('.structure-seg').forEach(c => {
        const i = parseInt(c.dataset.sectionIndex, 10);
        c.classList.toggle('current', i === idx);
        c.classList.toggle('done', idx >= 0 && i < idx);
      });
      currentSectionEl = sectionEl;
    }
    const litSection = lit ? sectionEl : null;
    if (litSection !== litSectionEl) {
      litSectionEl?.classList.remove('current-section');
      litSection?.classList.add('current-section');
      // Only auto-scroll while actually following along (clock running) —
      // this same "current section changed" branch also fires once on
      // page load (bar 0 → the Intro section becomes "current" the
      // instant sections[] populates), which used to scroll the page
      // immediately on opening a song, shoving the title/nav bar out of
      // view before the reader had done anything. Highlighting (above)
      // still updates either way — it's only the forced scroll that's
      // playback-only now.
      if (litSection && following && !tabFollowed && !(barEl && barEl.offsetParent)) litSection.scrollIntoView({ block: 'start', behavior: 'smooth' });
      litSectionEl = litSection;
    }
    // Lyrics mode's own finer-grained position indicator: which LINE
    // within the current section, not just which section. The data model
    // has no per-line bar/timing of its own (see song.LyricLine — a line
    // is just chord/text segments, nothing ties it to a bar range), so
    // this approximates by spreading a section's lines evenly across its
    // own [startBar, startBar+barCount) span — not exact against the real
    // phrasing, but close enough to keep the highlighted line moving
    // roughly in time, which is the whole point for a singer who doesn't
    // want to track position by hand while performing. (.lyric-line.current
    // already existed in app.css but nothing ever set the class — this was
    // a genuinely dead/disconnected rule, a second bug alongside the
    // layout one.) Scoped to lyrics mode only: other modes already have
    // their own finer position cue (the current .bar highlight inside the
    // chord grid), so this isn't needed there.
    if (lyricsMode() && lit && curSec && sectionEl) {
      const lineEls = sectionEl.querySelectorAll('.lyric-line');
      let lineEl = null;
      if (lineEls.length) {
        // On the singer's phrases when the recording leads and its voice
        // is known (lyric-lines.js); else spread evenly over the bars.
        const voiced = ref && window.songbooksLyricLine ? window.songbooksLyricLine(curSec, lineEls.length) : null;
        if (voiced !== null) {
          lineEl = lineEls[voiced] || null;
        } else {
          const span = Math.max(1, curSec.barCount);
          const offset = Math.max(0, bar - curSec.startBar);
          const fraction = Math.min(0.999, offset / span);
          lineEl = lineEls[Math.floor(fraction * lineEls.length)] || null;
        }
      }
      if (lineEl !== currentLineEl) {
        currentLineEl?.classList.remove('current');
        lineEl?.classList.add('current');
        if (lineEl && following) lineEl.scrollIntoView({ block: 'center', behavior: 'smooth' });
        currentLineEl = lineEl;
      }
    } else if (currentLineEl) {
      currentLineEl.classList.remove('current');
      currentLineEl = null;
    }
    requestAnimationFrame(highlightTick);
  }
  requestAnimationFrame(highlightTick);

  // The top mini-metro row's play button is now the ONLY play/pause
  // control on this page — the full sheet that used to have its own
  // separate #play-pause-btn (kept in sync via a second listener) is
  // gone, so there's nothing left to mirror.
  const playPauseBtn = document.getElementById('mini-metro-play');
  function updateBpmDisplay() {
    const input = document.getElementById('tempo-input');
    if (input && document.activeElement !== input) input.value = Math.round(clock.bpm);
  }
  updateBpmDisplay();

  const iconPlay = playPauseBtn.innerHTML;
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor"><rect x="5" y="5" width="10" height="10" rx="1.5"/></svg>';

  // ---- View / layout controls ----------------------------------------------
  // Shared by the mode switch (chords+lyrics/jazz/lyrics) and the metronome's
  // sound/click-mode/intro-length pickers — an icon-button group instead of
  // a <select>, so every option is visible and tappable at a glance rather
  // than hidden behind a dropdown.
  function wireSegmented(id, onChange) {
    const el = document.getElementById(id);
    el.querySelectorAll('button').forEach(btn => {
      btn.addEventListener('click', () => {
        el.querySelectorAll('button').forEach(b => b.classList.toggle('active', b === btn));
        el.dataset.value = btn.dataset.value;
        onChange(btn.dataset.value);
      });
    });
  }

  // Metronome mode/click sound/count-in are no longer configurable on this
  // page at all — that's now exclusively the dedicated /metronome page's
  // job (see the mini-metro-settings link). The in-page quick play below
  // always behaves like the old panel's own default ("no click" — nobody
  // should be surprised by audio the moment they hit play, and a reader
  // clicking a section to follow along never asked for a click track).
  metronome.enabled = false;

  // No count-in branch any more — metronome.enabled is always false here
  // (settings, including count-in bars, live only on /metronome now), so
  // playback always starts immediately, same as the old panel's own
  // "off" mode already did.
  function setPlaying(playing) {
    if (!playing) {
      clock.pause(); metronome.stop();
      metronome.enabled = false;
      playPauseBtn.innerHTML = iconPlay;
      playPauseBtn.title = t('play');
      playPauseBtn.classList.remove('active');
      return;
    }
    clock.start();
    metronome.enabled = false;
    metronome.start();
    playPauseBtn.innerHTML = iconPause;
    playPauseBtn.title = t('pause');
    playPauseBtn.classList.add('active');
  }
  playPauseBtn.addEventListener('click', () => setPlaying(!clock.running));
  // The song player's bar for a song with no recording (audio-player.js
  // "No recording"): ▶ runs this follow-along clock, the click on; its
  // readout reads where it is. The player's panel sets that click there
  // (its Click and Tempo lines): its sound and volume, the tempo, by tap.
  window.songbooksFollowClock = {
    toggle(withClick) {
      setPlaying(!clock.running);
      if (clock.running && withClick) metronome.enabled = true;
      return clock.running;
    },
    setSound(sound) { if (sound) metronome.sound = sound; },
    setVolume(v) { metronome.volume = Math.max(0, Number(v) || 0); },
    setBpm(n) {
      const v = Math.max(20, Math.min(300, Math.round(Number(n) || clock.bpm)));
      clock.setBpm(v);
      updateBpmDisplay();
      return v;
    },
    tap() { document.getElementById('mini-metro-tap')?.click(); return Math.round(clock.bpm); },
    state: () => ({
      running: clock.running,
      bpm: clock.bpm,
      bar: currentBarEl ? (currentBarEl.dataset.played ? String(currentPlayedBar) : currentBarEl.dataset.globalBar || '') : '',
      section: currentSectionEl ? currentSectionEl.dataset.label || '' : '',
    }),
  };

  document.getElementById('mini-metro-fade').addEventListener('click', () => {
    if (clock.running) return; // a one-off cue only makes sense from a stop
    metronome.playBarThenFade(clock.bpm, clock.beatsPerBar);
  });
  document.getElementById('mini-metro-tap').addEventListener('click', () => {
    if (!clock.running) { const g = clock.tapDetermine(); if (g) { clock.setBpm(g); updateBpmDisplay(); } }
    else { clock.tapResync(); updateBpmDisplay(); }
  });

  // Beat dots: one per main pulse in the bar, lit as the clock ticks
  // through it — visual timing feedback that works whether or not the
  // click is audible. Compound meters (6/8, 9/8, 12/8) get a real
  // engraving distinction here too: each main pulse IS a dotted quarter —
  // three eighth notes — so each dot is drawn as a cluster of 3 sub-ticks,
  // the middle one lighting up as the clock moves through that pulse's
  // 2nd and 3rd eighth note, not just one flat "beat happened" light. 3/4
  // and 4/4 are simple meters (each beat is one plain quarter, no
  // sub-pulse to show) and keep the single-dot display.
  const timeNum = parseInt(transportEl.dataset.timeNum, 10);
  const timeDen = transportEl.dataset.timeDen;
  const isCompound = timeDen === '8' && [6, 9, 12].includes(timeNum);
  // The metronome should actually sound the subdivision this visual
  // already shows — "1 . . 4 . ." should be heard, not just lit up —
  // the 2nd/3rd eighth of each pulse clicking softer/duller than the
  // main pulse, not silent. See Metronome._tick() in clock.js.
  metronome.isCompound = isCompound;
  const beatDotsEl = document.getElementById('beat-dots');
  for (let i = 0; i < beatsPerBar; i++) {
    const d = document.createElement('span');
    d.className = 'beat-dot' + (i === 0 ? ' downbeat' : '') + (isCompound ? ' compound' : '');
    if (isCompound) {
      for (let j = 0; j < 3; j++) {
        const sub = document.createElement('span');
        sub.className = 'beat-subtick';
        d.appendChild(sub);
      }
    }
    beatDotsEl.appendChild(d);
  }
  const beatDotEls = [...beatDotsEl.children];
  let lastLitBeat = -1;
  let lastLitSub = -1;
  function tickBeatDots() {
    const cur = clock.currentBeat();
    const beat = Math.floor(cur);
    const idx = ((beat % beatsPerBar) + beatsPerBar) % beatsPerBar;
    if (idx !== lastLitBeat) {
      beatDotEls.forEach((d, i) => d.classList.toggle('lit', i === idx));
      lastLitBeat = idx;
    }
    if (isCompound) {
      const sub = Math.min(2, Math.max(0, Math.floor((cur - beat) * 3)));
      if (sub !== lastLitSub || idx !== lastLitBeat) {
        beatDotEls[idx]?.querySelectorAll('.beat-subtick').forEach((s, i) => s.classList.toggle('lit', i === sub));
        lastLitSub = sub;
      }
    }
    requestAnimationFrame(tickBeatDots);
  }
  requestAnimationFrame(tickBeatDots);
  // Per-mode page sizing. Lyrics-only is a reading mode: one centered
  // column as wide as its longest line (scrolling while singing is fine).
  // The Jazz chart sizes itself in CSS, 4 bars a line at any width.
  function applyPageBox() {
    sheet.style.fontSize = '';
    sheet.style.height = '';
    sheet.style.margin = '';
    sheet.style.maxWidth = '';
    sheet.style.width = '';
    sheet.style.columnCount = '';
    const mode = sheet.dataset.renderMode;
    const availW = sheet.parentElement.clientWidth;

    if (mode === 'lyrics') {
      // A reading mode: one centered column that grows with its content
      // (scrolling while singing is fine), as wide as its longest lyric
      // line so that every line reads on one line, within the width there
      // is, and never narrower than a comfortable page. (It used to be an
      // A4 page as tall as the screen below the sheet: with the player
      // above, a thin column, and one that changed with the scroll.)
      sheet.style.width = availW + 'px';
      sheet.classList.add('measuring-lines');
      // Each line's need: its own width plus everything around it, the
      // sheet's padding and border and its section's own indent.
      let need = 0;
      sheet.querySelectorAll('.lyric-line').forEach((l) => {
        need = Math.max(need, l.offsetWidth + sheet.offsetWidth - l.parentElement.clientWidth);
      });
      sheet.classList.remove('measuring-lines');
      const minW = 30 * parseFloat(getComputedStyle(document.documentElement).fontSize);
      sheet.style.width = Math.min(availW, Math.max(minW, Math.ceil(need + 2))) + 'px';
      sheet.style.margin = '0 auto';
      return;
    }
    // Song map: a continuous score, not a printed page — the opposite of
    // jazz/lyrics' narrow A4 box. It wants the FULL available width so
    // reflowStaffLines() can fit as many bars per line as the row
    // actually has room for (8 or more), same as the normal chart. No
    // custom width/margin to set — the reset at the top of this function
    // already cleared any earlier page-box sizing, so this is a no-op by
    // design, left explicit rather than silently falling through.
    if (mode === 'map') return;
    if (mode !== 'jazz') return;
    // The Jazz chart sizes itself in CSS (jazz.css): 4 bars a line at any
    // width. All that's left is a copyist's squeeze for a crowded bar.
    fitJazzChords();
  }
  // Squeeze a chord horizontally when it would run into the next one (four
  // chords in a bar, a long quality), the way a hand-copied chart does.
  function fitJazzChords() {
    if (sheet.dataset.renderMode !== 'jazz') return;
    sheet.querySelectorAll('.jazz-chart .jz-bar').forEach((bar) => {
      const chs = [...bar.querySelectorAll(':scope > .jz-at')];
      if (!chs.length) return;
      chs.forEach((c) => { c.style.transform = ''; });
      const right = bar.getBoundingClientRect().right;
      chs.forEach((c, i) => {
        const r = c.getBoundingClientRect();
        const limit = (chs[i + 1] ? chs[i + 1].getBoundingClientRect().left : right) - 2;
        const avail = limit - r.left;
        if (r.width > avail && avail > 0) c.style.transform = `scaleX(${Math.max(0.55, avail / r.width).toFixed(3)})`;
      });
    });
  }
  // Layout (single column vs. the two-page-column spread) is automatic —
  // pure CSS media queries on viewport width, no toggle to get wrong or to
  // leave in the "wrong" state. Jazz mode used to also force a full-screen
  // "popup" layout (hidden header/sidebar, a floating exit button) on top
  // of that — removed: jazz is a view mode like song map or lyrics-only,
  // reusing the normal page chrome, not a separate full-screen app state.
  // The view last picked for this very song in this coaching community
  // (by a click, not by the restore at load), per device. Only there: a
  // view picked for the song anywhere else (a band, the library) is the
  // reader's default elsewhere, never a reason to skip the community's
  // Lyrics only.
  const songModeKey = sheet.dataset.coaching
    ? `songbooks-mode:c${sheet.dataset.coaching}:${sheet.dataset.variantId || pageVariantId}`
    : '';
  let restoringMode = false;
  wireSegmented('mode-segmented', v => {
    // "edit" sits in this same segmented control (easiest place to find
    // it, next to the other ways of looking at the song) but it's an
    // action — open the drawer — not a persistent view state like the
    // other four, so it never "wins" .active and is never saved as the
    // reader's default mode. The drawer open/close itself is wired
    // separately below (editToggle's own click listener), since this
    // callback only needs to undo wireSegmented's own active-class toggle.
    if (v === 'edit') {
      const real = document.querySelector(`#mode-segmented button[data-value="${sheet.dataset.renderMode}"]`);
      document.querySelectorAll('#mode-segmented button').forEach(b => b.classList.toggle('active', b === real));
      return;
    }
    sheet.dataset.renderMode = v;
    // Instrument picker (#instrument-picker, play.html) only makes sense
    // for Song view and Song map — both read the SAME attachTabRiffs-
    // mutated sections and so share one chosen track — not Jazz chart
    // (chords only, "any instrument", no per-track overlay at all),
    // Drummer (its own instrument, not pickable), Lyrics only, or
    // Reference. The server renders it unconditionally whenever the song
    // has any melodic track at all (so it's there for the common normal-
    // mode case on load without a round trip), so hiding it outside
    // those two modes is this handler's job, not the template's.
    document.getElementById('instrument-picker')?.toggleAttribute('hidden', v !== 'normal' && v !== 'map');
    // Leaving jazz mode: its own fit search (applyPageBox) can leave
    // #sheet's font-size shrunk from whatever it set last — reflow below
    // measures bar widths at #sheet's CURRENT font-size, so switching
    // straight to normal/lyrics/map without resetting it first would
    // size this pass off jazz's leftover (often smaller) pixel value
    // instead of the new mode's real one, under-splitting rows that then
    // render too narrow once the browser repaints at the real size.
    sheet.style.fontSize = '';
    // A grid hidden in the PREVIOUS mode reads clientWidth 0 while
    // reflowStaffLines() skips it, so switching to a mode that shows it
    // needs a fresh pass now that it's actually laid out and measurable.
    // Same reasoning for Song Map's .map-score and reflowMapLines().
    reflowStaffLines();
    reflowMapLines();
    refreshBarsCache();
    applyPageBox();
    // The pass above ran at the PREVIOUS mode's page box (jazz's narrow
    // A4 width until applyPageBox clears it), and whether a lone last bar
    // fits on the row before it depends on that width: once more at the
    // real one. Jazz re-runs it inside its own fit search already.
    if (v !== 'jazz') { reflowStaffLines(); refreshBarsCache(); }
    // Drummer view's own continuous chart (.drum-map — buildDrumMap,
    // handlers_pages.go) is hidden by CSS in every other mode — the same
    // "width=0, invisible" trap a hidden .grid/.map-score row would hit,
    // except alphaTab (unlike this app's own CSS-only bar markup) only
    // ever mounts a row ONCE, at page load (alphatab-tab.js's
    // renderAll), when this mode's row is still hidden for anyone who
    // doesn't already have "drummer" saved as their default mode —
    // confirmed live: alphaTab logs "skipped rendering because of
    // width=0" and never retries on its own once visible. Mount it now
    // that it's actually visible if it hasn't been already; harmless/
    // idempotent to call again for a row alphaTab already painted (it
    // just reconstructs the same SVG from the same data-tex).
    //
    // refreshDrumMap first: renderAll's own call ran while .drum-map was
    // still width=0/hidden for exactly this reader (same trap as above),
    // so it measured nothing and skipped — this is that same width-aware
    // re-fetch getting its real first chance, now that the container is
    // actually visible and has a real width to measure. Self-guarded
    // (data-width-fetched), so this is a no-op on every mode switch after
    // the first. Re-queries the row list after it resolves: a successful
    // refetch replaces .drum-map's innerHTML wholesale, so the elements
    // captured before awaiting it would be stale/detached.
    if (v === 'drummer' && window.AlphaTabTab) {
      window.AlphaTabTab.refreshDrumMap().finally(() => {
        sheet.querySelectorAll('.drum-map .alphatab-row[data-tex]').forEach(row => {
          if (!row.querySelector('svg')) window.AlphaTabTab.render(row);
        });
      });
    }
    // Never save "reference" as the reader's default mode — unlike the
    // other four (a real, lasting preference: "I always want the jazz
    // chart"), landing on the video/audio page by default on every song
    // opened next would replace the chart nobody asked to stop seeing.
    // Same reasoning edit's own early return above already applies, and
    // for the Mixer: a workbench opened for a task, not a way of reading.
    // Picked, that is: not the view restored at load (a coaching
    // community's Lyrics only must not become the default elsewhere).
    if (!restoringMode && v !== 'reference' && v !== 'mixer' && v !== 'score' && !document.body.classList.contains('embed-playlist')) {
      try { localStorage.setItem('songbooks-default-mode', v); } catch (e) { /* ignore */ }
      // And for this song in this coaching community: the view picked
      // there wins over its default (Lyrics only, below) next time.
      if (songModeKey) { try { localStorage.setItem(songModeKey, v); } catch (e) { /* ignore */ } }
    }
  });
  // Reflow before the jazz-mode fit search (applyPageBox): it decides how
  // many bars fit per row at the CURRENT font-size, so it needs to run
  // first, before that search starts shrinking the font under it. A
  // resize is also the one case rebindSheet() never covers on its own
  // (same DOM, same content, just a different viewport), so both need
  // re-running here, not just applyPageBox. Debounced — resize fires
  // continuously while dragging/rotating, and rebuilding every .staff-line
  // from scratch on every single event is needless churn.
  // Lyrics-only measures its lines: again once the web fonts are in.
  document.fonts?.ready.then(() => { if (sheet.dataset.renderMode === 'lyrics' || sheet.dataset.renderMode === 'jazz') applyPageBox(); });
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => { reflowStaffLines(); reflowMapLines(); refreshBarsCache(); applyPageBox(); }, 100);
  });

  // Open every song in the reader's own last-used view instead of always
  // "chords + lyrics" — a drummer who always wants the jazz chart, or a
  // singer who always wants lyrics-only, shouldn't have to re-pick it on
  // every song in a set. Per-device (localStorage), not song data.
  try {
    // A song whose content is all in its Guitar Pro file ("content: gp")
    // opens on that file.
    // Beside a playlist's player (playlist.html), the song shows as its map.
    // A course's lesson (the lesson panel above the song) opens on the
    // song's Guitar Pro file, the score the lesson is practised on.
    const embedded = document.body.classList.contains('embed-playlist');
    const lesson = document.getElementById('lesson-panel') && sheet.dataset.gpFile;
    // Opened in a vocal coaching community (one of its songbooks, its
    // page, an event or an assignment: data-coaching, handlers_pages.go),
    // a song opens on its lyrics, in practice mode, following the music
    // (the "Follow the music" switch is on unless turned off for this
    // song), unless the reader has picked another view for this song.
    const coaching = sheet.dataset.coaching && document.querySelector('#mode-segmented button[data-value="lyrics"]');
    const saved = embedded ? 'map' : sheet.dataset.gpOnly || lesson ? 'score'
      : coaching ? (localStorage.getItem(songModeKey) || 'lyrics')
      : localStorage.getItem('songbooks-default-mode');
    // Drummer view only for a song that has a drum part: on any other it
    // is a page of empty staves, so the song opens as a chart instead.
    const hasDrums = [...document.querySelectorAll('[data-realbars]')].some((e) => !e.dataset.restonly);
    if (saved && saved !== 'normal' && saved !== 'reference' && saved !== 'mixer' && (saved !== 'drummer' || hasDrums)) {
      restoringMode = true;
      try { document.querySelector(`#mode-segmented button[data-value="${saved}"]`)?.click(); } finally { restoringMode = false; }
    }
  } catch (e) { /* ignore */ }

  // Instrument picker (#instrument-picker, play.html) — a pill row of
  // icon buttons now (same view-tabs look as the mode row above it, see
  // the UX complaint this replaced: the old native <select> was both
  // visually mismatched — taller, misaligned — and easy to miss, which
  // silently hid real GP-imported notation behind the default "Chords"
  // view). Still server-rendered, not a client-side re-render: Song
  // view's own chord-chart overlay and Song map (both built from the
  // SAME attachTabRiffs-mutated sections, handlers_pages.go) only know
  // which track to show once the page is rendered with
  // ?instrument=NAME, so picking a different one just updates that
  // query param and reloads, rather than threading every available
  // track's own alphaTeX through the page up front for a client-side
  // swap. URLSearchParams, not string-splicing the query string by
  // hand, so this doesn't clobber ?setlist=/?band=/?songbook= or any
  // other param already on the URL.
  document.getElementById('instrument-picker')?.addEventListener('click', e => {
    const btn = e.target.closest('button[data-instrument]');
    if (!btn) return;
    const url = new URL(location.href);
    // Always explicit, "chords" included: with no instrument at all a
    // song without lyrics opens on its most interesting score track.
    url.searchParams.set('instrument', btn.dataset.instrument || 'chords');
    location.href = url.toString();
  });

  // ---- Reference video (YouTube) ----------------------------------------
  // Extracts a video id from any common YouTube URL shape (watch?v=,
  // youtu.be/, shorts/, embed/) so the band can all pull up the exact same
  // version without needing to know or paste a special "embed" URL.
  function youtubeId(url) {
    try {
      const u = new URL(url);
      const host = u.hostname.replace(/^www\.|^m\./, '');
      if (host === 'youtu.be') return u.pathname.slice(1).split('/')[0] || null;
      if (host === 'youtube.com' || host === 'youtube-nocookie.com') {
        if (u.pathname === '/watch') return u.searchParams.get('v');
        const m = u.pathname.match(/^\/(shorts|embed|live)\/([^/?]+)/);
        if (m) return m[2];
      }
    } catch (e) { /* not a URL */ }
    return null;
  }
  (function initReference() {
    const block = document.getElementById('reference-block');
    if (!block) return;
    const variantId = block.dataset.variantId;
    const embedEl = document.getElementById('reference-embed');
    const linkEl = document.getElementById('reference-link');
    const editBtn = document.getElementById('reference-edit-btn');
    const editRow = document.getElementById('reference-edit-row');
    const urlInput = document.getElementById('reference-url-input');
    const saveBtn = document.getElementById('reference-save-btn');

    // Always rendered now (no more show/hide tap) — Reference is its own
    // dedicated page (data-render-mode="reference"), not a cramped drawer
    // sharing space with the chart, so there's no tight-viewport reason
    // left to hide it by default. Still built lazily (only once the
    // reader actually switches to this mode, via the #sheet
    // data-render-mode observer below), not eagerly on every page load —
    // loading the iframe for a mode most views of a song never visit
    // would be pure waste.
    let embedBuilt = false;
    function renderEmbed(url) {
      embedEl.innerHTML = '';
      const vid = url && youtubeId(url);
      if (!vid) return;
      const iframe = document.createElement('iframe');
      iframe.src = `https://www.youtube-nocookie.com/embed/${vid}`;
      iframe.title = t('reference video');
      iframe.loading = 'lazy';
      iframe.allow = 'accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture';
      iframe.allowFullscreen = true;
      embedEl.appendChild(iframe);
    }
    function ensureEmbedBuilt() {
      if (embedBuilt) return;
      embedBuilt = true;
      renderEmbed(block.dataset.url);
    }
    if (sheet.dataset.renderMode === 'reference') ensureEmbedBuilt();
    new MutationObserver(() => {
      if (sheet.dataset.renderMode === 'reference') ensureEmbedBuilt();
    }).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });

    function setEditOpen(open) {
      editRow.hidden = !open;
      editBtn.setAttribute('aria-expanded', String(open));
      if (open) urlInput.focus();
    }
    editBtn.addEventListener('click', () => setEditOpen(editRow.hidden));

    // The reference in the header's third column (play.html
    // "song-ref-head"): opens this view, the ⋯ menu's Reference;
    // "+ Reference" opens it on the link field. Kept in step with the link
    // saved below and with the recordings (a band recording is the
    // reference when there's no link). Its name is the link's site, as
    // the server names it (song_ref_head.go refLinkName).
    const head = document.getElementById('topbar-ref');
    const headBtn = document.getElementById('topbar-ref-btn');
    const headName = document.getElementById('topbar-ref-name');
    let hasBand = head?.dataset.kind === 'band';
    // The video's or page's title, fetched by the server when the link is
    // saved (handlePutMeta's referenceTitle, reference_title.go).
    let refTitle = headName?.dataset.title || '';
    function refSiteName(url) {
      try {
        const host = new URL(url).hostname.toLowerCase().replace(/^(www|m)\./, '');
        return /^(youtube\.com|youtu\.be|music\.youtube\.com)$/.test(host) ? 'YouTube' : host || url;
      } catch (e) { return url; }
    }
    function renderHead() {
      if (!head) return;
      const url = block.dataset.url || '';
      const vid = url && youtubeId(url);
      const kind = vid && /^[A-Za-z0-9_-]{6,20}$/.test(vid) ? 'youtube' : url ? 'link' : hasBand ? 'band' : '';
      head.dataset.kind = kind;
      head.hidden = !kind && !head.dataset.canEdit;
      const label = kind ? headBtn.dataset.openLabel : headBtn.dataset.addLabel;
      headBtn.title = label;
      headBtn.setAttribute('aria-label', label);
      if (headName) {
        headName.textContent = kind === 'band' ? (head.dataset.bandLabel || '') : url ? (refTitle || refSiteName(url.trim())) : '';
        if (refTitle && kind !== 'band') headName.title = refTitle; else headName.removeAttribute('title');
      }
      const thumb = head.querySelector('.topbar-ref-thumb');
      let img = thumb.querySelector('img');
      if (kind !== 'youtube') { img?.remove(); return; }
      const src = `https://i.ytimg.com/vi/${vid}/mqdefault.jpg`;
      if (img && img.getAttribute('src') === src) return;
      if (!img) {
        img = document.createElement('img');
        img.alt = '';
        img.width = 96; img.height = 54;
        img.loading = 'lazy';
        img.decoding = 'async';
        img.referrerPolicy = 'no-referrer';
        thumb.prepend(img);
      }
      img.src = src;
    }
    headBtn?.addEventListener('click', () => {
      const add = head.dataset.kind === '';
      document.querySelector('#mode-segmented button[data-value="reference"]')?.click();
      if (add) setEditOpen(true);
    });
    document.addEventListener('songbooks:audio-categories', (e) => {
      hasBand = (e.detail?.categories || []).includes('band');
      renderHead();
    });
    // A real <form>: Enter in the field saves, and forms.js checks the
    // type="url" before this runs.
    editRow.addEventListener('submit', async (e) => {
      e.preventDefault();
      const url = urlInput.value.trim();
      await Forms.run(editRow, saveBtn, async () => {
        const res = await fetch(`/api/songs/${variantId}/meta`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ key: 'reference', value: url }),
        });
        if (res.status === 403) throw new Error(t("You can't change this song's reference video."));
        if (res.status === 409) throw new Error(t('This song changed since you opened it. Reload the page, then save the link again.'));
        if (!res.ok) throw new Error(t("Couldn't save the link. Try again."));
        const saved = await res.json().catch(() => ({}));
        refTitle = (url && saved.referenceTitle) || '';
        block.dataset.url = url;
        linkEl.href = url;
        linkEl.hidden = !url;
        linkEl.textContent = (refTitle || linkEl.dataset.defaultLabel || '') + ' \u2197';
        const label = url ? t('Change the reference video') : t('Add a reference video');
        editBtn.title = label;
        editBtn.setAttribute('aria-label', label);
        renderEmbed(url);
        embedBuilt = true;
        renderHead();
        setEditOpen(false);
      });
    });
  })();

  // ---- Transpose (view only) -------------------------------------------
  // Shifts every rendered chord's pitch class by a number of semitones and
  // re-spells it — the song's own `key:`/grid text on disk never changes,
  // which is the whole point: the ORIGINAL key stays visible (#key-original)
  // at all times, with the transposed key shown next to it only when the
  // offset is non-zero, so nobody loses track of what the chart actually
  // says versus what's being played right now. Resets to 0 on reload —
  // this is "capo math for tonight," not a saved preference.
  const transposeUp = document.getElementById('transpose-up');
  // A song with no key written has no −/+ (play.html), but a coach's key
  // (data-key-offset) and the player's Key (audio-player.js, mixer.js)
  // still move its chords.
  if (sheet) {
    const PC = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };
    const SHARP_NAMES = ['C', 'C♯', 'D', 'D♯', 'E', 'F', 'F♯', 'G', 'G♯', 'A', 'A♯', 'B'];
    const FLAT_NAMES = ['C', 'D♭', 'D', 'E♭', 'E', 'F', 'G♭', 'G', 'A♭', 'A', 'B♭', 'B'];
    const flats = sheet.dataset.flats === '1';
    const origKey = sheet.dataset.key || '';
    // Opened from a coaching student's or group's songbook, the song is
    // in the key the coach gave it there (data-key-offset, play.html).
    let offset = parseInt(sheet.dataset.keyOffset, 10) || 0;

    function spell(pc) {
      const arr = flats ? FLAT_NAMES : SHARP_NAMES;
      return arr[((pc % 12) + 12) % 12];
    }
    // The server's spelling table (chordsym.TransposedSpellings): every
    // pitch class as the key the song is moved to reads it — flats in F
    // or in C for a bIII/bVI/bVII, sharps in E. Without a key, spell().
    let spellTable = null;
    try { spellTable = sheet.dataset.spell ? JSON.parse(sheet.dataset.spell) : null; } catch (e) { spellTable = null; }
    const glyphs = (n) => n[0] + n.slice(1).replace('b', '♭').replace('#', '♯');
    function spellIn(pc) {
      const o = ((shownOffset() % 12) + 12) % 12;
      if (!spellTable) return spell(pc);
      return glyphs(spellTable.names[o][((pc % 12) + 12) % 12]);
    }
    function rootPitchClass(root) {
      if (!root) return -1;
      let pc = PC[root[0]];
      if (pc === undefined) return -1;
      if (root[1] === '#') pc++;
      if (root[1] === 'b') pc--;
      return ((pc % 12) + 12) % 12;
    }

    const transposeAmount = document.getElementById('transpose-amount');
    const keyTransposed = document.getElementById('key-transposed');
    // Diagrams registered for other keys ("chord[G]:" lines), rendered by
    // the server into an inert <template> outside #sheet (play.html).
    const diagramSets = document.getElementById('chord-diagram-sets');
    // Same reading of a key as chordsym.ParseKey: a root, then nothing or
    // a major/minor marker.
    const origKeyParts = origKey.match(/^([A-G][b#]?)\s*(m|mi|min|minor|-|maj|major)?\s*$/i);

    // A chord diagram only fits the key it was entered for: the song's own
    // row shows at its written key (or a whole octave away), another key's
    // row when the song has one for the displayed key, otherwise a short
    // note rather than shapes that no longer match the chords.
    // The editor shows the song in its own key: it changes the song
    // itself, for everyone who has it, not the key this page plays it in
    // (a songbook's, or tonight's −/+). Back in it when the editor closes.
    const shownOffset = () => (document.body.classList.contains('drawer-open') ? 0 : offset);
    function applyDiagrams(displayedKey) {
      sheet.querySelectorAll('.chord-legend-other, .chord-legend-note').forEach(el => el.remove());
      const own = sheet.querySelector('.chord-legend-own');
      const shifted = ((shownOffset() % 12) + 12) % 12 !== 0;
      if (own) own.hidden = shifted;
      if (!shifted || sheet.dataset.hasDiagrams !== '1') return;
      let node = null;
      if (origKeyParts && diagramSets) {
        const pc = ((rootPitchClass(origKeyParts[1]) + shownOffset()) % 12 + 12) % 12;
        const minor = /^(m|mi|min|minor|-)$/i.test(origKeyParts[2] || '');
        const set = [...diagramSets.content.querySelectorAll('.chord-legend-other')]
          .find(el => parseInt(el.dataset.keyPc, 10) === pc && (el.dataset.keyMinor === '1') === minor);
        if (set) node = set.cloneNode(true);
      }
      if (!node) {
        node = document.createElement('p');
        node.className = 'hint chord-legend-note';
        node.textContent = displayedKey ? t('No chord diagrams for %s yet.', displayedKey) : t('No chord diagrams for this key yet.');
      }
      if (own) own.after(node); else sheet.prepend(node);
    }

    // The Jazz chart's chords are music-font glyphs: each says its name
    // (aria-label, built by the server: root, quality, bass) and shows it
    // as its tooltip. A transpose moves the root and the bass, so both are
    // put back together around them, as is the spoken key.
    function relabelJazzChart() {
      const chart = sheet.querySelector('.jazz-chart');
      if (!chart) return;
      const flatW = chart.dataset.sayFlat || 'flat';
      const sharpW = chart.dataset.saySharp || 'sharp';
      const overW = chart.dataset.sayOver || 'over';
      const say = (n) => (n ? n[0] + (n[1] === '♭' ? ' ' + flatW : n[1] === '♯' ? ' ' + sharpW : '') : '');
      chart.querySelectorAll('.jz-at').forEach((at) => {
        const rootEl = at.querySelector('.jz-root[data-name]:not(.jz-broot)');
        if (!rootEl) return;
        const bassEl = at.querySelector('.jz-broot[data-name]');
        const root = rootEl.dataset.name;
        const bass = bassEl ? bassEl.dataset.name : '';
        const q = at.dataset.sayQ || '';
        at.setAttribute('aria-label', say(root) + (q ? ' ' + q : '') + (bass ? ` ${overW} ${say(bass)}` : ''));
        at.title = root + (at.dataset.q || '') + (bass ? '/' + bass : '');
      });
      const keySay = chart.querySelector('.jz-key-say');
      const keyRoot = chart.querySelector('.jz-keyroot[data-name]');
      if (keySay && keyRoot) {
        const rest = keySay.dataset.sayRest || '';
        keySay.textContent = say(keyRoot.dataset.name) + (rest ? ' ' + rest : '');
      }
    }
    function applyTranspose() {
      if (transposeAmount) transposeAmount.textContent = (offset > 0 ? '+' : '') + offset;
      const shown = shownOffset();
      sheet.querySelectorAll('[data-pc]').forEach(el => {
        const pc = parseInt(el.dataset.pc, 10);
        if (pc < 0 || Number.isNaN(pc)) return;
        // Untransposed, chords read as the song writes them.
        if (el.dataset.orig === undefined) el.dataset.orig = el.innerHTML;
        if (el.dataset.jz && el.dataset.name0 === undefined) el.dataset.name0 = el.dataset.name || '';
        if (((shown % 12) + 12) % 12 === 0) {
          el.innerHTML = el.dataset.orig;
          if (el.dataset.jz) el.dataset.name = el.dataset.name0;
          return;
        }
        const name = spellIn(pc + shown);
        if (el.dataset.jz) {
          el.dataset.name = name;
          // The Jazz chart: a letter and a music-font accidental.
          const acc = name[1] === '♭' ? '\uE260' : name[1] === '♯' ? '\uE262' : '';
          el.innerHTML = `<span class="jz-l">${name[0]}</span>` + (acc ? `<span class="jz-acc">${acc}</span>` : '');
          return;
        }
        el.textContent = name;
      });
      relabelJazzChart();
      // A moved chord can be wider than the one it replaced (C → D♭):
      // squeeze the Jazz chart's crowded bars again.
      fitJazzChords();
      if (shown === 0 || !origKey || !keyTransposed) {
        if (keyTransposed) keyTransposed.hidden = true;
        if (keyTransposed) keyTransposed.textContent = '';
        applyDiagrams('');
        return;
      }
      // The key string is "root + everything else" (e.g. "F#m" -> root
      // "F#", suffix "m") — only the root needs transposing, the suffix
      // (minor marker, anything else written after it) carries over as-is.
      const m = origKey.match(/^([A-G][b#]?)(.*)$/);
      if (!m) { keyTransposed.hidden = true; applyDiagrams(''); return; }
      const pc = rootPitchClass(m[1]);
      if (pc < 0) { keyTransposed.hidden = true; applyDiagrams(''); return; }
      keyTransposed.textContent = spellTable ? glyphs(spellTable.keys[((shown % 12) + 12) % 12]) : spell(pc + shown) + m[2];
      keyTransposed.hidden = false;
      applyDiagrams(keyTransposed.textContent);
    }
    transposeUp?.addEventListener('click', () => { offset++; applyTranspose(); });
    document.getElementById('transpose-down')?.addEventListener('click', () => { offset--; applyTranspose(); });
    reapplyTranspose = applyTranspose;
    if (offset) applyTranspose();
    // The practice player's Key moves the chords with the recording
    // (audio-player.js), −/+ or not.
    window.songbooksShiftChords = (d) => { offset += d; applyTranspose(); };
    // The Mixer's chord row (mixer.js) names its chords as the page does.
    // Spelled for the key the song is moved to, as the chart is (spellIn).
    window.songbooksChordInView = (name) => {
      if (!offset || !name) return name;
      const m = name.match(/^([A-G][b#]?)(.*?)(?:\/([A-G][b#]?))?$/);
      if (!m || rootPitchClass(m[1]) < 0) return name;
      return spellIn(rootPitchClass(m[1]) + offset) + m[2] + (m[3] ? '/' + spellIn(rootPitchClass(m[3]) + offset) : '');
    };
  }

  const themeBtn = document.getElementById('theme-toggle');
  // Mirrors icons.html's icon-theme/-light/-dark — a 3-state toggle (auto,
  // which reads as "half-filled circle", then light, then dark) needs a
  // visibly different icon per state, or clicking it looks like nothing
  // happened two times out of three.
  const THEME_ICONS = {
    '': '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><circle cx="10" cy="10" r="6.5"/><path d="M10 3.5a6.5 6.5 0 0 1 0 13z" fill="currentColor" stroke="none"/></svg>',
    light: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><circle cx="10" cy="10" r="3.6" fill="currentColor" stroke="none"/><path d="M10 2v1.8M10 16.2V18M18 10h-1.8M3.8 10H2M15.4 4.6l-1.3 1.3M5.9 14.1l-1.3 1.3M15.4 15.4l-1.3-1.3M5.9 5.9 4.6 4.6"/></svg>',
    dark: '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" stroke="none" aria-hidden="true"><path d="M16.5 12.8A7 7 0 0 1 7.2 3.5a7 7 0 1 0 9.3 9.3z"/></svg>',
  };
  const THEME_TITLES = { '': t('theme: auto (click for light)'), light: t('theme: light (click for dark)'), dark: t('theme: dark (click for auto)') };
  function applyTheme(t) {
    document.body.dataset.theme = document.documentElement.dataset.theme = t || '';
    themeBtn.innerHTML = THEME_ICONS[t] || THEME_ICONS[''];
    themeBtn.title = THEME_TITLES[t] || THEME_TITLES[''];
  }
  try { applyTheme(localStorage.getItem('songbooks-theme') || ''); } catch (e) { applyTheme(''); }
  themeBtn.addEventListener('click', () => {
    const order = ['', 'light', 'dark'];
    const next = order[(order.indexOf(document.body.dataset.theme || '') + 1) % order.length];
    applyTheme(next);
    try { localStorage.setItem('songbooks-theme', next); } catch (e) { /* ignore */ }
  });

  // ---- Edit drawer: a first-class, bookmarkable editing state, not a
  // disposable popup — see dirty-tracking and the ?edit=1 history wiring
  // below. On desktop/mobile this already renders full-screen (Phase 35);
  // what was missing was the URL/history/exit-safety side of "feels like a
  // real page": reload or share the link mid-edit and you land back in the
  // editor, Back actually backs out of it instead of leaving the app, and
  // unsaved work is never silently dropped. ------------------------------
  const drawer = document.getElementById('edit-drawer');
  const drawerBackdrop = document.getElementById('edit-drawer-backdrop');
  const editToggle = document.getElementById('edit-toggle');
  const closeEditBtn = document.getElementById('edit-drawer-close');
  const dirtyIndicator = document.getElementById('edit-dirty-indicator');
  let savedSheetHTML = sheet.innerHTML;

  // ---- Unsaved-changes tracking — previously nonexistent: every exit path
  // (X, backdrop, Escape, Back, tab close) silently discarded in-progress
  // edits with zero warning. markDirty() is called from every place this
  // file actually mutates editable state (matrix slots, chord toolbar
  // buttons, bar insert/delete, repeat toggle, line-width, lyrics text,
  // header fields, the raw-text textarea); clearDirty() only after a
  // confirmed, successful save.
  let isDirty = false;
  const discardBtn = document.getElementById('edit-discard-btn');
  function markDirty() {
    if (isDirty) return;
    isDirty = true;
    if (dirtyIndicator) dirtyIndicator.hidden = false;
    if (discardBtn) discardBtn.disabled = false;
  }
  function clearDirty() {
    isDirty = false;
    if (dirtyIndicator) dirtyIndicator.hidden = true;
    if (discardBtn) discardBtn.disabled = true;
  }
  // The Sync sheet saves chart fixes as a revision: not over unsaved edits.
  window.songbooksEditorDirty = () => isDirty;
  // A catalogue edit sent for review, or made another recording's version
  // (catalogue-edit.js): nothing left unsaved before the page moves on.
  window.songbooksEditorClean = clearDirty;
  // A native confirm() is the same idiom this file already uses for other
  // destructive choices (redo-song-btn) — OK means "discard and proceed",
  // Cancel means "stay, let me hit Save instead." Every exit path funnels
  // through this one check, so there is exactly one place that decides
  // whether it's safe to leave.
  function confirmDiscardIfDirty() {
    if (!isDirty) return true;
    return confirm(t('Close the editor and lose your unsaved changes? Choose Cancel to go back and save them.'));
  }
  window.addEventListener('beforeunload', e => {
    if (!isDirty) return;
    e.preventDefault();
    e.returnValue = '';
  });

  // ---- URL/history: ?edit=1 is now the real, persistent address of "the
  // editor is open" — pushed when opening (so Back closes it instead of
  // leaving the page), kept on reload (so a bookmarked/shared edit link
  // lands back in the editor), and popped when closing via any in-app
  // control (X/backdrop/Escape) so Back and those controls leave history
  // in the same state either way. suppressHistory lets setDrawerOpen be
  // called FROM a popstate handler without pushing a second entry.
  function editURL(open) {
    const params = new URLSearchParams(location.search);
    if (open) params.set('edit', '1'); else params.delete('edit');
    const qs = params.toString();
    return location.pathname + (qs ? '?' + qs : '') + location.hash;
  }
  function setDrawerOpen(opening, { suppressHistory } = {}) {
    if (!opening && !confirmDiscardIfDirty()) return false;
    drawer.hidden = !opening;
    drawerBackdrop.hidden = !opening;
    document.body.classList.toggle('drawer-open', opening);
    reapplyTranspose(); // the editor's chart in the song's own key (Transpose below)
    // Phase 46: editToggle moved out of the mode-segmented pill into its
    // own sidebar row, so its .active state is toggled on its own element
    // directly now, separately from the view-mode tabs' own active-button
    // bookkeeping (which editToggle no longer participates in).
    editToggle.classList.toggle('active', opening);
    if (!opening) {
      const real = document.querySelector(`#mode-segmented button[data-value="${sheet.dataset.renderMode}"]`);
      document.querySelectorAll('#mode-segmented button').forEach(b => b.classList.toggle('active', b === real));
    }
    if (opening) {
      document.getElementById('raw-text')?.focus();
    } else {
      // Closing without necessarily having saved — restore the last-known
      // saved render so the chart never silently shows an unsaved draft,
      // and drop any in-memory edits now that the user has confirmed (or
      // there was nothing to confirm).
      sheet.innerHTML = savedSheetHTML;
      rebindSheet();
      clearDirty();
    }
    if (!suppressHistory) {
      const wantURL = editURL(opening);
      if (wantURL !== location.pathname + location.search + location.hash) {
        history.pushState({ songbooksEdit: opening }, '', wantURL);
      }
    }
    return true;
  }
  // Browser Back/Forward across the edit boundary: re-sync the drawer to
  // whatever the URL now says rather than trusting the gesture alone — a
  // user who hits Back with unsaved changes gets the same confirm as any
  // other exit path, and Cancel re-asserts the ?edit=1 URL instead of
  // leaving history pointing somewhere the UI didn't actually go.
  window.addEventListener('popstate', () => {
    const wantOpen = new URLSearchParams(location.search).get('edit') === '1';
    if (wantOpen === !drawer.hidden) return;
    if (wantOpen && (editOffer || !pageCanEdit)) return;
    if (!setDrawerOpen(wantOpen, { suppressHistory: true })) {
      // User cancelled closing — the URL already changed out from under us
      // (that's how popstate fired at all); push it back to ?edit=1 so the
      // address bar still matches "the editor is open."
      history.pushState({ songbooksEdit: true }, '', editURL(true));
    }
  });
  // Edit on a song this viewer can't, or shouldn't, change here never
  // copies anything on its own (it used to: a silent personal copy, and
  // people kept working on "their" copy of a band's song). The editor's
  // data-edit-offer says what Edit offers instead, in #edit-offer-dialog
  // (play.html, handleSong): "band", make the song the band's own and edit
  // that (a member opening the catalogue's song from the band's songbook or
  // set list); "copy", the person's own copy, asked for explicitly. Either
  // lands on the new song with the editor open, the page's context (?band,
  // ?setlist, ?songbook) kept.
  const editOffer = editorElForFork?.dataset.editOffer || '';
  const offerDialog = document.getElementById('edit-offer-dialog');
  function openEditOffer() {
    if (!offerDialog || offerDialog.open) return;
    Forms.status(document.getElementById('edit-offer-status'), '');
    offerDialog.showModal();
  }
  // The native dialog gives focus back to what had it, except when that was
  // a row of the phone's View sheet, closed by then: back to Edit, then.
  offerDialog?.addEventListener('close', () => {
    const at = document.activeElement;
    if ((!at || at === document.body || offerDialog.contains(at)) && editToggle.offsetParent) editToggle.focus();
  });
  // "Make Mercy's version", "Make the community's version", "Make my
  // version" (docs/ownership-model.md §5): the version takes the link's
  // place in the songbook the page was opened from (data-songbook-id),
  // else in every songbook of its owner's that links the song.
  document.getElementById('edit-offer-go')?.addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const status = document.getElementById('edit-offer-status');
    const action = btn.dataset.action;
    const url = action === 'band'
      ? `/api/bands/${btn.dataset.bandId}/catalog/fork/${pageVariantId}`
      : action === 'community'
        ? `/api/communities/${btn.dataset.communityId}/catalog/fork/${pageVariantId}`
        : `/api/variants/${pageVariantId}/fork`;
    const body = btn.dataset.songbookId ? {songbookId: Number(btn.dataset.songbookId)} : {};
    Forms.status(status, '');
    Forms.busy(btn, true);
    const res = await fetch(url, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
    }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : null;
    if (!res || !res.ok || !data || !data.variantId) {
      Forms.busy(btn, false);
      Forms.status(status, (data && data.message) || (action === 'band'
        ? t("Couldn't make the band's arrangement of this song. Try again.")
        : action === 'community'
          ? t("Couldn't make the community's arrangement of this song. Try again.")
          : t("Couldn't make your own arrangement of this song. Try again.")), 'error');
      return;
    }
    const params = new URLSearchParams(location.search);
    params.set('edit', '1');
    location.href = `/song/${data.variantId}?${params.toString()}`;
  });
  // New version from this (the More menu): named, beside this one in the
  // songbook; it opens in the editor.
  const versionDialog = document.getElementById('new-version-dialog');
  // The ⋯ menu's item, and the phone's Song sheet's row (song-controls.js
  // closes the sheet).
  document.querySelectorAll('[data-new-version-open]').forEach((el) => el.addEventListener('click', () => {
    if (!versionDialog || versionDialog.open) return;
    Forms.status(document.getElementById('new-version-status'), '');
    versionDialog.showModal();
    document.getElementById('new-version-name')?.focus();
  }));
  document.getElementById('new-version-form')?.addEventListener('submit', (e) => {
    if (e.submitter && e.submitter.value === 'cancel') return;
    e.preventDefault();
    document.getElementById('new-version-go')?.click();
  });
  document.getElementById('new-version-go')?.addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const status = document.getElementById('new-version-status');
    const input = document.getElementById('new-version-name');
    const name = (input?.value || '').trim();
    if (!name) {
      Forms.status(status, t('Name the new arrangement.'), 'error');
      input?.focus();
      return;
    }
    const body = {name};
    if (btn.dataset.songbookId) body.songbookId = Number(btn.dataset.songbookId);
    Forms.busy(btn, true);
    const res = await fetch(`/api/variants/${pageVariantId}/versions`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
    }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : null;
    if (!res || !res.ok || !data || !data.variantId) {
      Forms.busy(btn, false);
      Forms.status(status, (data && (data.message || data.error)) || t("Couldn't make the new arrangement. Try again."), 'error');
      return;
    }
    const params = new URLSearchParams(location.search);
    params.set('edit', '1');
    location.href = `/song/${data.variantId}?${params.toString()}`;
  });
  // Make public / Make private (docs/ownership-model.md §2): everything
  // goes with the song — its chart, reference, recordings, stems, Guitar
  // Pro file.
  document.querySelectorAll('[data-song-publish]').forEach((el) => el.addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const makePublic = btn.dataset.songPublish !== 'true';
    if (makePublic && !confirm(t('Make this song public? Everyone signed in will see it — its chart, reference, recordings, stems and Guitar Pro file — and can link it or make their own arrangement.'))) return;
    // Private again: who links it keeps a frozen copy; said first.
    if (!makePublic && !confirm(await songUses('private'))) return;
    Forms.busy(btn, true);
    const res = await fetch(`/api/variants/${pageVariantId}/public`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({public: makePublic}),
    }).catch(() => null);
    if (!res || !res.ok) {
      Forms.busy(btn, false);
      alert(t("Couldn't change who sees this song. Try again."));
      return;
    }
    location.reload();
  }));
  // Delete this song… (docs/ownership-model.md §5): the confirmation says
  // who links it (each keeps a frozen copy) and who has their own version.
  async function songUses(what) {
    const res = await fetch(`/api/variants/${pageVariantId}/uses?for=${what}`).catch(() => null);
    const data = res && res.ok ? await res.json().catch(() => null) : null;
    if (data && data.message) return data.message;
    return what === 'delete' ? t('Delete this song? This can’t be undone.') : t('Make this song private?');
  }
  document.querySelectorAll('[data-song-delete]').forEach((el) => el.addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    if (!confirm(await songUses('delete'))) return;
    Forms.busy(btn, true);
    const res = await fetch(`/api/songs/${pageVariantId}`, {method: 'DELETE'}).catch(() => null);
    if (!res || !res.ok) {
      Forms.busy(btn, false);
      alert(t("Couldn't delete this song. Try again."));
      return;
    }
    const back = document.querySelector('a.song-back[href]');
    location.href = back ? back.getAttribute('href') : '/';
  }));
  editToggle.addEventListener('click', () => {
    if (drawer.hidden && editOffer && pageVariantId) { openEditOffer(); return; }
    setDrawerOpen(drawer.hidden);
  });
  closeEditBtn.addEventListener('click', () => setDrawerOpen(false));
  drawerBackdrop.addEventListener('click', () => setDrawerOpen(false));
  // Desktop sidebar's "← Back to Play" row (only visible while the editor
  // is open, see app.css body.drawer-open) — same close path as the X
  // button, including the confirmDiscardIfDirty prompt.
  document.getElementById('sidebar-back-to-play')?.addEventListener('click', e => {
    e.preventDefault();
    setDrawerOpen(false);
  });
  document.addEventListener('keydown', e => {
    // Escape closes whatever is open on top first (a popup menu, the
    // section menu, a focused field): each of those handlers calls
    // preventDefault, and only a bare Escape closes the drawer.
    if (e.key !== 'Escape' || drawer.hidden || e.defaultPrevented) return;
    const t = document.activeElement;
    if (t && t !== document.body && drawer.contains(t) && (t.matches('input, textarea, select') || t.isContentEditable)) { t.blur(); return; }
    setDrawerOpen(false);
  });
  // ?edit=1 on load (a direct link, a reload mid-edit, or landing here
  // straight from the implicit-fork redirect above) opens the editor AND
  // keeps the URL as-is — previously this stripped the param immediately
  // via replaceState, which is exactly why the editor never felt
  // bookmarkable: reloading or sharing the link always dropped you back on
  // the read-only chart.
  if (new URLSearchParams(location.search).get('edit') === '1') {
    if (editOffer || !pageCanEdit) {
      // Not this viewer's to edit here: the address loses ?edit=1, and Edit
      // says why (and what it can do instead), when it can do something.
      history.replaceState(history.state, '', editURL(false));
      if (editOffer) openEditOffer();
    } else {
      setDrawerOpen(true, { suppressHistory: true });
    }
  }

  // No off-canvas controls sheet any more — #mini-metro-settings is a
  // plain link to /metronome now (see play.html), not a panel toggle,
  // so there's nothing left to wire up here.

  document.querySelectorAll('.prep-tab').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.prep-tab').forEach(b => b.classList.toggle('active', b === btn));
      document.querySelectorAll('.prep-panel').forEach(p => { p.hidden = p.dataset.panel !== btn.dataset.tab; });
      // The sidebar toolbox (#toolbox-sections/#toolbox-toolbar) is only
      // ever meaningful for the "Blocks" (structure) tab — it lives
      // outside .prep-panel's own [hidden] toggle above, so it needs its
      // own show/hide here: cleared on Source, rebuilt on switching back
      // (structSections itself is untouched by the tab switch, only the
      // sidebar DOM was emptied).
      if (btn.dataset.tab === 'structure') renderStructureUI();
      else { document.getElementById('toolbox-sections').innerHTML = ''; document.getElementById('toolbox-toolbar').innerHTML = ''; }
      // Two-pane preview starts empty (it's only worth the round trip
      // once the panel is actually visible) — fill it in the moment
      // Source is first opened, same lazy trigger the Scores tab below
      // uses for its own per-section fetches.
      if (btn.dataset.tab === 'raw' && sourcePreview && !sourcePreview.innerHTML) renderPreview();
      if (btn.dataset.tab === 'scores') { window.ScoreEditor?.init(); refreshDiagramTodo(); }
    });
  });

  // ---- Top-bar Save/Discard — one obvious pair of actions regardless of
  // which tab (Chart/Source) is active, instead of two separate buried
  // per-panel Save buttons (still present, just hidden, and proxied here
  // so neither tab's own save/version-conflict logic had to be
  // duplicated). Discard reverts to the last-saved version by refetching
  // it from the server — the same reload Save itself already does on
  // success — rather than trying to hand-unwind in-memory edits.
  document.getElementById('edit-save-btn')?.addEventListener('click', () => {
    const activeTab = document.querySelector('.prep-tab.active')?.dataset.tab;
    // Import has no save of its own — an applied draft lands in Source's
    // raw text (see the import wizard below), so Save there is what
    // commits it, same as any other raw-text edit.
    const target = (activeTab === 'raw' || activeTab === 'import') ? 'save-btn' : 'save-structure';
    document.getElementById(target)?.click();
  });
  document.getElementById('edit-discard-btn')?.addEventListener('click', () => {
    if (!isDirty) return;
    if (!confirm(t("Throw away all your unsaved changes to this song? This can't be undone."))) return;
    clearDirty();
    window.location.reload();
  });

  // ---- Raw editor + live preview --------------------------------------------
  const editorEl = document.getElementById('editor');
  let rawVersion = editorEl.dataset.version;
  const rawTextarea = document.getElementById('raw-text');

  // ---- Syntax highlighting (textarea-over-<pre> overlay, no CDN deps) ------
  const backdrop = document.getElementById('code-backdrop');
  const highlightCode = document.getElementById('code-highlight');
  function escapeHTML(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
  function highlightLine(line) {
    let l = escapeHTML(line);
    if (/^\s*#/.test(line)) return `<span class="tok-comment">${l}</span>`;
    let m;
    if ((m = line.match(/^(\[.*?\])(.*)$/))) {
      return `<span class="tok-section">${escapeHTML(m[1])}</span>${escapeHTML(m[2])}`;
    }
    if ((m = line.match(/^(grid|bars|time|score|chord|lead):/))) {
      l = l.replace(/^([a-z]+:)/, '<span class="tok-keyword">$1</span>');
    } else if ((m = line.match(/^([a-z][a-z0-9_-]*):\s/i))) {
      l = l.replace(/^([a-z][a-z0-9_-]*:)/i, '<span class="tok-header-key">$1</span>');
    }
    l = l.replace(/\[([^\]]*)\]/g, '[<span class="tok-chord">$1</span>]');
    return l;
  }
  // Incremental per-line patching, not a full-document innerHTML rebuild
  // on every keystroke — the original cursor-tracking bug report ("the
  // highlighted text visibly lags the real caret on a long song"):
  // highlightLine already tokenizes one line at a time independently, so
  // the fix is to only touch the DOM nodes for lines that actually
  // changed, making the per-keystroke cost O(1) amortized instead of
  // O(document length). Debouncing would hide the SAME lag behind a
  // delay instead of removing it — the caret would still visibly outrun
  // stale highlighting while typing, just with extra latency added on
  // top, so this is a real fix, not a cosmetic workaround.
  let lastLines = [];
  function renderHighlight() {
    const lines = rawTextarea.value.split('\n');
    while (highlightCode.children.length > lines.length) {
      highlightCode.removeChild(highlightCode.lastChild);
    }
    for (let i = 0; i < lines.length; i++) {
      if (lines[i] === lastLines[i] && highlightCode.children[i]) continue;
      let el = highlightCode.children[i];
      if (!el) {
        el = document.createElement('div');
        highlightCode.appendChild(el);
      }
      // A blank line still needs real height to stay aligned with the
      // textarea's own row for it — an empty <div> collapses to 0px.
      el.innerHTML = highlightLine(lines[i]) || '<br>';
    }
    lastLines = lines;
  }
  rawTextarea.addEventListener('input', renderHighlight);
  rawTextarea.addEventListener('input', markDirty);
  rawTextarea.addEventListener('scroll', () => {
    backdrop.scrollTop = rawTextarea.scrollTop;
    backdrop.scrollLeft = rawTextarea.scrollLeft;
  });
  renderHighlight();

  const sectionMenu = document.getElementById('section-menu');
  document.getElementById('code-toolbar').addEventListener('click', e => {
    const btn = e.target.closest('button');
    if (!btn) return;
    if (btn.id === 'section-menu-custom-add') {
      const input = document.getElementById('section-menu-custom');
      const label = input.value.trim();
      if (label) {
        rawTextarea.focus();
        insertSectionLabel(label);
        input.value = '';
      }
      sectionMenu.open = false;
      return;
    }
    rawTextarea.focus();
    if (btn.dataset.insertLabel) {
      insertSectionLabel(btn.dataset.insertLabel);
      sectionMenu.open = false;
    }
    else if (btn.dataset.insert === 'grid') insertAtCursor('grid: |  |  |  |  |\n');
    else if (btn.dataset.insert === 'bars') insertAtCursor('bars: 4\n');
    else if (btn.dataset.insert === 'comment') insertAtCursor('# ');
    else if (btn.dataset.insert === 'score') insertAtCursor('score[drum]: bd4 hh8 hh8 sn4 hh8 hh8\n');
    else if (btn.dataset.insert === 'chord') insertAtCursor('chord: Cm 8:XX3(111)\n');
    else if (btn.dataset.wrap === 'chord') {
      const start = rawTextarea.selectionStart, end = rawTextarea.selectionEnd;
      const sel = rawTextarea.value.slice(start, end) || 'G';
      rawTextarea.value = rawTextarea.value.slice(0, start) + '[' + sel + ']' + rawTextarea.value.slice(end);
      rawTextarea.dispatchEvent(new Event('input'));
    }
  });
  // Enter in the custom-label field acts like clicking "Add", and
  // outside-click/Escape close the menu — <details> alone only gives the
  // open/close toggle itself, not these two usual popover conveniences.
  document.getElementById('section-menu-custom')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      e.preventDefault();
      document.getElementById('section-menu-custom-add').click();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      sectionMenu.open = false;
    }
  });
  document.addEventListener('click', e => {
    if (sectionMenu?.open && !sectionMenu.contains(e.target)) sectionMenu.open = false;
  });

  const sourcePreview = document.getElementById('source-preview');
  const sourcePreviewInstruments = document.getElementById('source-preview-instruments');
  let previewInstrument = '';
  let previewTimer = null;
  async function renderPreview() {
    try {
      const res = await fetch('/api/preview', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: rawTextarea.value, instrument: previewInstrument }),
      });
      const html = await res.text();
      sheet.innerHTML = html;
      rebindSheet();
      // The Source panel's own two-pane preview (see play.html) — a
      // second, non-interactive mount of the exact same markup, since
      // the real #sheet sits behind this drawer while it's open.
      if (sourcePreview) {
        sourcePreview.innerHTML = html;
        if (window.AlphaTabTab) {
          sourcePreview.querySelectorAll('.alphatab-row[data-tex]').forEach(window.AlphaTabTab.render);
        }
      }
    } catch (e) { /* leave the last good render in place */ }
  }
  rawTextarea.addEventListener('input', () => { clearTimeout(previewTimer); previewTimer = setTimeout(renderPreview, 400); });
  // Instrument picker for the Source panel's own live preview — client-
  // side only (no page reload, unlike the real Play page's own picker:
  // there's no saved page to reload mid-edit), just re-renders the same
  // preview with a different track selected.
  sourcePreviewInstruments?.addEventListener('click', e => {
    const btn = e.target.closest('button[data-instrument]');
    if (!btn) return;
    previewInstrument = btn.dataset.instrument;
    sourcePreviewInstruments.querySelectorAll('button').forEach(b => b.classList.toggle('active', b === btn));
    renderPreview();
  });

  // saveRaw saves Source; the import wizard calls it too, before the
  // steps that need the chart saved (markers go on saved sections).
  async function saveRaw() {
    const status = document.getElementById('save-status');
    status.className = ''; status.textContent = t('Saving…');
    const res = await fetch(`/api/songs/${pageVariantId}/raw`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: rawVersion, text: rawTextarea.value }),
    });
    if (res.status === 409) { status.className = 'err'; status.textContent = t('Not saved: this song changed since you opened it. Copy your changes, reload the page, then save again.'); return false; }
    if (!res.ok) { status.className = 'err'; status.textContent = t("Couldn't save. Your changes are still here; try again."); return false; }
    const data = await res.json();
    rawVersion = data.version;
    status.className = 'ok'; status.textContent = t('Saved.');
    window.SongStructure?.reload();
    await renderPreview();
    savedSheetHTML = sheet.innerHTML; // this is now the saved state
    clearDirty();
    // A tempo saved here is the player's at once, as Tap tempo's is
    // (audio-player.js listens): the bars it estimates on the recording
    // start from it.
    const tm = /^\s*tempo:\s*([\d.]+)\s*$/m.exec(rawTextarea.value);
    const playerBpm = parseFloat(document.getElementById('transport')?.dataset.bpm || '0');
    if (tm && +tm[1] > 0 && +tm[1] !== playerBpm) document.dispatchEvent(new CustomEvent('songbooks:tempo-saved', { detail: { tempo: +tm[1], version: rawVersion, from: 'source' } }));
    return true;
  }
  document.getElementById('save-btn').addEventListener('click', saveRaw);

  // ---- Redo this song (destructive: erases body, keeps book/stem identity) --
  document.getElementById('redo-song-btn')?.addEventListener('click', async () => {
    const title = document.querySelector('.topbar-current h1')?.textContent?.trim() || 'untitled';
    if (!confirm(t("Erase all the lyrics and chords of “%s”? Its title, artist and other details stay. This can't be undone.", title))) return;
    const status = document.getElementById('save-status');
    const redoStatus = document.getElementById('redo-song-status');
    const redoBtn = document.getElementById('redo-song-btn');
    Forms.busy(redoBtn, true);
    const res = await fetch(`/api/songs/${pageVariantId}/redo`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title, version: rawVersion }),
    }).finally(() => Forms.busy(redoBtn, false));
    if (res.status === 409) { Forms.status(redoStatus, t('This song changed since you opened it. Reload the page, then start over again.'), 'error'); return; }
    if (!res.ok) { Forms.status(redoStatus, t("Couldn't erase the song. Try again."), 'error'); return; }
    Forms.status(redoStatus, '');
    const data = await res.json();
    rawVersion = data.version;
    rawTextarea.value = data.text;
    // Drop the user straight into the same raw/structure editing surface a
    // brand-new song's creation flow uses — not back onto the (now blank)
    // chart — since this is explicitly "start the wizard over for this
    // song," not "go look at what you just erased."
    document.querySelectorAll('.prep-tab').forEach(b => b.classList.toggle('active', b.dataset.tab === 'raw'));
    document.querySelectorAll('.prep-panel').forEach(p => { p.hidden = p.dataset.panel !== 'raw'; });
    setDrawerOpen(true);
    await renderPreview();
    savedSheetHTML = sheet.innerHTML;
    clearDirty();
    if (status) { status.className = 'ok'; status.textContent = t('Erased. Fill the song back in here.'); }
  });

  // ---- Importers --------------------------------------------------------------
  function titleArtist() {
    const title = (rawTextarea.value.match(/^title:\s*(.+)$/m) || [, ''])[1].trim();
    const artist = (rawTextarea.value.match(/^artist:\s*(.+)$/m) || [, ''])[1].trim();
    return { title, artist };
  }

  function insertAtCursor(text) {
    const start = rawTextarea.selectionStart ?? rawTextarea.value.length;
    const end = rawTextarea.selectionEnd ?? rawTextarea.value.length;
    rawTextarea.value = rawTextarea.value.slice(0, start) + text + rawTextarea.value.slice(end);
    rawTextarea.dispatchEvent(new Event('input'));
  }

  // insertSectionLabel drops "[Label]\n" right before the CURSOR, or —
  // when text is selected (e.g. a block of pasted, unsectioned lyrics
  // the person just highlighted as "this is the chorus") — right before
  // the START of that selection, leaving the selected text itself
  // untouched rather than replacing it the way insertAtCursor's plain
  // start/end splice would. A blank line is added first UNLESS the
  // insertion point is already at the very start of the text or right
  // after an existing blank line, so repeated taps don't pile up extra
  // blank lines between sections.
  function insertSectionLabel(label) {
    const at = rawTextarea.selectionStart ?? rawTextarea.value.length;
    const before = rawTextarea.value.slice(0, at);
    const needsBlankLine = before.length > 0 && !/\n\s*\n$/.test(before) && !before.endsWith('\n\n');
    const prefix = (needsBlankLine ? (before.endsWith('\n') ? '\n' : '\n\n') : '');
    const insertion = prefix + '[' + label + ']\n';
    rawTextarea.value = before + insertion + rawTextarea.value.slice(at);
    const caret = at + insertion.length;
    rawTextarea.selectionStart = rawTextarea.selectionEnd = caret;
    rawTextarea.dispatchEvent(new Event('input'));
  }

  // ---- Transpose the song (Source) ---------------------------------------------
  // Rewrites the Source text into another key for real, unlike the view-only
  // transpose above. The rewrite runs on the server (POST /api/transpose,
  // song.TransposeText): the .song parser lives in Go, and a JS copy of its
  // line rules (which lines are grids, lyrics, score voices) would drift
  // from it. The new text lands here unsaved: the preview shows it, Discard
  // or Cmd/Ctrl+Z drops it, Save stores it in the song's history like any
  // edit. Chord diagrams of the old key stay, marked "chord[Old]:".
  async function postTranspose(body) {
    const res = await fetch('/api/transpose', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: rawTextarea.value, ...body }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || t("Couldn't transpose the song. Try again."));
    return data;
  }
  function signed(n) { return n > 0 ? '+' + n : '−' + Math.abs(n); }

  const transposeOpen = document.getElementById('transpose-open');
  const transposePanel = document.getElementById('transpose-panel');
  if (transposeOpen && transposePanel) {
    const form = document.getElementById('transpose-form');
    // The form's wrapper, not the form, gets hidden: .form-stack's own
    // display:flex would beat the [hidden] attribute.
    const ask = document.getElementById('transpose-ask');
    const fromField = document.getElementById('transpose-from-field');
    const fromSel = document.getElementById('transpose-from');
    const toSel = document.getElementById('transpose-to');
    const applyBtn = document.getElementById('transpose-apply');
    const done = document.getElementById('transpose-done');
    const doneText = document.getElementById('transpose-done-text');
    const diagramsBtn = document.getElementById('transpose-diagrams-btn');

    function fillTargets(targets) {
      toSel.innerHTML = '';
      toSel.add(new Option(t('Choose a key'), ''));
      targets.forEach(t => toSel.add(new Option(`${t.key} (${signed(t.semitones)})`, t.key)));
    }
    async function analyze(from) {
      Forms.status(form, '');
      try {
        const data = await postTranspose(from ? { from } : {});
        fromField.hidden = !data.keyGuessed;
        if (data.keyGuessed && !from) {
          fromSel.innerHTML = '';
          data.allKeys.forEach(k => fromSel.add(new Option(k, k, false, k === data.key)));
        }
        fillTargets(data.targets);
        if (!data.key) Forms.status(form, t('This song has no chords to transpose yet.'), 'error');
      } catch (e) { Forms.status(form, e.message, 'error'); }
    }
    function setOpen(open) {
      transposePanel.hidden = !open;
      transposeOpen.setAttribute('aria-expanded', String(open));
      if (!open) return;
      ask.hidden = false;
      done.hidden = true;
      analyze('');
      toSel.focus();
    }
    transposeOpen.addEventListener('click', () => setOpen(transposePanel.hidden));
    document.getElementById('transpose-cancel').addEventListener('click', () => setOpen(false));
    fromSel.addEventListener('change', () => analyze(fromSel.value));

    form.addEventListener('submit', e => {
      e.preventDefault();
      Forms.run(form, applyBtn, async () => {
        const data = await postTranspose({ from: fromField.hidden ? '' : fromSel.value, to: toSel.value });
        replaceSource(data.text);
        ask.hidden = true;
        done.hidden = false;
        let msg = t('Transposed from %s to %s (%s). Check the preview, then save.', data.from, data.to, signed(data.semitones));
        if (data.kept) msg += ' ' + t('The chord diagrams for %s stay saved, for when you play it in %s.', data.from, data.from);
        doneText.textContent = msg;
        // Diagrams for the new key: the song's chords, already transposed,
        // that have no shape yet ("missing" may be empty when the song held
        // a set for this key from an earlier transpose).
        diagramsBtn.hidden = !data.missing.length;
        diagramsBtn.textContent = t('Enter chord diagrams for %s', data.to);
        diagramsBtn.dataset.first = data.missing[0] || '';
      });
    });

    diagramsBtn.addEventListener('click', () => {
      document.querySelector('.prep-tab[data-tab="scores"]')?.click();
      const name = document.getElementById('cde-name');
      if (name && diagramsBtn.dataset.first) {
        name.value = diagramsBtn.dataset.first;
        name.dispatchEvent(new Event('input'));
      }
      document.getElementById('chord-diagram-heading')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
  }

  // replaceSource swaps the whole Source text through the editing API
  // rather than assigning .value, so the textarea's own undo (Cmd/Ctrl+Z)
  // can take it back; the assignment is the fallback where that API
  // doesn't apply.
  function replaceSource(text) {
    rawTextarea.focus();
    rawTextarea.select();
    let ok = false;
    try { ok = document.execCommand('insertText', false, text); } catch (e) { ok = false; }
    if (!ok || rawTextarea.value !== text) {
      rawTextarea.value = text;
      rawTextarea.dispatchEvent(new Event('input'));
    }
    rawTextarea.setSelectionRange(0, 0);
    rawTextarea.scrollTop = 0;
  }

  // ---- Chord diagrams still to enter (Scores tab, next to the builder) --------
  // The chords the song plays with no diagram yet for its key, as chips;
  // tapping one puts its name in the builder (score-editor.js).
  const cdeTodo = document.getElementById('cde-todo');
  async function refreshDiagramTodo() {
    if (!cdeTodo) return;
    let data;
    try { data = await postTranspose({}); } catch (e) { return; }
    const chips = document.getElementById('cde-todo-chips');
    chips.innerHTML = '';
    cdeTodo.hidden = !data.missing.length;
    if (!data.missing.length) return;
    document.getElementById('cde-todo-text').textContent = data.key && !data.keyGuessed
      ? t('Chords in this song with no diagram for %s yet:', data.key)
      : t('Chords in this song with no diagram yet:');
    data.missing.forEach(sym => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'chip-link';
      b.textContent = sym;
      b.addEventListener('click', () => {
        const name = document.getElementById('cde-name');
        if (!name) return;
        name.value = sym;
        name.dispatchEvent(new Event('input'));
        document.getElementById('cde-grid')?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
      });
      chips.appendChild(b);
    });
  }
  // A "chord:" line only counts in the header, before the first [Section],
  // but the builder inserts at the Source caret wherever it was left. This
  // runs first (capture phase on document) and puts the caret after the
  // header's last diagram line; the list refreshes once the line is in.
  document.addEventListener('click', e => {
    if (!e.target.closest || !e.target.closest('#cde-insert')) return;
    placeCaretForDiagram();
    setTimeout(refreshDiagramTodo, 0);
  }, true);
  function placeCaretForDiagram() {
    const text = rawTextarea.value;
    let pos = 0, slot = 0, lastDiagram = -1;
    for (const line of text.split('\n')) {
      const t = line.trim();
      if (/^\[[^\[\]]+\](\s*\(repeat\))?$/.test(t)) break;
      const end = pos + line.length + 1;
      if (/^chord(\[[^\]]*\])?:/.test(t)) lastDiagram = end;
      if (t && !t.startsWith('#')) slot = end;
      pos = end;
    }
    let at = lastDiagram >= 0 ? lastDiagram : slot;
    if (at > text.length) {
      rawTextarea.value = text + '\n';
      at = rawTextarea.value.length;
    }
    rawTextarea.selectionStart = rawTextarea.selectionEnd = at;
  }

  // ---- Import wizard (play.html #import-wizard): a dialog of five steps,
  // Chart · Review · Lyrics · Recording · Sync, the song check on the
  // first one (/api/songs/{id}/import/assess) saying which are worth
  // doing. One primary action per step, in the footer: Next does that
  // step's work (fetch the chosen chart, put the reviewed result or the
  // placed lyrics into Source, use the best-rated recording, save the
  // sync). Previews show a result before it goes in. The Import tab's
  // buttons open it at their step; its Source tools (iReal Pro's chords,
  // cleanup) work on Source right there. Server side: internal/songimport.
  (function wireImportWizard() {
    const wizard = document.getElementById('import-wizard');
    if (!wizard) return;
    const $id = (id) => document.getElementById(id);
    const ORDER = ['find', 'review', 'lyrics', 'recording', 'sync'];
    const steps = Object.fromEntries(ORDER.map(n => [n, wizard.querySelector(`[data-step="${n}"]`)]));
    const dots = wizard.querySelectorAll('[data-step-dot]');
    const backBtn = $id('import-wiz-back');
    const skipBtn = $id('import-wiz-skip');
    const nextBtn = $id('import-wiz-next');
    const applyStatus = $id('import-apply-status');
    const reviewText = $id('import-review-text');
    const reviewHint = $id('import-review-hint');
    const stripChordsCheck = $id('import-strip-chords');
    const irealCheckBtn = $id('import-ireal-check-btn');
    const irealStatus = $id('import-ireal-status');
    const irealCandidatesHost = $id('import-ireal-candidates');
    const cleanNotes = $id('import-clean-notes');
    const urlInput = $id('import-url-input');

    let current = 'find';
    const passed = new Set(); // the steps left with Next or Skip: ticked
    let suggested = new Set(); // the steps the song check suggests
    let hasDraft = false; // Review has a fetched draft to go back to
    let importSourceURL = '';
    let importMeta = {}; // { title, artist, capo } from the fetch, for an appended draft's header
    let assessment = null;

    async function postJSON(url, body) {
      const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || res.statusText);
      return data;
    }
    function fillList(host, items) {
      host.innerHTML = '';
      items.forEach(text => { const li = document.createElement('li'); li.textContent = text; host.appendChild(li); });
    }
    // setText puts text into the draft or into Source; Source then needs
    // saving, and its preview redrawing.
    function setText(target, text) {
      target.value = text;
      if (target === rawTextarea) {
        markDirty();
        rawTextarea.dispatchEvent(new Event('input'));
      }
    }
    function showInSource() {
      document.querySelectorAll('.prep-tab').forEach(b => b.classList.toggle('active', b.dataset.tab === 'raw'));
      document.querySelectorAll('.prep-panel').forEach(p => { p.hidden = p.dataset.panel !== 'raw'; });
      rawTextarea.focus();
    }
    // A chart text rendered the way the song page shows it.
    async function previewInto(el, text) {
      el.hidden = false;
      try {
        const res = await fetch('/api/preview', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text }) });
        el.innerHTML = await res.text();
      } catch (e) { el.textContent = t("Couldn't show the preview just now."); }
    }
    function fmtTime(s) { s = Math.round(s || 0); return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`; }

    function stripChordMarkers(text) {
      return text.split('\n').map(line => {
        if (/^grid:\s*/.test(line)) return line;
        if (/^\[[^\]]*\](?:\s*\(repeat\))?$/.test(line.trim())) return line;
        return line.replace(/\[[^\]\n]*\]/g, '');
      }).join('\n');
    }
    function hasHeaderField(key) {
      return new RegExp(`^${key}:\\s*\\S`, 'm').test(rawTextarea.value);
    }
    function mergeHeaderFields(meta) {
      const value = rawTextarea.value;
      const m = value.match(/^\[/m);
      const headerEnd = m ? m.index : value.length;
      let headerZone = value.slice(0, headerEnd);
      const rest = value.slice(headerEnd);
      let additions = '';
      if (meta.title && !hasHeaderField('title')) additions += `title: ${meta.title}\n`;
      if (meta.artist && !hasHeaderField('artist')) additions += `artist: ${meta.artist}\n`;
      if (meta.capo && !hasHeaderField('capo')) additions += `capo: ${meta.capo}\n`;
      if (!additions) return;
      // A blank "title:" placeholder (the one genuinely-empty-title
      // creation path leaves) would otherwise sit right next to the
      // real one this just added — drop it rather than leave both.
      headerZone = headerZone.replace(/^title:[ \t]*\n/, '');
      const trimmed = headerZone.replace(/\s+$/, '');
      rawTextarea.value = (trimmed ? trimmed + '\n' + additions : additions) + '\n' + rest.replace(/^\n+/, '');
    }
    function escapeHtml(s) {
      return s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }
    // host shows the candidates; target is the textarea the accepted ones
    // go into (the draft, or Source itself when fixing the song as it is).
    function renderIRealCandidates(candidates, host = irealCandidatesHost, target = reviewText, statusEl = irealStatus) {
      const irealCandidatesHost = host;
      irealCandidatesHost.innerHTML = '';
      if (!candidates.length) return;
      candidates.forEach(c => {
        const row = document.createElement('div');
        row.className = 'import-ireal-candidate' + (c.Safe ? ' is-safe' : '');
        const flag = c.Safe
          ? t('same key — safe to accept')
          : (c.Offset ? t('transposed %s semitones — please check before accepting', (c.Offset > 0 ? '+' : '') + c.Offset) : t('low-confidence match — please check'));
        row.innerHTML = `
          <div class="import-ireal-candidate-head">
            <span class="import-ireal-candidate-label">[${c.Label}]</span>
            <span class="import-ireal-candidate-flag${c.Safe ? ' is-safe' : ''}">${flag}</span>
          </div>
          <div class="import-ireal-candidate-grids">
            <div><span class="label">${t('current')}</span>${c.CurrentGrid ? escapeHtml(c.CurrentGrid) : t('(none)')}</div>
            <div><span class="label">iReal Pro</span>${escapeHtml(c.ProposedGrid)}</div>
          </div>
          <div class="import-ireal-candidate-actions">
            <label><input type="checkbox" class="import-ireal-accept" ${c.Safe ? 'checked' : ''}> ${t('Accept this section')}</label>
          </div>`;
        row.querySelector('.import-ireal-accept').dataset.label = c.Label;
        irealCandidatesHost.appendChild(row);
      });
      const applyRow = document.createElement('div');
      applyRow.className = 'import-row';
      const applyIrealBtn = document.createElement('button');
      applyIrealBtn.type = 'button';
      applyIrealBtn.textContent = t('Apply accepted sections to draft');
      const applyIrealStatus = document.createElement('span');
      applyIrealStatus.className = 'hint';
      applyRow.append(applyIrealBtn, applyIrealStatus);
      irealCandidatesHost.appendChild(applyRow);
      applyIrealBtn.addEventListener('click', async () => {
        const { title, artist } = titleArtist();
        const acceptLabels = Array.from(irealCandidatesHost.querySelectorAll('.import-ireal-accept:checked')).map(el => el.dataset.label);
        if (!acceptLabels.length) { applyIrealStatus.textContent = t('Tick at least one section first.'); applyIrealStatus.className = 'err'; return; }
        applyIrealBtn.disabled = true;
        applyIrealStatus.textContent = t('Applying…'); applyIrealStatus.className = 'hint';
        try {
          const res = await fetch('/api/import/ireal/merge', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ currentText: target.value.trim(), title, artist: artist || '', apply: true, acceptLabels }),
          });
          const data = await res.json().catch(() => ({}));
          if (!res.ok) throw new Error(t("Couldn't apply those sections. Try again."));
          setText(target, data.text);
          irealCandidatesHost.innerHTML = '';
          statusEl.textContent = t('Done. The sections you accepted now use the iReal Pro chords.'); statusEl.className = 'hint';
        } catch (e) {
          applyIrealStatus.textContent = e.message; applyIrealStatus.className = 'err';
        } finally {
          applyIrealBtn.disabled = false;
        }
      });
    }
    // ---- The steps -----------------------------------------------------
    function nextLabel(name) {
      switch (name) {
        case 'find': {
          const choice = chartChoice();
          return (choice === 'keep' ? t('Keep this chart') : t('Fetch')) + ' →';
        }
        case 'review': return t('Put into Source') + ' →';
        case 'lyrics': return (placedText ? t('Put into Source') : t('Next')) + ' →';
        case 'recording': return (bestUnused() ? t('Use the best-rated') : t('Next')) + ' →';
        default: return t('Save and finish');
      }
    }
    function refreshNav() {
      const at = ORDER.indexOf(current);
      dots.forEach((d, i) => {
        const name = ORDER[i];
        d.classList.toggle('active', i === at);
        d.classList.toggle('done', i !== at && passed.has(name));
        d.classList.toggle('suggested', suggested.has(name) && !passed.has(name) && i !== at);
        if (i === at) d.setAttribute('aria-current', 'step'); else d.removeAttribute('aria-current');
        // Said to screen readers: its state, not only its colour.
        const state = i === at ? t('current step') : passed.has(name) ? t('done') : suggested.has(name) ? t('suggested') : '';
        d.setAttribute('aria-label', `${i + 1}. ${d.querySelector('.stepper-label').textContent}${state ? ', ' + state : ''}`);
      });
      // Phones show the current step's name under the numbers.
      $id('import-wizard-caption').textContent = t('Step %s of %s: %s', at + 1, ORDER.length, dots[at].querySelector('.stepper-label').textContent);
      backBtn.hidden = at === 0;
      skipBtn.hidden = !['lyrics', 'recording', 'sync'].includes(current);
      skipBtn.textContent = current === 'sync' ? t('Close without saving') : t('Skip');
      nextBtn.textContent = nextLabel(current);
    }
    function showStep(name) {
      current = name;
      Object.entries(steps).forEach(([k, el]) => { el.hidden = k !== name; });
      applyStatus.textContent = ''; applyStatus.className = 'hint';
      refreshNav();
      if (name === 'recording') enterRecording();
      if (name === 'sync') runAnalysis();
    }
    // The step after this one the song check suggests (or simply the
    // next one).
    function nextSuggested(from) {
      const rest = ORDER.slice(ORDER.indexOf(from) + 1).filter(n => n !== 'review');
      return rest.find(n => suggested.has(n)) || rest[0];
    }
    async function busy(on, label) {
      Forms.busy(nextBtn, on);
      if (label !== undefined) { applyStatus.textContent = label; applyStatus.className = 'hint'; }
    }
    function fail(msg) { applyStatus.textContent = msg; applyStatus.className = 'err'; }

    // ---- 1. Chart: the song check, and where its chart comes from ------
    const assessHost = $id('import-assess');
    const chartChoice = () => wizard.querySelector('input[name="import-chart"]:checked').value;
    wizard.querySelectorAll('input[name="import-chart"]').forEach(r => r.addEventListener('change', () => {
      $id('import-url-field').hidden = chartChoice() !== 'url';
      refreshNav();
    }));
    (function buildSearchLink() {
      const { title, artist } = titleArtist();
      const a = document.createElement('a');
      a.href = `https://www.ultimate-guitar.com/search.php?search_type=title&value=${encodeURIComponent(`${artist} ${title}`.trim())}`;
      a.target = '_blank'; a.rel = 'noopener';
      a.textContent = t('Search on Ultimate Guitar ↗');
      $id('import-search-links').appendChild(a);
    })();
    const verdictWords = {
      new: () => t('A new song: start with its chart.'),
      improve: () => t('This chart is worth keeping and improving.'),
      restart: () => t('This chart is better started again.'),
    };
    async function loadAssessment() {
      assessHost.innerHTML = '';
      try {
        const res = await fetch(`/api/songs/${pageVariantId}/import/assess`);
        if (!res.ok) return;
        assessment = await res.json();
      } catch (e) { return; }
      const head = document.createElement('p');
      head.className = 'import-assess-verdict is-' + assessment.verdict;
      head.textContent = (verdictWords[assessment.verdict] || (() => assessment.verdict))();
      const ul = document.createElement('ul');
      ul.className = 'import-notes';
      // The facts; the reasons only when they argue for starting again
      // (otherwise they restate the facts).
      [...(assessment.summary || []), ...(assessment.verdict === 'restart' ? assessment.reasons || [] : [])].forEach(s => { const li = document.createElement('li'); li.textContent = s; ul.appendChild(li); });
      assessHost.append(head, ul);
      suggested = new Set(assessment.steps || []);
      // The choices the check points to: keep a chart worth keeping,
      // fetch one otherwise; merge into a structure worth keeping.
      const choice = assessment.verdict === 'improve' ? 'keep' : 'ug';
      wizard.querySelector(`input[name="import-chart"][value="${choice}"]`).checked = true;
      wizard.querySelector(`input[name="import-merge"][value="${assessment.verdict === 'improve' ? 'merge' : 'replace'}"]`).checked = true;
      $id('import-url-field').hidden = true;
      refreshNav();
    }

    async function fetchChart(choice) {
      const { title, artist } = titleArtist();
      if (choice !== 'url' && !title) return fail(t('Give the song a title first (in the Chart tab), then search.'));
      let url;
      if (choice === 'ug') url = `/api/import/ug/find?title=${encodeURIComponent(title)}&artist=${encodeURIComponent(artist || '')}`;
      if (choice === 'vocal') url = `/api/import/vocal?title=${encodeURIComponent(title)}&artist=${encodeURIComponent(artist || '')}`;
      busy(true, choice === 'vocal' ? t('Searching Ultimate Guitar and iReal Pro…') : t('Searching Ultimate Guitar…'));
      try {
        let d;
        if (choice === 'url') {
          const address = urlInput.value.trim();
          if (!address) throw new Error(t('Give the address of the Ultimate Guitar page.'));
          d = await postJSON('/api/import/chords', { url: address });
          d.url = address;
          reviewHint.textContent = t('From %s — pasted manually, not cross-checked.', address);
        } else {
          const res = await fetch(url);
          d = await res.json().catch(() => ({}));
          if (!res.ok) throw new Error(t("Couldn't find this song on Ultimate Guitar. Try its address instead."));
          reviewHint.textContent = choice === 'vocal'
            ? t('From %s, for singers: lyrics without chords, iReal Pro grids where its chart matches.', d.url)
            : t('From %s — %s.', d.url, d.confidence);
        }
        importSourceURL = d.url;
        importMeta = { title: d.title, artist: d.artist, capo: d.capo };
        reviewText.value = d.text;
        stripChordsCheck.checked = choice === 'vocal';
        fillList(cleanNotes, d.notes || []);
        if (choice !== 'vocal') await cleanDraft();
        hasDraft = true;
        passed.add('find');
        busy(false, '');
        showView('draft');
        showStep('review');
      } catch (e) {
        busy(false);
        fail(e.message);
      }
    }

    // ---- 2. Review: the draft, how it comes in, and the result ---------
    const mergeMode = () => wizard.querySelector('input[name="import-merge"]:checked').value;
    const reviewPreview = $id('import-review-preview');
    const mergeNotes = $id('import-merge-notes');
    const viewTabs = wizard.querySelectorAll('.import-view-tabs [data-view]');
    function draftText() {
      const text = reviewText.value.trim();
      return stripChordsCheck.checked ? stripChordMarkers(text).trim() : text;
    }
    // The song as it would be: the draft merged into Source, or
    // replacing its chart, or added below it.
    async function mergedResult() {
      const r = await postJSON('/api/import/merge', { currentText: rawTextarea.value, draft: draftText(), mode: mergeMode() });
      return r;
    }
    async function showView(view) {
      viewTabs.forEach(b => { const on = b.dataset.view === view; b.classList.toggle('active', on); b.setAttribute('aria-selected', on); });
      reviewText.hidden = view !== 'draft';
      reviewPreview.hidden = view !== 'result';
      if (view !== 'result') { mergeNotes.innerHTML = ''; return; }
      try {
        const r = await mergedResult();
        fillList(mergeNotes, r.notes || []);
        await previewInto(reviewPreview, r.text);
      } catch (e) { fillList(mergeNotes, [t("Couldn't build the result: %s", e.message)]); }
    }
    viewTabs.forEach(b => b.addEventListener('click', () => showView(b.dataset.view)));
    wizard.querySelectorAll('input[name="import-merge"]').forEach(r => r.addEventListener('change', () => {
      if (!reviewPreview.hidden) showView('result');
    }));
    async function applyDraft() {
      if (!draftText()) { fail(t('Nothing to put into Source yet.')); return false; }
      try {
        const r = await mergedResult();
        if (mergeMode() === 'append') mergeHeaderFields(importMeta);
        setText(rawTextarea, mergeMode() === 'append'
          ? rawTextarea.value.replace(/\s+$/, '') + `\n\n# imported from ${importSourceURL}\n${draftText()}\n`
          : r.text);
        importSourceURL = ''; importMeta = {};
        return true;
      } catch (e) { fail(t("Couldn't build the result: %s", e.message)); return false; }
    }

    // Cleanup: every fetched draft goes through it, and the button runs
    // it again (after the chords-out box is ticked, say).
    async function cleanDraft() {
      if (!reviewText.value.trim()) return;
      try {
        const data = await postJSON('/api/import/cleanup', { text: reviewText.value, stripInlineChords: stripChordsCheck.checked });
        reviewText.value = data.text;
        fillList(cleanNotes, (data.notes || []).map(n => t('Cleaned up: %s', n)));
        if (!reviewPreview.hidden) showView('result');
      } catch (e) { /* the draft as fetched is still there */ }
    }
    $id('import-clean-btn').addEventListener('click', cleanDraft);
    stripChordsCheck.addEventListener('change', cleanDraft);

    function keyLines(k) {
      const out = [];
      let chart = t('Chart: %s', k.chart || '?') + (k.chartFrom === 'chords' ? ' ' + t('(from its chords)') : '');
      if (k.capo) chart += ' — ' + t('capo %s, sounding %s', k.capo, k.sounding);
      out.push(chart);
      if (k.ireal) out.push(t('iReal Pro: %s', k.ireal) + (k.irealAgrees ? ' ✓' : ' ✗'));
      if (k.audio) out.push(t('Recording: %s (%s% sure)', k.audio, Math.round((k.audioConfidence || 0) * 100)) + (k.audioAgrees ? ' ✓' : ' ✗'));
      if (k.shiftToAudio) out.push(t('Transpose the chart %s semitones to match the recording (Chart tab, Transpose).', (k.shiftToAudio > 0 ? '+' : '') + k.shiftToAudio));
      (k.notes || []).forEach(n => out.push(t(n)));
      return out;
    }
    $id('import-key-btn').addEventListener('click', async () => {
      const text = reviewText.value.trim();
      if (!text) return;
      const { title, artist } = titleArtist();
      const header = `title: ${title}\nartist: ${artist || ''}\n`;
      try {
        const k = await postJSON('/api/import/keycheck', { text: /^title:/m.test(text) ? text : header + text, songId: pageVariantId });
        fillList(cleanNotes, keyLines(k));
      } catch (e) { fillList(cleanNotes, [t("Couldn't check the key just now.")]); }
    });
    // iReal Pro: proposes bar-accurate grids section by section, ticked
    // ones applied to the draft (see internal/irealpro/merge.go on why a
    // transposed match is never applied on its own).
    irealCheckBtn.addEventListener('click', async () => {
      const { title, artist } = titleArtist();
      const currentText = reviewText.value.trim();
      if (!title || !currentText) return;
      irealCheckBtn.disabled = true;
      irealStatus.textContent = t('Checking iReal Pro…'); irealStatus.className = 'hint';
      irealCandidatesHost.innerHTML = '';
      try {
        const data = await postJSON('/api/import/ireal/merge', { currentText, title, artist: artist || '' });
        renderIRealCandidates(data.candidates || []);
        irealStatus.textContent = (data.candidates || []).length
          ? t('Found a chart. Check each section below before accepting it.')
          : t('Found a chart, but it has nothing new to add.');
      } catch (e) {
        irealStatus.textContent = t('No iReal Pro chart found for this song.'); irealStatus.className = 'err';
      } finally {
        irealCheckBtn.disabled = false;
      }
    });

    // ---- 3. Lyrics, into the sections the song already has -------------
    const lyricsUrl = $id('import-lyrics-url');
    const lyricsText = $id('import-lyrics-text');
    const lyricsStatus = $id('import-lyrics-status');
    const lyricsPlacements = $id('import-lyrics-placements');
    const lyricsPreview = $id('import-lyrics-preview');
    let placedText = '';
    const lyricsSource = () => wizard.querySelector('input[name="import-lyrics-source"]:checked').value;
    wizard.querySelectorAll('input[name="import-lyrics-source"]').forEach(r => r.addEventListener('change', () => {
      lyricsUrl.hidden = lyricsSource() !== 'url';
      lyricsText.hidden = lyricsSource() !== 'text';
    }));
    const placementWords = {
      placed: () => t('placed'), reused: () => t('the chorus again'), kept: () => t('kept its own lyrics'),
      missing: () => t('no lyrics found for it'), instrumental: () => t('instrumental'),
    };
    $id('import-lyrics-btn').addEventListener('click', async (e) => {
      const btn = e.currentTarget; // null once an await has run
      const { title, artist } = titleArtist();
      const source = lyricsSource();
      const input = source === 'url' ? lyricsUrl.value.trim() : source === 'text' ? lyricsText.value : '';
      if (source !== 'ug' && source !== 'own' && !input) { lyricsStatus.textContent = source === 'url' ? t('Give the address of the lyrics page.') : t('Paste the lyrics first.'); lyricsStatus.className = 'err'; return; }
      Forms.busy(btn, true);
      lyricsStatus.textContent = t('Fetching…'); lyricsStatus.className = 'hint';
      placedText = ''; lyricsPreview.hidden = true; refreshNav();
      try {
        const r = await postJSON('/api/import/lyrics/place', {
          currentText: rawTextarea.value, source, input, title, artist: artist || '',
          replace: $id('import-lyrics-replace').checked,
        });
        placedText = r.text;
        lyricsStatus.textContent = source === 'own' ? t('Re-split from this song\'s own lyrics: nothing dropped or rewritten.') : tn(r.stanzas, '%d stanza', '%d stanzas') + ' ' + t('from %s.', r.from);
        const items = (r.placements || []).map(p => `${p.Section}: ${(placementWords[p.Status] || (() => p.Status))()}`);
        if ((r.unused || []).length) items.push(tn(r.unused.length, '%d stanza not placed: the song may need more sections.', '%d stanzas not placed: the song may need more sections.'));
        (r.notes || []).forEach(n => items.push(n));
        fillList(lyricsPlacements, items);
        await previewInto(lyricsPreview, placedText);
        refreshNav();
      } catch (err) {
        lyricsStatus.textContent = t("Couldn't get lyrics: %s", err.message); lyricsStatus.className = 'err';
      } finally {
        Forms.busy(btn, false);
      }
    });
    function applyPlaced() {
      setText(rawTextarea, placedText);
      placedText = ''; lyricsPlacements.innerHTML = ''; lyricsStatus.textContent = ''; lyricsPreview.hidden = true;
    }

    // ---- 4. Recording: YouTube's candidates, listened to, starred -------
    const ytStatus = $id('import-yt-status');
    const ytCurrent = $id('import-yt-current');
    const ytList = $id('import-yt-candidates');
    const ytSort = $id('import-yt-sort');
    const stemWords = { queued: () => t('waiting to be split into voice and backing'), running: () => t('being split into voice and backing'), done: () => t('split into voice and backing'), failed: () => t("couldn't be split into voice and backing") };
    let candidates = null; // this opening's search results
    let currentURL = '';
    let stemTimer = null;
    // Stars are this browser's notes while choosing (localStorage, per
    // song): 1 to 3, sorted on when asked, the best one used by Next.
    const starsKey = `sb-import-stars-${pageVariantId}`;
    function loadStars() { try { return JSON.parse(localStorage.getItem(starsKey) || '{}'); } catch (e) { return {}; } }
    function saveStars(st) { try { localStorage.setItem(starsKey, JSON.stringify(st)); } catch (e) { /* stars just don't stick */ } }
    let stars = loadStars();
    function bestUnused() {
      if (!candidates) return null;
      const best = candidates.filter(c => stars[c.id]).sort((a, b) => stars[b.id] - stars[a.id])[0];
      return best && best.url !== currentURL ? best : null;
    }
    async function showRecording() {
      clearTimeout(stemTimer);
      try {
        const res = await fetch(`/api/songs/${pageVariantId}/audio`);
        const data = await res.json();
        const yt = (data.categories || []).find(c => c.category === 'youtube');
        if (!yt) { currentURL = ''; ytCurrent.textContent = t('No reference recording yet.'); return; }
        currentURL = yt.sourceUrl || '';
        ytCurrent.textContent = t('Current recording: %s, %s.', yt.sourceUrl || t('uploaded'), (stemWords[yt.stemStatus] || (() => yt.stemStatus || ''))());
        if (yt.stemStatus === 'queued' || yt.stemStatus === 'running') stemTimer = setTimeout(showRecording, 15000);
      } catch (e) { /* the list still works */ }
      if (current === 'recording') refreshNav();
    }
    function enterRecording() {
      showRecording();
      if (!candidates) findRecordings();
    }
    async function findRecordings() {
      const { title, artist } = titleArtist();
      if (!title) { ytStatus.textContent = t('Give the song a title first (in the Chart tab), then search.'); ytStatus.className = 'err'; return; }
      ytStatus.textContent = t('Searching YouTube…'); ytStatus.className = 'hint';
      ytList.innerHTML = '';
      try {
        const res = await fetch(`/api/import/youtube/find?title=${encodeURIComponent(title)}&artist=${encodeURIComponent(artist || '')}`);
        const data = await res.json();
        if (!res.ok) throw new Error();
        candidates = data.candidates || [];
        ytStatus.textContent = candidates.length ? '' : t('Nothing found on YouTube.');
        renderCandidates();
        if (current === 'recording') refreshNav(); // Next may now use a starred one
      } catch (err) {
        ytStatus.textContent = t("Couldn't search YouTube just now."); ytStatus.className = 'err';
      }
    }
    $id('import-yt-find-btn').addEventListener('click', findRecordings);
    ytSort.addEventListener('change', renderCandidates);
    function renderCandidates() {
      ytList.innerHTML = '';
      const list = (candidates || []).slice();
      if (ytSort.checked) list.sort((a, b) => (stars[b.id] || 0) - (stars[a.id] || 0));
      list.forEach(c => {
        const li = document.createElement('li');
        li.className = 'import-yt-candidate' + (c.official ? ' is-official' : '') + (c.url === currentURL ? ' is-current' : '');
        const title = document.createElement('a');
        title.href = c.url; title.target = '_blank'; title.rel = 'noopener'; title.className = 'import-yt-title';
        title.textContent = c.title;
        const meta = document.createElement('div');
        meta.className = 'hint';
        meta.textContent = [c.channel + (c.official ? ' — ' + t("the artist's channel") : ''), fmtTime(c.duration), c.note ? t(c.note) : '', c.url === currentURL ? t('in use') : ''].filter(Boolean).join(' · ');
        const actions = document.createElement('div');
        actions.className = 'import-yt-actions';
        const listen = document.createElement('button');
        listen.type = 'button'; listen.textContent = '▶ ' + t('Listen');
        const embed = document.createElement('div');
        embed.className = 'reference-embed import-yt-embed';
        listen.addEventListener('click', () => {
          if (embed.firstChild) { embed.innerHTML = ''; listen.textContent = '▶ ' + t('Listen'); return; }
          const iframe = document.createElement('iframe');
          iframe.src = `https://www.youtube-nocookie.com/embed/${c.id}?autoplay=1`;
          iframe.title = c.title;
          iframe.allow = 'autoplay; encrypted-media; picture-in-picture';
          iframe.allowFullscreen = true;
          embed.appendChild(iframe);
          listen.textContent = '■ ' + t('Stop');
        });
        const rate = document.createElement('span');
        rate.className = 'import-stars';
        rate.setAttribute('role', 'group');
        rate.setAttribute('aria-label', t('Stars'));
        for (let n = 1; n <= 3; n++) {
          const s = document.createElement('button');
          s.type = 'button';
          s.className = 'import-star' + ((stars[c.id] || 0) >= n ? ' on' : '');
          s.textContent = (stars[c.id] || 0) >= n ? '★' : '☆';
          s.setAttribute('aria-label', tn(n, '%d star', '%d stars'));
          s.setAttribute('aria-pressed', (stars[c.id] || 0) >= n);
          s.addEventListener('click', () => {
            stars[c.id] = stars[c.id] === n ? 0 : n;
            if (!stars[c.id]) delete stars[c.id];
            saveStars(stars);
            renderCandidates(); refreshNav();
          });
          rate.appendChild(s);
        }
        const use = document.createElement('button');
        use.type = 'button'; use.textContent = t('Use this one');
        use.disabled = c.url === currentURL;
        use.addEventListener('click', () => useRecording(c, use));
        actions.append(listen, rate, use);
        li.append(title, meta, actions, embed);
        ytList.appendChild(li);
      });
    }
    async function useRecording(c, btn) {
      if (btn) Forms.busy(btn, true);
      busy(true, t('Downloading the sound… (up to a minute)'));
      try {
        await postJSON(`/api/songs/${pageVariantId}/audio/youtube`, { url: c.url });
        // The chart names its reference too (its "reference:" line).
        const m = rawTextarea.value.match(/^reference:.*$/m);
        setText(rawTextarea, m ? rawTextarea.value.replace(/^reference:.*$/m, `reference: ${c.url}`) : rawTextarea.value.replace(/^(title:.*\n(?:[a-z][\w-]*:.*\n)*)/, `$1reference: ${c.url}\n`));
        currentURL = c.url;
        // The player picks the new recording up (the Sync sheet opens on it).
        document.dispatchEvent(new CustomEvent('songbooks:audio-changed'));
        busy(false, t('Downloaded: its voice and backing are being split, then the sync can place the sections.'));
        await showRecording();
        renderCandidates();
        return true;
      } catch (err) {
        busy(false);
        fail(t("Couldn't download it: %s", err.message));
        return false;
      } finally {
        if (btn) Forms.busy(btn, false);
      }
    }

    // ---- 5. Sync: tempo, time, key and sections, checked by ear ---------
    const syncStatus = $id('import-sync-status');
    const syncResult = $id('import-sync-result');
    const syncOverwriteLabel = $id('import-sync-overwrite-label');
    const audio = $id('import-sync-audio');
    let proposal = null;
    let auditionTimer = null;
    function audition(at, btn) {
      clearTimeout(auditionTimer);
      wizard.querySelectorAll('.import-sync-play.playing').forEach(b => { b.classList.remove('playing'); b.textContent = '▶'; });
      if (!audio.src) audio.src = `/song/${pageVariantId}/audio.mp3?category=youtube`;
      if (btn.dataset.playing === '1') { audio.pause(); btn.dataset.playing = ''; return; }
      wizard.querySelectorAll('.import-sync-play').forEach(b => { b.dataset.playing = ''; });
      audio.currentTime = Math.max(0, at - 0.5);
      audio.play().catch(() => {});
      btn.dataset.playing = '1'; btn.classList.add('playing'); btn.textContent = '■';
      auditionTimer = setTimeout(() => { audio.pause(); btn.dataset.playing = ''; btn.classList.remove('playing'); btn.textContent = '▶'; }, 8000);
    }
    async function runAnalysis() {
      syncResult.innerHTML = ''; syncOverwriteLabel.hidden = true; proposal = null;
      busy(true);
      if (isDirty && !(await saveRaw())) {
        busy(false);
        syncStatus.textContent = t("Couldn't save the chart: markers go on its saved sections."); syncStatus.className = 'err';
        return;
      }
      syncStatus.textContent = t('Listening to the recording… (a few seconds)'); syncStatus.className = 'hint';
      try {
        // In the page's language: its notes are said on the server.
        const p = await postJSON(`/api/songs/${pageVariantId}/audio/youtube/analyze?lang=${encodeURIComponent(document.documentElement.lang || '')}`, {});
        proposal = p;
        syncStatus.textContent = '';
        const lines = [
          t('Recording: %s, sound from %ss. Its own estimate: %s bpm in %s.', fmtTime(p.duration), p.audioStart.toFixed(2), Math.round(p.audioTempo), p.audioTimeSig),
          t('Chart: tempo %s in %s (time signature from %s). Fit with the voice: %s%.', p.tempo, p.timeSig,
            { chart: t('the chart'), ireal: 'iReal Pro', audio: t('the recording') }[p.timeSigFrom] || p.timeSigFrom, Math.round(p.score * 100)),
          ...keyLines(p.key),
          ...(p.notes || []),
        ];
        const ul = document.createElement('ul'); ul.className = 'import-notes';
        lines.forEach(l => { const li = document.createElement('li'); li.textContent = l; ul.appendChild(li); });
        const table = document.createElement('table'); table.className = 'import-sync-sections';
        const head = table.createTHead().insertRow();
        [t('Section'), t('Starts at'), ''].forEach(h => { const th = document.createElement('th'); th.textContent = h; head.appendChild(th); });
        const tbody = table.createTBody();
        // "Verse ×10" says nothing: number a repeated name that has none.
        const count = {}, seen = {};
        (p.sections || []).forEach(s => { count[s.label] = (count[s.label] || 0) + 1; });
        (p.sections || []).forEach(s => {
          const tr = tbody.insertRow();
          seen[s.label] = (seen[s.label] || 0) + 1;
          tr.insertCell().textContent = count[s.label] > 1 && !/\d\s*$/.test(s.label) ? `${s.label} ${seen[s.label]}` : s.label;
          const when = tr.insertCell();
          when.textContent = `${fmtTime(s.time)} (${s.time.toFixed(2)}s)`;
          // The chart's bars don't fit here (songimport.ProposeSync): say
          // how many the recording plays.
          if (s.recordingBars) {
            const warn = document.createElement('div');
            warn.className = 'import-sync-bars';
            warn.textContent = t('%d bars in the chart, about %d in the recording', s.chartBars, s.recordingBars);
            when.appendChild(warn);
          }
          const play = document.createElement('button');
          play.type = 'button'; play.className = 'icon-btn import-sync-play'; play.textContent = '▶';
          play.setAttribute('aria-label', t('Play %s from its start', s.label));
          play.addEventListener('click', () => audition(s.time, play));
          tr.insertCell().appendChild(play);
        });
        syncResult.append(ul, table);
        // Or place them by ear, starting from this proposal (sync-sheet.js).
        if (window.songbooksSyncSheet && (p.sections || []).length) {
          const byEar = document.createElement('button');
          byEar.type = 'button';
          byEar.className = 'audio-pill-btn import-sync-by-ear';
          byEar.textContent = t('Adjust by ear…');
          byEar.title = t('open the Sync sheet with this proposal, to accept or correct section by section');
          byEar.addEventListener('click', () => {
            const proposal = p.sections.map(s => ({ sectionIndex: s.index, timeSeconds: s.time }));
            wizard.close();
            window.songbooksSyncSheet.open({ category: 'youtube', proposal, source: 'voice', notes: p.notes || [] });
          });
          syncResult.appendChild(byEar);
        }
        syncOverwriteLabel.hidden = false;
      } catch (err) {
        syncStatus.textContent = t("Couldn't analyse the recording: %s", err.message); syncStatus.className = 'err';
      } finally {
        busy(false);
      }
    }
    async function saveSync() {
      if (!proposal) { wizard.close(); return; }
      busy(true, t('Saving…'));
      try {
        const r = await postJSON(`/api/songs/${pageVariantId}/audio/youtube/sync`, { version: rawVersion, overwrite: $id('import-sync-overwrite').checked });
        rawTextarea.value = r.text; rawVersion = r.version; clearDirty();
        busy(false, '');
        passed.add('sync');
        wizard.close();
        // The player picks the new sync up as it is (an estimate until
        // checked by ear: its badge offers to correct it).
        document.dispatchEvent(new CustomEvent('songbooks:audio-changed'));
        Forms.status?.(document.getElementById('save-status'), t('Saved: the player follows the new sync. Check it by ear from its “Sync estimated” badge.'), 'ok');
      } catch (err) {
        busy(false);
        fail(t("Couldn't save: %s", err.message));
      }
    }

    // ---- Navigation ------------------------------------------------------
    nextBtn.addEventListener('click', async () => {
      switch (current) {
        case 'find': {
          const choice = chartChoice();
          if (choice !== 'keep') return fetchChart(choice);
          passed.add('find');
          return showStep(nextSuggested('find'));
        }
        case 'review':
          if (await applyDraft()) { passed.add('review'); showStep('lyrics'); }
          return;
        case 'lyrics':
          if (placedText) applyPlaced();
          passed.add('lyrics');
          return showStep('recording');
        case 'recording': {
          const best = bestUnused();
          if (best && !(await useRecording(best))) return;
          passed.add('recording');
          return showStep('sync');
        }
        default:
          return saveSync();
      }
    });
    skipBtn.addEventListener('click', () => {
      if (current === 'sync') { wizard.close(); return; }
      passed.add(current);
      showStep(ORDER[ORDER.indexOf(current) + 1]);
    });
    backBtn.addEventListener('click', () => {
      const at = ORDER.indexOf(current);
      showStep(current === 'lyrics' && !hasDraft ? 'find' : ORDER[Math.max(0, at - 1)]);
    });
    dots.forEach(d => d.addEventListener('click', () => showStep(d.dataset.stepDot)));
    $id('import-wiz-close').addEventListener('click', () => wizard.close());
    // What the wizard put into Source, to check (and save, if the sync
    // hasn't already).
    wizard.addEventListener('close', () => {
      clearTimeout(stemTimer); clearTimeout(auditionTimer); audio.pause();
      wizard.querySelectorAll('.import-yt-embed').forEach(e => { e.innerHTML = ''; });
      passed.clear(); hasDraft = false; candidates = null; proposal = null;
      if (isDirty) showInSource();
    });
    // The Import tab's buttons open the wizard at their step.
    document.querySelectorAll('[data-goto]').forEach(btn => btn.addEventListener('click', async () => {
      if (!wizard.open) wizard.showModal();
      showStep(btn.dataset.goto);
      await loadAssessment();
    }));

    // ---- The Import tab's tools on Source as it is -----------------------
    const sourceStatus = $id('import-source-status');
    $id('import-source-clean-btn').addEventListener('click', async () => {
      try {
        const data = await postJSON('/api/import/cleanup', { text: rawTextarea.value, stripInlineChords: false });
        if (data.text === rawTextarea.value) { sourceStatus.textContent = t('Nothing to clean up.'); return; }
        setText(rawTextarea, data.text);
        sourceStatus.textContent = t('Cleaned up: %s. Check it, then save.', (data.notes || []).join(', '));
        showInSource();
      } catch (e) { sourceStatus.textContent = t("Couldn't clean up just now."); }
    });
    $id('import-source-ireal-btn').addEventListener('click', async () => {
      const { title, artist } = titleArtist();
      const host = $id('import-source-ireal-candidates');
      sourceStatus.textContent = t('Checking iReal Pro…');
      try {
        const data = await postJSON('/api/import/ireal/merge', { currentText: rawTextarea.value, title, artist: artist || '' });
        renderIRealCandidates(data.candidates || [], host, rawTextarea, sourceStatus);
        sourceStatus.textContent = (data.candidates || []).length
          ? t('Found a chart. Check each section below before accepting it.')
          : t('Found a chart, but it has nothing new to add.');
      } catch (e) { sourceStatus.textContent = t('No iReal Pro chart found for this song.'); }
    });
  })();

  // ---- Structure editor -------------------------------------------------------
  const structureEl = document.getElementById('structure');
  let structVersion = null, structMeta = [], structSections = [];
  // chord: diagram lines — not edited here, only carried through so a
  // Chart save (the server re-serializes the whole song from this JSON)
  // doesn't drop them.
  let structChords = [];
  // Diagrams entered for other keys ("chord[A]:" lines): carried through
  // the same way, or a Chart save would lose them.
  let structOtherChords = [];
  // The song's "lead:" line ({lead, leadMeasures}): carried through the
  // same way, so the preview draws it and a Chart save keeps it.
  let structLead = {};

  // ---- Undo/redo — a bounded-depth snapshot stack over structMeta/
  // structSections, the same in-memory state Save/Discard already work
  // against. Not a general command pattern (that's real work for a
  // feature this scoped) — pushUndo() is called explicitly at the START
  // of each mutation this editor actually has (chord taps, bar insert/
  // remove, section insert/delete — see call sites), capturing the state
  // to RESTORE TO if that action is undone. Doesn't survive a reload or
  // a Save, same as the rest of this in-memory editing session.
  const UNDO_DEPTH = 50;
  let undoStack = [], redoStack = [];
  function snapshotState() {
    return JSON.parse(JSON.stringify({ meta: structMeta, sections: structSections }));
  }
  function pushUndo() {
    undoStack.push(snapshotState());
    if (undoStack.length > UNDO_DEPTH) undoStack.shift();
    redoStack.length = 0;
    updateUndoRedoButtons();
  }
  // edited: the one call every Chart mutation makes before changing
  // anything — an undo step to come back to, and the unsaved marker.
  function edited() { pushUndo(); markDirty(); }
  // A text field records one undo step per focus, at its first keystroke,
  // rather than one per character.
  function trackTextEdits(el) {
    let armed = true;
    el.addEventListener('focus', () => { armed = true; });
    el.addEventListener('input', () => { if (armed) { pushUndo(); armed = false; } markDirty(); });
  }
  function confirmDeleteSection(sec) {
    const filled = (sec.measures || []).some(m => (m.tokens || []).some(t => t && t !== '.')) || (sec.lyrics || []).length || (sec.scores || []).length;
    return !filled || confirm(t('Delete the section “%s”? (Undo brings it back.)', sec.label || t('Untitled')));
  }
  function restoreSnapshot(snap) {
    structMeta = snap.meta;
    structSections = snap.sections;
    markDirty();
    renderHeaderFields();
    // The Chart re-renders when it is showing; an undo from another tab
    // must not fill the Chart's sidebar toolbox (switching back renders it).
    if (!structureEl.hidden) renderStructureUI();
    updateUndoRedoButtons();
  }
  function undoEdit() {
    if (!undoStack.length) return;
    redoStack.push(snapshotState());
    restoreSnapshot(undoStack.pop());
  }
  function redoEdit() {
    if (!redoStack.length) return;
    undoStack.push(snapshotState());
    restoreSnapshot(redoStack.pop());
  }
  function updateUndoRedoButtons() {
    const u = document.getElementById('edit-undo-btn'), r = document.getElementById('edit-redo-btn');
    if (u) u.disabled = !undoStack.length;
    if (r) r.disabled = !redoStack.length;
  }
  // The drawer's in-memory song, for other editors on the page to edit
  // with the same save, discard and undo.
  window.SongEditorBridge = {
    sections: () => structSections,
    meta: () => structMeta,
    measures: (sec) => joinVoltaGroups(sec),
    begin: () => { pushUndo(); markDirty(); },
    dirty: markDirty,
    undo: undoEdit,
    redo: redoEdit,
  };
  document.getElementById('edit-undo-btn')?.addEventListener('click', undoEdit);
  document.getElementById('edit-redo-btn')?.addEventListener('click', redoEdit);
  document.addEventListener('keydown', e => {
    if (!(e.metaKey || e.ctrlKey)) return;
    if (document.getElementById('structure')?.hidden) return; // Chart tab only
    const drawer = document.getElementById('edit-drawer');
    if (!drawer || drawer.hidden) return;
    const k = e.key.toLowerCase();
    if (k === 'z' && !e.shiftKey) { e.preventDefault(); undoEdit(); }
    else if (k === 'z' && e.shiftKey || k === 'y') { e.preventDefault(); redoEdit(); }
  });

  // ---- Keyboard chord entry (desktop focus editor) --------------------------
  // The focus editor's bar grid is the REAL rendered chart markup, not
  // <input> elements (see setChordPart's own "Focus mode's real chart has
  // no <input>" comment) — so a selected slot has no native text box to
  // just type into, and typing a chord by hand silently did nothing. This
  // mirrors the chord toolbar's own two-step "tap a root, then tap a
  // quality" model — same setChordPart/setSlotToken calls the buttons
  // themselves use — just driven by single keystrokes onto whichever slot
  // is currently selected (lastSelSec/lastSelIndex, updated by selectSlot/
  // focusSlot above) instead of a mouse click. A root letter still
  // respects a pending ♭/♯ tapped on the toolbar first, exactly like
  // tapping the root button would (sec._pendingAcc/_bassMode are shared
  // state either input path reads).
  const KEY_QUALITY = { m: 'm', '7': '7', M: 'maj7', o: 'dim', '0': 'dim', '+': 'aug' };
  document.addEventListener('keydown', e => {
    if (!focusModeActive || e.metaKey || e.ctrlKey || e.altKey) return;
    const t = document.activeElement;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return;
    const drawer = document.getElementById('edit-drawer');
    if (!drawer || drawer.hidden) return;
    if (document.getElementById('structure')?.hidden) return; // Chart tab only
    const sec = lastSelSec, i = lastSelIndex;
    if (!sec || !sec._sel) return;

    const key = e.key;
    if (/^[A-Ga-g]$/.test(key)) {
      e.preventDefault();
      const acc = sec._pendingAcc || '';
      const root = key.toUpperCase() + acc;
      if (sec._bassMode) { setChordPart(sec, i, 'bass', root); sec._bassMode = false; }
      else setChordPart(sec, i, 'root', root);
      sec._pendingAcc = '';
    } else if (key in KEY_QUALITY) {
      e.preventDefault();
      setChordPart(sec, i, 'quality', KEY_QUALITY[key]);
    } else if (key === '.') {
      e.preventDefault();
      setSlotToken(sec, i, '.');
      moveSelection(sec, i, 1);
    } else if (key === 'Backspace' || key === 'Delete') {
      e.preventDefault();
      setSlotToken(sec, i, '');
    }
    // Toolbar buttons re-render the WHOLE toolbar/grid (buildChordToolbar
    // is called again elsewhere, re-reading sec._pendingAcc/_bassMode) —
    // this path edits the same shared state those buttons read without
    // forcing a full rebuild, so the ♭/♯/bass "pending" button glow (a
    // separate, non-reactive CSS class toggled only in the button's own
    // click handler) can't be kept in sync without one; harmless since a
    // keyboard-only session never taps those buttons to see it drift.
  });

  // ---- Desktop "focus" editor (Guitar Pro-inspired: one section at a
  // time, persistent left toolbar, infinite single-line staff) vs.
  // mobile's existing iReal Pro-style stacked blocks list. Gated on the
  // same ≥901px breakpoint the global sidebar/tab-bar split already uses
  // (Phase 28) — a deliberately different, wider threshold than
  // isMobileEditWidth()'s 760px, which only controls toolbar DOCKING
  // within the (mobile-only) blocks list, a separate concern. Mobile's
  // renderBlocksList() is completely unmodified by any of this; every
  // former call site of it now goes through this dispatcher instead, so
  // switching width (or resizing across the breakpoint) always re-renders
  // the correct surface. focusModeActive is read by renderMatrix/
  // buildChordToolbar to decide single-line-vs-wrapped bars and the
  // split-count progression — see their own comments.
  let focusModeActive = false;
  let focusIndex = 0;
  function isDesktopWidth() {
    return window.matchMedia('(min-width: 901px)').matches;
  }
  function renderStructureUI() {
    if (isDesktopWidth()) { focusModeActive = true; renderFocusEditor(); }
    else { focusModeActive = false; renderBlocksList(); }
  }
  window.addEventListener('resize', () => {
    if (isDesktopWidth() !== focusModeActive && structureEl && !structureEl.hidden) {
      renderStructureUI();
    }
  });

  // ---- Header fields (time signature / tempo), editable inline ------------
  // Previously the only way to change these was the raw "time: 4/4" /
  // "tempo: 120" header lines in Source — fine as the advanced-syntax
  // escape hatch, but everyday tempo/meter tweaks shouldn't require
  // switching tabs. These write into the same structMeta array the Blocks
  // "Save" button already round-trips through song.Write, so there is no
  // separate save path or serializer to keep in sync.
  function metaGet(key) {
    const f = structMeta.find(f => f.key === key);
    return f ? f.value : '';
  }
  function metaSet(key, value) {
    const f = structMeta.find(f => f.key === key);
    if (f) f.value = value;
    else structMeta.push({ key, value });
  }
  function renderHeaderFields() {
    const host = document.getElementById('song-header-fields');
    if (!host) return;
    host.innerHTML = '';

    // Title/artist: the only two header fields with no other editing
    // surface on this page at all (the Source tab's raw title:/artist:
    // lines are the sole alternative) — a new, blank song (title
    // "untitled") had no way to name itself from the structured Chart
    // view, reported directly. Title changes also retitle the page/tab
    // (see the save success handler's reload), so it's the field most in
    // need of being right here, not buried in Source.
    const titleField = document.createElement('label');
    titleField.className = 'header-field';
    titleField.append(t('Title'));
    const titleInput = document.createElement('input');
    titleInput.id = 'song-title-input';
    titleInput.type = 'text';
    titleInput.autocomplete = 'off';
    titleInput.value = metaGet('title');
    titleInput.addEventListener('input', () => { markDirty(); metaSet('title', titleInput.value.trim()); });
    titleField.appendChild(titleInput);

    const artistField = document.createElement('label');
    artistField.className = 'header-field';
    artistField.append(t('Artist'));
    const artistInput = document.createElement('input');
    artistInput.id = 'song-artist-input';
    artistInput.type = 'text';
    artistInput.autocomplete = 'off';
    artistInput.value = metaGet('artist');
    artistInput.addEventListener('input', () => { markDirty(); metaSet('artist', artistInput.value.trim()); });
    artistField.appendChild(artistInput);

    const timeField = document.createElement('label');
    timeField.className = 'header-field';
    timeField.append(t('Time signature') + ' ');
    const timeInput = document.createElement('input');
    // No inputmode="numeric": the phone number pad has no "/" key.
    timeInput.type = 'text'; timeInput.className = 'header-field-input';
    timeInput.autocomplete = 'off'; timeInput.spellcheck = false;
    timeInput.placeholder = t('e.g. 4/4');
    timeInput.value = metaGet('time');
    timeInput.title = t('e.g. 4/4, 3/4, 6/8 — affects beat slots and bar-fullness below');
    timeInput.addEventListener('input', () => { markDirty(); metaSet('time', timeInput.value.trim()); renderStructureUI(); });
    timeField.appendChild(timeInput);

    const tempoField = document.createElement('label');
    tempoField.className = 'header-field';
    tempoField.append(t('Tempo') + ' ');
    const tempoInput = document.createElement('input');
    tempoInput.type = 'text'; tempoInput.inputMode = 'numeric'; tempoInput.className = 'header-field-input';
    tempoInput.autocomplete = 'off';
    tempoInput.placeholder = t('e.g. 120');
    tempoInput.value = metaGet('tempo');
    tempoInput.title = t('beats per minute, saved to the song header');
    tempoInput.addEventListener('input', () => { markDirty(); metaSet('tempo', tempoInput.value.trim()); });
    tempoField.append(tempoInput, ' bpm');
    // Tap: the beat tapped fills the field (tap-tempo.js, the ⋯ menu's
    // Tap tempo's arithmetic), from the second tap; saved with the rest.
    // Outside the label, so a tap doesn't also focus the field.
    let tempoTap = null;
    if (window.SongbooksTapTempo) {
      const tapper = window.SongbooksTapTempo.create();
      tempoTap = document.createElement('button');
      tempoTap.type = 'button';
      tempoTap.className = 'ghost-btn header-field-tap';
      tempoTap.textContent = t('Tap');
      tempoTap.title = t('tap the beat to fill in the tempo');
      let tappedAt = -Infinity;
      const tapOnce = () => {
        tappedAt = performance.now();
        const bpm = tapper.tap(tappedAt);
        if (bpm == null || !window.SongbooksTapTempo.usable(bpm)) return;
        tempoInput.value = String(bpm);
        markDirty();
        metaSet('tempo', String(bpm));
      };
      tempoTap.addEventListener('pointerdown', (e) => { if (e.button === 0) { e.preventDefault(); tapOnce(); } });
      // Space or Enter on the button (a pointer's tap was already counted).
      tempoTap.addEventListener('click', () => { if (performance.now() - tappedAt > 400) tapOnce(); });
    }

    // Swing: written straight notes played as a shuffle — the song
    // header's "swing:" (8 or 16; see the song format docs).
    const swingField = document.createElement('label');
    swingField.className = 'header-field';
    swingField.append(t('Swing') + ' ');
    const swingSelect = document.createElement('select');
    swingSelect.className = 'header-field-input';
    swingSelect.title = t('plays written eighths (or sixteenths) as a shuffle, like Guitar Pro\'s triplet feel');
    for (const [value, text] of [['', t('Straight')], ['8', t('Swung 8ths')], ['16', t('Swung 16ths')]]) {
      swingSelect.add(new Option(text, value));
    }
    swingSelect.value = ['8', '16'].includes(metaGet('swing')) ? metaGet('swing') : '';
    swingSelect.addEventListener('change', () => {
      markDirty();
      // Straight is no header line at all, not an empty "swing:".
      if (swingSelect.value) metaSet('swing', swingSelect.value);
      else structMeta = structMeta.filter((f) => f.key !== 'swing');
    });
    swingField.appendChild(swingSelect);

    // Genre lives on the song's parent WORK, not in the raw .song body
    // (see migration 0030), so it saves independently via its own
    // endpoint on blur rather than going through structMeta/the
    // structure-tab "Save" button like time/tempo above.
    const genreField = document.createElement('label');
    genreField.className = 'header-field';
    genreField.append(t('Genre') + ' ');
    const genreInput = document.createElement('input');
    genreInput.type = 'text'; genreInput.className = 'header-field-input header-field-genre';
    genreInput.placeholder = t('e.g. Soul');
    genreInput.autocomplete = 'off';
    genreInput.value = structureEl?.dataset.genre || '';
    genreInput.title = t('Saved as soon as you leave this field');
    // Saves on its own (no Save button), so it says when it has.
    const genreStatus = document.createElement('span');
    genreStatus.className = 'hint form-status';
    genreStatus.setAttribute('role', 'status');
    genreInput.addEventListener('change', async () => {
      const variantId = structureEl?.dataset.variantId;
      if (!variantId) return;
      const res = await fetch(`/api/variants/${variantId}/genre`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ genre: genreInput.value.trim() }),
      }).catch(() => null);
      Forms.status(genreStatus, res && res.ok ? t('Genre saved.') : t("Couldn't save the genre. Try again."), res && res.ok ? 'ok' : 'error');
    });
    genreField.appendChild(genreInput);

    const titleArtistRow = document.createElement('div');
    titleArtistRow.className = 'song-title-artist-fields';
    titleArtistRow.append(titleField, artistField);

    const chipsRow = document.createElement('div');
    chipsRow.className = 'song-meta-chips-row';
    chipsRow.append(timeField, tempoField, ...(tempoTap ? [tempoTap] : []), swingField, genreField, genreStatus);

    host.append(titleArtistRow, chipsRow);

    // A song with a Guitar Pro file can be all in it ("content: gp": an
    // instrumental, a lesson): the page opens on the file's score, and the
    // sections here are extras.
    if (songHasGPFile()) {
      const gpRow = document.createElement('label');
      gpRow.className = 'check song-gp-only';
      const box = document.createElement('input');
      box.type = 'checkbox';
      box.checked = (metaGet('content') || '').trim().toLowerCase() === 'gp';
      box.addEventListener('change', () => {
        edited();
        if (box.checked) metaSet('content', 'gp');
        else structMeta = structMeta.filter((f) => f.key !== 'content');
        renderHeaderFields();
      });
      gpRow.append(box, ' ' + t('The song is all in its Guitar Pro file'));
      host.appendChild(gpRow);
      if (box.checked) {
        const note = document.createElement('p');
        note.className = 'hint song-gp-only-note';
        note.textContent = structSections.length
          ? t('The song opens on the file’s score; the sections below are extras, one tap away in the Song view.')
          : t('The song opens on the file’s score.');
        host.appendChild(note);
      }
    }
  }

  // The song's own beatsPerBar as edited here, read fresh from structMeta
  // every time (not the page-load `beatsPerBar` const above, which is
  // baked from the server-rendered chart and would go stale the moment
  // the time-signature field above is touched).
  function editorBeatsPerBar() {
    const n = parseInt((metaGet('time') || '').split('/')[0], 10);
    return n > 0 ? n : beatsPerBar;
  }

  // The desktop focus editor's default bar-split progression: whole bar
  // → the main half-bar split → every main pulse. "Main pulse count" for
  // a compound numerator (6, 9, 12) is NOT the raw numerator — a 6/8 bar
  // has 2 real pulses (dotted quarters), not 6 — same compound-meter
  // table clock.js's Metronome already uses for accenting (6→2, 9→3,
  // 12→4 main pulses); a simple-meter numerator (2, 3, 4...) already IS
  // its own pulse count.
  function barSplitProgression(bpb) {
    const pulses = { 6: 2, 9: 3, 12: 4 }[bpb] || bpb;
    const opts = new Set([1]);
    if (pulses % 2 === 0) opts.add(pulses / 2);
    opts.add(pulses);
    return [...opts].sort((a, b) => a - b);
  }

  function emptySection(label, repeat) {
    return { label, repeat, time: '', score: '', measures: [], lyrics: [], endings: [], notes: [] };
  }

  // Splits a flat measures array (as loaded/saved — plain consecutive
  // measures, a trailing run tagged with a Volta label) into the editor's
  // own {common, endings} shape — see renderAlternatives/joinVoltaGroups.
  // The split point is simply "the first measure with a volta label":
  // LilyPond's \alternative block always follows the common repeated
  // body, never interrupts it.
  function splitVoltaGroups(measures) {
    const firstVoltaIdx = measures.findIndex(m => m.volta);
    if (firstVoltaIdx < 0) return { common: measures, endings: [] };
    const common = measures.slice(0, firstVoltaIdx);
    const tail = measures.slice(firstVoltaIdx);
    const endings = [];
    let cur = null;
    for (const m of tail) {
      if (!cur || cur.label !== m.volta) {
        cur = { label: m.volta, measures: [] };
        endings.push(cur);
      }
      cur.measures.push(m);
    }
    return { common, endings };
  }
  function emptyMeasure() { return { tokens: [''], open: false, close: false, volta: '', meter: '' }; }

  // ---- Raw-text fallback — the original design's "advanced syntax" guard:
  // a section whose grid uses something the bar-matrix widget doesn't
  // visually model (a per-bar meter override, a duration-tagged token like
  // "Gm:2", or more open/close repeat barlines than the matrix's single
  // "repeats" toggle can represent) still loads and saves correctly — its
  // fields all survive untouched through joinVoltaGroups/the structured
  // save — but the visual builder is disabled for it so editing it here
  // can't silently misrepresent what's actually in the file. Edit it in
  // Source instead, same as the original plan's "disable the widget,
  // keep advanced-syntax sections in raw text" rule.
  function sectionHasAdvancedSyntax(sec) {
    const measures = sec.measures || [];
    if (measures.some(m => m.meter)) return true;
    if (measures.some(m => (m.tokens || []).some(t => /:/.test(t)))) return true;
    if (measures.filter(m => m.open).length > 1) return true;
    if (measures.filter(m => m.close).length > 1) return true;
    // An explicit repeat count ("|:4 ... :|") has no control in this
    // builder (the "repeats" toggle only ever means the implicit,
    // unlabeled x2) — editing here would silently drop it on save.
    if (measures.some(m => m.repeatCount)) return true;
    // A repeat bracket that covers only a PREFIX of the section's bars
    // (e.g. a 4-bar "|: :|" phrase followed by 4 more plain bars, same
    // shape as "Synth Intro"/"Instru"/"Break"/"Outro" in a real song)
    // isn't representable by this builder's single open/close "repeats"
    // toggle, which always treats the section's own last bar as the
    // close — joinVoltaGroups would otherwise silently move the real
    // close flag there, extending the bracket over bars it never
    // covered. Skip this check when alternative endings are in play:
    // there the true close legitimately lives in an ending, not in
    // `measures` itself (see the "repeats (|: :|)" toggle's own save
    // handler) — that's the one case this builder DOES model correctly.
    const hasEndings = sec.endings && sec.endings.length > 0;
    if (!hasEndings) {
      const openIdx = measures.findIndex(m => m.open);
      if (openIdx >= 0) {
        const closeIdx = measures.findIndex(m => m.close);
        if (closeIdx < 0 || closeIdx !== measures.length - 1) return true;
      }
    }
    return false;
  }

  const expanded = new Set(); // section indices currently showing their bar matrix
  const expandedNotes = new Set(); // section indices currently showing their notes textarea

  // Chunk measures into display rows of barsPerLine (mirrors the Go
  // groupMeasures helper used for the real chart, so what you build here
  // looks the same once saved).
  function chunkRows(measures, barsPerLine) {
    const n = Math.max(1, barsPerLine || 4);
    const rows = [];
    for (let i = 0; i < measures.length; i += n) rows.push(measures.slice(i, i + n));
    return rows;
  }

  // The song's own beatsPerBar, in the same "pulse" unit used everywhere
  // else in this app (a 6/8 bar counts as 2 pulses, not 6 raw eighths —
  // see clock.js/isCompound) — so a bar split mid-way reads the same way
  // the metronome and beat-dots already count it.
  function pulseLabel(pulseIndex) { return isCompound ? 1 + pulseIndex * 3 : pulseIndex + 1; }
  // A "." slot is a sustain, not a new event — labeling it "beat 3" reads
  // as if a chord actually changes there, when the whole point is that it
  // doesn't (Ain't No Love's "Gm . . Cm": Gm holds through beats 1-3, Cm
  // is the one real change, landing on beat 4). Slot 0 always gets its
  // label even when blank, since that's simply where the bar starts.
  function slotBeatLabel(slotIndex, slotCount, token) {
    if (slotCount <= 1) return null;
    if (slotIndex > 0 && (token === '.' || token === '')) return null;
    const pulse = Math.round((slotIndex * editorBeatsPerBar()) / slotCount);
    return pulseLabel(pulse);
  }

  // ---- Guitar-Pro-style duration placement: given a chord just typed or
  // picked into slot ti of bar mi, "n" is how many consecutive slots it
  // occupies ('bar' = the rest of the bar). The slots after the first are
  // filled with "." (sustain) — never left blank — and the cursor
  // auto-advances to the first slot past what was just filled, spilling
  // into the next bar when this one is now full. This is also what a
  // plain Enter/Tab keypress in a slot does (with n = 'bar'): "move on"
  // always means "the bar I'm leaving balances", mirroring how Guitar Pro
  // never lets you walk away from an underfull measure.
  function applyDuration(sec, mi, ti, n) {
    edited();
    const m = sec.measures[mi];
    const end = n === 'bar' ? m.tokens.length : Math.min(m.tokens.length, ti + n);
    for (let k = ti + 1; k < end; k++) m.tokens[k] = '.';
    renderMatrix(sec, secIndexOf(sec));
    if (end < m.tokens.length) focusSlot(sec, mi, end);
    else if (sec.measures[mi + 1]) focusSlot(sec, mi + 1, 0);
  }
  // ---- Bar split count ("how many chords can this bar hold") — iReal Pro
  // has no per-bar form control for this at all: you simply tap a bar and
  // keep typing, and the bar's own internal subdivision follows from where
  // you tapped. This app's data model still needs an explicit slot count
  // per measure, but that choice belongs on the TOOLBAR (acting on
  // whichever bar is selected), not as a <select> dropdown sitting inside
  // the bar cell itself — a dropdown-in-a-box is exactly the "numbered
  // box list" look this phase removes. Tapping a bar first, then a split
  // button here, mirrors the duration buttons' "act on the selection"
  // model exactly.
  function setBarSplit(sec, mi, n) {
    const m = sec.measures[mi];
    if (!m) return;
    edited();
    // Each chord keeps its place in the bar: slot k of L lands on slot
    // k*n/L of n (the next free one when two meet), so C . G . split in
    // two is C G, and C G split in four is C . G . — not C G . .
    const old = m.tokens, L = old.length || 1;
    const next = new Array(n).fill('.');
    next[0] = '';
    old.forEach((t, k) => {
      if (!t || t === '.') return;
      let j = Math.floor(k * n / L + 1e-9);
      while (j < n && next[j] !== '.' && next[j] !== '') j++;
      if (j < n) next[j] = t;
    });
    m.tokens = next;
    renderMatrix(sec, secIndexOf(sec));
    focusSlot(sec, mi, 0);
  }
  function focusSlot(sec, mi, ti) {
    const i = secIndexOf(sec);
    sec._sel = { mi, ti };
    lastSelSec = sec; lastSelIndex = i;
    requestAnimationFrame(() => {
      const host = document.getElementById('matrix-host-' + i);
      const el = host && host.querySelector(`.matrix-slot[data-mi="${mi}"][data-ti="${ti}"]`);
      // Only a real <input> (the mobile/box grid) takes native keyboard
      // focus — focus mode's real-chart slots are plain spans (tabIndex
      // -1, see wireRealChartEditing), selected purely via sec._sel/CSS,
      // same as a click already does there. Either way the highlight/
      // toolbar refresh below is unconditional, so a focus-mode caller
      // (applyDuration, setBarSplit, the repeat-bar button, …) always
      // sees its new selection reflected, not just when an <input> exists.
      if (el && el.tagName === 'INPUT') el.focus();
      highlightSelection(sec, i);
      refreshToolbarHighlight(sec, i);
    });
  }

  // ---- Persistent chord toolbar — iReal Pro's always-on root/quality
  // keyboard plus a Guitar-Pro-style duration/placement row, replacing the
  // old design where this was all buried behind a tiny per-slot "♪" popup
  // (openChordPicker, removed). It lives once per section, pinned above
  // that section's bar grid, and always acts on whichever slot is
  // currently SELECTED below (see selectSlot/highlightSelection) — tapping
  // a bar slot selects it, the toolbar lights up to show what's already
  // there, and every tap here edits that same slot in place. Typing
  // directly into a slot (still supported) and tapping buttons up here
  // are two paths into the exact same state: both just set
  // sec.measures[mi].tokens[ti] and dispatch the slot's own 'input'
  // listener, so nothing can get out of sync between them. ------------------
  const ROOTS = ['A', 'B', 'C', 'D', 'E', 'F', 'G'];
  const QUALITIES_PRIMARY = ['', 'm', '7', 'm7', 'maj7', 'dim', 'aug', 'sus2', 'sus4', 'm7b5'];
  const QUALITIES_MORE = ['6', 'm6', '9', 'maj9', 'm9', '11', '13', 'add9', '6/9'];

  // Splits a chord token into {root, accidental, quality, bass} for display
  // and for editing one part of it without disturbing the others — mirrors
  // internal/chordsym.Split (root = one letter + optional accidental,
  // everything up to an optional "/bass" is quality) but keeps the
  // accidental as its own glyph so a root button's active state can be
  // compared directly against ROOTS. "%"/"."/"N.C." and blank all come
  // back as {root:''}, same as chordsym treats them as non-root symbols.
  // Accidentals are normalized to ASCII 'b'/'#' here — same convention
  // chordsym.Split/rootPitchClass use everywhere else in this app — even
  // though the matched token might already contain the printed ♭/♯ glyph
  // (e.g. a chord typed with the glyph directly, or saved by an earlier,
  // buggier version of this toolbar). composeChord below always writes
  // the ASCII form back out, so re-editing a glyph-accidental chord
  // through the toolbar also quietly fixes it forward.
  function parseChordDisplay(tok) {
    const blank = { root: '', accidental: '', quality: '', bass: '' };
    if (!tok || tok === '.' || tok === '%' || tok === 'N.C.') return blank;
    const m = /^([A-G])([b#♭♯]?)([^/]*)(?:\/([A-G])([b#♭♯]?))?$/.exec(tok);
    if (!m) return { ...blank, quality: tok };
    const toAscii = a => (a === '♭' ? 'b' : a === '♯' ? '#' : a || '');
    const acc = toAscii(m[2]);
    const bass = m[4] ? m[4] + toAscii(m[5]) : '';
    return { root: m[1], accidental: acc, quality: m[3] || '', bass };
  }
  function composeChord(p) {
    return (p.root || '') + (p.accidental || '') + (p.quality || '') + (p.bass ? '/' + p.bass : '');
  }
  function getSlotInput(i, mi, ti) {
    const host = document.getElementById('matrix-host-' + i);
    // input.matrix-slot, not just .matrix-slot — focus mode's real chart
    // (wireRealChartEditing) tags real .beat-slot/.bar DIVs with this same
    // class so selection/highlight code can address them uniformly, but
    // setting .value on a div and dispatching 'input' at it is a silent
    // no-op (no listener, no visible effect) — a bare class match here
    // took that branch instead of the real-chart refreshRealChart() path,
    // so edits updated the data model but the chart never visibly redrew.
    return host && host.querySelector(`input.matrix-slot[data-mi="${mi}"][data-ti="${ti}"]`);
  }
  // Same lookup, but matches EITHER kind of tagged slot — the mobile
  // <input> above, or the real-chart's plain .beat-slot/.bar DIV
  // (wireRealChartEditing tags both ".matrix-slot", only the mobile one
  // is actually an <input>). getSlotInput() is deliberately narrow so
  // setChordPart/setSlotToken can branch on "found → mobile, not found →
  // real chart" — but that same narrowness meant highlightSelection()
  // (a read-only visual op, no branching need) always got `null` for a
  // real-chart selection and silently skipped drawing the dashed-ring/
  // caret overlay entirely: the selection worked (sec._sel updated, the
  // sidebar status line changed), it just never became visible on the
  // chart itself — confirmed live as the reported "I click around and
  // can't tell anything got selected" bug.
  function getSlotElement(i, mi, ti) {
    const host = document.getElementById('matrix-host-' + i);
    return host && host.querySelector(`.matrix-slot[data-mi="${mi}"][data-ti="${ti}"]`);
  }
  // All {mi, ti} slot coordinates in this section, in reading order — used
  // for arrow-key/Tab navigation that has to cross bar boundaries.
  function allSlots(sec) {
    const out = [];
    sec.measures.forEach((m, mi) => m.tokens.forEach((t, ti) => out.push({ mi, ti })));
    return out;
  }
  function moveSelection(sec, i, delta) {
    const slots = allSlots(sec);
    if (!slots.length) return;
    const cur = sec._sel ? slots.findIndex(s => s.mi === sec._sel.mi && s.ti === sec._sel.ti) : -1;
    const next = Math.max(0, Math.min(slots.length - 1, cur + delta));
    focusSlot(sec, slots[next].mi, slots[next].ti);
  }
  // Tracks whichever (section, index) selectSlot was last called for — the
  // keyboard-chord-entry handler below (desktop focus editor only) has no
  // other way to know which section's toolbar is "live" right now, since
  // selection state otherwise lives entirely on the section object itself
  // (sec._sel), not in one shared place.
  let lastSelSec = null, lastSelIndex = -1;
  function selectSlot(sec, i, mi, ti) {
    sec._sel = { mi, ti };
    lastSelSec = sec; lastSelIndex = i;
    highlightSelection(sec, i);
    refreshToolbarHighlight(sec, i);
    dockToolbar(i);
  }
  // ---- Mobile keyboard docking — iReal Pro's own phone editor docks its
  // chord keyboard to the real BOTTOM of the viewport, with the chart
  // staying put and scrollable above it; it is never a floating popup and
  // never covers the whole chart. Below the drawer's own mobile
  // breakpoint (see .edit-drawer:not([hidden]) in app.css), whichever
  // section's toolbar was most recently built or interacted with gets
  // docked; every other section's toolbar (if more than one happens to be
  // expanded at once) drops back to the plain inline position so only one
  // toolbar is ever fixed on screen at a time. On desktop this is a no-op
  // — the toolbar stays sticky-above-the-grid as before. ---------------
  function isMobileEditWidth() {
    return window.matchMedia('(max-width: 760px)').matches;
  }
  function dockToolbar(i) {
    document.querySelectorAll('.chord-toolbar.toolbar-docked').forEach(el => el.classList.remove('toolbar-docked'));
    document.querySelectorAll('.edit-drawer-body.has-docked-toolbar').forEach(el => el.classList.remove('has-docked-toolbar'));
    if (!isMobileEditWidth()) return;
    const host = document.getElementById('matrix-host-' + i);
    const toolbar = host && host.querySelector('.chord-toolbar');
    if (!toolbar) return;
    toolbar.classList.add('toolbar-docked');
    const drawerBody = document.querySelector('.edit-drawer-body');
    if (drawerBody) drawerBody.classList.add('has-docked-toolbar');
  }
  // Visual selection state on the grid itself — a clear outline/background
  // change on the selected slot AND its parent bar, so it's unambiguous on
  // a phone screenshot which cell the toolbar above is about to edit.
  function highlightSelection(sec, i) {
    const host = document.getElementById('matrix-host-' + i);
    if (!host) return;
    const sel = sec._sel;
    host.querySelectorAll('.matrix-slot-row.slot-selected').forEach(el => el.classList.remove('slot-selected'));
    host.querySelectorAll('.matrix-cell.cell-selected').forEach(el => el.classList.remove('cell-selected'));
    host.querySelectorAll('.matrix-slot.editing').forEach(el => el.classList.remove('editing'));
    host.querySelectorAll('.bar.editing').forEach(el => el.classList.remove('editing'));
    if (!sel) return;
    const input = getSlotElement(i, sel.mi, sel.ti);
    if (!input) return;
    const row = input.closest('.matrix-slot-row');
    if (row) {
      // Mobile/box grid: the old per-cell outline.
      row.classList.add('slot-selected');
      input.closest('.matrix-cell')?.classList.add('cell-selected');
    } else {
      // Focus mode's real chart: a thin overlay on the UNCHANGED real
      // markup — a dashed ring + caret (CSS), nothing about the bar's own
      // layout/background changes.
      input.classList.add('editing');
      input.closest('.bar')?.classList.add('editing');
    }
  }
  // Lights up the toolbar's own buttons to match the currently selected
  // slot's content (iReal Pro's keyboard does the same when you tap an
  // already-filled cell) — the toolbar reflects state, it doesn't just
  // blindly emit taps.
  function refreshToolbarHighlight(sec, i) {
    const host = document.getElementById('matrix-host-' + i);
    // Desktop focus mode moved the toolbar out of host and into the
    // sidebar rail (see renderMatrix) — fall back to it there.
    const toolbar = (host && host.querySelector('.chord-toolbar')) || document.querySelector('#toolbox-toolbar .chord-toolbar');
    if (!toolbar) return;
    const sel = sec._sel;
    const tok = sel && sec.measures[sel.mi] ? (sec.measures[sel.mi].tokens[sel.ti] || '') : '';
    const parts = parseChordDisplay(tok);
    toolbar.querySelectorAll('[data-root]').forEach(b => b.classList.toggle('active', !!sel && b.dataset.root === parts.root));
    toolbar.querySelectorAll('[data-quality]').forEach(b => b.classList.toggle('active', !!sel && b.dataset.quality === parts.quality));
    toolbar.querySelectorAll('[data-acc]').forEach(b => b.classList.toggle('active', !!sel && !!parts.accidental && b.dataset.acc === parts.accidental));
    const bassBtn = toolbar.querySelector('.toolbar-bass-btn');
    if (bassBtn) bassBtn.classList.toggle('active', !!sel && !!parts.bass);
    const barSlotCount = sel && sec.measures[sel.mi] ? sec.measures[sel.mi].tokens.length : 0;
    toolbar.querySelectorAll('[data-split]').forEach(b => b.classList.toggle('active', !!sel && parseInt(b.dataset.split, 10) === barSlotCount));
    // Terse now — the cell's own caret (.cell-selected::before, CSS) is
    // the primary cursor cue; this text is a screen-reader-friendly
    // fallback, not a sentence a sighted user needs to read to know where
    // they are. "bar 3 · 2/4" reads as a compact coordinate, not a caption.
    const slotLabel = toolbar.querySelector('.toolbar-slot-label');
    if (slotLabel) {
      // Focus mode's status line is the "caret position" the research
      // proposal asked for (section · bar · beat) — mobile's own toolbar
      // already reads unambiguously without the section name repeated (one
      // section is ever expanded/docked at a time there), so this prefix
      // is focus-mode only.
      const secPart = focusModeActive ? `${sec.label || (sec.repeat ? t('(repeat)') : t('Untitled'))} · ` : '';
      slotLabel.textContent = sel
        ? `${secPart}${t('bar %d', sel.mi + 1)} · ${sel.ti + 1}/${sec.measures[sel.mi].tokens.length}${tok ? ' · ' + tok : ''}`
        : (focusModeActive ? `${sec.label || t('Untitled')} · ${t('no bar selected')}` : t('no bar selected'));
    }
    toolbar.classList.toggle('no-selection', !sel);
  }
  // Writes one part (root/accidental/quality/bass) of the SELECTED slot's
  // chord, keeping the rest — tapping "m7" after tapping "C" gives "Cm7",
  // not just "m7" (iReal Pro's own "build it up one tap at a time" model).
  // Goes through the slot's real <input> and its existing 'input' listener
  // so the fullness indicator and beat label update exactly the same way a
  // keystroke would.
  function setChordPart(sec, i, part, value) {
    const sel = sec._sel;
    if (!sel || !sec.measures[sel.mi]) return;
    pushUndo(); markDirty();
    const cur = sec.measures[sel.mi].tokens[sel.ti] || '';
    const parts = parseChordDisplay(cur);
    parts[part] = value;
    // Tapping a root button always means "replace the root AND whatever
    // accidental was tapped alongside it" (r + acc from the root-button
    // handler) — if a 'root' write here left the OLD chord's own
    // accidental in place, re-rooting an existing chord that had one
    // (e.g. retapping "Ab" to "D") silently kept composing it back in
    // ("Db" instead of "D") even with no flat/sharp pending. The root
    // button handler already folds the pending accidental into `value`
    // (e.g. "Bb"), so a plain 'root' write here must clear parts.accidental
    // outright — parseChordDisplay re-splits whatever was just composed if
    // `value` itself carried one.
    if (part === 'root') { const split = parseChordDisplay(value); parts.root = split.root; parts.accidental = split.accidental; }
    const text = composeChord(parts);
    const input = getSlotInput(i, sel.mi, sel.ti);
    if (input) { input.value = text; input.dispatchEvent(new Event('input')); }
    else { sec.measures[sel.mi].tokens[sel.ti] = text; refreshRealChart(sec, i); }
    refreshToolbarHighlight(sec, i);
  }
  function setSlotToken(sec, i, token) {
    const sel = sec._sel;
    if (!sel || !sec.measures[sel.mi]) return;
    pushUndo(); markDirty();
    const input = getSlotInput(i, sel.mi, sel.ti);
    if (input) { input.value = token; input.dispatchEvent(new Event('input')); }
    else { sec.measures[sel.mi].tokens[sel.ti] = token; refreshRealChart(sec, i); }
    refreshToolbarHighlight(sec, i);
  }
  // Focus mode's real chart has no <input> to dispatch an 'input' event
  // through (see setChordPart/setSlotToken's own comments) — a direct
  // sec.measures write there needs its own explicit re-fetch instead.
  // Debounced per-section so a quick run of taps (root, then quality,
  // then duration) collapses into one /api/preview round trip, not one
  // per tap.
  const realChartRefreshTimers = new Map();
  function refreshRealChart(sec, i) {
    if (!focusModeActive) return;
    clearTimeout(realChartRefreshTimers.get(i));
    realChartRefreshTimers.set(i, setTimeout(() => {
      const chartHost = document.querySelector('#matrix-host-' + i + ' .real-chart-host');
      if (chartHost) renderRealChart(sec, i, chartHost);
    }, 120));
  }

  function buildChordToolbar(sec, i) {
    // Each full rebuild starts a fresh "staging" state — any in-progress
    // flat/sharp or bass-mode tap that hadn't yet landed on a root tap is
    // discarded rather than left silently applying to a different render.
    sec._pendingAcc = '';
    sec._bassMode = false;

    const bar = document.createElement('div');
    bar.className = 'chord-toolbar no-selection';

    // Time signature / tempo, surfaced right here rather than only in the
    // header fields above the Blocks list — this is "the top of the
    // section being edited" the grid itself needs, not buried off-screen.
    const meta = document.createElement('div');
    meta.className = 'chord-toolbar-meta';
    const timeSpan = document.createElement('span'); timeSpan.className = 'toolbar-meta-chip';
    timeSpan.textContent = (metaGet('time') || '4/4');
    const tempoSpan = document.createElement('span'); tempoSpan.className = 'toolbar-meta-chip toolbar-meta-tempo';
    tempoSpan.textContent = metaGet('tempo') ? metaGet('tempo') + ' bpm' : '';
    const slotLabel = document.createElement('span'); slotLabel.className = 'toolbar-slot-label hint';
    slotLabel.textContent = t('tap a bar slot below to select it');
    meta.append(timeSpan, tempoSpan, slotLabel);
    bar.appendChild(meta);

    const rootRow = document.createElement('div'); rootRow.className = 'toolbar-row toolbar-roots';
    // Stored as ASCII 'b'/'#' — same convention as typing a chord directly
    // and as rootPitchClass()/transpose above: internal/chordsym.Split only
    // recognizes the ASCII accidental when splitting a root off a token
    // (the ♭/♯ glyph is purely a RENDERING substitution chordsym.esc()
    // applies afterward). Composing the glyph straight into the token here
    // would silently break transpose/PitchClass for anything built via
    // these buttons, so the button LABEL is the glyph but its stored value
    // stays ASCII, same split used for displaying it back (parseChordDisplay
    // below).
    const flat = document.createElement('button'); flat.type = 'button'; flat.className = 'toolbar-btn toolbar-acc'; flat.dataset.acc = 'b'; flat.textContent = '♭';
    flat.title = t('flat the next root tapped');
    flat.addEventListener('click', () => { sec._pendingAcc = sec._pendingAcc === 'b' ? '' : 'b'; flat.classList.toggle('pending', sec._pendingAcc === 'b'); sharp.classList.remove('pending'); });
    const sharp = document.createElement('button'); sharp.type = 'button'; sharp.className = 'toolbar-btn toolbar-acc'; sharp.dataset.acc = '#'; sharp.textContent = '♯';
    sharp.title = t('sharp the next root tapped');
    sharp.addEventListener('click', () => { sec._pendingAcc = sec._pendingAcc === '#' ? '' : '#'; sharp.classList.toggle('pending', sec._pendingAcc === '#'); flat.classList.remove('pending'); });
    rootRow.append(flat);
    ROOTS.forEach(r => {
      const b = document.createElement('button'); b.type = 'button'; b.className = 'toolbar-btn toolbar-root'; b.dataset.root = r; b.textContent = r;
      b.addEventListener('click', () => {
        if (!sec._sel) return;
        const acc = sec._pendingAcc || '';
        if (sec._bassMode) { setChordPart(sec, i, 'bass', r + acc); sec._bassMode = false; bassBtn.classList.remove('pending'); }
        else setChordPart(sec, i, 'root', r + acc);
        sec._pendingAcc = ''; flat.classList.remove('pending'); sharp.classList.remove('pending');
      });
      rootRow.appendChild(b);
    });
    rootRow.append(sharp);
    bar.appendChild(rootRow);

    const qualRow = document.createElement('div'); qualRow.className = 'toolbar-row toolbar-qualities';
    QUALITIES_PRIMARY.forEach(q => {
      const b = document.createElement('button'); b.type = 'button'; b.className = 'toolbar-btn toolbar-quality'; b.dataset.quality = q; b.textContent = q || 'maj';
      b.addEventListener('click', () => { if (sec._sel) setChordPart(sec, i, 'quality', q); });
      qualRow.appendChild(b);
    });
    const moreBtn = document.createElement('button'); moreBtn.type = 'button'; moreBtn.className = 'toolbar-btn toolbar-more'; moreBtn.textContent = t('more…');
    moreBtn.title = t('more chord qualities');
    moreBtn.addEventListener('click', () => openMoreQualities(moreBtn, sec, i));
    qualRow.appendChild(moreBtn);
    bar.appendChild(qualRow);

    const utilRow = document.createElement('div'); utilRow.className = 'toolbar-row toolbar-util';
    const bassBtn = document.createElement('button'); bassBtn.type = 'button'; bassBtn.className = 'toolbar-btn toolbar-bass-btn'; bassBtn.textContent = t('/ bass');
    bassBtn.title = t('next root tapped becomes the bass note, e.g. C/E');
    bassBtn.addEventListener('click', () => { sec._bassMode = !sec._bassMode; bassBtn.classList.toggle('pending', sec._bassMode); });
    const oneBeat = document.createElement('button'); oneBeat.type = 'button'; oneBeat.className = 'toolbar-btn'; oneBeat.textContent = t('1 beat');
    oneBeat.title = t('this chord holds for 1 beat-slot, then advance');
    oneBeat.addEventListener('click', () => { if (sec._sel) applyDuration(sec, sec._sel.mi, sec._sel.ti, 1); });
    const twoBeat = document.createElement('button'); twoBeat.type = 'button'; twoBeat.className = 'toolbar-btn'; twoBeat.textContent = t('2 beats');
    twoBeat.title = t('this chord holds for 2 beat-slots, then advance');
    twoBeat.addEventListener('click', () => { if (sec._sel) applyDuration(sec, sec._sel.mi, sec._sel.ti, 2); });
    const wholeBar = document.createElement('button'); wholeBar.type = 'button'; wholeBar.className = 'toolbar-btn'; wholeBar.textContent = t('whole bar');
    wholeBar.title = t('this chord sustains through the rest of the bar, then advance to the next bar');
    wholeBar.addEventListener('click', () => { if (sec._sel) applyDuration(sec, sec._sel.mi, sec._sel.ti, 'bar'); });
    const sustainBtn = document.createElement('button'); sustainBtn.type = 'button'; sustainBtn.className = 'toolbar-btn'; sustainBtn.textContent = '.';
    sustainBtn.title = t('sustain the previous chord into this slot');
    sustainBtn.addEventListener('click', () => { if (sec._sel) { setSlotToken(sec, i, '.'); moveSelection(sec, i, 1); } });
    const repeatBarBtn = document.createElement('button'); repeatBarBtn.type = 'button'; repeatBarBtn.className = 'toolbar-btn'; repeatBarBtn.textContent = '%';
    repeatBarBtn.title = t('repeat the previous bar');
    repeatBarBtn.addEventListener('click', () => { if (!sec._sel) return; edited(); sec.measures[sec._sel.mi].tokens = ['%']; renderMatrix(sec, i); focusSlot(sec, Math.min(sec._sel.mi + 1, sec.measures.length - 1), 0); });
    const clearBtn = document.createElement('button'); clearBtn.type = 'button'; clearBtn.className = 'toolbar-btn toolbar-clear'; clearBtn.textContent = t('clear');
    clearBtn.title = t('clear the selected slot');
    clearBtn.addEventListener('click', () => { if (sec._sel) setSlotToken(sec, i, ''); });
    utilRow.append(bassBtn, oneBeat, twoBeat, wholeBar, sustainBtn, repeatBarBtn, clearBtn);
    bar.appendChild(utilRow);

    // Bar-split row: "how many chords does the SELECTED bar hold" — acts
    // on sec._sel.mi, replacing the old per-cell "N chord(s)" dropdown
    // that used to sit inside every bar box (see setBarSplit above).
    const splitRow = document.createElement('div'); splitRow.className = 'toolbar-row toolbar-split';
    const splitLabel = document.createElement('span'); splitLabel.className = 'hint toolbar-split-label'; splitLabel.textContent = t('split bar:');
    splitRow.appendChild(splitLabel);
    const bpb = editorBeatsPerBar();
    // Desktop focus editor: the default progression is musical, not a
    // flat beat count — whole bar, then the main half-bar split (beats
    // 1 & 3 in 4/4), then the full per-beat split, generalized to
    // compound meters via the same main-pulse structure clock.js's own
    // ACCENTS table encodes for the metronome (6/8 = 2 pulses, 12/8 = 4
    // pulses) — see barSplitProgression. Mobile keeps the old flat
    // [1,2,3,bpb] list unchanged.
    const splitOptions = focusModeActive ? barSplitProgression(bpb) : (bpb >= 3 ? [1, 2, 3, bpb] : [1, bpb]);
    [...new Set(splitOptions)].forEach(n => {
      const b = document.createElement('button'); b.type = 'button'; b.className = 'toolbar-btn toolbar-split-btn'; b.dataset.split = String(n);
      b.textContent = n === 1 ? '1' : String(n);
      b.title = n === 1 ? t('one chord for the whole bar') : t('split into %d chords', n);
      b.addEventListener('click', () => { if (sec._sel) setBarSplit(sec, sec._sel.mi, n); });
      splitRow.appendChild(b);
    });
    bar.appendChild(splitRow);

    return bar;
  }

  function openMoreQualities(anchorBtn, sec, i) {
    closeAnyMenu();
    const menu = document.createElement('div');
    menu.className = 'insert-menu chord-pick-menu';
    const row = document.createElement('div'); row.className = 'chord-pick-row quality';
    QUALITIES_MORE.forEach(q => {
      const b = document.createElement('button'); b.type = 'button'; b.textContent = q;
      b.addEventListener('click', () => { closeAnyMenu(); if (sec._sel) setChordPart(sec, i, 'quality', q); });
      row.appendChild(b);
    });
    menu.appendChild(row);
    positionMenu(menu, anchorBtn);
  }

  function buildMatrixCell(sec, mi) {
    const m = sec.measures[mi];
    const cell = document.createElement('div');
    cell.className = 'matrix-cell';
    if (m.open) cell.classList.add('cell-open');
    if (m.close) cell.classList.add('cell-close');
    // Widen the cell for a higher beat-density so its extra slots have
    // somewhere to sit instead of being squeezed to near-zero width —
    // the 7rem single-slot basis only ever assumed one chord per bar.
    // Wrapped in min(…, 100%): plain `20.8rem` is a HARD floor — CSS
    // resolves a conflicting min-width/max-width in min-width's favor
    // every time, so on a phone-width drawer (this editor was built and
    // verified mostly at desktop width) a busy 4-slot bar forced itself
    // wider than the viewport itself, overflowing uncontrollably with no
    // way for .matrix-slots' own flex-wrap (below) to ever kick in and
    // catch it. min() makes 100% of the matrix row's own available width
    // a real ceiling instead: once that's the smaller side, the cell
    // shrinks to actually fit, and .matrix-slots wraps its slots onto
    // more internal rows to compensate — taller, not wider than the
    // screen. Desktop is unaffected (plenty of room, the rem side stays
    // smaller).
    const cellWidth = 7 + Math.max(0, m.tokens.length - 1) * 4.6;
    cell.style.flexBasis = `min(${cellWidth}rem, 100%)`;
    cell.style.minWidth = `min(${cellWidth}rem, 100%)`;

    const top = document.createElement('div');
    top.className = 'matrix-cell-top';
    const barNo = document.createElement('span');
    barNo.className = 'matrix-bar-no';
    barNo.textContent = mi + 1;
    top.appendChild(barNo);

    // No per-bar "N chord(s)" dropdown here any more — that control now
    // lives once, on the persistent toolbar's "split bar" row, and acts on
    // whichever bar is selected (see setBarSplit/buildChordToolbar). A
    // <select> inside every single cell was exactly the "numbered box
    // with a dropdown" look this phase removes: the bar itself should
    // read as a real measure on a staff, not a form control.

    // Bar-fullness feedback (Guitar Pro's "does this measure balance?"):
    // green/solid once every slot holds something (a chord or a "."
    // sustain), amber/dashed while any slot is still blank. Recomputed
    // after every edit to a slot in this bar, not just on load.
    const fullness = document.createElement('span');
    fullness.className = 'matrix-fullness';
    function updateFullness() {
      const complete = m.tokens.every(t => t !== '');
      cell.classList.toggle('bar-complete', complete);
      cell.classList.toggle('bar-underfull', !complete);
      fullness.textContent = complete ? '✓' : `${m.tokens.filter(t => t !== '').length}/${m.tokens.length}`;
      fullness.title = complete ? t('bar is filled') : t('bar is missing chords/sustains — pick a duration or press Enter to fill the rest');
    }
    top.appendChild(fullness);
    cell.appendChild(top);

    const slots = document.createElement('div');
    slots.className = 'matrix-slots';
    m.tokens.forEach((tok, ti) => {
      const slotWrap = document.createElement('div');
      const lab = document.createElement('div'); lab.className = 'matrix-beat-label';
      const updateLabel = tokv => {
        const label = slotBeatLabel(ti, m.tokens.length, tokv);
        lab.textContent = label != null ? t('beat %d', label) : '';
      };
      updateLabel(tok);
      slotWrap.appendChild(lab);
      const row = document.createElement('div'); row.className = 'matrix-slot-row';
      const input = document.createElement('input');
      input.type = 'text'; input.className = 'matrix-slot'; input.value = tok;
      input.dataset.mi = String(mi); input.dataset.ti = String(ti);
      input.placeholder = ti === 0 ? '%' : '.';
      input.setAttribute('aria-label', t('Bar %d, beat %d chord', mi + 1, ti + 1));
      input.autocapitalize = 'off'; input.spellcheck = false;
      trackTextEdits(input);
      input.addEventListener('input', () => { m.tokens[ti] = input.value; updateLabel(input.value); updateFullness(); refreshToolbarHighlight(sec, secIndexOf(sec)); });
      // Tapping/focusing a slot selects it — the toolbar above reflects
      // and acts on whichever slot was last focused, whether that focus
      // came from a click, a Tab, or an arrow-key move (see below): one
      // shared selection, not a separate "selected" idea per input method.
      input.addEventListener('focus', () => selectSlot(sec, secIndexOf(sec), mi, ti));
      input.addEventListener('click', () => selectSlot(sec, secIndexOf(sec), mi, ti));
      // Enter/Tab: the same "finish the bar" action the toolbar's "whole
      // bar" duration button gives you, reached straight from the
      // keyboard — typing "Cm7" and hitting Enter sustains it through
      // whatever's left of the bar and jumps to the next one, exactly
      // like tapping it via the toolbar does. Both input paths land on
      // the one applyDuration() call, so there is only ever one place
      // that decides what "auto-complete this bar" means. Left/Right at
      // the edge of the text move the selection to the previous/next
      // slot instead of doing nothing, same shared selection state the
      // toolbar reads — this is the "arrow-key navigation" keyboard path.
      input.addEventListener('keydown', e => {
        if (e.key === 'Enter' || e.key === 'Tab') {
          e.preventDefault();
          applyDuration(sec, mi, ti, 'bar');
        } else if (e.key === 'ArrowRight' && input.selectionStart === input.value.length) {
          e.preventDefault();
          moveSelection(sec, secIndexOf(sec), 1);
        } else if (e.key === 'ArrowLeft' && input.selectionStart === 0) {
          e.preventDefault();
          moveSelection(sec, secIndexOf(sec), -1);
        } else if (e.key === 'ArrowUp' && focusModeActive) {
          // Desktop focus editor only: a section is one continuous
          // horizontal staff line (no row above/below to move into), so
          // Up/Down instead move the caret between SECTIONS — the
          // TuxGuitar/Guitar Pro analogue of moving between tracks/the
          // next structural unit, same prev/next the nav strip's own
          // ‹/› buttons already do.
          e.preventDefault();
          if (focusIndex > 0) { focusIndex -= 1; renderFocusEditor(); }
        } else if (e.key === 'ArrowDown' && focusModeActive) {
          e.preventDefault();
          if (focusIndex < structSections.length - 1) { focusIndex += 1; renderFocusEditor(); }
        }
      });
      row.append(input);
      slotWrap.appendChild(row);
      slots.appendChild(slotWrap);
    });
    cell.appendChild(slots);
    updateFullness();

    // No per-bar freeform "ending" text field any more — alternate
    // endings are now their own dedicated panel below the common bars
    // (see renderAlternatives), modeled on LilyPond's own \alternative:
    // one or more SIBLING endings, each its own span of bars tagged with
    // which repeat pass(es) it plays on, not a label typed onto an
    // arbitrary bar in the middle of the common grid.
    const row2 = document.createElement('div');
    row2.className = 'matrix-cell-bottom';
    const delBar = document.createElement('button');
    delBar.type = 'button'; delBar.innerHTML = ICONS.close; delBar.title = t('remove this bar');
    delBar.addEventListener('click', () => { edited(); sec.measures.splice(mi, 1); renderMatrix(sec, secIndexOf(sec)); });
    row2.appendChild(delBar);
    cell.appendChild(row2);

    return cell;
  }

  function secIndexOf(sec) { return structSections.indexOf(sec); }

  // ---- Lyric line <-> plain text, mirroring internal/song/parse.go's
  // parseLyric exactly (chord segments as "[Chord]text" runs) so what's
  // typed here round-trips through the same grammar the raw-text editor
  // and the Typst pipeline both read. ------------------------------------
  function lyricLineToText(line) {
    return (line.segments || []).map(seg => (seg.hasChord ? `[${seg.chord}]` : '') + (seg.text || '')).join('');
  }
  function parseLyricLine(text) {
    const segs = [];
    let rest = text, pendingChord = null, havePending = false, buf = '';
    const flush = () => { if (buf.length || havePending) segs.push({ hasChord: havePending, chord: pendingChord || '', text: buf }); };
    while (rest.length) {
      const o = rest.indexOf('[');
      if (o < 0) { buf += rest; rest = ''; break; }
      buf += rest.slice(0, o);
      rest = rest.slice(o + 1);
      const c = rest.indexOf(']');
      if (c < 0) { buf += '[' + rest; rest = ''; break; }
      const chord = rest.slice(0, c);
      rest = rest.slice(c + 1);
      flush();
      pendingChord = chord; havePending = true; buf = '';
    }
    flush();
    return segs.length ? segs : [{ hasChord: false, chord: '', text: '' }];
  }
  function lyricsToText(lyrics) { return (lyrics || []).map(lyricLineToText).join('\n'); }
  function textToLyrics(text) {
    if (!text) return [];
    return text.split('\n').map(line => ({ segments: parseLyricLine(line) }));
  }

  function renderMatrix(sec, i) {
    const host = document.getElementById('matrix-host-' + i);
    if (!host) return;
    host.innerHTML = '';
    if (!sec.measures) sec.measures = [];

    // The persistent chord-entry toolbar — always visible above this
    // section's grid, not a popup. Desktop focus mode relocates it (and
    // the bar-tools block below) into the real sidebar rail — the actual
    // nav column the approved GP8 mockup repurposes as the toolbox —
    // instead of a second fake column squeezed into the main canvas
    // (the earlier .bar-matrix.focus-mode grid hack this replaces). Mobile
    // keeps it inline, first child of host, exactly as before.
    const toolbarEl = buildChordToolbar(sec, i);
    const toolboxToolbar = focusModeActive && !structureEl.hidden ? document.getElementById('toolbox-toolbar') : null;
    if (toolboxToolbar) { toolboxToolbar.innerHTML = ''; toolboxToolbar.appendChild(toolbarEl); }
    else host.appendChild(toolbarEl);

    // Structural tools (line width, add bar, repeat-phrase fill, repeat
    // barline) are collapsed by default: tap a bar to start editing its
    // chords, and these rarer whole-section actions stay one tap away
    // instead of pushing the actual chart below the fold.
    const toolsDetails = document.createElement('details');
    toolsDetails.className = 'matrix-tools';
    // Desktop focus editor: these tools live in the always-visible left
    // column (plenty of room), so there's no reason to keep them behind a
    // disclosure the way the cramped mobile blocks list needs to.
    if (focusModeActive) toolsDetails.open = true;
    const toolsSummary = document.createElement('summary');
    toolsSummary.textContent = t('bar tools (insert/repeat, line width)');
    toolsDetails.appendChild(toolsSummary);

    const toolbar = document.createElement('div');
    toolbar.className = 'matrix-toolbar';
    const perLine = document.createElement('label');
    perLine.className = 'hint';
    perLine.textContent = t('Bars per line') + ' ';
    const perLineInput = document.createElement('input');
    perLineInput.type = 'text'; perLineInput.inputMode = 'numeric'; perLineInput.style.width = '3rem';
    perLineInput.value = (sec.barsPerLine && sec.barsPerLine[0]) || 4;
    perLineInput.addEventListener('change', () => {
      edited();
      const n = parseInt(perLineInput.value, 10);
      sec.barsPerLine = n > 0 ? [n] : undefined;
      renderMatrix(sec, i);
    });
    perLine.appendChild(perLineInput);
    toolbar.appendChild(perLine);

    // Insert at the cursor, not just append at the end — Guitar Pro/
    // MuseScore both let you insert a bar/beat exactly where you're
    // looking, not only at the tail of the piece. Falls back to "append at
    // the end" when nothing is selected, so this never dead-ends on an
    // empty section. The new bar is selected afterward (bar N+1 lands at
    // the insertion point either way), so you can start typing into it
    // immediately — no extra click to find it.
    const insertBefore = document.createElement('button');
    insertBefore.type = 'button'; insertBefore.textContent = t('+ bar before');
    insertBefore.title = t('insert a new bar before the selected one');
    insertBefore.addEventListener('click', () => {
      pushUndo(); markDirty();
      const at = sec._sel ? sec._sel.mi : sec.measures.length;
      sec.measures.splice(at, 0, emptyMeasure());
      renderMatrix(sec, i);
      focusSlot(sec, at, 0);
    });
    const insertAfter = document.createElement('button');
    insertAfter.type = 'button'; insertAfter.textContent = t('+ bar after');
    insertAfter.title = t('insert a new bar after the selected one');
    insertAfter.addEventListener('click', () => {
      pushUndo(); markDirty();
      const at = sec._sel ? sec._sel.mi + 1 : sec.measures.length;
      sec.measures.splice(at, 0, emptyMeasure());
      renderMatrix(sec, i);
      focusSlot(sec, at, 0);
    });
    // Remove the selected bar — insert-before/-after existed, nothing to
    // take a bar back out again (the only way to shorten a section was
    // deleting the whole thing and starting over).
    const removeBar = document.createElement('button');
    removeBar.type = 'button'; removeBar.textContent = t('− bar');
    removeBar.title = t('remove the selected bar');
    removeBar.addEventListener('click', () => {
      if (!sec._sel || !sec.measures.length) return;
      pushUndo(); markDirty();
      const at = sec._sel.mi;
      sec.measures.splice(at, 1);
      sec._sel = null;
      renderMatrix(sec, i);
      if (sec.measures.length) focusSlot(sec, Math.min(at, sec.measures.length - 1), 0);
    });
    toolbar.append(insertBefore, insertAfter, removeBar);

    // Repeat toggle — LilyPond's own \repeat volta N: the common bars play
    // through, then repeat. Previously there was no way to set this at
    // all from the Blocks editor (open/close were readable from loaded
    // data but nothing ever wrote them). The actual close-repeat barline
    // belongs at the end of the LAST alternative ending when there are
    // any (see joinVoltaGroups) — a plain repeat with no endings closes
    // on its own last common bar instead.
    const repeatToggle = document.createElement('label');
    repeatToggle.className = 'hint matrix-repeat-toggle';
    const repeatCheckbox = document.createElement('input');
    repeatCheckbox.type = 'checkbox';
    // The open flag normally sits on the first COMMON bar — but a song
    // can have alternative endings with no shared common bars at all
    // (every bar belongs to some ending, e.g. Wake Up Alone's Chorus:
    // "(1,2,3.) D | ... | (4.) C ..." has no plain prefix), in which case
    // it landed on the first ending's first bar instead when loaded.
    const openBar = sec.measures[0] || (sec.endings[0] && sec.endings[0].measures[0]);
    repeatCheckbox.checked = !!(openBar && openBar.open);
    if (sec.measures.length === 0 && repeatCheckbox.checked) {
      // Already repeats, but entirely within the endings (no common
      // prefix) — this control can only toggle the common bars, so leave
      // it showing the true state but non-interactive rather than letting
      // a click silently do nothing.
      repeatCheckbox.disabled = true;
      repeatToggle.title = t('this section has no common bars before its endings — edit the repeat bracket in Source');
    }
    repeatCheckbox.addEventListener('change', () => {
      edited();
      if (sec.measures.length === 0) { repeatCheckbox.checked = false; return; }
      sec.measures.forEach(m => { m.open = false; m.close = false; });
      if (repeatCheckbox.checked) {
        sec.measures[0].open = true;
        // The close flag always lands on the real last bar at save time
        // (joinVoltaGroups), accounting for any alternative endings — set
        // it here too so the grid reads correctly before a save happens.
        if (!sec.endings || sec.endings.length === 0) sec.measures[sec.measures.length - 1].close = true;
      }
      renderMatrix(sec, i);
    });
    repeatToggle.append(repeatCheckbox, document.createTextNode(' ' + t('repeats (|: :|)')));
    toolbar.appendChild(repeatToggle);
    toolsDetails.appendChild(toolbar);
    if (toolboxToolbar) toolboxToolbar.appendChild(toolsDetails);
    else host.appendChild(toolsDetails);

    // Desktop focus editor: one continuous, horizontally-scrolling staff
    // line (Guitar Pro's "infinite staff") rather than the wrapped rows
    // the mobile/print layout uses — a LOCAL chunk-size override only,
    // never written back to sec.barsPerLine (that field is real saved
    // data the print/PDF pipeline also reads; overwriting it here would
    // silently corrupt the song's actual line-wrap intent).
    // Restore the selection highlight/toolbar state after a full rebuild
    // (every edit re-renders this host) — clamp first in case the bar or
    // slot that was selected no longer exists (bar deleted, density
    // lowered the slot count). Done BEFORE building the grid below:
    // focus mode's real-chart path (renderRealChart) needs sec._sel
    // already clamped when it does its own post-fetch highlight.
    if (sec._sel && (!sec.measures[sec._sel.mi] || sec._sel.ti >= sec.measures[sec._sel.mi].tokens.length)) {
      sec._sel = null;
    }

    if (focusModeActive) {
      // Desktop: the chart canvas is the REAL read-only Play-view chart,
      // not a lookalike — same "sheet-body" template, fetched from the
      // section's current (possibly unsaved) in-memory state via
      // /api/preview, so it can never visually drift from what reading
      // this song actually looks like. Editing is a thin overlay on top
      // (wireRealChartEditing): a .matrix-slot class + data-mi/data-ti on
      // each real .beat-slot, so the existing selection/toolbar machinery
      // (getSlotInput, highlightSelection, setChordPart, …) just works —
      // they already fall back to writing straight into sec.measures
      // when getSlotInput finds no <input>, which a real .beat-slot never
      // is. Mobile keeps the old bespoke box/table grid below unchanged.
      const chartHost = document.createElement('div');
      chartHost.className = 'real-chart-host';
      host.appendChild(chartHost);
      renderRealChart(sec, i, chartHost);
    } else {
      const rows = chunkRows(sec.measures, (sec.barsPerLine && sec.barsPerLine[0]) || 4);
      let mi = 0;
      rows.forEach(row => {
        const rowEl = document.createElement('div');
        rowEl.className = 'matrix-row';
        row.forEach(() => { rowEl.appendChild(buildMatrixCell(sec, mi)); mi++; });
        host.appendChild(rowEl);
      });
      if (sec.measures.length === 0) {
        const hint = document.createElement('p'); hint.className = 'hint'; hint.textContent = t('No bars yet — "+ bar" to start, or use fill for a repeating phrase.');
        host.appendChild(hint);
      }
    }

    renderAlternatives(sec, i, host);

    refreshToolbarHighlight(sec, i);
    dockToolbar(i);
    // Focus mode's grid highlight happens inside renderRealChart's own
    // fetch callback instead (the grid doesn't exist yet at this point —
    // it's still in flight) — the mobile/box path's grid is already in
    // the DOM synchronously above, so it highlights right away as before.
    if (!focusModeActive) highlightSelection(sec, i);
  }

  // ---- Focus mode's real chart canvas -----------------------------------
  // Fetches the SAME "sheet-body" HTML the real Play "Chords + lyrics" view
  // renders (internal/httpapi's handlePreview, given this song's current
  // structured state instead of saved .song text) and shows exactly the
  // section being edited, with a thin click-to-select overlay layered on
  // top. realChartReqId guards against an in-flight fetch from an earlier
  // edit landing AFTER a newer one and clobbering it (every keystroke that
  // calls renderMatrix kicks off a new fetch; only the latest should win).
  let realChartReqId = 0;
  // readOnly: render the real chart with no click-to-select overlay — used
  // for an "advanced syntax" section (sectionHasAdvancedSyntax), which has
  // no editable matrix at all, so a reader can at least SEE the staff
  // instead of just a text notice ("show the staff read-only" — the
  // notice alone wasn't good enough). The caller still can't edit it here;
  // the Source tab remains the only way to change it.
  async function renderRealChart(sec, i, chartHost, readOnly) {
    const myReq = ++realChartReqId;
    // joinVoltaGroups forces the Close flag onto a section's OWN last
    // bar — correct for the simple "repeats (|: :|)" toggle's own shape,
    // but WRONG for an advanced section (e.g. a repeat bracket covering
    // only a bars PREFIX — see sectionHasAdvancedSyntax's own doc
    // comment): it would move the close marker onto a bar that was never
    // the bracket's real end, rendering a silently corrupted preview —
    // exactly the bug sectionHasAdvancedSyntax exists to avoid. Advanced
    // sections pass their own measures through untouched instead, since
    // those were loaded correctly from the server and this code path
    // never edits them.
    const sectionsForPreview = structSections.map(s => ({
      ...s, measures: sectionHasAdvancedSyntax(s) ? s.measures : joinVoltaGroups(s), endings: undefined,
    }));
    let html;
    try {
      const res = await fetch('/api/preview', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ song: { meta: structMeta, chords: structChords, sections: sectionsForPreview, ...structLead } }),
      });
      html = await res.text();
    } catch (e) {
      return; // network hiccup — the previous render just stays on screen
    }
    if (myReq !== realChartReqId) return; // superseded by a newer edit
    const tmp = document.createElement('div');
    tmp.innerHTML = html;
    const match = tmp.querySelector(`.sheet-section[data-section-index="${i}"]`);
    chartHost.innerHTML = '';
    chartHost.classList.toggle('real-chart-host-readonly', !!readOnly);
    if (match) {
      chartHost.appendChild(match);
      if (!readOnly) wireRealChartEditing(sec, i, match);
    } else {
      const hint = document.createElement('p'); hint.className = 'hint';
      hint.textContent = sec.repeat ? t('(repeat marker — nothing to show)') : t('No bars yet — "+ bar" to start.');
      chartHost.appendChild(hint);
    }
    if (!readOnly) {
      highlightSelection(sec, i);
      refreshToolbarHighlight(sec, i);
    }
  }

  // Layers click-to-select onto the real chart markup without touching its
  // own structure: one .matrix-slot class + data-mi/data-ti per real
  // .beat-slot (one per token/beat, matching sec.measures[mi].tokens[ti]
  // exactly), so every existing selection/toolbar function keeps working
  // unmodified — they already address a slot purely by data-mi/data-ti,
  // never by assuming it's an <input>.
  function wireRealChartEditing(sec, i, sectionEl) {
    sectionEl.querySelectorAll('.bar').forEach(barEl => {
      const mi = parseInt(barEl.dataset.bar, 10);
      if (Number.isNaN(mi) || !sec.measures[mi]) return;
      const beatSlots = [...barEl.querySelectorAll('.beat-slot')];
      if (beatSlots.length === 0) {
        // A simile/rest/placeholder bar has no .beat-slot children at all
        // (it's a % mark or a whole-bar rest glyph) — still selectable, as
        // slot 0, so the toolbar's "clear"/duration buttons can turn it
        // back into a real chord.
        barEl.classList.add('matrix-slot');
        barEl.dataset.mi = String(mi); barEl.dataset.ti = '0';
        barEl.tabIndex = -1;
        barEl.addEventListener('click', () => selectSlot(sec, i, mi, 0));
        return;
      }
      beatSlots.forEach((slotEl, ti) => {
        if (!sec.measures[mi].tokens[ti] && sec.measures[mi].tokens[ti] !== '') return;
        slotEl.classList.add('matrix-slot');
        slotEl.dataset.mi = String(mi); slotEl.dataset.ti = String(ti);
        slotEl.tabIndex = -1;
        slotEl.addEventListener('click', () => selectSlot(sec, i, mi, ti));
      });
    });
  }

  // ---- Alternative endings — LilyPond's \alternative { \volta "1,2,3" {…}
  // \volta "4" {…} }: one or more SIBLING endings after the common bars,
  // each tagged with which repeat pass(es) it plays on (a comma-separated
  // numberlist, not just "1st"/"2nd" — "1,2,3" then "4" is completely
  // normal and exactly what Wake Up Alone's own Chorus grid already uses).
  // Replaces the old per-bar freeform "ending (1.)" text field, which had
  // no notion of a span of bars belonging together as one alternative, no
  // bracket, and no "which passes" concept at all — just a label typed
  // onto whichever single bar happened to be in front of you. -------------
  function nextVoltaLabel(endings) {
    const n = endings.length + 1;
    return n + '.';
  }
  function renderAlternatives(sec, i, host) {
    if (!sec.endings) sec.endings = [];
    const panel = document.createElement('div');
    panel.className = 'alt-endings-panel';
    const head = document.createElement('div');
    head.className = 'alt-endings-head';
    const title = document.createElement('span'); title.className = 'hint'; title.textContent = t('alternative endings');
    const addEnding = document.createElement('button');
    addEnding.type = 'button'; addEnding.className = 'alt-add-btn';
    addEnding.innerHTML = ICONS.plus + ' ' + t('ending');
    addEnding.addEventListener('click', () => {
      const barCount = sec.endings.length ? sec.endings[0].measures.length : 1;
      const measures = []; for (let b = 0; b < barCount; b++) measures.push(emptyMeasure());
      edited();
      sec.endings.push({ label: nextVoltaLabel(sec.endings), measures });
      renderMatrix(sec, i);
    });
    head.append(title, addEnding);
    panel.appendChild(head);

    if (sec.endings.length === 0) {
      const hint = document.createElement('p'); hint.className = 'hint';
      hint.textContent = t('No alternative endings — the common bars above just repeat identically each time.');
      panel.appendChild(hint);
    }

    const bracketRow = document.createElement('div');
    bracketRow.className = 'alt-endings-row';
    sec.endings.forEach((end, ei) => {
      const box = document.createElement('div');
      box.className = 'alt-ending';

      const endHead = document.createElement('div');
      endHead.className = 'alt-ending-head';
      const playsOn = document.createElement('label'); playsOn.className = 'hint'; playsOn.textContent = t('Plays on');
      const labelInput = document.createElement('input');
      labelInput.type = 'text'; labelInput.className = 'alt-label-input'; labelInput.value = end.label;
      labelInput.id = `alt-label-${i}-${ei}`; playsOn.htmlFor = labelInput.id;
      labelInput.title = t('which repeat pass(es) — e.g. "1." or "1,2,3." (LilyPond\'s own volta numberlist)');
      trackTextEdits(labelInput);
      labelInput.addEventListener('input', () => { end.label = labelInput.value; });
      const removeEnding = document.createElement('button');
      removeEnding.type = 'button'; removeEnding.className = 'icon-btn'; removeEnding.innerHTML = ICONS.close; removeEnding.title = t('Remove this ending');
      removeEnding.setAttribute('aria-label', t('Remove ending %d', ei + 1));
      removeEnding.addEventListener('click', () => { edited(); sec.endings.splice(ei, 1); renderMatrix(sec, i); });
      endHead.append(playsOn, labelInput, removeEnding);
      box.appendChild(endHead);

      const cells = document.createElement('div');
      cells.className = 'alt-ending-cells';
      end.measures.forEach((m, bi) => {
        const cell = document.createElement('div'); cell.className = 'alt-ending-cell';
        const input = document.createElement('input');
        // Space-separated tokens, not just the first one — an ending bar
        // can split beats same as any other bar (e.g. "Eb . Bb ." from
        // Ain't No Love's own Verse volta). Showing/editing only tokens[0]
        // would silently drop the rest the moment this field is touched;
        // this keeps full round-trip fidelity without needing the full
        // density-select sub-editor the common-bar grid has.
        input.type = 'text'; input.value = (m.tokens || []).join(' '); input.placeholder = t('chord (space-separate for sub-beats, e.g. Eb . Bb .)');
        input.setAttribute('aria-label', t('Ending %d, bar %d chords', ei + 1, bi + 1));
        input.autocapitalize = 'off'; input.spellcheck = false;
        trackTextEdits(input);
        input.addEventListener('input', () => { m.tokens = input.value.split(/\s+/).filter(Boolean); if (!m.tokens.length) m.tokens = ['']; });
        cell.appendChild(input);
        if (end.measures.length > 1) {
          const rm = document.createElement('button'); rm.type = 'button'; rm.innerHTML = ICONS.close; rm.title = t('remove this bar');
          rm.addEventListener('click', () => { edited(); end.measures.splice(bi, 1); renderMatrix(sec, i); });
          cell.appendChild(rm);
        }
        cells.appendChild(cell);
      });
      const addBarBtn = document.createElement('button');
      addBarBtn.type = 'button'; addBarBtn.className = 'alt-add-bar'; addBarBtn.textContent = t('+ bar');
      addBarBtn.addEventListener('click', () => { end.measures.push(emptyMeasure()); renderMatrix(sec, i); });
      cells.appendChild(addBarBtn);
      box.appendChild(cells);

      bracketRow.appendChild(box);
    });
    if (sec.endings.length) panel.appendChild(bracketRow);

    host.appendChild(panel);
  }

  // Converts {common bars, endings} back into one flat measures array for
  // saving — the grammar (and the existing renderer) only ever see plain
  // consecutive measures with a Volta label, same as parsing "(1.) Gm |
  // (2.) Eb" from raw text; this is purely a nicer way to AUTHOR that,
  // not a new file format. The close-repeat barline lands on the true
  // last bar (the last ending's last bar, when there are any), matching
  // where LilyPond's own repeat bracket actually ends.
  function joinVoltaGroups(sec) {
    const common = (sec.measures || []).map(m => ({ ...m, volta: '' }));
    const endingMeasures = (sec.endings || []).flatMap(end =>
      end.measures.map((m, bi) => ({ ...m, volta: end.label, open: false, close: false }))
    );
    const all = [...common, ...endingMeasures];
    if (all.length && all.some(m => m.open)) {
      // A repeat is in effect (first bar already marked open by the
      // toggle) — make sure exactly the true last bar is marked close,
      // regardless of where endings moved it to.
      all.forEach(m => { m.close = false; });
      all[all.length - 1].close = true;
    }
    return all;
  }

  // ---- Blocks editor — Kit (email editor) style: each section is a card
  // in a vertical stack, dragged by its own handle rather than nudged with
  // up/down buttons, with an "insert here" seam between every pair of
  // cards instead of a single trailing "add section" action. Chords and
  // lyrics both live in the card body — previously lyrics were invisible
  // here and only reachable in the raw Source tab, which is exactly the
  // split this editor exists to remove. -----------------------------------
  // ---- Generic popup menu — reused for the insert "+" (section-type
  // picker) and the chord quick-pick. Fixed-position so it isn't clipped
  // by the drawer's own scrolling body, closes on an outside click or
  // Escape, and never stacks more than one open menu at a time. ----------
  let openMenuEl = null;
  function closeAnyMenu() {
    if (openMenuEl) { openMenuEl.remove(); openMenuEl = null; }
    document.removeEventListener('mousedown', onOutsideMenuClick, true);
    document.removeEventListener('keydown', onMenuEscape, true);
  }
  function onOutsideMenuClick(e) { if (openMenuEl && !openMenuEl.contains(e.target)) closeAnyMenu(); }
  function onMenuEscape(e) { if (e.key === 'Escape') { e.preventDefault(); closeAnyMenu(); } }
  function positionMenu(menu, anchorEl) {
    document.body.appendChild(menu);
    const r = anchorEl.getBoundingClientRect();
    const top = Math.min(r.bottom + 4, window.innerHeight - menu.offsetHeight - 8);
    const left = Math.min(r.left, window.innerWidth - menu.offsetWidth - 8);
    menu.style.top = Math.max(8, top) + 'px';
    menu.style.left = Math.max(8, left) + 'px';
    openMenuEl = menu;
    setTimeout(() => {
      document.addEventListener('mousedown', onOutsideMenuClick, true);
      document.addEventListener('keydown', onMenuEscape, true);
    }, 0);
  }

  // One preset list, in the order sections usually come in a song, for
  // both the Chart's insert menu and Source's "+ Section" menu.
  const SECTION_QUICK_LABELS = ['Intro', 'Verse', 'Pre-Chorus', 'Chorus', 'Post-Chorus', 'Bridge', 'Solo', 'Interlude', 'Break', 'Outro', 'Coda', 'Tag'];
  (function fillSourceSectionMenu() {
    const host = document.getElementById('section-menu-presets');
    if (!host) return;
    for (const label of SECTION_QUICK_LABELS) {
      const b = document.createElement('button');
      b.type = 'button'; b.dataset.insertLabel = label; b.textContent = label;
      host.appendChild(b);
    }
  })();

  // A label the song already has is usually that section coming back:
  // a bare repeat of it (the chart shows the earlier one's bars and
  // lyrics again, numbered "Chorus 2"), not a new empty one.
  function insertRepeatAt(i, label) {
    edited();
    structSections.splice(i, 0, emptySection(label, true));
    focusIndex = i;
    renderStructureUI();
  }

  function insertSectionAt(i, label, openChords) {
    pushUndo(); markDirty();
    const sec = emptySection(label, false);
    // A brand-new section used to start with ZERO bars — reasonable for a
    // "bare chord sequence" fast-path (openChords), but every other new
    // section (picking a label like "Verse") landed on a totally blank
    // staff too, forcing the user to add bars one at a time from nothing
    // before they could type a single chord. Starts with 4 bars now in
    // every case — the same floor the "bare chord sequence" path already
    // used on mobile. Desktop's "bare chord sequence" path keeps its own
    // larger 8-bar default (its one continuous staff line, per the
    // Guitar Pro-inspired spec) since that path is specifically for
    // "I'm about to type out a real progression," not a label pick.
    const n = openChords && isDesktopWidth() ? 8 : 4;
    sec.measures = Array.from({ length: n }, () => emptyMeasure());
    structSections.splice(i, 0, sec);
    expanded.add(i);
    focusIndex = i;
    renderStructureUI();
    requestAnimationFrame(() => {
      const row = document.querySelector(`.block-row[data-index="${i}"] .block-label`) || document.querySelector('.focus-section-head .block-label');
      if (row) { row.focus(); if (!openChords) row.select(); }
    });
  }

  // The insert "+" menu — one spot for everything a new block in the flow
  // could be: a labeled section (one tap for the common ones), a bare
  // chord sequence with no label of its own (a turnaround/interlude that
  // doesn't deserve a full Verse/Chorus name), or a custom-labeled one.
  function openInsertMenu(anchorBtn, i) {
    closeAnyMenu();
    const menu = document.createElement('div');
    menu.className = 'insert-menu';
    const groupLabel = document.createElement('div'); groupLabel.className = 'insert-menu-group-label'; groupLabel.textContent = t('Section');
    menu.appendChild(groupLabel);
    const presets = document.createElement('div'); presets.className = 'insert-menu-grid';
    SECTION_QUICK_LABELS.forEach(label => {
      const item = document.createElement('button');
      item.type = 'button'; item.className = 'insert-menu-item'; item.textContent = label;
      item.addEventListener('click', () => { closeAnyMenu(); insertSectionAt(i, label, false); });
      presets.appendChild(item);
    });
    menu.appendChild(presets);
    const custom = document.createElement('input');
    custom.type = 'text'; custom.className = 'insert-menu-custom'; custom.placeholder = t('Other name… ⏎'); custom.maxLength = 40;
    custom.setAttribute('aria-label', t('Other section name')); custom.enterKeyHint = 'done';
    custom.addEventListener('keydown', e => {
      if (e.key !== 'Enter') return;
      e.preventDefault();
      const label = custom.value.trim();
      if (!label) return;
      closeAnyMenu();
      insertSectionAt(i, label, false);
    });
    menu.appendChild(custom);

    const existing = [...new Set(structSections.filter(s => !s.repeat).map(s => s.label).filter(Boolean))];
    if (existing.length) {
      const rsep = document.createElement('div'); rsep.className = 'insert-menu-sep'; menu.appendChild(rsep);
      const rlabel = document.createElement('div'); rlabel.className = 'insert-menu-group-label'; rlabel.textContent = t('Play again');
      menu.appendChild(rlabel);
      existing.forEach(label => {
        const item = document.createElement('button');
        item.type = 'button'; item.className = 'insert-menu-item'; item.textContent = '↻ ' + label;
        item.title = t('the %s again: same bars and lyrics, no new content to fill in', label);
        item.addEventListener('click', () => { closeAnyMenu(); insertRepeatAt(i, label); });
        menu.appendChild(item);
      });
    }

    const sep = document.createElement('div'); sep.className = 'insert-menu-sep'; menu.appendChild(sep);
    const contentLabel = document.createElement('div'); contentLabel.className = 'insert-menu-group-label'; contentLabel.textContent = t('Content');
    menu.appendChild(contentLabel);
    const chordItem = document.createElement('button');
    chordItem.type = 'button'; chordItem.className = 'insert-menu-item';
    chordItem.textContent = '▦ ' + t('Chord sequence');
    chordItem.title = t('a bar/beat grid with no label — a turnaround or interlude');
    chordItem.addEventListener('click', () => { closeAnyMenu(); insertSectionAt(i, 'Chords', true); });
    menu.appendChild(chordItem);

    positionMenu(menu, anchorBtn);
  }

  function buildInsertSeam(i) {
    const seam = document.createElement('div');
    seam.className = 'block-seam';
    const btn = document.createElement('button');
    btn.type = 'button'; btn.className = 'block-seam-btn'; btn.innerHTML = ICONS.plus; btn.title = t('insert here');
    btn.addEventListener('click', () => openInsertMenu(btn, i));
    seam.appendChild(btn);
    return seam;
  }

  let dragFrom = null;

  function renderBlocksList() {
    const list = document.getElementById('chip-list');
    list.innerHTML = '';
    // A single seam at the very top only — Notion doesn't put a visible
    // insertion affordance in every gap either; each row's own gutter "+"
    // (below) covers "insert after this one", which is every other gap.
    list.appendChild(buildInsertSeam(0));
    structSections.forEach((sec, i) => {
      const row = document.createElement('div');
      row.className = 'block-row' + (sec.repeat ? ' block-repeat' : '');
      row.draggable = true;
      row.dataset.index = String(i);

      row.addEventListener('dragstart', e => {
        dragFrom = i;
        row.classList.add('dragging');
        e.dataTransfer.effectAllowed = 'move';
      });
      row.addEventListener('dragend', () => { row.classList.remove('dragging'); dragFrom = null; });
      row.addEventListener('dragover', e => { e.preventDefault(); row.classList.add('drag-over'); });
      row.addEventListener('dragleave', () => row.classList.remove('drag-over'));
      row.addEventListener('drop', e => {
        e.preventDefault();
        row.classList.remove('drag-over');
        if (dragFrom === null || dragFrom === i) return;
        edited();
        const [moved] = structSections.splice(dragFrom, 1);
        // Re-inserting at the target's own original index i is correct in
        // both directions without any further adjustment: dragging down
        // (dragFrom < i), the removal already shifted the target left by
        // one, so index i now lands just AFTER it; dragging up
        // (dragFrom > i), the removal never touched indices before it, so
        // index i lands right BEFORE it. Either way the dragged row ends
        // up taking the target's old slot, which is what the row it's
        // dropped onto visually expects.
        structSections.splice(i, 0, moved);
        renderStructureUI();
      });

      // Left-margin gutter — hidden until this row is hovered/focused,
      // same as Notion's own per-block "⠿ +" pair (see the CSS comment on
      // .block-gutter). The gutter's own "+" inserts a new row right after
      // this one, covering every gap except the very first.
      const gutter = document.createElement('div'); gutter.className = 'block-gutter';
      const handle = document.createElement('span'); handle.className = 'block-handle'; handle.innerHTML = ICONS.grip; handle.title = t('drag to reorder');
      const addBtn = document.createElement('button'); addBtn.type = 'button'; addBtn.className = 'block-add-btn'; addBtn.innerHTML = ICONS.plus; addBtn.title = t('insert after this section');
      addBtn.addEventListener('click', () => openInsertMenu(addBtn, i + 1));
      gutter.append(handle, addBtn);
      row.appendChild(gutter);

      const head = document.createElement('div');
      head.className = 'block-head';
      const label = document.createElement('input'); label.type = 'text'; label.className = 'block-label'; label.value = sec.label;
      label.setAttribute('aria-label', t('Section name'));
      trackTextEdits(label);
      label.addEventListener('input', () => { sec.label = label.value; });
      const repeatToggle = document.createElement('button');
      repeatToggle.type = 'button'; repeatToggle.className = 'block-repeat-toggle';
      repeatToggle.textContent = sec.repeat ? t('repeat marker') : t('has content');
      repeatToggle.title = sec.repeat ? t('make this its own section with real content') : t('turn into a bare repeat marker (no content of its own)');
      repeatToggle.addEventListener('click', () => { edited(); sec.repeat = !sec.repeat; renderStructureUI(); });
      const bars_ = (sec.measures || []).length;
      const barsBadge = document.createElement('span'); barsBadge.className = 'block-bars-badge';
      barsBadge.textContent = bars_ ? tn(bars_, '%d bar', '%d bars') : '';
      const actions = document.createElement('div'); actions.className = 'block-row-actions';
      const dup = document.createElement('button'); dup.type = 'button'; dup.className = 'icon-btn'; dup.innerHTML = ICONS.duplicate; dup.title = t('duplicate as a repeat marker');
      dup.addEventListener('click', () => { edited(); structSections.splice(i + 1, 0, emptySection(sec.label, true)); renderStructureUI(); });
      const del = document.createElement('button'); del.type = 'button'; del.className = 'icon-btn'; del.title = t('delete this section');
      del.innerHTML = ICONS.close;
      del.addEventListener('click', () => { if (!confirmDeleteSection(sec)) return; edited(); structSections.splice(i, 1); renderStructureUI(); });
      actions.append(dup, del);
      head.append(label, repeatToggle, barsBadge, actions);
      row.appendChild(head);

      if (!sec.repeat) {
        const body = document.createElement('div');
        body.className = 'block-body';

        if (sectionHasAdvancedSyntax(sec)) {
          const notice = document.createElement('p');
          notice.className = 'hint advanced-syntax-notice';
          notice.textContent = tn(bars_, "Advanced grid syntax (%d bar) — a per-bar meter override, duration tag, or a repeat bracket this builder can't show as an editable grid. Shown below read-only; edit it precisely in the Source tab.", "Advanced grid syntax (%d bars) — a per-bar meter override, duration tag, or a repeat bracket this builder can't show as an editable grid. Shown below read-only; edit it precisely in the Source tab.");
          body.appendChild(notice);
          // Read-only, not just a text notice — same real chart markup
          // Play itself renders (renderRealChart), minus the click-to-
          // select overlay (readOnly=true). Lets a reader actually SEE
          // what they're about to go edit in Source instead of guessing
          // from the raw grid text alone.
          const chartHost = document.createElement('div');
          chartHost.className = 'real-chart-host';
          body.appendChild(chartHost);
          requestAnimationFrame(() => renderRealChart(sec, i, chartHost, true));
        } else {
          if (songHasGPFile()) body.appendChild(gpBarsRow(sec, i));
          const chordToggle = document.createElement('button');
          chordToggle.type = 'button'; chordToggle.className = 'block-chord-toggle';
          chordToggle.textContent = (expanded.has(i) ? '▾ ' : '▸ ') + (bars_ ? tn(bars_, 'chords (%d bar)', 'chords (%d bars)') : t('+ chord sequence'));
          chordToggle.addEventListener('click', () => {
            if (!bars_ && !expanded.has(i)) { sec.measures = [emptyMeasure(), emptyMeasure(), emptyMeasure(), emptyMeasure()]; }
            if (expanded.has(i)) expanded.delete(i); else expanded.add(i);
            renderStructureUI();
          });
          body.appendChild(chordToggle);
          if (expanded.has(i)) {
            const matrix = document.createElement('div');
            matrix.className = 'bar-matrix';
            matrix.id = 'matrix-host-' + i;
            body.appendChild(matrix);
            requestAnimationFrame(() => renderMatrix(sec, i));
          }
        }

        // Performance/arrangement notes — "GTR + claviers only", "Entree
        // Basse (ronde)..." — free text about this section OCCURRENCE,
        // not lyrics and not tied to a specific bar (see song.Section.Notes
        // and the iReal Pro reference this was modeled on). Collapsed by
        // default like the chord grid, since most sections never need one.
        if (!sec.notes) sec.notes = [];
        const notesToggle = document.createElement('button');
        notesToggle.type = 'button'; notesToggle.className = 'block-chord-toggle';
        notesToggle.textContent = (expandedNotes.has(i) ? '▾ ' : '▸ ') + (sec.notes.length ? t('notes (%d)', sec.notes.length) : t('+ note'));
        notesToggle.addEventListener('click', () => {
          if (expandedNotes.has(i)) expandedNotes.delete(i); else expandedNotes.add(i);
          renderStructureUI();
        });
        body.appendChild(notesToggle);
        if (expandedNotes.has(i) || sec.notes.length) {
          const notesBox = document.createElement('textarea');
          notesBox.className = 'block-notes';
          notesBox.placeholder = t('performance directions — one per line, e.g. "GTR + claviers only"');
          notesBox.setAttribute('aria-label', t('Performance notes for this section'));
          notesBox.value = sec.notes.join('\n');
          notesBox.rows = Math.max(2, Math.min(6, sec.notes.length + 1));
          trackTextEdits(notesBox);
          notesBox.addEventListener('input', () => { sec.notes = notesBox.value.split('\n').map(s => s.trim()).filter(Boolean); });
          body.appendChild(notesBox);
        }

        const lyricsBox = document.createElement('textarea');
        lyricsBox.className = 'block-lyrics';
        lyricsBox.placeholder = t('words for this section — [Chord] before a word attaches it there');
        lyricsBox.setAttribute('aria-label', t('Lyrics for this section'));
        lyricsBox.value = lyricsToText(sec.lyrics);
        lyricsBox.rows = Math.max(2, Math.min(10, (sec.lyrics || []).length + 1));
        lyricsBox.addEventListener('input', () => { markDirty(); sec.lyrics = textToLyrics(lyricsBox.value); });
        body.appendChild(lyricsBox);

        row.appendChild(body);
      }

      list.appendChild(row);
    });
    if (structSections.length === 0) {
      const hint = document.createElement('p'); hint.className = 'hint'; hint.textContent = t('No sections yet — use the + above to start.');
      list.appendChild(hint);
    }
  }

  // ---- Bars from the song's Guitar Pro file (gp-bar-picker.js) -----------
  // A section's "gp[Track]: bars" lines, chosen on a map of the file. They
  // live in structSections[i].gp, so Save, Discard and Undo cover them.
  const songHasGPFile = () => !!(document.getElementById('sheet') && document.getElementById('sheet').dataset.gpFile) && !!window.GPBarPicker;
  // How many bars a section plays, as the chart counts them: "|: :|"
  // brackets replayed (volta bars once), "times:" passes, a bare repeat
  // as long as the section it repeats; null when its bars come from the file.
  function drawerBarCount(i) {
    const sec = structSections[i];
    const ms = joinVoltaGroups(sec) || [];
    if (!ms.length) {
      if ((sec.gp || []).length) return null;
      for (let j = i - 1; j >= 0; j--) {
        if ((structSections[j].label || '').trim().toLowerCase() === (sec.label || '').trim().toLowerCase() && (structSections[j].measures || []).length) return drawerBarCount(j);
      }
      return 0;
    }
    let n = 0;
    for (let k = 0; k < ms.length; k++) {
      if (ms[k].open) {
        let e = k;
        while (e < ms.length && !ms[e].close) e++;
        if (e < ms.length) {
          const times = ms[e].repeatCount >= 2 ? ms[e].repeatCount : 2;
          for (let x = k; x <= e; x++) n += ms[x].volta ? 1 : times;
          k = e;
          continue;
        }
      }
      n++;
    }
    return n * Math.max(1, sec.times || 1);
  }
  function pickerSectionsFromDrawer() {
    const seen = {};
    const total = {};
    for (const sec of structSections) { const k = (sec.label || '').trim(); total[k] = (total[k] || 0) + 1; }
    return structSections.map((sec, i) => {
      const k = (sec.label || '').trim();
      seen[k] = (seen[k] || 0) + 1;
      return { index: i, label: total[k] > 1 ? `${k} ${seen[k]}` : k, barCount: drawerBarCount(i), gp: (sec.gp || []).map((g) => ({ ...g })) };
    });
  }
  function openBarPicker(i, track) {
    const variantId = document.getElementById('sheet').dataset.variantId;
    GPBarPicker.open({
      variantId, sections: pickerSectionsFromDrawer(), current: i, track,
      onApply: ({ index, track: t, spec }) => {
        edited();
        const sec = structSections[index];
        sec.gp = sec.gp || [];
        const g = sec.gp.find((x) => (x.track || '').toLowerCase() === (t || '').toLowerCase());
        if (g) g.spec = spec; else sec.gp.push({ track: t, spec });
      },
      onRemove: ({ index, track: t }) => {
        edited();
        const sec = structSections[index];
        sec.gp = (sec.gp || []).filter((x) => (x.track || '').toLowerCase() !== (t || '').toLowerCase());
      },
      onClose: (changed) => { if (changed) renderStructureUI(); },
    });
  }
  function gpBarsRow(sec, i) {
    const row = document.createElement('div');
    row.className = 'gp-bars-row';
    const label = document.createElement('span');
    label.className = 'gp-bars-label';
    label.textContent = t('Bars from the file');
    row.appendChild(label);
    for (const g of sec.gp || []) {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'gp-bars-chip';
      chip.textContent = `${g.track || t('All tracks')} · ${g.spec.replace(/(\d)-(\d)/g, '$1–$2')}`;
      chip.title = t('Change these bars');
      chip.addEventListener('click', () => openBarPicker(i, g.track));
      row.appendChild(chip);
    }
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'gp-bars-pick';
    btn.textContent = (sec.gp || []).length ? t('+ Another track') : t('Choose bars…');
    btn.addEventListener('click', () => openBarPicker(i, (sec.gp || []).length ? null : undefined));
    row.appendChild(btn);
    return row;
  }

  // ---- Desktop focus editor: one section at a time, Guitar Pro-style ----
  // Reuses buildChordToolbar/renderMatrix completely unmodified in their
  // own DOM-building logic — renderMatrix itself relocates the toolbar
  // (and bar-tools) it builds into the real sidebar rail (#toolbox-toolbar)
  // instead of appending it inline, and focusModeActive (read inside
  // renderMatrix/buildChordToolbar) switches the row-chunking and
  // split-progression behavior. No parallel chart-rendering code exists.
  function renderFocusEditor() {
    // Its toolbox lives in the page's sidebar, shared with every tab:
    // drawn only while the Chart shows (switching back re-renders it).
    if (structureEl.hidden) return;
    const list = document.getElementById('chip-list');
    list.innerHTML = '';

    if (structSections.length === 0) {
      const hint = document.createElement('p'); hint.className = 'hint';
      hint.textContent = t('No sections yet — use the + above to start.');
      list.appendChild(hint);
      document.getElementById('toolbox-sections').innerHTML = '';
      document.getElementById('toolbox-toolbar').innerHTML = '';
      return;
    }
    if (focusIndex >= structSections.length) focusIndex = structSections.length - 1;
    if (focusIndex < 0) focusIndex = 0;
    const i = focusIndex;
    const sec = structSections[i];

    // ---- Sections: a vertical list in the sidebar toolbox (GP8's own
    // track list lives in its left rail; this is the chord-chart
    // analogue), not a horizontal tab strip competing with the canvas for
    // width. Rebuilt in full on every render — cheap, and it's the only
    // thing here that needs to reflect every OTHER section's label too. --
    const sectionsList = document.getElementById('toolbox-sections');
    sectionsList.innerHTML = '';
    structSections.forEach((s, si) => {
      const row = document.createElement('button');
      row.type = 'button'; row.className = 'tbx-sec-row' + (si === i ? ' active' : '');
      row.title = s.repeat ? t('repeat marker') : tn((s.measures || []).length, '%d bar', '%d bars');
      const name = document.createElement('span'); name.textContent = s.label || (s.repeat ? t('(repeat)') : t('Untitled'));
      const count = document.createElement('span'); count.className = 'tbx-sec-count';
      count.textContent = s.repeat ? '—' : String((s.measures || []).length);
      row.append(name, count);
      row.addEventListener('click', () => { focusIndex = si; renderFocusEditor(); });
      sectionsList.appendChild(row);
    });
    const addRow = document.createElement('button');
    addRow.type = 'button'; addRow.className = 'tbx-sec-add';
    addRow.textContent = t('+ add section');
    addRow.addEventListener('click', () => openInsertMenu(addRow, i + 1));
    sectionsList.appendChild(addRow);

    // Cleared unconditionally, same as the host it used to live inside —
    // a repeat marker or advanced-syntax section renders no matrix at all
    // below, so nothing repopulates this otherwise and the PREVIOUS
    // section's toolbar would otherwise linger, looking editable.
    document.getElementById('toolbox-toolbar').innerHTML = '';

    const wrap = document.createElement('div');
    wrap.className = 'focus-editor';

    // ---- Main: this one section's editable content ----------------------
    const main = document.createElement('div');
    main.className = 'focus-main';

    const headRow = document.createElement('div');
    headRow.className = 'focus-section-head';
    const labelInput = document.createElement('input');
    labelInput.type = 'text'; labelInput.className = 'block-label'; labelInput.value = sec.label;
    labelInput.setAttribute('aria-label', t('Section name'));
    trackTextEdits(labelInput);
    labelInput.addEventListener('input', () => {
      sec.label = labelInput.value;
      const row = sectionsList.children[i];
      if (row) row.firstChild.textContent = labelInput.value || t('Untitled');
    });
    const repeatToggle = document.createElement('button');
    repeatToggle.type = 'button'; repeatToggle.className = 'block-repeat-toggle';
    repeatToggle.textContent = sec.repeat ? t('repeat marker') : t('has content');
    repeatToggle.addEventListener('click', () => { edited(); sec.repeat = !sec.repeat; renderFocusEditor(); });
    const delBtn = document.createElement('button');
    delBtn.type = 'button'; delBtn.className = 'icon-btn'; delBtn.title = t('delete this section'); delBtn.innerHTML = ICONS.close;
    delBtn.addEventListener('click', () => {
      if (!confirmDeleteSection(sec)) return;
      edited();
      structSections.splice(i, 1);
      if (focusIndex >= structSections.length) focusIndex = structSections.length - 1;
      renderFocusEditor();
    });
    headRow.append(labelInput, repeatToggle, delBtn);
    main.appendChild(headRow);
    if (!sec.repeat && songHasGPFile()) main.appendChild(gpBarsRow(sec, i));

    if (!sec.repeat) {
      if (sectionHasAdvancedSyntax(sec)) {
        const notice = document.createElement('p');
        notice.className = 'hint advanced-syntax-notice';
        notice.textContent = t('Advanced grid syntax (a per-bar meter override, duration tag, or a repeat bracket this builder can\'t show as an editable grid) — shown below read-only; edit it precisely in the Source tab.');
        main.appendChild(notice);
        const chartHost = document.createElement('div');
        chartHost.className = 'real-chart-host';
        main.appendChild(chartHost);
        requestAnimationFrame(() => renderRealChart(sec, i, chartHost, true));
      } else if ((!sec.measures || sec.measures.length === 0) && (sec.gp || []).length) {
        // Bars from the Guitar Pro file only: blank chord bars here would
        // turn it into an empty chart on the next save.
        const notice = document.createElement('p');
        notice.className = 'hint';
        notice.textContent = t('This section’s bars come from the Guitar Pro file.') + ' ';
        const addChords = document.createElement('button');
        addChords.type = 'button'; addChords.textContent = t('Add chord bars');
        addChords.addEventListener('click', () => { edited(); sec.measures = Array.from({ length: 4 }, () => emptyMeasure()); renderFocusEditor(); });
        notice.appendChild(addChords);
        main.appendChild(notice);
      } else if ((!sec.measures || sec.measures.length === 0) && (sec.scores || []).length) {
        // A score with no chord bars (an intro riff, a drum groove) takes
        // its bars from the score: giving it blank chord bars here would
        // hide it on the chart after the next save.
        const notice = document.createElement('p');
        notice.className = 'hint';
        notice.textContent = t('This section is a score only (edit it in the Scores tab): the chart takes its bars from the score.') + ' ';
        const addChords = document.createElement('button');
        addChords.type = 'button'; addChords.textContent = t('Add chord bars');
        addChords.addEventListener('click', () => { edited(); sec.measures = Array.from({ length: 4 }, () => emptyMeasure()); renderFocusEditor(); });
        notice.appendChild(addChords);
        main.appendChild(notice);
      } else {
        if (!sec.measures || sec.measures.length === 0) {
          sec.measures = Array.from({ length: 8 }, () => emptyMeasure());
        }
        const matrixHost = document.createElement('div');
        matrixHost.className = 'bar-matrix focus-mode';
        matrixHost.id = 'matrix-host-' + i;
        main.appendChild(matrixHost);
        requestAnimationFrame(() => {
          renderMatrix(sec, i);
          // Default chord placement on beat 1: land the selection there
          // the moment this section's grid is shown, rather than making
          // the user hunt for the first empty slot before the toolbar
          // does anything. selectSlot() (not focusSlot()) on purpose —
          // it updates sec._sel/the toolbar/highlight directly, which
          // works unconditionally; a page-load-time programmatic
          // el.focus() (what focusSlot ultimately does) is routinely
          // ignored by the browser outside a user gesture, so relying on
          // it alone left the auto-selection silently not happening.
          if (!sec._sel && sec.measures.length) {
            selectSlot(sec, i, 0, 0);
            const host = document.getElementById('matrix-host-' + i);
            // preventScroll: this runs on every section load — letting
            // the browser's default focus()-scrolls-into-view behavior
            // fire here visibly yanked the whole drawer body around on
            // every section switch, fighting the page's own scroll
            // position rather than quietly pre-arming the toolbar.
            host && host.querySelector('.matrix-slot[data-mi="0"][data-ti="0"]')?.focus({ preventScroll: true });
          }
        });
      }

      const notesBox = document.createElement('textarea');
      notesBox.className = 'block-notes';
      notesBox.placeholder = t('performance directions — one per line, e.g. "GTR + claviers only"');
      notesBox.setAttribute('aria-label', t('Performance notes for this section'));
      if (!sec.notes) sec.notes = [];
      notesBox.value = sec.notes.join('\n');
      notesBox.rows = Math.max(2, Math.min(6, sec.notes.length + 1));
      trackTextEdits(notesBox);
      notesBox.addEventListener('input', () => { sec.notes = notesBox.value.split('\n').map(s => s.trim()).filter(Boolean); });

      const lyricsBox = document.createElement('textarea');
      lyricsBox.className = 'block-lyrics';
      lyricsBox.placeholder = t('words for this section — [Chord] before a word attaches it there');
      lyricsBox.setAttribute('aria-label', t('Lyrics for this section'));
      lyricsBox.value = lyricsToText(sec.lyrics);
      lyricsBox.rows = Math.max(3, Math.min(12, (sec.lyrics || []).length + 1));
      lyricsBox.addEventListener('input', () => { markDirty(); sec.lyrics = textToLyrics(lyricsBox.value); });

      const lyricsNotes = document.createElement('div');
      lyricsNotes.className = 'focus-lyrics-notes';
      lyricsNotes.append(notesBox, lyricsBox);
      main.appendChild(lyricsNotes);
    }

    wrap.appendChild(main);
    list.appendChild(wrap);
  }
  const addSectionBtn = document.getElementById('add-section');
  addSectionBtn.addEventListener('click', () => openInsertMenu(addSectionBtn, structSections.length));
  document.getElementById('save-structure').addEventListener('click', async () => {
    const status = document.getElementById('structure-status');
    status.className = ''; status.textContent = t('Saving…');
    // joinVoltaGroups flattens each section's {common, endings} editing
    // shape back into the plain measures array the API/grammar actually
    // stores — built fresh for the request only, so the live sections
    // being edited keep their split common/endings shape afterward.
    const sectionsForSave = structSections.map(sec => ({ ...sec, measures: joinVoltaGroups(sec), endings: undefined }));
    const res = await fetch(`/api/songs/${pageVariantId}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: structVersion, song: { meta: structMeta, chords: structChords, otherChords: structOtherChords, sections: sectionsForSave, ...structLead } }),
    });
    if (res.status === 409) { status.className = 'err'; status.textContent = t('Not saved: this song changed since you opened it. Reload the page, then make your changes again.'); return; }
    if (!res.ok) { status.className = 'err'; status.textContent = t("Couldn't save. Your changes are still here; try again."); return; }
    const data = await res.json();
    structVersion = data.version;
    status.className = 'ok'; status.textContent = t('Saved. Reloading the chart…');
    // Clear BEFORE the reload, not after — isDirty was still true at the
    // moment of this save, and leaving it true would trip the beforeunload
    // guard on the very reload that just confirmed the save succeeded (a
    // real bug caught during verification: a browser-native "leave site?"
    // prompt appearing right after a successful Save).
    clearDirty();
    setTimeout(() => window.location.reload(), 600);
  });
  // Also run again after a Source save (window.SongStructure.reload), so
  // the Chart tab edits the text just saved rather than the one the page
  // opened with — a Chart save over a transposed song would hit a version
  // conflict otherwise.
  async function loadStructure() {
    const res = await fetch(`/api/songs/${pageVariantId}`);
    const data = await res.json();
    // A brand-new song (created blank, never saved through the structure
    // editor yet) returns meta: null, not []  — metaGet/metaSet's .find()
    // crashed on it, which silently killed renderHeaderFields() entirely
    // (no title/time/tempo/genre fields rendered at all, not just title)
    // for exactly this "editing a song that's never been touched" case —
    // reported directly as "can't edit the title," caught here instead.
    structVersion = data.version; structMeta = data.song.meta || []; structChords = data.song.chords || []; structOtherChords = data.song.otherChords || [];
    structLead = { lead: data.song.lead, leadMeasures: data.song.leadMeasures };
    // Split each section's flat measures into {common, endings} once, up
    // front — see splitVoltaGroups. Every editing function from here on
    // (renderMatrix, the Alternatives panel) only ever deals with that
    // split shape; joinVoltaGroups recombines it only at save time.
    structSections = data.song.sections.map(sec => {
      const { common, endings } = splitVoltaGroups(sec.measures || []);
      return { ...sec, measures: common, endings };
    });
    renderHeaderFields();
    if (!structureEl.hidden) renderStructureUI();
  }
  loadStructure();
  window.SongStructure = { reload: loadStructure };
  // The ⋯ menu's Tap tempo saved the song's tempo (tap-tempo.js): the
  // editor takes the saved song, unless it holds changes of its own (their
  // Save then says the song changed since).
  document.addEventListener('songbooks:tempo-saved', async () => {
    if (isDirty) return;
    loadStructure();
    const res = await fetch(`/api/songs/${pageVariantId}/raw`).catch(() => null);
    if (!res || !res.ok || isDirty) return;
    const data = await res.json();
    rawTextarea.value = data.text;
    rawVersion = data.version;
    renderHighlight();
  });

  // ---- Keyboard & handheld-pedal navigation ---------------------------------
  // Space = play/pause; Up/Down = previous/next section in this song;
  // Left/Right and PageUp/PageDown = previous/next song in the setlist. A
  // Bluetooth page-turn pedal is, to the browser, just a keyboard sending
  // one of these — AirTurn's common defaults are Left/Right or
  // PageUp/PageDown, both covered so a pedal works without reconfiguring it.
  document.addEventListener('keydown', e => {
    const t = document.activeElement;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return;
    const drawer = document.getElementById('edit-drawer');
    if (drawer && !drawer.hidden) return; // editing — never hijack keys here

    switch (e.key) {
      case ' ':
      case 'Spacebar':
        e.preventDefault();
        setPlaying(!clock.running);
        break;
      case 'ArrowUp': {
        if (!sections.length) break;
        e.preventDefault();
        const idx = currentSectionEl ? parseInt(currentSectionEl.dataset.sectionIndex, 10) : 0;
        jumpToSection(Math.max(0, idx - 1));
        break;
      }
      case 'ArrowDown': {
        if (!sections.length) break;
        e.preventDefault();
        const idx = currentSectionEl ? parseInt(currentSectionEl.dataset.sectionIndex, 10) : -1;
        jumpToSection(Math.min(sections.length - 1, idx + 1));
        break;
      }
      case 'ArrowLeft':
      case 'PageUp': {
        const prev = document.querySelector('.setlist-nav-card.prev');
        if (prev) { e.preventDefault(); window.location.href = prev.href; }
        break;
      }
      case 'ArrowRight':
      case 'PageDown': {
        const next = document.querySelector('.setlist-nav-card.next');
        if (next) { e.preventDefault(); window.location.href = next.href; }
        break;
      }
    }
  });

  rebindSheet();
})();

// Boots the vendored alphaTab JS renderer (static/js/vendor/alphatab)
// into every ".alphatab-row[data-tex]" container the server emitted —
// one per fully riff-covered display row (see attachTabRiffs,
// internal/httpapi/handlers_pages.go, and internal/tab.ToAlphaTex for
// the alphaTeX source each one carries). Real engraved tab notation,
// including real beam-bar grouping: TabRhythmMode.ShowWithBeams
// (despite its name) forces individual hooked flags — ShowWithBars is
// alphaTab's actual real-beams mode for a tab-only staff, confirmed
// this session against both a plain guitar riff and a real 4-string
// bass riff (Kawasaki) with its F clef. See this file's own render()
// for the settings that depend on that finding.
(function () {
  if (typeof alphaTab === 'undefined') return;

  const theme = () => getComputedStyle(document.body);

  function color(cssVar, fallback) {
    const v = theme().getPropertyValue(cssVar).trim();
    if (!v) return fallback;
    // getComputedStyle gives back whatever the CSS custom property's
    // own text is — this app's own tokens are always hex (#rrggbb), the
    // only form alphaTab.model.Color's fromJson-less constructor below
    // can parse directly without pulling in a full CSS color parser for
    // a one-off internal palette.
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(v);
    if (!m) return fallback;
    return new alphaTab.model.Color(parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16));
  }

  function render(container, retriesLeft) {
    const tex = container.dataset.tex;
    if (!tex) return;
    retriesLeft = retriesLeft === undefined ? 5 : retriesLeft;

    try {
      renderInner(container, tex, retriesLeft);
    } catch (e) {
      // Catches a throw ANYWHERE in renderInner — Settings construction
      // included, not just the AlphaTabApi construction/.tex() call a
      // narrower try/catch covered before. A code study of the vendored
      // bundle (this session) found the real DOM-clearing mechanism is
      // resizeRender()'s append/clear/reuseViewport pipeline, driven by
      // a SHARED, page-lifetime singleton ResizeObserver reused across
      // every AlphaTabApi instance on the page — if the iOS Safari
      // toolbar event leaves that shared observer/engine state
      // corrupted, a RETRY's own construction can throw synchronously.
      // Before any try/catch existed here, that throw aborted render()
      // immediately, which — critically — meant the 8-second watchdog
      // (only ever registered AFTER construction/.tex() succeeded)
      // never even got set up, and the poll/resize guards only act once
      // real content has painted at least once. Net effect: a
      // construction-time throw left NOTHING watching for the failure
      // at all, which is almost certainly why every previous recovery
      // attempt this session appeared to do nothing — not that the
      // retry logic itself was wrong, but that it never ran. This is
      // the outermost safety net: whatever throws, however early,
      // retry (with the same backoff-free immediacy the construction-
      // time watchdog would eventually reach anyway) or, once
      // retriesLeft is exhausted, fall back to location.reload() — the
      // one thing proven 100% reliable this session.
      if (retriesLeft <= 0) {
        location.reload();
        return;
      }
      container.innerHTML = '';
      render(container, retriesLeft - 1);
    }
  }

  function renderInner(container, tex, retriesLeft) {
    const settings = new alphaTab.Settings();
    settings.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    settings.core.useWorkers = false; // small, synchronous riffs — no benefit from a background worker, and it avoids a console error when the worker script can't resolve under this app's own static path
    // core.enableLazyLoading: forced OFF (alphaTab's default is on).
    // The vendored bundle's lazy loading registers ONE
    // IntersectionObserver on the whole container (threshold [0, .01,
    // 1]), paints whatever portion is visible the FIRST time it fires,
    // then unobserves — a one-shot partial paint, not a scroll-driven
    // virtualization that repaints as more of the container comes into
    // view. For a tall row (a long GP-imported riff rendered as one
    // continuous system — see attachTabRiffs), whatever was below the
    // fold at that one firing stays blank forever: confirmed live,
    // scrolling down a long chart to systems that were off-screen at
    // load shows a permanently empty gap, not a late-filling one. An
    // earlier version of this comment claimed turning this off was
    // WORSE ("No alphaTex data found", never recovers) — re-tested
    // directly against a live page with the exact construction order
    // already used here (settings set before construction, api.tex()
    // called right after) and it rendered correctly every time; that
    // finding doesn't reproduce. The watchdog below still earns its
    // keep for the other real failure mode it was written for — the
    // vendored bundle's own "AlphaTab container was invisible while
    // autosizing" case, when the container's width isn't resolved yet
    // at construction time.
    settings.core.enableLazyLoading = false;
    // player.scrollMode defaults to Continuous, targeting "html,body" —
    // the WHOLE PAGE — even though player.enablePlayer is false and
    // this app never uses alphaTab's own playback/cursor system (chart
    // following is this app's own clock.js-driven highlightTick, a
    // completely separate mechanism — see play.js/song.js). Confirmed
    // this session as the real cause of an "auto-scroll" complaint that
    // survived two earlier, narrower fixes to our OWN scroll code:
    // alphaTab's library default was scrolling the page on its own,
    // underneath whatever we do. Off entirely — nothing in this app
    // wants alphaTab touching page scroll position, ever.
    settings.player.scrollMode = alphaTab.ScrollMode.Off;
    // A percussion track (internal/tab.ToAlphaTexDrums — detected here by
    // its own literal "instrument percussion" directive, the one marker
    // every drum alphaTeX block carries) has no tab staff at all: there
    // are no frets to show, only GM percussion noteheads on a standard
    // 5-line staff. StaveProfile.Tab (every other riff kind's own
    // setting) would try to draw a tab staff that doesn't exist for this
    // track and leave the row blank; Score (confirmed against the
    // vendored library's own StaveProfile enum — "Default", "ScoreTab",
    // "Score", "Tab", "TabMixed"; there is no "ScoreDefault") renders the
    // standard-notation-only staff alphaTab already draws percussion on.
    const isDrumTrack = tex.includes('instrument percussion');
    settings.display.staveProfile = isDrumTrack ? alphaTab.StaveProfile.Score : alphaTab.StaveProfile.Tab;
    // Every row this app hands to alphaTab is its OWN separate document
    // (one bar, or a handful) — meaning every one of them is, as far as
    // alphaTab's own layout is concerned, the LAST (and only) system of
    // its score, and alphaTab's real default (lastSystemFillThreshold=1,
    // i.e. justifyLastSystem=false — confirmed against the vendored
    // library) deliberately does NOT stretch a trailing, possibly-
    // incomplete system to fill the page width, the normal engraving
    // convention. Every alphaTab mount in this app sits inside a
    // container ALREADY sized to its own content (a melodic TabOnly
    // row's --cap-based width, or Drummer view's own per-section
    // percentage width — buildDrumMap, handlers_pages.go) — so
    // stretching to fill that container, rather than standing out as
    // its own undersized system within it, is the right default for
    // every mount here, drum or melodic alike.
    settings.display.justifyLastSystem = true;
    settings.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
    // The "Guitar/Bass Standard Tuning" caption above the staff — this
    // app's own tuning is already implied by the song/section (and the
    // alphaTeX \tuning directive exists only to get alphaTab's string
    // count/clef inference right, not to be displayed), so it's a
    // redundant line that just eats vertical space for nothing a real
    // tab chart prints. GuitarTuning covers this element regardless of
    // instrument (confirmed: same enum entry gates the bass-tuning
    // caption too).
    settings.notation.elements.set(alphaTab.NotationElement.GuitarTuning, false);

    const fg = color('--fg', new alphaTab.model.Color(0x24, 0x1e, 0x18));
    settings.display.resources.staffLineColor = color('--fg-muted', fg);
    settings.display.resources.mainGlyphColor = fg;
    settings.display.resources.secondaryGlyphColor = fg;
    settings.display.resources.scoreInfoColor = fg;
    settings.display.resources.barSeparatorColor = fg;

    // Chord names ({ch "..."} in the generated alphaTeX — see
    // internal/tab.ToAlphaTex) are their own text glyph
    // (NotationElement.EffectChordNames), styled independently of the
    // rest of the engraving — match it to the real computed style of
    // this chart's own chord labels (.csym in app.css: font-family
    // var(--font-chart), font-size 1.4rem, font-weight 400, italic —
    // confirmed via getComputedStyle against a live chart rather than
    // read off the CSS source, since 1.4rem only means something once
    // resolved against the actual root font size). alphaTab draws the
    // whole symbol as one glyph, so this can't reproduce .csym-root's
    // bold-root/plain-quality split styling, but matching the
    // PREDOMINANT weight (regular, not bold — only the root letter is
    // ever bold) and the real resolved size reads far closer than the
    // earlier bold/fixed-16 guess did.
    const remPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    settings.display.resources.elementFonts.set(
      alphaTab.NotationElement.EffectChordNames,
      new alphaTab.model.Font('EB Garamond', remPx * 1.4, alphaTab.model.FontStyle.Italic, alphaTab.model.FontWeight.Regular)
    );

    // Reserve the row's approximate final height BEFORE alphaTab paints
    // anything, so the page doesn't suddenly grow by hundreds or
    // thousands of pixels the instant rendering finishes — confirmed
    // this session to be the real cause behind what reads as an
    // unwanted auto-scroll on a slower device (an iPad in particular):
    // .alphatab-row starts at 0 height with nothing reserved, so once a
    // long continuous riff (see attachTabRiffs — a whole GP-imported
    // section can be one row now) finally paints, everything below it
    // gets shoved down by its full height in one frame — on a slow
    // device that jump can land well after the reader has already
    // started scrolling/reading, which is exactly what reads as the
    // page yanking itself around. The estimate only has to be in the
    // right ballpark (it's replaced by the real layout the moment
    // content actually paints — see clearReservedHeight below): 6 bars
    // per system and ~150px per system are rough figures read off this
    // app's own rendered charts, not a documented alphaTab constant.
    const barCount = (tex.match(/\|/g) || []).length;
    const estSystems = Math.max(1, Math.ceil(barCount / 6));
    container.style.minHeight = (estSystems * 150) + 'px';
    const clearReservedHeight = () => { container.style.minHeight = ''; };

    // attemptNum (1 on the very first render() call, climbing on each
    // recursive retry) drives both the backoff delay and when to give
    // up on in-place retrying — threaded through retriesLeft on every
    // recursive render() call, not a fresh per-call counter, so it
    // actually accumulates across the whole retry chain instead of
    // resetting to 0 every time render() re-runs.
    const attemptNum = 6 - retriesLeft; // retriesLeft starts at 5 → attemptNum starts at 1

    // recover is the one retry path EVERY failure mode below funnels
    // into — construction-time ("container invisible while
    // autosizing", no width yet) and resize-triggered (confirmed this
    // session via screen recording: alphaTab's own container-width
    // watcher re-renders a row whenever it thinks the width changed,
    // and iOS Safari's dynamic toolbar collapsing/re-expanding as you
    // scroll fires that essentially instantly, far faster than a
    // user's own scroll gesture — the row goes blank in the SAME frame
    // the toolbar animation completes, not gradually). A single retry
    // attempt wasn't enough: confirmed live, pinch-zoom/rotation/window
    // resize (all of which SHOULD have re-triggered a one-shot retry)
    // did nothing, and only a full page reload reliably recovered it —
    // consistent with iOS Safari's viewport geometry getting stuck
    // wrong for a while after that animation, not instantly correct
    // again. So: more attempts, growing backoff between them (give the
    // browser's own geometry time to resettle rather than retrying into
    // the same bad state), and — since a real reload is the only thing
    // proven 100% reliable — an actual location.reload() as the final
    // fallback if every in-place attempt still comes up empty, rather
    // than leaving a reader staring at a permanently blank chart.
    function recover() {
      if (container.querySelector('svg text')) return; // already fine
      if (retriesLeft <= 0) {
        location.reload();
        return;
      }
      container.innerHTML = '';
      container.style.minHeight = (estSystems * 150) + 'px';
      render(container, retriesLeft - 1);
    }

    let hadContent = false;
    const resizeGuard = new ResizeObserver(() => {
      if (!hadContent) return;
      clearTimeout(resizeGuard._t);
      // Backoff grows with each attempt (600ms, 1.2s, 2.4s, ...) — a
      // fixed short delay kept retrying into the SAME still-unsettled
      // geometry right after a toolbar animation, confirmed live.
      resizeGuard._t = setTimeout(() => {
        if (hadContent && !container.querySelector('svg text')) {
          resizeGuard.disconnect();
          clearInterval(pollGuard);
          recover();
        }
      }, 600 * Math.pow(2, attemptNum - 1));
    });
    resizeGuard.observe(container);

    // pollGuard: a second, trigger-AGNOSTIC safety net alongside
    // resizeGuard above. Confirmed this session (a second screen
    // recording, after shipping the resize-based recovery): the chart
    // went blank and then stayed blank for the full several-second
    // clip with NO sign of any retry attempt ever firing — meaning
    // resizeGuard's own ResizeObserver, which only watches THIS
    // container's own box, never fired at all for whatever actually
    // caused this. Rather than keep guessing at the exact DOM event
    // responsible (two guesses — player.scrollMode, then a
    // container-resize — have both been wrong, or at least incomplete),
    // this just polls plainly: once a row has shown real content, check
    // every 1.5s whether it's still there, independent of any specific
    // event. Guaranteed to catch the problem within 1.5s of it
    // happening no matter what causes it, at the cost of a trivial,
    // infrequent DOM query while content is healthy.
    const pollGuard = setInterval(() => {
      if (!hadContent) return;
      if (container.querySelector('svg text')) return;
      clearInterval(pollGuard);
      resizeGuard.disconnect();
      recover();
    }, 1500);

    // Construction and .tex() are wrapped: a code study of the vendored
    // bundle (this session) found the real DOM-clearing mechanism is
    // resizeRender()'s append/clear/reuseViewport pipeline, driven by a
    // SHARED, page-lifetime singleton ResizeObserver reused across every
    // AlphaTabApi instance — if the iOS Safari toolbar event leaves that
    // shared observer/engine state corrupted, a RETRY's own construction
    // can throw synchronously. Before this try/catch, that throw aborted
    // render() immediately, which — critically — meant the 8-second
    // watchdog below (registered only AFTER construction/`.tex()`
    // succeed) never even got set up, and pollGuard/resizeGuard above
    // only act once hadContent has been true at least once. Net effect:
    // a construction-time throw left NOTHING watching for the failure at
    // all, which is almost certainly why every previous recovery attempt
    // this session appeared to do nothing — not that the retry logic was
    // wrong, but that it never ran.
    let finished = false;
    try {
      const api = new alphaTab.AlphaTabApi(container, settings);
      api.renderFinished.on(() => {
        finished = true;
        pollForChordFix(container, tex, 0, () => {
          hadContent = true;
          clearReservedHeight();
        });
      });
      api.tex(tex);
    } catch (e) {
      clearInterval(pollGuard);
      resizeGuard.disconnect();
      recover();
      return;
    }

    // Construction-time watchdog: the vendored bundle's "AlphaTab
    // container was invisible while autosizing" case, when the
    // container's width isn't resolved yet at construction (renderAll's
    // own requestAnimationFrame delay usually avoids this, but isn't a
    // hard guarantee across every layout path). Gated on renderFinished
    // rather than a short fixed delay: a long continuous riff can
    // legitimately take a few seconds to lay out on a slower device,
    // and destroying a still-in-progress render to "retry" it only
    // makes a slow render look like a permanently blank one.
    setTimeout(() => {
      if (finished || container.querySelector('svg text')) return;
      clearInterval(pollGuard);
      resizeGuard.disconnect();
      recover();
    }, 8000);
  }

  // pollForChordFix retries fixChordNameSpacing across a few animation
  // frames instead of running it once right after renderFinished — a
  // real alphaTab quirk, confirmed this session: with lazy loading on
  // (its default, required — see the enableLazyLoading comment above),
  // renderFinished can fire before the actual SVG content is painted
  // (the lazy-loaded content fills in asynchronously, on its own
  // schedule, sometimes a further frame or more later) — fixing a
  // "<text>" that doesn't exist yet is a silent no-op, not an error, so
  // the single-rAF version of this call looked like it was failing to
  // find chord text at all even though the function itself was correct.
  function pollForChordFix(container, tex, attempt, onContent) {
    attempt = attempt || 0;
    requestAnimationFrame(() => {
      const hasContent = [...container.querySelectorAll('svg text')].some(t => t.textContent.trim() !== '');
      if (hasContent) {
        fixChordNameSpacing(container, tex);
        onContent?.();
        return;
      }
      if (attempt < 20) pollForChordFix(container, tex, attempt + 1, onContent);
    });
  }

  // fixChordNameSpacing corrects a real alphaTab layout gap, not a
  // choice of ours: its chord-name text glyph (NotationElement.
  // EffectChordNames) is stacked above the tab staff with a FIXED
  // inter-effect margin that doesn't scale with the glyph's own font
  // size — confirmed empirically this session, the gap is negative
  // (the chord name's bounding box overlaps the first string line) even
  // at alphaTab's own untouched default chord-name font size, not just
  // at the larger size this app sets to match its own chord-label
  // typography. There's no public Settings knob for this (read
  // ChordsEffectInfo's own glyph-creation code: the glyph itself has no
  // margin, and the surrounding stack spacing isn't exposed), and
  // patching the 1.2MB vendored file for a ~13px gap isn't worth the
  // fragility against future vendor updates — so this nudges the
  // rendered SVG's own chord-name <text> elements up by whatever's
  // needed to match this chart's own .chord-label convention (a clean
  // ~6-7px gap above the staff, confirmed via getComputedStyle against
  // a live normal-grid chord row this same session).
  function fixChordNameSpacing(container, tex) {
    const chordNames = [...new Set([...tex.matchAll(/\{ch\s+"([^"]+)"\}/g)].map(m => m[1]))];
    if (chordNames.length === 0) return;
    const MIN_GAP = 6;
    container.querySelectorAll('svg').forEach(svg => {
      const stringLineTops = [...svg.querySelectorAll('rect')]
        .map(r => r.getBoundingClientRect())
        .filter(r => r.width > 50 && r.height < 3)
        .map(r => r.top);
      if (stringLineTops.length === 0) return;
      const topLineY = Math.min(...stringLineTops);
      const chordTexts = [...svg.querySelectorAll('text')].filter(t => chordNames.includes(t.textContent));
      let shift = 0;
      for (const t of chordTexts) {
        const r = t.getBoundingClientRect();
        if (r.top >= topLineY) continue; // starts at/below the staff — not a chord-name glyph (the overlap bug this corrects makes the BOTTOM check unreliable: an overlapping chord name's bottom can already be past topLineY)
        shift = Math.max(shift, MIN_GAP - (topLineY - r.bottom));
      }
      if (shift <= 0) return;
      for (const t of chordTexts) {
        const y = parseFloat(t.getAttribute('y'));
        if (!Number.isNaN(y)) t.setAttribute('y', (y - shift).toFixed(2));
      }
    });
  }

  function renderAll() {
    // A container's width is still 0 at the exact moment a bottom-of-
    // body <script> runs — alphaTab measures the element synchronously
    // on construction and silently skips rendering ("AlphaTab skipped
    // rendering because of width=0 (element invisible)") rather than
    // waiting for layout. One rAF is enough to land after the browser's
    // first layout pass (confirmed empirically this session: the exact
    // same construction succeeds immediately when run a tick later from
    // the console, with no other change).
    requestAnimationFrame(() => {
      document.querySelectorAll('.alphatab-row[data-tex]').forEach(render);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', renderAll);
  } else {
    renderAll();
  }

  // Fallback repaint nudge, belt-and-suspenders alongside the CSS
  // compositing-layer hint on .alphatab-row (app.css) — the real WebKit
  // bug this works around (tall SVG content scrolled off-screen and
  // back not repainting, confirmed iPad-only, DOM content intact, only
  // a full reload otherwise recovers it) was reported by the compositing
  // layer hint alone NOT being sufficient in every case historically for
  // this bug class, so this adds an explicit forced-reflow nudge
  // (toggling a sub-pixel transform forces WebKit to recompute the
  // layer) on scroll, debounced to the scroll settling rather than
  // firing every frame.
  let scrollNudgeTimer = null;
  window.addEventListener('scroll', () => {
    clearTimeout(scrollNudgeTimer);
    scrollNudgeTimer = setTimeout(() => {
      document.querySelectorAll('.alphatab-row[data-tex]').forEach(row => {
        if (!row.querySelector('svg')) return;
        row.style.transform = 'translateZ(0.01px)';
        requestAnimationFrame(() => { row.style.transform = 'translateZ(0)'; });
      });
    }, 150);
  }, { passive: true });

  // Exposed for the score editor's live preview (score-editor.js): it
  // writes a fresh data-tex onto its own preview container on every
  // edit and needs to re-trigger this same renderer on demand, not just
  // once at page load.
  window.AlphaTabTab = { render };
})();

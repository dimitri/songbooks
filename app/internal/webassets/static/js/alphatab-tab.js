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

  // alphaTab's OWN layout engine already knows, precisely, when a row's
  // container is too narrow for what it's being asked to draw — it logs
  // exactly that ("Bars 1, 7 do not fit into the available width ...
  // up to 32px missing. Notation elements might overlap.") through
  // alphaTab.Logger whenever a bar gets compressed below its own
  // computed minimum. Confirmed live as the real cause of a Drummer-
  // view multi-bar-rest glyph sitting left-shifted in its own measure
  // with a dead gap before the next barline, at a container width this
  // app's own hand-fitted min-width formula considered more than
  // enough: three rounds of guessing that formula from screenshots
  // (bars-based, then real-bars/rest-runs-based) never priced in this
  // specific compression, because it only shows up through alphaTab's
  // own internal per-bar layout math, not anything visible from the
  // outside. Wrapping the logger's own sink to capture that number
  // turns "guess a width, screenshot it, guess again" into "ask
  // alphaTab how many px it was short, add exactly that." The original
  // sink is kept and still called — this never silences the warning,
  // only also captures it.
  let lastMissingPx = 0;
  const originalLogSink = alphaTab.Logger.log;
  alphaTab.Logger.log = {
    debug: (...a) => originalLogSink.debug(...a),
    info: (...a) => originalLogSink.info(...a),
    error: (...a) => originalLogSink.error(...a),
    warning: (category, message, ...rest) => {
      const m = typeof message === 'string' && message.match(/up to (\d+(?:\.\d+)?)px missing/);
      if (m) lastMissingPx = Math.max(lastMissingPx, parseFloat(m[1]));
      originalLogSink.warning(category, message, ...rest);
    },
  };

  // giveUp replaces every retry chain's final fallback. A silent
  // location.reload() here used to double as this app's own crash
  // recovery — reliable for the resize-corruption case it was written
  // for, but just as happy to fire on a genuinely malformed alphaTeX
  // document (a real bug, not a transient glitch no retry will ever fix)
  // and reload the WHOLE page under a reader who has no idea why,
  // discarding anything else live on it (reference-audio playback,
  // mid-edit form state). Leave the row visibly failed instead: no more
  // retries, no page reload, just a small note so it's obviously inert
  // rather than silently blank.
  function giveUp(container) {
    container.style.minHeight = '';
    container.style.visibility = '';
    container.innerHTML = '<p class="alphatab-render-failed">Couldn’t render this chart. <a href="javascript:location.reload()">Reload the page</a> to try again.</p>';
  }

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

  // How much room a bar's notation needs, relative to its neighbours:
  // mostly how many things it prints — one unit per beat, growing only
  // slightly with its length (a quarter about 1.5 sixteenths), grace
  // notes a little less — plus a small base for the barline and margins.
  function barContentWeight(bar) {
    let w = 0;
    for (const v of bar.voices) {
      let vw = 1;
      for (const b of v.beats) {
        if (b.graceType) { vw += 0.7; continue; }
        vw += 1 + 0.25 * Math.log2(Math.max(1, (b.playbackDuration || 960) / 240));
      }
      w = Math.max(w, vw);
    }
    return w;
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
      // retriesLeft is exhausted, give up in place (see giveUp's own
      // doc comment for why this is no longer a page reload).
      if (retriesLeft <= 0) {
        giveUp(container);
        return;
      }
      container.innerHTML = '';
      render(container, retriesLeft - 1);
    }
  }

  function renderInner(container, tex, retriesLeft) {
    // Drummer view's own rows are all constructed in the SAME
    // synchronous forEach (renderAll, below) — confirmed live as a real,
    // reproducible bad-layout case distinct from the lazy-load race the
    // watchdog further down already guards against: the very FIRST
    // render of a row can come out with a multi-rest glyph crowded
    // right up against the next bar despite correct settings (barsPerRow
    // already the right rendered-measure count, width already wide
    // enough — no "do not fit" warning fires for it, nothing here
    // reports anything wrong), while reconstructing that EXACT same
    // container with the EXACT same settings a moment later, once the
    // other rows being built in that same loop have settled, renders it
    // correctly every time. Hidden (not skipped) for the first attempt
    // so the user never sees the wrong layout flash by — restored once
    // the resettled rebuild's own tex() call returns (see below).
    if (container.dataset.bars && !container.dataset.resettled) {
      container.style.visibility = 'hidden';
      container.dataset.resettled = '1';
      setTimeout(() => {
        container.innerHTML = '';
        render(container, retriesLeft);
      }, 400);
      return;
    }

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
    // Drummer view's own "\tempo" boundary markers (ToAlphaTexDrums,
    // internal/tab/alphatex.go — see its own doc comment) are a pure
    // multiBarRest-consolidation trick: they must never actually draw a
    // "♩ = N" marking. Every OTHER mount in this app is unaffected —
    // nothing else ever emits a "\tempo" directive.
    settings.notation.elements.set(alphaTab.NotationElement.EffectTempo, false);
    // Drummer view's own rows (data-bars/data-restonly, set only there —
    // see buildDrumMap, handlers_pages.go) are always exactly ONE mount
    // per line, full page width, even where a section boundary falls
    // mid-line — there is never a sibling cell in the row to share space
    // or wrap against (the two previous designs both split a shared line
    // into several per-section mounts and spent three separate CSS fixes
    // fighting exactly that). Pinning barsPerRow still matters even at
    // full width: it stops alphaTab's own auto-wrap from ever drawing a
    // line's bars across TWO internal lines when the viewport is narrow.
    //
    // barsPerRow is the row's RENDERED measure count (RealBars+RestRuns,
    // data-realbars/data-restruns — each real bar is its own measure, a
    // whole consolidated rest is ONE measure regardless of how many raw
    // bars it folds in), NOT Bars (the raw bar count, data-bars — e.g. 8
    // for "2 rest + 6 rest" bars that render as only 2 measures). Using
    // Bars here was the actual cause of a multi-rest glyph sitting
    // left-hugged against its own barline with a dead gap after it, or
    // (once min-width grew enough to silence alphaTab's own "do not fit"
    // warning below) a short rest glyph stretched out to sit far from
    // where it belongs: telling alphaTab to divide the row into 8 slots
    // when only 2 are ever drawn makes it size and place those 2 against
    // a grid of mostly-phantom bars that don't exist, not the actual 2
    // it's drawing. Confirmed live: the exact same content, same width,
    // with barsPerRow set to the real "2" instead of the raw "8",
    // triggers no compression warning at all and centers correctly —
    // this is the fix the width-compression-retry below was papering
    // over, not a case that still needs it, though the retry stays on
    // as a backstop for whatever content this doesn't anticipate.
    // min-width is a legibility floor, not the row's primary width
    // (which is simply the full page width) — a backstop for a genuinely
    // narrow viewport.
    //
    // TOTAL bar count turned out to be the wrong signal for a mixed row
    // (buildDrumMap — real notation sharing a line with a consolidated
    // rest from a different section): an 8-bar row split 2 real/6 rest
    // and one split 6 real/2 rest both carry the same "Bars", but need
    // very different widths — each real bar's own notation needs its
    // own real room, while a consolidated rest's glyph needs roughly the
    // same small, fixed room regardless of how many bars it folds in.
    // Two rounds of this chasing a flat bars-based floor (confirmed live
    // against two different real rows on the actual re-imported song,
    // not just one) is why this is keyed on RealBars/RestRuns
    // (data-realbars/data-restruns, buildDrumMap) instead: each real bar
    // adds its own width, each separate consolidated-rest glyph adds its
    // own roughly-fixed floor, once each, regardless of how many bars
    // it represents. Coefficients fitted against two real measured
    // cases — a 2-real/2-rest-run row needing ~480px clean and a
    // 4-real/2-rest-run row (busier content: toms, a crash, more
    // syncopation) needing ~700px — with real margin kept on both: a
    // pixel-level check (canvas ImageData, not a screenshot) on an
    // earlier, narrower floor found the glyphs technically not
    // touching, but only by a few px, thin enough that a compressed
    // phone-width screenshot still read as a dead collision.
    // remPx is needed below (the row width floor) before it's otherwise
    // used for chord-font sizing, so it's computed here rather than
    // right next to that later use.
    const remPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;

    // This app is read hands-off while actually playing (band practice,
    // a gig) — nobody can stop to swipe a chart sideways mid-song, so a
    // row must never need horizontal scrolling, full stop. Confirmed
    // live this is NOT the same thing as just capping the container's
    // width: alphaTab's own answer to "fit this content into less width
    // than it wants" is to compress note SPACING, which has a floor —
    // past that floor it starts drawing noteheads, stems, chord names
    // and hi-hat crosses on top of each other instead of refusing to
    // shrink further. A capped width with no scale adjustment reliably
    // reproduced exactly that: readable glyphs stacked into an
    // unreadable mess, not just "tighter." The fix is display.scale, a
    // genuine zoom of the WHOLE engraving (noteheads, stems, text AND
    // their spacing, together) — everything shrinks in proportion, so
    // nothing can overlap that didn't already overlap at scale 1; it
    // just reads smaller, the same tradeoff a real pocket chart makes.
    // availablePx is measured from the row's own parent (.drum-row —
    // already laid out at its final width by this point, since the
    // global ".staff-line.alphatab-row { width: 100% }" rule, not this
    // container's own not-yet-set width, is what's sized it so far)
    // rather than assumed from the viewport, so it's correct inside a
    // narrower host too (an embed, a split view).
    const restFloorRem = (n) => n < 10 ? 8.5 : 11;
    if (container.dataset.bars) {
      const bars = parseInt(container.dataset.bars, 10);
      const realBars = parseInt(container.dataset.realbars, 10) || 0;
      const restRuns = parseInt(container.dataset.restruns, 10) || 0;
      settings.display.barsPerRow = Math.max(1, realBars + restRuns);
      const idealRem = container.dataset.restonly ? restFloorRem(bars) : (9.5 + realBars * 4.5 + restRuns * 8.5);
      const idealPx = idealRem * remPx;
      // A width-compression retry (below, driven by alphaTab's own "do
      // not fit" warning) already left container.style.minWidth at the
      // exact px value that fixed it — recomputing the formula here on
      // that retry pass would throw that correction straight away and
      // render the same too-narrow row all over again.
      if (!container.dataset.widthRetriesLeft) {
        const availablePx = container.parentElement.getBoundingClientRect().width;
        container.style.minWidth = Math.min(idealPx, availablePx) + 'px';
      }
      // scale is derived from whatever width is actually in effect right
      // now — the floor just set above, OR (on a retry pass, which skips
      // that block) a prior pass's own width-compression-retry growth —
      // against idealPx, the width that formula's coefficients were
      // measured AT scale 1. That ratio reproduces the same proportions
      // (same relative gaps, same relative glyph size) in whatever
      // smaller space is actually available, rather than leaving layout
      // at scale 1 and asking alphaTab to squeeze note spacing to fit —
      // confirmed live that squeeze has a floor, and past it alphaTab
      // draws noteheads, stems, chord names and hi-hat crosses on top of
      // each other instead of refusing to shrink further. Recomputed on
      // every pass (not just the first) so a retry's own width growth
      // keeps scale consistent with it too, instead of resetting to 1.
      const currentWidthPx = parseFloat(container.style.minWidth) || idealPx;
      settings.display.scale = Math.min(1, currentWidthPx / idealPx);
    }

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
    // the same bad state), and giveUp() in place as the final fallback
    // if every in-place attempt still comes up empty, rather than either
    // leaving a reader staring at a permanently blank chart OR reloading
    // the whole page out from under them (see giveUp's own doc comment).
    function recover() {
      if (container.querySelector('svg text')) return; // already fine
      if (retriesLeft <= 0) {
        giveUp(container);
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
    // data-systems ("5,4,4,4"): bars per system for a row whose song opens
    // with a pickup (viewerLine.Systems, handlers_pages.go), so the tab
    // keeps the chord grid's pickup + 4s phrasing instead of alphaTab's
    // own automatic line breaks.
    const systems = (container.dataset.systems || '').split(',').map(Number).filter((v) => v > 0);
    if (systems.length) settings.display.systemsLayoutMode = alphaTab.SystemsLayoutMode.UseModelLayout;

    let finished = false;
    lastMissingPx = 0;
    try {
      const api = new alphaTab.AlphaTabApi(container, settings);
      // A row opening with a pickup (\\ac, see attachTabRiffs) hides that
      // bar's number; alphaTab always prints bar index + 1, so shift the
      // rest down by one after every render: the first full bar reads 1,
      // as in print. Bar numbers are the only text drawn in the bar-number
      // colour (fret numbers have no fill).
      if (/\\ac\b/.test(tex)) {
        const numberColor = settings.display.resources.barNumberColor.rgba.toLowerCase();
        api.postRenderFinished.on(() => {
          for (const t of container.querySelectorAll('svg text[fill]')) {
            if (t.dataset.renumbered || (t.getAttribute('fill') || '').toLowerCase() !== numberColor) continue;
            const m = /^(\s*)(\d+)(\s*)$/.exec(t.textContent);
            if (!m) continue;
            t.textContent = m[1] + (Number(m[2]) - 1) + m[3];
            t.dataset.renumbered = '1';
          }
        });
      }
      if (systems.length) {
        api.scoreLoaded.on((score) => {
          for (const t of score.tracks) { t.systemsLayout = systems; t.defaultSystemsLayout = systems[systems.length - 1]; }
          // The model layout gives every bar of a system the same width
          // unless told otherwise: weight each bar by what it holds, as
          // alphaTab's automatic layout does, so a run of sixteenths gets
          // room and a held note or a pickup doesn't hog it. Each system
          // still fills the row.
          score.masterBars.forEach((mb, i) => {
            let weight = 0;
            for (const t of score.tracks) for (const st of t.staves) {
              const bar = st.bars[i];
              if (bar) weight = Math.max(weight, barContentWeight(bar));
            }
            mb.displayScale = weight;
            for (const t of score.tracks) for (const st of t.staves) if (st.bars[i]) st.bars[i].displayScale = weight;
          });
        });
      }
      // Stashed on the container itself — not returned, since render()'s
      // own callers never awaited a return value before this and a
      // retry/reconstruction can replace this same api instance later
      // (the container is the one thing that stays stable across that);
      // reading it straight off the container when needed (the GP
      // import wizard's own click-to-seek, gp-import.js) beats changing
      // this function's call signature for one caller's extra need.
      container._alphaTabApi = api;
      api.renderFinished.on(() => {
        finished = true;
        pollForChordFix(container, tex, 0, () => {
          hadContent = true;
          clearReservedHeight();
        });
      });
      api.tex(tex);
      container.style.visibility = '';
    } catch (e) {
      clearInterval(pollGuard);
      resizeGuard.disconnect();
      recover();
      return;
    }

    // Score loading/layout (unlike the actual SVG paint, which lazy
    // loading can still defer) runs synchronously inside tex(), so any
    // "Bars ... do not fit" warning it triggers is already captured by
    // the time tex() returns above: widen by exactly what alphaTab
    // itself says is missing (plus a small buffer for rounding) and
    // reconstruct, rather than leaving a known-too-narrow row on
    // screen. widthRetriesLeft is independent of retriesLeft (the
    // lazy-load/paint watchdogs' own budget) — the two problems are
    // unrelated and each needs its own retry ceiling so one can't
    // starve the other.
    //
    // The grown width is capped at the row's own parent's width
    // (availablePx — same measurement the drum-row floor above uses):
    // growing past that is exactly how a row ends up wider than the
    // screen during a gig, which this app must never do. If there's no
    // room left to grow at all (already at the container's own max),
    // skip the retry rather than reconstruct into the identical
    // still-too-narrow state forever — alphaTab's own compressed
    // layout, drawn within that width, is what stands.
    if (lastMissingPx > 0 && (container.dataset.widthRetriesLeft === undefined || +container.dataset.widthRetriesLeft > 0)) {
      const currentPx = container.getBoundingClientRect().width || parseFloat(container.style.minWidth) * remPx;
      const availablePx = container.parentElement.getBoundingClientRect().width;
      const grownPx = Math.min(currentPx + lastMissingPx + 8, availablePx);
      if (grownPx > currentPx + 0.5) {
        const widthRetriesLeft = container.dataset.widthRetriesLeft === undefined ? 3 : +container.dataset.widthRetriesLeft;
        container.style.minWidth = Math.ceil(grownPx) + 'px';
        container.dataset.widthRetriesLeft = widthRetriesLeft - 1;
        clearInterval(pollGuard);
        resizeGuard.disconnect();
        container.innerHTML = '';
        render(container, retriesLeft);
        return;
      }
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

  // refreshDrumMap re-fetches Drummer view's own rows (handleGetDrummerRows,
  // handlers_pages.go) packed against THIS container's real measured
  // width, before anything in it is mounted. The server's first-paint
  // HTML packs rows against a generous, desktop-oriented default (no way
  // for it to know the real viewport yet) — fine on an actual desktop
  // window, routinely too wide on a phone, which is what forced every
  // row through display.scale's shrink-to-fit before this existed.
  // Re-running the EXACT SAME packing logic (packDrumRowBounds,
  // handlers_pages.go) server-side against the width that's actually
  // available is the whole point: it's one formula, in one language,
  // always right for whatever device loads the page, rather than a
  // second, hand-ported copy of that packing logic in JS (which a future
  // tweak to the real one could silently drift out of sync with) or a
  // fixed "mobile" guess that's still wrong for a tablet or a split view.
  //
  // Called from two places: renderAll below (the common case — Drummer
  // is the reader's saved default mode, so .drum-map is already visible
  // at page load) and song.js's mode-switch handler (a reader whose
  // default is some OTHER mode, who then clicks into Drummer for the
  // first time this page load — .drum-map is width=0/invisible at
  // renderAll's own call, same "hidden rows never get a first paint"
  // trap every other per-mode row in this app already has to work
  // around). Self-guards via data-width-fetched so the second call site
  // is a cheap no-op on every page load where the first already ran, and
  // so flipping BACK to Drummer mode later doesn't re-fetch and re-tex
  // every row again for no reason.
  async function refreshDrumMap() {
    const drumMap = document.querySelector('.drum-map[data-variant-id]');
    if (!drumMap || drumMap.dataset.widthFetched) return;
    const variantId = drumMap.dataset.variantId;
    if (!variantId || variantId === '0') return; // unsaved editor preview (handlePutSectionScore) — nothing to re-fetch against
    const remPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    const widthRem = drumMap.getBoundingClientRect().width / remPx;
    if (!(widthRem > 0)) return; // hidden (not in drummer mode yet) — nothing to measure, don't mark fetched: the other call site retries once it's visible
    try {
      const res = await fetch(`/api/songs/${variantId}/drummer-rows?width=${widthRem.toFixed(2)}`);
      if (res.ok) {
        drumMap.innerHTML = await res.text();
        drumMap.dataset.widthFetched = '1';
      }
    } catch (e) {
      // Network hiccup or similar — the SSR default rows already in the
      // DOM stand as-is; display.scale remains the backstop for whatever
      // they don't perfectly fit. Not marked fetched: a later call (e.g.
      // the reader switching modes again) gets another chance.
    }
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
      refreshDrumMap().finally(() => {
        document.querySelectorAll('.alphatab-row[data-tex]').forEach(render);
      });
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

  // ---- Playback cursor, Guitar Pro style: the playing bar is tinted and
  // a vertical line sweeps across it from note to note. song.js calls this
  // every frame with the song position as a fractional performed bar
  // (from the reference recording or the metronome clock); each row knows
  // its first bar (data-first-bar) and alphaTab's boundsLookup knows where
  // every bar and beat was engraved. Returns true when a visible tab row
  // holds that bar, so song.js leaves the scrolling to this.
  let cursorRow = null;
  let cursorSystem = null;
  function cursorEls(row) {
    let bar = row.querySelector(':scope > .tab-cursor-bar');
    let beat = row.querySelector(':scope > .tab-cursor-beat');
    if (!bar) { bar = document.createElement('div'); bar.className = 'tab-cursor-bar'; row.appendChild(bar); }
    if (!beat) { beat = document.createElement('div'); beat.className = 'tab-cursor-beat'; row.appendChild(beat); }
    return { bar, beat };
  }
  function hideCursor() {
    if (!cursorRow) return;
    cursorRow.querySelectorAll(':scope > .tab-cursor-bar, :scope > .tab-cursor-beat').forEach((el) => { el.hidden = true; });
    cursorRow = null;
  }
  function tabCursor(position, following) {
    if (position === null || position === undefined || position < 0) { hideCursor(); return false; }
    const barNo = Math.floor(position);
    let row = null, api = null, local = -1;
    for (const r of document.querySelectorAll('.alphatab-row[data-first-bar]')) {
      const a = r._alphaTabApi;
      if (!r.offsetParent || !a || !a.score || !a.renderer || !a.renderer.boundsLookup) continue;
      const i = barNo - parseInt(r.dataset.firstBar, 10);
      if (i >= 0 && i < a.score.masterBars.length) { row = r; api = a; local = i; break; }
    }
    if (!row) { hideCursor(); return false; }
    const lookup = api.renderer.boundsLookup;
    const mbBounds = lookup.findMasterBarByIndex(local);
    if (!mbBounds) { hideCursor(); return false; }
    if (cursorRow !== row) {
      hideCursor();
      cursorRow = row;
    }

    // Position inside the bar in ticks. A pickup is timed as a whole bar
    // with its notes at the end, but engraved only as long as its notes.
    const mb = api.score.masterBars[local];
    const full = (mb.timeSignatureNumerator * 3840) / mb.timeSignatureDenominator;
    const len = mb.calculateDuration ? mb.calculateDuration() : full;
    const tick = Math.max(0, (position - barNo) * full - (full - len));

    // x of the beat being played, sliding towards the next one.
    const r = mbBounds.realBounds;
    let x = r.x;
    const staffBar = api.score.tracks[0] && api.score.tracks[0].staves[0] && api.score.tracks[0].staves[0].bars[local];
    const beats = staffBar && staffBar.voices[0] ? staffBar.voices[0].beats : [];
    const xOf = (b) => { const bb = lookup.findBeat(b); return bb ? (bb.onNotesX || bb.visualBounds.x) : null; };
    for (let i = 0; i < beats.length; i++) {
      const b = beats[i];
      if (b.playbackStart > tick) break;
      const x0 = xOf(b);
      if (x0 === null) continue;
      const next = beats[i + 1];
      const x1 = next ? xOf(next) : r.x + r.w - 4;
      const end = next ? next.playbackStart : len;
      const f = end > b.playbackStart ? Math.min(1, (tick - b.playbackStart) / (end - b.playbackStart)) : 0;
      x = x0 + (x1 !== null ? (x1 - x0) * f : 0);
    }

    const { bar, beat } = cursorEls(row);
    const sys = mbBounds.lineAlignedBounds || mbBounds.visualBounds || r;
    Object.assign(bar.style, { left: `${r.x}px`, top: `${sys.y}px`, width: `${r.w}px`, height: `${sys.h}px` });
    Object.assign(beat.style, { left: `${x}px`, top: `${sys.y}px`, height: `${sys.h}px` });
    bar.hidden = false;
    beat.hidden = false;
    // One alphaTab row can hold several systems: follow the system (line
    // of music) being played, not just the row.
    const system = `${row.dataset.firstBar}:${Math.round(sys.y)}`;
    if (following && system !== cursorSystem && window.songbooksKeepInView) window.songbooksKeepInView(bar);
    cursorSystem = system;
    return true;
  }
  window.songbooksTabCursor = tabCursor;

  // Exposed for the score editor's live preview (score-editor.js): it
  // writes a fresh data-tex onto its own preview container on every
  // edit and needs to re-trigger this same renderer on demand, not just
  // once at page load.
  window.AlphaTabTab = { render, refreshDrumMap };
})();

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
    retriesLeft = retriesLeft === undefined ? 2 : retriesLeft;

    const settings = new alphaTab.Settings();
    settings.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    settings.core.useWorkers = false; // small, synchronous riffs — no benefit from a background worker, and it avoids a console error when the worker script can't resolve under this app's own static path
    // core.enableLazyLoading: left at its default (true). Tried forcing
    // it off twice this session to work around what looked like an
    // IntersectionObserver race leaving the row permanently empty —
    // both times it was WORSE: with it off, the AlphaTabApi constructor
    // makes its own synchronous "initial render" attempt before
    // api.tex(tex) below has given it anything to render, logs "No
    // alphaTex data found", and — confirmed by directly retrying the
    // whole construction 3 times in a row and watching every attempt
    // fail identically — never recovers, not even on a fresh instance.
    // The watchdog below (retrying the whole render when nothing
    // painted in time) is the real mitigation for the occasional lazy-
    // loading stall; this setting isn't part of the fix.
    settings.display.staveProfile = alphaTab.StaveProfile.Tab;
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

    const api = new alphaTab.AlphaTabApi(container, settings);
    api.renderFinished.on(() => pollForChordFix(container, tex));
    api.tex(tex);

    // Watchdog: lazy loading's IntersectionObserver-gated content fill
    // is unreliable — confirmed repeatedly this session, root cause not
    // fully pinned down (page/tab visibility, scroll position and
    // script load order were all checked and ruled out as the trigger).
    // Sometimes it never fires at all, leaving the container's
    // placeholder divs permanently empty even though renderFinished
    // already fired and the score loaded fine. Neither a same-instance
    // api.render() call nor a full reconstruction with lazy loading
    // forced OFF reliably recovers it (the latter is also separately
    // broken — see the enableLazyLoading comment above); a full
    // reconstruction with lazy loading left at ITS DEFAULT is the one
    // thing that gets a fresh IntersectionObserver registration, which
    // is the only part of this actually under suspicion.
    const watchdog = () => {
      if (retriesLeft <= 0) return;
      if (container.querySelector('svg text')) return;
      container.innerHTML = '';
      retriesLeft--;
      render(container, retriesLeft);
    };
    setTimeout(watchdog, 1200);
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
  function pollForChordFix(container, tex, attempt) {
    attempt = attempt || 0;
    requestAnimationFrame(() => {
      const hasContent = [...container.querySelectorAll('svg text')].some(t => t.textContent.trim() !== '');
      if (hasContent) {
        fixChordNameSpacing(container, tex);
        return;
      }
      if (attempt < 20) pollForChordFix(container, tex, attempt + 1);
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

  // Exposed for the score editor's live preview (score-editor.js): it
  // writes a fresh data-tex onto its own preview container on every
  // edit and needs to re-trigger this same renderer on demand, not just
  // once at page load.
  window.AlphaTabTab = { render };
})();

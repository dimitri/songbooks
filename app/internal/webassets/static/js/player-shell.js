// player-shell.js — what every player in the app shares (app/reports/
// player-proposal.md §3): a bar always there, that opens into a panel —
// most of the screen on a wide one, a sheet from the bottom on a phone —
// and closes again with Escape, its close or grab button, a click beside
// it, or a swipe down. The rehearsals' player (band_recordings.html
// "rec-player", also on set lists) was the first; the song page's dock,
// the click track and the warm-ups use the same pieces.
//
//   PlayerShell.panel(opts) → {open(on), isOpen()}: the open/close
//     mechanics. opts:
//       root     the element that takes .is-open (the bar and its panel);
//       card     the element that becomes the dialog while open
//                (role=dialog, aria-modal), focus kept in it with Tab;
//       openers  buttons that open it (aria-expanded follows), the first
//                gets the focus back when it closes;
//       closers  elements that close it (×, grab handle, backdrop);
//       focus()  the element to focus on opening (default: the first
//                closer that is shown);
//       swipeIgnore  a selector inside card where a swipe isn't a close
//                (sliders, lists that scroll);
//       closedRole   the card's role when closed ("region"), or none;
//       onChange(on) after it opened or closed.
//     While open the page under it doesn't scroll (html.rp-locked).
//
//   PlayerShell.fit(opts) → refit(): the tray's tools on one line, by
//     priority (the song dock's, PR 1): where they don't all fit, the tools
//     that are off show as icons, then leave from the right, one by one,
//     into "+N"; a tool that is on never folds; last, the time goes and
//     the labels of the tools that are on. opts:
//       bar      the element that takes the classes (opts-compact,
//                opts-notime, opts-tight, has-more);
//       row      the line that must not overflow;
//       tray     the tools' container ([data-tool] children, in priority
//                order);
//       count    where "+N" is written;
//       skip()   true while not to fit (a phone, the bar hidden, the "+N"
//                menu open);
//       onFit(n) after a fit, with the number folded.
(function () {
  const locks = new Set();
  function lock(who, on) {
    if (on) locks.add(who); else locks.delete(who);
    document.documentElement.classList.toggle('rp-locked', locks.size > 0);
  }

  function panel(opts) {
    const root = opts.root, card = opts.card || opts.root;
    const openers = [...(opts.openers || [])].filter(Boolean);
    const closers = [...(opts.closers || [])].filter(Boolean);
    const isOpen = () => root.classList.contains('is-open');
    let returnTo = null;
    function open(on) {
      on = !!on;
      if (on === isOpen()) return;
      root.classList.toggle('is-open', on);
      lock(api, on);
      openers.forEach((b) => b.setAttribute('aria-expanded', String(on)));
      if (on) {
        returnTo = document.activeElement && root.contains(document.activeElement) ? null : document.activeElement;
        card.setAttribute('role', 'dialog');
        card.setAttribute('aria-modal', 'true');
        const f = opts.focus ? opts.focus() : closers.find((c) => c.offsetParent !== null);
        f?.focus({ preventScroll: true });
      } else {
        if (opts.closedRole) card.setAttribute('role', opts.closedRole); else card.removeAttribute('role');
        card.removeAttribute('aria-modal');
        const back = openers.find((b) => b.offsetParent !== null) || (returnTo && returnTo.isConnected ? returnTo : null);
        back?.focus({ preventScroll: true });
      }
      opts.onChange?.(on);
    }
    closers.forEach((el) => el.addEventListener('click', () => open(false)));
    // Escape closes it wherever the focus is in it (before anything else
    // hears it); Tab stays in it.
    window.addEventListener('keydown', (e) => {
      if (!isOpen()) return;
      if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault();
        e.stopPropagation();
        open(false);
      } else if (e.key === 'Tab') {
        const els = [...card.querySelectorAll('button, input, select, summary, a[href], [tabindex="0"]')].filter((el) => !el.disabled && el.offsetParent !== null);
        if (!els.length) return;
        const first = els[0], last = els[els.length - 1];
        if (!card.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
        else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    }, true);
    // A swipe down from the top of it closes it (a phone's sheet).
    let y0 = null;
    card.addEventListener('touchstart', (e) => {
      y0 = isOpen() && card.scrollTop <= 0 && !(opts.swipeIgnore && e.target.closest(opts.swipeIgnore)) ? e.touches[0].clientY : null;
    }, { passive: true });
    card.addEventListener('touchend', (e) => {
      if (y0 !== null && isOpen() && e.changedTouches[0].clientY - y0 > 80) open(false);
      y0 = null;
    });
    const api = { open, isOpen };
    return api;
  }

  function fit(opts) {
    const { bar, row, tray } = opts;
    if (!bar || !row || !tray) return () => {};
    const tools = () => [...tray.querySelectorAll(':scope > [data-tool]')];
    const toolOn = (el) => el.matches('.active, [aria-pressed="true"]') || !!el.querySelector('.active, [aria-pressed="true"]');
    let observer = null;
    function refit() {
      if (opts.skip?.() === 'keep') return;
      bar.classList.remove('opts-compact', 'opts-tight', 'opts-notime', 'has-more');
      const list = tools();
      list.forEach((el) => { delete el.dataset.folded; el.classList.remove('is-tight'); el.classList.toggle('is-on', toolOn(el)); });
      let n = 0;
      if (!opts.skip?.()) {
        const over = () => row.scrollWidth > row.clientWidth + 1;
        if (over()) {
          bar.classList.add('opts-compact');
          for (const el of [...list].reverse()) {
            if (!over()) break;
            if (el.hidden || el.classList.contains('is-on')) continue;
            el.dataset.folded = '1';
            n++;
            bar.classList.add('has-more');
            if (opts.count) opts.count.textContent = `+${n}`;
          }
          if (over()) bar.classList.add('opts-notime');
          for (const el of [...list].reverse()) {
            if (!over()) break;
            if (el.hidden || el.dataset.folded) continue;
            el.classList.add('is-tight');
            bar.classList.add('opts-tight');
          }
        }
      }
      if (opts.count) opts.count.textContent = n ? `+${n}` : '';
      opts.onFit?.(n);
      observer?.takeRecords();
    }
    let queued = false;
    function queue() {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => { queued = false; refit(); });
    }
    if (window.ResizeObserver) {
      const ro = new ResizeObserver(queue);
      ro.observe(bar);
      ro.observe(tray);
    }
    // A tool switched on or off, or its label changed: fit again.
    if (window.MutationObserver) {
      observer = new MutationObserver(queue);
      observer.observe(tray, { subtree: true, attributes: true, attributeFilter: ['class', 'aria-pressed', 'hidden'], childList: true, characterData: true });
    }
    queue();
    return queue;
  }

  window.PlayerShell = { panel, fit, lock };
})();

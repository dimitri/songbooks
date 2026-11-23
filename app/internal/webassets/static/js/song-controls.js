// song-controls.js — the song page's own controls (play.html .song-toolbar
// and .song-context-bar, app/reports/menu-proposal.md §3.5): the ⋯ menu
// (Versions, Historique, Référence, Exporter en PDF…, the Guitar Pro
// files), the PDF dialog, and on a phone the song bar's three sheets (Vue,
// Outils, Morceau) and its ‹. The view switcher (#mode-segmented) and the
// actions keep their ids and are wired where they always were (song.js,
// song-notes.js, audio-player.js, tap-tempo.js): a sheet's row forwards a
// click to the control it stands for.
(function () {
  const modeEl = document.getElementById('mode-segmented');
  const view = () => (modeEl && modeEl.dataset.value) || 'normal';
  const extraViews = ['drummer', 'mixer', 'reference'];
  let readingView = extraViews.includes(view()) ? 'normal' : view();
  const hasDrums = () => [...document.querySelectorAll('[data-realbars]')].some((e) => !e.dataset.restonly);
  // A row's control: Edit is its own button; the rest are views.
  function forward(value) {
    const real = value === 'edit'
      ? document.getElementById('edit-toggle')
      : document.querySelector(`#mode-segmented button[data-value="${value}"]`);
    real?.click();
  }

  // ---- The song bar's sheets (a phone) --------------------------------------
  const sheets = ['view-stack', 'tools-stack', 'song-stack'].map((id) => ({
    sheet: document.getElementById(id),
    btn: document.getElementById(id + '-btn'),
    backdrop: document.querySelector(`.stack-backdrop[data-stack="${id}"]`),
  })).filter((s) => s.sheet && s.btn);
  let open = null;
  function setSheet(s, on, returnFocus) {
    if (!s) return;
    s.sheet.classList.toggle('open', on);
    s.backdrop?.classList.toggle('open', on);
    s.btn.classList.toggle('active', on);
    s.btn.setAttribute('aria-expanded', String(on));
    if (on) {
      if (open && open !== s) setSheet(open, false);
      open = s;
      const first = [...s.sheet.querySelectorAll('.stack-sheet-row, .stack-sheet-chips button')].find((el) => !el.hidden && el.offsetParent !== null);
      (first || s.sheet).focus({ preventScroll: true });
    } else {
      if (open === s) open = null;
      if (returnFocus) s.btn.focus({ preventScroll: true });
    }
  }
  sheets.forEach((s) => {
    s.btn.addEventListener('click', () => setSheet(s, !s.sheet.classList.contains('open'), true));
    s.backdrop?.addEventListener('click', () => setSheet(s, false, true));
    s.sheet.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setSheet(s, false, true); }
    });
    // A row does its thing and the sheet goes: a view, Edit, a tool (the
    // player opens itself at its line: audio-player.js data-player-line),
    // the notes (song-notes.js), the PDF dialog, a page.
    s.sheet.addEventListener('click', (e) => {
      const row = e.target.closest('.stack-sheet-row');
      if (!row || row.hidden) return;
      const value = row.dataset.forward;
      if (value) {
        // The drums and the mixer: a second tap goes back to the chart.
        forward(row.closest('#tools-stack') && view() === value ? readingView : value);
      }
      setSheet(s, false, false);
    });
  });
  // The view sheet's Part: the same server-rendered choice as the
  // desktop's #instrument-picker, forwarded to it.
  document.getElementById('view-stack-instruments')?.addEventListener('click', (e) => {
    const b = e.target.closest('button[data-instrument]');
    if (b) document.querySelector(`#instrument-picker button[data-instrument="${b.dataset.instrument}"]`)?.click();
  });
  // The rows say which view is on, the restore at load included.
  function markViews() {
    const v = view();
    if (!extraViews.includes(v)) readingView = v;
    document.querySelectorAll('#view-stack .stack-sheet-row[data-forward]').forEach((row) => {
      row.classList.toggle('active', row.dataset.forward === v);
      if (row.dataset.forward === v) row.setAttribute('aria-current', 'true'); else row.removeAttribute('aria-current');
    });
    document.querySelectorAll('#tools-stack .stack-sheet-row[data-forward]').forEach((row) => {
      row.classList.toggle('active', row.dataset.forward === v);
      row.setAttribute('aria-pressed', String(row.dataset.forward === v));
    });
    const drums = document.querySelector('#tools-stack .tools-drummer-row');
    if (drums) drums.hidden = !hasDrums();
  }
  if (modeEl) new MutationObserver(markViews).observe(modeEl, { attributes: true, attributeFilter: ['data-value'] });
  markViews();

  // ‹: back to where the song was opened from. Coming from there, it's the
  // browser's back (the list scrolled where it was, menu-proposal.md §3.10);
  // else the link.
  document.querySelectorAll('.song-bar-back, .song-back').forEach((a) => a.addEventListener('click', (e) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    try {
      const from = document.referrer && new URL(document.referrer);
      const to = new URL(a.href);
      if (from && from.origin === location.origin && from.pathname === to.pathname && history.length > 1) {
        e.preventDefault();
        history.back();
      }
    } catch (err) { /* the link */ }
  }));

  // Reference: a render mode, reached from the ⋯ menu, the Morceau sheet
  // and the player's "+ Enregistrement" (its own wiring) — not a tab.
  ['reference-toggle', 'audio-empty-reference-link'].forEach((id) => document.getElementById(id)?.addEventListener('click', () => forward('reference')));

  // ---- ⋯: a menu button (WAI-ARIA menu button pattern) ----------------------
  const btn = document.getElementById('song-more-btn');
  const menu = document.getElementById('song-more-menu');
  if (btn && menu) {
    const items = () => [...menu.querySelectorAll('[role="menuitem"]')].filter((el) => !el.hidden);
    const isOpen = () => !menu.hidden;
    function setOpen(on, focus) {
      menu.hidden = !on;
      btn.setAttribute('aria-expanded', String(on));
      if (on && focus) (focus === 'last' ? items().at(-1) : items()[0])?.focus();
      if (!on && focus) btn.focus();
    }
    btn.addEventListener('click', () => setOpen(!isOpen(), isOpen() ? 'button' : 'first'));
    btn.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        setOpen(true, e.key === 'ArrowUp' ? 'last' : 'first');
      }
    });
    menu.addEventListener('keydown', (e) => {
      // The menu's keys stay in the menu: the page's own (song.js: ↑/↓ a
      // section, Space play) listen on the document.
      if (['ArrowDown', 'ArrowUp', 'Home', 'End', 'Escape', ' ', 'Spacebar'].includes(e.key)) e.stopPropagation();
      const list = items();
      const i = list.indexOf(document.activeElement);
      const step = { ArrowDown: 1, ArrowUp: -1 }[e.key];
      if (step) { e.preventDefault(); list[(i + step + list.length) % list.length]?.focus(); return; }
      if (e.key === 'Home') { e.preventDefault(); list[0]?.focus(); return; }
      if (e.key === 'End') { e.preventDefault(); list.at(-1)?.focus(); return; }
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setOpen(false, 'button'); return; }
      if (e.key === 'Tab') setOpen(false);
    });
    // An item chosen closes the menu (links go on to their page).
    menu.addEventListener('click', (e) => { if (e.target.closest('[role="menuitem"]')) setOpen(false); });
    document.addEventListener('pointerdown', (e) => {
      if (isOpen() && !menu.contains(e.target) && !btn.contains(e.target)) setOpen(false);
    });
  }

  // ---- "Exporter en PDF…": the layout and the notes, then the download -------
  const dialog = document.getElementById('song-pdf-dialog');
  if (dialog) {
    let opener = null;
    document.querySelectorAll('[data-pdf-dialog]').forEach((b) => b.addEventListener('click', () => {
      const sheet = b.closest('.stack-sheet');
      opener = b.closest('[role="menu"]') ? document.getElementById('song-more-btn') : sheet ? document.getElementById(sheet.id + '-btn') : b;
      // The layout on screen is the likely one.
      const view = document.getElementById('mode-segmented')?.dataset.value;
      const radio = dialog.querySelector(`.pdf-mode-radio[value="${view === 'normal' ? '' : view}"]`);
      if (radio && !radio.checked) { radio.checked = true; radio.dispatchEvent(new Event('change', { bubbles: true })); }
      if (typeof dialog.showModal === 'function') dialog.showModal(); else dialog.setAttribute('open', '');
    }));
    // Downloading is the end of it.
    dialog.querySelector('.pdf-export-link')?.addEventListener('click', () => setTimeout(() => dialog.close(), 0));
    dialog.addEventListener('click', (e) => { if (e.target === dialog) dialog.close(); });
    // Its keys stay in it (song.js plays on Space, steps sections on ↑/↓).
    dialog.addEventListener('keydown', (e) => e.stopPropagation());
    dialog.addEventListener('close', () => { if (opener && opener.offsetParent !== null) opener.focus(); });
  }

  // ---- "Accordeur": the tuner in place ---------------------------------------
  // The ⋯ menu's item and the Morceau sheet's row open it here, the
  // song's tunings first (tuner.js); the music pauses (the microphone
  // would hear it), and closing it stops the microphone. Without this
  // script the links go to /tuner.
  const tuner = document.getElementById('tuner-dialog');
  if (tuner && typeof tuner.showModal === 'function') {
    let opener = null;
    document.querySelectorAll('[data-tuner-open]').forEach((a) => a.addEventListener('click', (e) => {
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      const sheet = a.closest('.stack-sheet');
      opener = a.closest('[role="menu"]') ? document.getElementById('song-more-btn') : sheet ? document.getElementById(sheet.id + '-btn') : a;
      document.dispatchEvent(new CustomEvent('songbooks:pause-reference'));
      const clock = window.songbooksFollowClock;
      if (clock && clock.state().running) clock.toggle(false);
      tuner.showModal();
      document.getElementById('tuner-start')?.focus();
    }));
    tuner.addEventListener('click', (e) => { if (e.target === tuner) tuner.close(); });
    tuner.addEventListener('keydown', (e) => e.stopPropagation());
    tuner.addEventListener('close', () => {
      document.getElementById('tuner')?.dispatchEvent(new CustomEvent('tuner:stop'));
      if (opener && opener.offsetParent !== null) opener.focus();
    });
  }
})();

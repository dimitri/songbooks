// song-controls.js — the song page's own controls (play.html .song-toolbar,
// app/reports/menu-proposal.md §3.5): the ⋯ menu (Versions, Historique,
// Référence, Exporter en PDF…, the Guitar Pro files) and the PDF dialog.
// The view switcher (#mode-segmented) and the actions keep their ids and
// are wired where they always were (song.js, song-notes.js, stage.js).
(function () {
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
      opener = b.closest('[role="menu"]') ? document.getElementById('song-more-btn') : b;
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
})();

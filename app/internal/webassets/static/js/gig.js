// Gig view: read-only, so there's nothing to wire up but the theme toggle —
// same localStorage key/cycle as the song page, so a theme choice carries
// across both.
(function () {
  const themeBtn = document.getElementById('gig-theme-toggle');
  function applyTheme(t) { document.body.dataset.theme = document.documentElement.dataset.theme = t || ''; }
  try { applyTheme(localStorage.getItem('songbooks-theme') || ''); } catch (e) { /* ignore */ }
  themeBtn?.addEventListener('click', () => {
    const order = ['', 'light', 'dark'];
    const next = order[(order.indexOf(document.body.dataset.theme || '') + 1) % order.length];
    applyTheme(next);
    try { localStorage.setItem('songbooks-theme', next); } catch (e) { /* ignore */ }
  });
})();

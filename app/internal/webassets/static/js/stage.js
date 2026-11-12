// Stage mode: the song page for playing it — larger type, none of the
// page's chrome, the screen kept awake, and a swipe (or the pedal keys
// song.js already handles) for the next or previous song of the set list
// or songbook. It stays on from one song to the next for this tab
// (sessionStorage) until it's turned off.
(function () {
  const KEY = 'songbooks-stage';
  let lock = null;

  async function keepAwake(on) {
    try {
      if (on && !lock && 'wakeLock' in navigator && document.visibilityState === 'visible') {
        lock = await navigator.wakeLock.request('screen');
        lock.addEventListener('release', () => { lock = null; });
      } else if (!on && lock) {
        await lock.release();
        lock = null;
      }
    } catch (e) { /* not allowed here: the screen may dim */ }
  }

  function set(on) {
    document.body.classList.toggle('stage', on);
    try { if (on) sessionStorage.setItem(KEY, '1'); else sessionStorage.removeItem(KEY); } catch (e) { /* this page only */ }
    keepAwake(on);
    // The chart and the alphaTab rows lay out again at the new size.
    window.dispatchEvent(new Event('resize'));
  }
  const on = () => document.body.classList.contains('stage');

  const exit = document.createElement('button');
  exit.type = 'button';
  exit.className = 'stage-exit icon-btn';
  exit.title = 'Leave stage mode';
  exit.setAttribute('aria-label', 'Leave stage mode');
  exit.innerHTML = '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M5 5l10 10M15 5L5 15"/></svg>';
  exit.addEventListener('click', () => set(false));
  document.body.appendChild(exit);

  // The previous and next songs' names, at the bottom of the screen.
  const foot = document.createElement('nav');
  foot.className = 'stage-foot';
  foot.setAttribute('aria-label', 'previous and next song');
  for (const dir of ['prev', 'next']) {
    const card = document.querySelector('.setlist-nav-card.' + dir);
    const a = document.createElement(card ? 'a' : 'span');
    a.className = 'stage-foot-' + dir;
    if (card) {
      a.href = card.href;
      a.textContent = (dir === 'prev' ? '‹ ' : '') + (card.querySelector('.topbar-nav-info strong')?.textContent || '') + (dir === 'next' ? ' ›' : '');
    }
    foot.appendChild(a);
  }
  document.body.appendChild(foot);

  document.querySelectorAll('.stage-mode-toggle').forEach((b) => b.addEventListener('click', () => set(!on())));
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && on() && !e.defaultPrevented) set(false); });
  document.addEventListener('visibilitychange', () => { if (on()) keepAwake(true); });

  // A clear sideways swipe goes to the previous or next song: the same
  // links as the desktop's previous/next cards and the pedal keys.
  let x0 = null, y0 = 0;
  document.addEventListener('touchstart', (e) => {
    if (!on() || e.touches.length !== 1) { x0 = null; return; }
    x0 = e.touches[0].clientX; y0 = e.touches[0].clientY;
  }, { passive: true });
  document.addEventListener('touchend', (e) => {
    if (x0 === null) return;
    const dx = e.changedTouches[0].clientX - x0, dy = e.changedTouches[0].clientY - y0;
    x0 = null;
    if (Math.abs(dx) < 80 || Math.abs(dx) < 2 * Math.abs(dy)) return;
    const link = document.querySelector(dx < 0 ? '.setlist-nav-card.next' : '.setlist-nav-card.prev');
    if (link) location.href = link.href;
  }, { passive: true });

  try { if (sessionStorage.getItem(KEY)) set(true); } catch (e) { /* off */ }
})();

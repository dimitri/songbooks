// practice-time.js — on a song opened from a songbook, counts the time a
// recording actually plays and sends it to /api/practice/played, which
// logs it as practice when it's a coaching student's own songbook (or
// their group's), and ignores it otherwise (practice.go). Loaded before
// the players: it watches every audio and video element's play, those
// made with new Audio() included, by wrapping HTMLMediaElement's play.
(function () {
  const ctx = window.songbooksPracticeContext;
  if (!ctx || !navigator.sendBeacon) return;
  const playing = new Set();
  const play = HTMLMediaElement.prototype.play;
  const watched = new WeakSet();
  HTMLMediaElement.prototype.play = function () {
    if (!watched.has(this)) {
      watched.add(this);
      const el = this;
      el.addEventListener('playing', () => playing.add(el));
      ['pause', 'ended', 'emptied', 'error'].forEach((ev) => el.addEventListener(ev, () => playing.delete(el)));
    }
    return play.apply(this, arguments);
  };
  let seconds = 0;
  const flush = () => {
    if (seconds < 5) return;
    const body = new Blob([JSON.stringify({ songbookId: ctx.songbookId, variantId: ctx.variantId, seconds })], { type: 'application/json' });
    if (navigator.sendBeacon('/api/practice/played', body)) seconds = 0;
  };
  setInterval(() => {
    // Muted elements playing alongside (a part kept for the mixer) are
    // the same practice: one second a second, whatever plays.
    for (const el of playing) {
      if (!el.paused) { seconds++; break; }
    }
    if (seconds >= 60) flush();
  }, 1000);
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') flush(); });
  window.addEventListener('pagehide', flush);
})();

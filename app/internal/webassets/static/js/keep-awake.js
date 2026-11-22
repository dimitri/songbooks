// keep-awake.js — the screen stays on while the song plays.
//
// Stage mode used to keep the song page awake; it's gone, so this does it
// while something on the page plays: any <audio>/<video> (the player's
// dock, a recording, the reference), and the Score view's synth, which says
// so with a 'songbooks:playing' event ({detail: {playing: true|false}}).
// A wake lock is released by the browser when the page is hidden; it's
// asked again when the page comes back while still playing. Where the API
// is missing (older browsers), nothing happens.
(function () {
  if (!('wakeLock' in navigator)) return;
  let lock = null;
  let synth = false;

  const playing = () => synth || [...document.querySelectorAll('audio, video')].some((m) => !m.paused && !m.ended);

  async function update() {
    const want = playing() && document.visibilityState === 'visible';
    if (want && !lock) {
      try {
        lock = await navigator.wakeLock.request('screen');
        lock.addEventListener('release', () => { lock = null; });
      } catch (e) { lock = null; }
    } else if (!want && lock) {
      const l = lock; lock = null;
      try { await l.release(); } catch (e) {}
    }
  }

  for (const type of ['play', 'playing', 'pause', 'ended', 'emptied']) {
    document.addEventListener(type, update, true);
  }
  document.addEventListener('songbooks:playing', (e) => { synth = !!(e.detail && e.detail.playing); update(); });
  document.addEventListener('visibilitychange', update);
})();

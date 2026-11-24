// What a lesson's one player plays (lesson-view.js): the lesson's video
// ("watch": the dock drives YouTube) or the song's recording with its
// score ("play": the dock as on every song, Recording / MIDI). One at a
// time, always: choosing one pauses the other, and either starting by
// itself (YouTube's own controls, the dock's play, a key) chooses it.
//
//   LessonSwitch.create({ mode, pauseVideo, pauseDock, onMode })
//     .mode                  'watch' or 'play'
//     .set(mode)             choose; pauses what the other one played
//     .videoPlaying()        YouTube says it plays: watch, the dock paused
//     .dockPlaying()         the dock plays: play, the video paused
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.LessonSwitch = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  function create(opts) {
    const o = opts || {};
    const sw = {
      mode: o.mode === 'watch' ? 'watch' : 'play',
      set(mode) {
        const m = mode === 'watch' ? 'watch' : 'play';
        if (m === 'watch') o.pauseDock?.(); else o.pauseVideo?.();
        if (m === sw.mode) return false;
        sw.mode = m;
        o.onMode?.(m);
        return true;
      },
      videoPlaying() { return sw.set('watch'); },
      dockPlaying() { return sw.set('play'); },
    };
    return sw;
  }
  return { create };
});

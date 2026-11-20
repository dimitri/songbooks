// player-mix.js — the song player's mix as plain functions, for
// audio-player.js ("My parts", "The voice") and its tests
// (internal/webassets/jstest/player-mix.test.mjs, run by go test).
//
// A recording split in the background (cmd/stemsd) has its parts:
// "vocals" and "no_vocals" (the voice and the backing), and with the
// four-part model also "drums", "bass" and "other". The player plays:
//   - the recording as it is when every level is 100;
//   - with only the voice down: the backing (no_vocals) driving the
//     clock, the voice beside it at its level;
//   - with any other part down: "other" driving the clock, the voice,
//     the drums and the bass beside it, each at its level.
// A part at 0 isn't played at all.
(function (root) {
  const OTHERS = ['drums', 'bass', 'other'];
  const clamp = (n) => Math.max(0, Math.min(100, Math.round(Number(n) || 0)));

  // What plays: {main, mainLevel, followers}. main is the part that
  // drives the clock in place of the recording ('' for the recording
  // itself); followers the parts beside it, those with a level above 0.
  // stems: the parts the recording has; level(stem): its level as played
  // (100 for a part nobody turned down).
  function mode(stems, level) {
    const has = (s) => stems.includes(s);
    const four = ['vocals', 'drums', 'bass', 'other'].every(has);
    if (four && OTHERS.some((s) => level(s) < 100)) {
      return { main: 'other', mainLevel: level('other'), followers: ['vocals', 'drums', 'bass'].filter((s) => level(s) > 0) };
    }
    if (has('vocals') && has('no_vocals') && level('vocals') < 100) {
      return { main: 'no_vocals', mainLevel: 100, followers: level('vocals') > 0 ? ['vocals'] : [] };
    }
    return { main: '', mainLevel: 100, followers: [] };
  }

  // The voice's switch (the karaoke button, V): off remembers the level it
  // was at, on brings that level back (100 the first time). v is
  // {voice, back}: the level now, and the one to come back to.
  function toggleVoice(v) {
    const back = v.back > 0 ? v.back : 100;
    if (v.voice > 0) return { voice: 0, back: v.voice };
    return { voice: back, back };
  }
  // The voice's slider: the level, and once let go (committed) above 0,
  // the level the switch comes back to.
  function setVoice(v, n, committed) {
    const voice = clamp(n);
    return { voice, back: committed && voice > 0 ? voice : (v.back > 0 ? v.back : 100) };
  }
  // Saved by an earlier version: {voice, voiceOff}, where the switch went
  // between 100 and voiceOff (the last level under 100 — 95 after a drag
  // up, so "off" kept the voice in).
  function voiceFromSaved(saved) {
    const voice = saved && saved.voice !== undefined ? clamp(saved.voice) : 100;
    const back = saved && saved.voiceBack > 0 ? clamp(saved.voiceBack) : (voice > 0 ? voice : 100);
    return { voice, back };
  }

  // Whether a part playing beside the clock must be moved to it: more
  // than 80 ms apart (closer, it would keep jumping), and not while it is
  // still getting to the last place it was sent (a seek, or a start, takes
  // a moment: sent again every tick it would never play — WebKit).
  // f: {time, seeking, sentAt}; now in ms.
  function shouldSeek(f, mainTime, now) {
    if (f.seeking) return false;
    if (f.sentAt && now - f.sentAt < 400) return false;
    return Math.abs(f.time - mainTime) > 0.08;
  }

  const api = { mode, toggleVoice, setVoice, voiceFromSaved, shouldSeek };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.SongbooksMix = api;
})(typeof window !== 'undefined' ? window : globalThis);

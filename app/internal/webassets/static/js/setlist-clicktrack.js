// Setlist click-track mode: drives the standalone metronome widget
// (metronome-page.js, loaded just before this script) through a
// pre-loaded setlist, song by song, inserting an explicit "break" step
// between sets rather than silently skipping the gap. Deliberately
// doesn't touch metronome-page.js's internals — it drives the SAME
// already-wired DOM controls (bpm input, time-sig picker buttons) via
// real events, so the one engine/control surface stays the single source
// of truth for tempo/time-sig state.
(() => {
  const dataEl = document.getElementById('clicktrack-data');
  const songs = JSON.parse(dataEl.textContent || '[]');
  if (!songs.length) return;

  // Flatten songs into steps, inserting a "break" pseudo-step right
  // before the first song of every set after the first — an explicit
  // step the user advances through, not a silent jump.
  const steps = [];
  songs.forEach(song => {
    if (song.isFirstInSet && song.setIndex > 0) {
      steps.push({ kind: 'break', setLabel: song.setLabel, setIndex: song.setIndex });
    }
    steps.push({ kind: 'song', song });
  });

  const posEl = document.getElementById('clicktrack-position');
  const titleEl = document.getElementById('clicktrack-title');
  const metaEl = document.getElementById('clicktrack-meta');
  const prevBtn = document.getElementById('clicktrack-prev');
  const nextBtn = document.getElementById('clicktrack-next');
  const nowPlayingCard = document.getElementById('clicktrack-now-playing');
  const breakCard = document.getElementById('clicktrack-break');
  const breakLabel = document.getElementById('clicktrack-break-label');
  const breakContinue = document.getElementById('clicktrack-break-continue');
  const metronomeWrap = document.getElementById('clicktrack-metronome-wrap');
  const bpmInput = document.getElementById('bpm-input');
  const playBtn = document.getElementById('play-pause-btn');

  const totalSongs = songs.length;
  let i = 0;

  function stopIfPlaying() {
    // setBpm (inside metronome-page.js) re-syncs the clock live even
    // while running, but moving to a different song mid-click is the one
    // moment a running click should stop on its own — the gap between
    // songs is for the band to reset, not to keep clicking through.
    if (playBtn && playBtn.title === 'stop') playBtn.click();
  }

  function setTimeSig(sig) {
    const row = document.getElementById('time-sig-picker');
    const btn = row && row.querySelector(`button[data-value="${sig}"]`);
    if (btn) btn.click();
    else {
      // Not one of the picker's preset buttons (e.g. an unusual
      // signature) — fall back to the closest preset rather than leaving
      // the picker on a stale value.
      const fallback = row && row.querySelector('button[data-value="4/4"]');
      if (fallback) fallback.click();
    }
  }

  function render() {
    const step = steps[i];
    stopIfPlaying();

    if (step.kind === 'break') {
      nowPlayingCard.hidden = true;
      metronomeWrap.hidden = true;
      breakCard.hidden = false;
      breakLabel.textContent = `Before ${step.setLabel || 'the next set'} — take a break, then continue when ready.`;
      prevBtn.disabled = i === 0;
      nextBtn.disabled = false;
      return;
    }

    breakCard.hidden = true;
    nowPlayingCard.hidden = false;
    metronomeWrap.hidden = false;

    const song = step.song;
    posEl.textContent = `${song.setLabel || 'Set'} · song ${song.index + 1} of ${totalSongs}`;
    titleEl.textContent = song.title;
    metaEl.textContent = [song.artist, song.key ? `key ${song.key}` : '', `${song.tempo} bpm`, song.timeSig]
      .filter(Boolean).join(' · ');

    bpmInput.value = String(song.tempo);
    bpmInput.dispatchEvent(new Event('change'));
    setTimeSig(song.timeSig);

    prevBtn.disabled = i === 0;
    nextBtn.disabled = i === steps.length - 1;
  }

  prevBtn.addEventListener('click', () => { if (i > 0) { i--; render(); } });
  nextBtn.addEventListener('click', () => { if (i < steps.length - 1) { i++; render(); } });
  breakContinue.addEventListener('click', () => { if (i < steps.length - 1) { i++; render(); } });

  render();
})();

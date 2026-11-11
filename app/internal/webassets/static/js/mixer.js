// Mixer view: plays the SCORE (AlphaSynth/MIDI synth, via the vendored
// alphaTab player — see alphatab-tab.js's own enablePlayer gate) rather
// than the reference-audio recording the rest of this page's player
// (audio-player.js) plays. The two are deliberately independent
// transports, not one blended mix: a synthesized score and a real
// recording are unrelated clocks (see this file's own pauseOther below),
// and conflating them would promise a "mix your real recording with the
// synth bass together" feature this app doesn't actually have.
//
// The structure strip and the reference-audio player, both OUTSIDE
// #sheet, stay visible in every render mode including "mixer" — see
// play.html's own comment on #mixer-page. This file only has to build
// the score side: a channel strip (mute/solo per named track) and one
// combined multi-track alphaTab mount (tab.ToAlphaTexMulti,
// handlers_mixer.go) so every track plays on one shared clock.
(function () {
  const page = document.getElementById('mixer-page');
  if (!page) return;
  const variantId = page.dataset.variantId;
  const scoreContainer = document.getElementById('mixer-score');
  const channelStrip = document.getElementById('mixer-channel-strip');
  const emptyEl = document.getElementById('mixer-empty');
  const playBtn = document.getElementById('mixer-play-btn');
  const stopBtn = document.getElementById('mixer-stop-btn');
  const loopToggle = document.getElementById('mixer-loop-toggle');
  const tempoSelect = document.getElementById('mixer-tempo-select');

  let currentSectionIndex = null;
  let tracks = []; // [{name, drums}], same order as the alphaTab Score's own tracks

  function sheetMode() {
    return document.getElementById('sheet')?.dataset.renderMode;
  }

  function api() {
    return scoreContainer._alphaTabApi || null;
  }

  // pauseOther: only one of the two transports on this page (reference
  // audio, score) should ever be making sound at a time — they're
  // unrelated clocks, so "both playing" just reads as noise, not a mix.
  // audio-player.js owns the <audio id="audio-player-body"> element;
  // this reaches it directly rather than adding a cross-file API for one
  // call each way.
  function pauseReferenceAudio() {
    const audioEl = document.querySelector('#audio-player audio');
    audioEl?.pause();
  }

  async function loadSection(index) {
    currentSectionIndex = index;
    const a = api();
    a?.pause();
    channelStrip.innerHTML = '';
    tracks = [];
    emptyEl.hidden = true;
    scoreContainer.hidden = false;
    try {
      const res = await fetch(`/api/songs/${variantId}/sections/${index}/mixer`);
      if (!res.ok) {
        scoreContainer.hidden = true;
        emptyEl.hidden = false;
        return;
      }
      const data = await res.json();
      tracks = data.tracks || [];
      buildChannelStrip();
      scoreContainer.dataset.tex = data.alphaTex;
      scoreContainer.innerHTML = '';
      window.AlphaTabTab && window.AlphaTabTab.render(scoreContainer);
    } catch (e) {
      scoreContainer.hidden = true;
      emptyEl.hidden = false;
    }
  }

  function buildChannelStrip() {
    tracks.forEach((t, i) => {
      const chip = document.createElement('div');
      chip.className = 'mixer-channel';
      const name = document.createElement('span');
      name.className = 'mixer-channel-name';
      name.textContent = t.name;
      const muteBtn = document.createElement('button');
      muteBtn.type = 'button';
      muteBtn.textContent = 'mute';
      const soloBtn = document.createElement('button');
      soloBtn.type = 'button';
      soloBtn.textContent = 'solo';
      muteBtn.addEventListener('click', () => {
        const a = api();
        if (!a || !a.score) return;
        const track = a.score.tracks[i];
        if (!track) return;
        const nowMuted = !muteBtn.classList.contains('active');
        a.changeTrackMute([track], nowMuted);
        muteBtn.classList.toggle('active', nowMuted);
      });
      soloBtn.addEventListener('click', () => {
        const a = api();
        if (!a || !a.score) return;
        const track = a.score.tracks[i];
        if (!track) return;
        const nowSolo = !soloBtn.classList.contains('active');
        a.changeTrackSolo([track], nowSolo);
        soloBtn.classList.toggle('active', nowSolo);
      });
      chip.append(name, muteBtn, soloBtn);
      channelStrip.appendChild(chip);
    });
  }

  playBtn?.addEventListener('click', () => {
    const a = api();
    if (!a) return;
    pauseReferenceAudio();
    a.playPause();
  });
  stopBtn?.addEventListener('click', () => { api()?.stop(); });
  loopToggle?.addEventListener('change', () => {
    const a = api();
    if (a) a.isLooping = loopToggle.checked;
  });
  tempoSelect?.addEventListener('change', () => {
    const a = api();
    if (a) a.playbackSpeed = parseFloat(tempoSelect.value) || 1;
  });

  // audio-player.js's own play button is the one thing on this page that
  // can start the reference recording — mirror the same mutual-exclusion
  // rule the other direction: starting it pauses the score transport.
  document.getElementById('audio-play-btn')?.addEventListener('click', () => { api()?.pause(); });

  // Reusing the structure strip's own section buttons (always visible,
  // every mode) rather than building a second section picker — only
  // acts while the Mixer tab is actually the active mode, so the strip's
  // existing jump-to-bar/metronome click (song.js) is untouched in every
  // other mode. Capture phase: song.js's own listener is bound with
  // addEventListener too (no stopPropagation anywhere in it), so both
  // run — this one just ALSO loads the section's score.
  document.addEventListener('click', (e) => {
    if (sheetMode() !== 'mixer') return;
    const btn = e.target.closest('.structure-seg');
    if (!btn) return;
    const index = parseInt(btn.dataset.sectionIndex, 10);
    if (!Number.isNaN(index)) loadSection(index);
  }, true);

  // Entering Mixer mode for the first time this page load: load whatever
  // section is currently marked "current" (metronome position), or the
  // first one with a score, so the view isn't empty on first open.
  let loadedOnce = false;
  const observer = new MutationObserver(() => {
    if (sheetMode() !== 'mixer' || loadedOnce) return;
    loadedOnce = true;
    const current = document.querySelector('.structure-seg.current') || document.querySelector('.structure-seg');
    if (current) loadSection(parseInt(current.dataset.sectionIndex, 10));
  });
  const sheetEl = document.getElementById('sheet');
  if (sheetEl) observer.observe(sheetEl, { attributes: true, attributeFilter: ['data-render-mode'] });
})();

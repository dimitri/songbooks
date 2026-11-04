// Reference-audio player: waveform (hand-drawn via Web Audio decodeAudioData
// + <canvas>, no CDN library — see app/docs/mp3-reference-audio.md §3),
// transport controls, and section markers synced against the song's own
// structure strip (#song-structure-strip's .structure-seg buttons, the
// SAME elements/CSS classes the metronome's own playback highlight uses
// in song.js — reused here, not reimplemented, just driven by
// audio.currentTime instead of the BPM clock).
(function () {
  const root = document.getElementById('audio-player');
  if (!root) return; // not on a page with the player (e.g. not logged in, or template guard failed)
  const book = root.dataset.book;
  const stem = root.dataset.stem;

  const emptyState = document.getElementById('audio-empty-state');
  const playerBody = document.getElementById('audio-player-body');
  const uploadFileInput = document.getElementById('audio-upload-file');
  const uploadFileBtn = document.getElementById('audio-upload-file-btn');
  const youtubeInput = document.getElementById('audio-youtube-url');
  const youtubeBtn = document.getElementById('audio-youtube-btn');
  const youtubeStatus = document.getElementById('audio-youtube-status');
  const replaceBtn = document.getElementById('audio-replace-btn');
  const deleteBtn = document.getElementById('audio-delete-btn');
  const canvas = document.getElementById('audio-waveform');
  const canvasCtx = canvas ? canvas.getContext('2d') : null;
  const playhead = document.getElementById('audio-playhead');
  const playBtn = document.getElementById('audio-play-btn');
  const stopBtn = document.getElementById('audio-stop-btn');
  const restartBtn = document.getElementById('audio-restart-btn');
  const back10Btn = document.getElementById('audio-back10-btn');
  const fwd10Btn = document.getElementById('audio-fwd10-btn');
  const timeLabel = document.getElementById('audio-time-label');
  const setMarkerBtn = document.getElementById('audio-set-marker-btn');

  const audio = new Audio();
  audio.preload = 'metadata';
  let peaks = null; // Float32Array of per-bucket peak amplitudes, 0..1
  let duration = 0;
  let markers = []; // [{sectionIndex, timeSeconds}], sorted by time
  let canEdit = root.dataset.canEdit === 'true';

  function fmtTime(s) {
    if (!isFinite(s) || s < 0) s = 0;
    const m = Math.floor(s / 60), sec = Math.floor(s % 60);
    return `${m}:${String(sec).padStart(2, '0')}`;
  }

  // ---- Waveform: decode once, draw peaks into a fixed-width bucket array
  // (independent of canvas pixel width, which can change on resize) so a
  // resize just re-renders from cached peaks, no re-decode. ----------------
  async function decodeWaveform() {
    const res = await fetch(`/b/${book}/${stem}/audio.mp3`);
    if (!res.ok) return;
    const arrayBuffer = await res.arrayBuffer();
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    let audioBuffer;
    try {
      audioBuffer = await ctx.decodeAudioData(arrayBuffer);
    } catch (e) {
      return; // unsupported/corrupt file — the player still works via <audio>, just no waveform
    } finally {
      ctx.close();
    }
    const channel = audioBuffer.getChannelData(0);
    const buckets = 800; // fixed resolution, downsampled further at draw time if canvas is narrower
    const samplesPerBucket = Math.floor(channel.length / buckets) || 1;
    const out = new Float32Array(buckets);
    for (let i = 0; i < buckets; i++) {
      let max = 0;
      const start = i * samplesPerBucket;
      const end = Math.min(start + samplesPerBucket, channel.length);
      for (let j = start; j < end; j++) {
        const v = Math.abs(channel[j]);
        if (v > max) max = v;
      }
      out[i] = max;
    }
    peaks = out;
    drawWaveform();
  }

  function drawWaveform() {
    if (!canvasCtx || !peaks) return;
    const dpr = window.devicePixelRatio || 1;
    const w = canvas.clientWidth, h = canvas.clientHeight;
    canvas.width = w * dpr; canvas.height = h * dpr;
    canvasCtx.setTransform(dpr, 0, 0, dpr, 0, 0);
    canvasCtx.clearRect(0, 0, w, h);
    const mid = h / 2;
    const barW = w / peaks.length;
    const style = getComputedStyle(root);
    canvasCtx.fillStyle = style.getPropertyValue('--waveform-color') || '#888';
    for (let i = 0; i < peaks.length; i++) {
      const amp = Math.max(peaks[i], 0.02) * mid;
      canvasCtx.fillRect(i * barW, mid - amp, Math.max(barW - 0.5, 0.5), amp * 2);
    }
  }

  // ---- Markers / structure-strip sync -------------------------------------
  const segEls = [...document.querySelectorAll('.structure-seg')];
  function markerForSection(idx) { return markers.find(m => m.sectionIndex === idx); }
  function currentMarkerIndex(t) {
    // The last marker whose time is <= t — "which section are we in."
    let found = -1;
    for (const m of markers) { if (m.timeSeconds <= t) found = m.sectionIndex; else break; }
    return found;
  }
  function highlightFromTime(t) {
    const idx = currentMarkerIndex(t);
    segEls.forEach(el => {
      const i = parseInt(el.dataset.sectionIndex, 10);
      el.classList.toggle('current', i === idx);
      el.classList.toggle('done', idx >= 0 && i < idx);
    });
  }
  // Clicking a structure-seg seeks to its marker, if it has one — doesn't
  // fight the existing click behavior (metronome section-jump) when no
  // audio/marker exists for that section, since nothing is added unless
  // markerForSection finds a hit.
  segEls.forEach(el => {
    el.addEventListener('click', () => {
      const idx = parseInt(el.dataset.sectionIndex, 10);
      const m = markerForSection(idx);
      if (m && duration) { audio.currentTime = m.timeSeconds; }
    });
  });

  function renderPlayheadAndMarkers() {
    if (!duration) return;
    const pct = Math.max(0, Math.min(1, audio.currentTime / duration));
    if (playhead) playhead.style.left = `${pct * 100}%`;
    if (timeLabel) timeLabel.textContent = `${fmtTime(audio.currentTime)} / ${fmtTime(duration)}`;
    highlightFromTime(audio.currentTime);
  }

  // ---- Transport -----------------------------------------------------------
  playBtn?.addEventListener('click', () => {
    if (audio.paused) { audio.play(); } else { audio.pause(); }
  });
  audio.addEventListener('play', () => { if (playBtn) playBtn.textContent = '⏸'; });
  audio.addEventListener('pause', () => { if (playBtn) playBtn.textContent = '▶'; });
  stopBtn?.addEventListener('click', () => { audio.pause(); audio.currentTime = 0; });
  restartBtn?.addEventListener('click', () => { audio.currentTime = 0; if (audio.paused) audio.play(); });
  back10Btn?.addEventListener('click', () => { audio.currentTime = Math.max(0, audio.currentTime - 10); });
  fwd10Btn?.addEventListener('click', () => { audio.currentTime = Math.min(duration || Infinity, audio.currentTime + 10); });
  audio.addEventListener('timeupdate', renderPlayheadAndMarkers);
  audio.addEventListener('loadedmetadata', () => { duration = audio.duration; renderPlayheadAndMarkers(); });

  // Click-to-seek on the waveform itself.
  canvas?.addEventListener('click', e => {
    if (!duration) return;
    const rect = canvas.getBoundingClientRect();
    const pct = (e.clientX - rect.left) / rect.width;
    audio.currentTime = pct * duration;
  });

  // "Set marker here" — attached to whichever structure-seg is currently
  // selected via a simple prompt-free flow: play/scrub to the spot, pick
  // the section from the strip (click still seeks if a marker exists
  // already; with none yet it does nothing but stays visually clickable),
  // then press "set marker" which writes it against the LAST section the
  // strip highlighted as current by playback position proximity — simplest
  // correct behavior without adding a second selection UI.
  let pendingSectionIndex = null;
  segEls.forEach(el => {
    el.addEventListener('click', () => { pendingSectionIndex = parseInt(el.dataset.sectionIndex, 10); });
  });
  setMarkerBtn?.addEventListener('click', async () => {
    if (pendingSectionIndex === null) {
      alert('Click a section in the structure strip first, then "set marker" to attach the current playback position to it.');
      return;
    }
    const t = audio.currentTime;
    const res = await fetch(`/api/songs/${book}/${stem}/audio/markers/${pendingSectionIndex}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ timeSeconds: t }),
    });
    if (!res.ok) { alert('Could not save the marker.'); return; }
    const existing = markerForSection(pendingSectionIndex);
    if (existing) existing.timeSeconds = t;
    else markers.push({ sectionIndex: pendingSectionIndex, timeSeconds: t });
    markers.sort((a, b) => a.timeSeconds - b.timeSeconds);
    renderPlayheadAndMarkers();
  });

  // ---- Load / upload ---------------------------------------------------
  async function loadMeta() {
    const res = await fetch(`/api/songs/${book}/${stem}/audio`);
    if (!res.ok) return;
    const data = await res.json();
    if (!data.hasAudio) {
      if (emptyState) emptyState.hidden = false;
      if (playerBody) playerBody.hidden = true;
      return;
    }
    markers = (data.markers || []).slice().sort((a, b) => a.timeSeconds - b.timeSeconds);
    if (emptyState) emptyState.hidden = true;
    if (playerBody) playerBody.hidden = false;
    audio.src = `/b/${book}/${stem}/audio.mp3`;
    decodeWaveform();
  }

  async function uploadFile(file) {
    const fd = new FormData();
    fd.append('file', file);
    const res = await fetch(`/api/songs/${book}/${stem}/audio`, { method: 'PUT', body: fd });
    if (!res.ok) { alert('Upload failed: ' + (await res.text())); return; }
    loadMeta();
  }
  uploadFileBtn?.addEventListener('click', () => uploadFileInput?.click());
  uploadFileInput?.addEventListener('change', () => {
    const f = uploadFileInput.files && uploadFileInput.files[0];
    if (f) uploadFile(f);
  });

  youtubeBtn?.addEventListener('click', async () => {
    const url = youtubeInput?.value.trim();
    if (!url) return;
    youtubeBtn.disabled = true;
    if (youtubeStatus) youtubeStatus.textContent = 'Downloading… this can take a minute.';
    try {
      const res = await fetch(`/api/songs/${book}/${stem}/audio/youtube`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      if (!res.ok) {
        if (youtubeStatus) youtubeStatus.textContent = 'Download failed: ' + (await res.text());
        return;
      }
      if (youtubeStatus) youtubeStatus.textContent = '';
      loadMeta();
    } finally {
      youtubeBtn.disabled = false;
    }
  });

  replaceBtn?.addEventListener('click', () => uploadFileInput?.click());
  deleteBtn?.addEventListener('click', async () => {
    if (!confirm('Remove this song’s reference audio?')) return;
    const res = await fetch(`/api/songs/${book}/${stem}/audio`, { method: 'DELETE' });
    if (res.ok) { audio.pause(); audio.removeAttribute('src'); markers = []; loadMeta(); }
  });

  window.addEventListener('resize', drawWaveform);
  loadMeta();
})();

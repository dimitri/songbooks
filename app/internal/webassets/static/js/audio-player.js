// Reference-audio player: waveform (hand-drawn via Web Audio decodeAudioData
// + <canvas>, no CDN library — see app/docs/mp3-reference-audio.md §3),
// transport controls, and section markers synced against the song's own
// structure strip (#song-structure-strip's .structure-seg buttons, the
// SAME elements/CSS classes the metronome's own playback highlight uses
// in song.js — reused here, not reimplemented, just driven by
// audio.currentTime instead of the BPM clock).
//
// Two reference CATEGORIES per song ("a personal choice of reference
// version (yt video, band recording: two categories only)" — store.
// AudioCategory's own doc comment): "youtube" and "band". Upload/replace/
// remove controls for both live in the Reference drawer (wired at the
// bottom of this file); THIS player is playback-only, with a toggle
// between categories when a song has both. Which category is selected is
// a per-browser preference (localStorage, keyed by book/stem), not song
// data — two listeners can prefer different reference versions of the
// same song without fighting over a shared setting.
(function () {
  const root = document.getElementById('audio-player');
  if (!root) return; // not on a page with the player
  const variantId = root.dataset.variantId;
  const canEdit = root.dataset.canEdit === 'true';
  const prefKey = `songbooks:audio-category:${variantId}`;

  const categoryToggle = document.getElementById('audio-category-toggle');
  const emptyState = document.getElementById('audio-empty-state');
  const playerBody = document.getElementById('audio-player-body');
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
  let categories = {}; // {youtube: {markers: [...], ...} | undefined, band: {...} | undefined}
  let activeCategory = null;

  function fmtTime(s) {
    if (!isFinite(s) || s < 0) s = 0;
    const m = Math.floor(s / 60), sec = Math.floor(s % 60);
    return `${m}:${String(sec).padStart(2, '0')}`;
  }

  // ---- Waveform: decode once per category switch, draw peaks into a
  // fixed-width bucket array (independent of canvas pixel width, which
  // can change on resize) so a resize just re-renders from cached peaks,
  // no re-decode. ------------------------------------------------------
  async function decodeWaveform() {
    peaks = null;
    const res = await fetch(`/song/${variantId}/audio.mp3?category=${activeCategory}`);
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
  function currentMarkers() { return (categories[activeCategory] && categories[activeCategory].markers) || []; }
  function markerForSection(idx) { return currentMarkers().find(m => m.sectionIndex === idx); }
  function currentMarkerIndex(t) {
    let found = -1;
    for (const m of currentMarkers()) { if (m.timeSeconds <= t) found = m.sectionIndex; else break; }
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
  let pendingSectionIndex = null;
  segEls.forEach(el => {
    el.addEventListener('click', () => {
      pendingSectionIndex = parseInt(el.dataset.sectionIndex, 10);
      const m = markerForSection(pendingSectionIndex);
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

  canvas?.addEventListener('click', e => {
    if (!duration) return;
    const rect = canvas.getBoundingClientRect();
    const pct = (e.clientX - rect.left) / rect.width;
    audio.currentTime = pct * duration;
  });

  setMarkerBtn?.addEventListener('click', async () => {
    if (pendingSectionIndex === null) {
      alert('Click a section in the structure strip first, then "set marker" to attach the current playback position to it.');
      return;
    }
    const t = audio.currentTime;
    const res = await fetch(`/api/songs/${variantId}/audio/${activeCategory}/markers/${pendingSectionIndex}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ timeSeconds: t }),
    });
    if (!res.ok) { alert('Could not save the marker.'); return; }
    const list = categories[activeCategory].markers || (categories[activeCategory].markers = []);
    const existing = list.find(m => m.sectionIndex === pendingSectionIndex);
    if (existing) existing.timeSeconds = t;
    else list.push({ sectionIndex: pendingSectionIndex, timeSeconds: t });
    list.sort((a, b) => a.timeSeconds - b.timeSeconds);
    renderPlayheadAndMarkers();
  });

  // ---- Category toggle / load -------------------------------------------
  function categoryLabel(c) { return c === 'youtube' ? 'YouTube' : 'Band recording'; }

  function selectCategory(cat) {
    activeCategory = cat;
    try { localStorage.setItem(prefKey, cat); } catch (e) {}
    categoryToggle?.querySelectorAll('button').forEach(b => b.classList.toggle('active', b.dataset.category === cat));
    audio.pause();
    audio.src = `/song/${variantId}/audio.mp3?category=${cat}`;
    duration = 0;
    decodeWaveform();
  }

  async function loadMeta() {
    const res = await fetch(`/api/songs/${variantId}/audio`);
    if (!res.ok) return;
    const data = await res.json();
    categories = {};
    (data.categories || []).forEach(c => { categories[c.category] = c; });
    const available = Object.keys(categories);

    if (available.length === 0) {
      if (emptyState) emptyState.hidden = false;
      if (playerBody) playerBody.hidden = true;
      if (categoryToggle) categoryToggle.hidden = true;
      return;
    }
    if (emptyState) emptyState.hidden = true;
    if (playerBody) playerBody.hidden = false;

    if (categoryToggle) {
      categoryToggle.hidden = available.length < 2;
      categoryToggle.querySelectorAll('button').forEach(b => { b.hidden = !categories[b.dataset.category]; });
    }

    let preferred = null;
    try { preferred = localStorage.getItem(prefKey); } catch (e) {}
    const target = (preferred && categories[preferred]) ? preferred : available[0];
    selectCategory(target);
  }

  categoryToggle?.addEventListener('click', e => {
    const btn = e.target.closest('button[data-category]');
    if (btn && categories[btn.dataset.category]) selectCategory(btn.dataset.category);
  });

  window.addEventListener('resize', drawWaveform);

  // ---- Reference-drawer upload controls (YouTube download + band file
  // upload) — moved here from the main player per direct request. Both
  // just call loadMeta() again afterward so the player above picks up
  // the new category without a full page reload. --------------------------
  if (!canEdit) { loadMeta(); return; }

  function uploadFile(category, file, onDone) {
    const fd = new FormData();
    fd.append('file', file);
    fetch(`/api/songs/${variantId}/audio/${category}`, { method: 'PUT', body: fd })
      .then(async res => {
        if (!res.ok) { alert('Upload failed: ' + (await res.text())); return; }
        loadMeta();
      })
      .finally(() => onDone && onDone());
  }

  // Band recording: plain file upload.
  const bandFileInput = document.getElementById('audio-band-file');
  const bandUploadBtn = document.getElementById('audio-band-upload-btn');
  const bandReplaceBtn = document.getElementById('audio-band-replace-btn');
  const bandDeleteBtn = document.getElementById('audio-band-delete-btn');
  bandUploadBtn?.addEventListener('click', () => bandFileInput?.click());
  bandReplaceBtn?.addEventListener('click', () => bandFileInput?.click());
  bandFileInput?.addEventListener('change', () => {
    const f = bandFileInput.files && bandFileInput.files[0];
    if (f) uploadFile('band', f, () => { bandFileInput.value = ''; });
  });
  bandDeleteBtn?.addEventListener('click', async () => {
    if (!confirm('Remove the band recording?')) return;
    const res = await fetch(`/api/songs/${variantId}/audio/band`, { method: 'DELETE' });
    if (res.ok) loadMeta();
  });

  // YouTube: downloads audio from the video URL already saved as this
  // song's reference (see #reference-block's data-url, kept current by
  // song.js's own initReference) — no second URL field to fill in.
  const ytDownloadBtn = document.getElementById('audio-youtube-download-btn');
  const ytReplaceBtn = document.getElementById('audio-youtube-replace-btn');
  const ytDeleteBtn = document.getElementById('audio-youtube-delete-btn');
  const ytStatus = document.getElementById('audio-youtube-status');
  const referenceBlock = document.getElementById('reference-block');

  async function downloadFromReference() {
    const url = referenceBlock && referenceBlock.dataset.url;
    if (!url) { alert('Add the YouTube video link above first, then download its audio.'); return; }
    ytDownloadBtn && (ytDownloadBtn.disabled = true);
    ytReplaceBtn && (ytReplaceBtn.disabled = true);
    // .btn-loading/.spinner (app.css) — this download can take a real
    // minute (yt-dlp fetch + transcode), and the only in-progress cue
    // before this was the status line's own text, easy to miss below the
    // fold on a long page. A spinner on whichever button was actually
    // clicked makes "it's working" visible right where the person is
    // looking.
    const activeBtn = (ytReplaceBtn && !ytReplaceBtn.hidden) ? ytReplaceBtn : ytDownloadBtn;
    const spinner = document.createElement('span');
    spinner.className = 'spinner';
    activeBtn && activeBtn.classList.add('btn-loading');
    activeBtn && activeBtn.appendChild(spinner);
    if (ytStatus) ytStatus.textContent = 'Downloading… this can take a minute.';
    try {
      const res = await fetch(`/api/songs/${variantId}/audio/youtube`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      if (!res.ok) {
        if (ytStatus) ytStatus.textContent = 'Download failed: ' + (await res.text());
        return;
      }
      if (ytStatus) ytStatus.textContent = '';
      loadMeta();
    } finally {
      ytDownloadBtn && (ytDownloadBtn.disabled = false);
      ytReplaceBtn && (ytReplaceBtn.disabled = false);
      activeBtn && activeBtn.classList.remove('btn-loading');
      spinner.remove();
    }
  }
  ytDownloadBtn?.addEventListener('click', downloadFromReference);
  ytReplaceBtn?.addEventListener('click', downloadFromReference);
  ytDeleteBtn?.addEventListener('click', async () => {
    if (!confirm('Remove the downloaded YouTube audio?')) return;
    const res = await fetch(`/api/songs/${variantId}/audio/youtube`, { method: 'DELETE' });
    if (res.ok) loadMeta();
  });

  // Reflect which categories already exist in the drawer's own
  // upload/replace/remove button visibility (upload hidden once present,
  // replace/remove shown instead) — re-run whenever loadMeta() refreshes
  // categories, not just once at load.
  const origLoadMeta = loadMeta;
  loadMeta = async function () {
    await origLoadMeta();
    const hasYouTube = !!categories.youtube;
    const hasBand = !!categories.band;
    if (ytDownloadBtn) ytDownloadBtn.hidden = hasYouTube;
    if (ytReplaceBtn) ytReplaceBtn.hidden = !hasYouTube;
    if (ytDeleteBtn) ytDeleteBtn.hidden = !hasYouTube;
    if (bandUploadBtn) bandUploadBtn.hidden = hasBand;
    if (bandReplaceBtn) bandReplaceBtn.hidden = !hasBand;
    if (bandDeleteBtn) bandDeleteBtn.hidden = !hasBand;
  };
  loadMeta();
})();

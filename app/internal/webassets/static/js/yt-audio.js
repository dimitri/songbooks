// yt-audio.js — the video's audio, under the YouTube video, its full
// width (the "yt-audio" partial, yt_audio.html; the server's
// yt_audio_jobs.go). The song's MP3 is fetched by itself in the
// background (a reference set or changed, the song opened without it);
// here, where it is:
//
//   getting it   "Getting the audio…", a progress bar, the % and the
//                time left; why it matters, and that the page can be left
//   ready        "✓ Audio ready · 3:42"; Again · Remove for an editor
//   failed       why, in one line; ↻ Try again and ⬆ Upload an MP3 (the
//                page's recording upload) for an editor, the reason alone
//                for anyone else
//   removed      (an editor's) none, and the same two ways to get one
//
// GET /api/songs/{id}/audio/youtube/status is asked again while it's
// queued or running: the server's wait while it moves, half as long again
// while it doesn't (30 s at most), paused while the page is hidden. Once
// ready, the player picks the audio up (songbooks:audio-changed), no
// reload. A reference saved on the page (song.js, songbooks:reference-
// saved) asks at once: its audio was queued with it.
//
// The plain logic is tested by internal/webassets/jstest/yt-audio.test.mjs.
(function (root) {
  const ACTIVE = new Set(['queued', 'running']);
  const MAX_DELAY = 30;
  const tr = (m, ...a) => (typeof root.t === 'function' ? root.t(m, ...a) : a.reduce((s, v) => s.replace(/%[ds]/, String(v)), m));

  function active(st) { return !!st && ACTIVE.has(st.state); }

  // moved: the state or its progress changed since the last answer.
  function moved(prev, cur) {
    if (!prev || !cur) return true;
    if (prev.state !== cur.state || prev.reason !== cur.reason) return true;
    return Math.round((prev.progress || 0) * 100) !== Math.round((cur.progress || 0) * 100);
  }

  // The next wait, in seconds.
  function nextDelay(prev, cur, last) {
    const base = Math.max(1, (cur && cur.poll) || 5);
    if (moved(prev, cur) || !last) return base;
    return Math.min(MAX_DELAY, Math.round(last * 1.5));
  }

  // A length: "3:42", "1:02:03".
  function clock(s) {
    s = Math.max(0, Math.round(s));
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = s % 60;
    const two = (n) => String(n).padStart(2, '0');
    return h ? `${h}:${two(m)}:${two(x)}` : `${m}:${two(x)}`;
  }

  // The time left, in words.
  function etaText(eta) {
    if (typeof eta !== 'number' || eta < 0) return '';
    if (eta < 10) return tr('a few seconds left');
    if (eta < 60) return tr('about %d s left', Math.round(eta / 5) * 5);
    return tr('about %d min left', Math.round(eta / 60));
  }

  // Why it failed, in one line.
  function reasonText(reason) {
    switch (reason) {
      case 'age_restricted': return tr('YouTube shows this video to signed-in adults only, so its audio can’t be taken from here.');
      case 'unavailable': return tr('This video is private, removed or unavailable, so its audio can’t be taken.');
      case 'blocked': return tr('YouTube refused the video to this server (blocked, or too many requests).');
      case 'too_long': return tr('This video is longer than 30 minutes: too long to keep its audio here.');
      default: return tr('Couldn’t get the audio from this video.');
    }
  }

  // What to show for a status: the part (running, ready, failed,
  // removed, or '' for nothing), and its words.
  function view(st, canEdit) {
    const out = { part: '', pct: '', eta: '', bar: null, dur: '', reason: '' };
    if (!st) return out;
    switch (st.state) {
      case 'running': {
        const p = Math.max(0, Math.min(1, st.progress || 0));
        out.part = 'running';
        out.bar = p;
        out.pct = `${Math.round(p * 100)}%`;
        out.eta = etaText(st.eta);
        break;
      }
      case 'queued':
        out.part = 'running';
        out.bar = 0;
        if (st.reason && typeof st.retryIn === 'number') {
          out.eta = st.retryIn < 40 ? tr('trying again in a moment') : tr('trying again in %d min', Math.max(1, Math.round(st.retryIn / 60)));
        } else {
          out.eta = tr('waiting its turn');
        }
        break;
      case 'ready':
        out.part = 'ready';
        if (typeof st.duration === 'number' && st.duration > 0) out.dur = clock(st.duration);
        break;
      case 'failed':
        out.part = 'failed';
        out.reason = reasonText(st.reason);
        break;
      case 'removed':
      case 'missing':
        out.part = canEdit ? 'removed' : '';
        break;
    }
    return out;
  }

  const api = { active, moved, nextDelay, clock, etaText, reasonText, view, MAX_DELAY };
  if (typeof module !== 'undefined' && module.exports) { module.exports = api; return; }
  root.YTAudio = api;

  // ---- The page ------------------------------------------------------------
  const boxes = Array.from(document.querySelectorAll('[data-yt-audio]'));
  if (!boxes.length) return;
  const variantId = boxes[0].dataset.variantId;
  const canEdit = boxes[0].dataset.canEdit === '1';
  let status = null;
  try { status = boxes[0].dataset.status ? JSON.parse(boxes[0].dataset.status) : null; } catch (e) { status = null; }

  function paint(st) {
    const v = view(st, canEdit);
    for (const box of boxes) {
      box.hidden = !v.part;
      box.dataset.state = st ? st.state : '';
      box.querySelectorAll('[data-part]').forEach((p) => { p.hidden = p.dataset.part !== v.part; });
      const bar = box.querySelector('.yt-audio-bar');
      if (bar && v.bar !== null) {
        bar.querySelector('span').style.width = `${(v.bar * 100).toFixed(1)}%`;
        bar.setAttribute('aria-valuenow', String(Math.round(v.bar * 100)));
        bar.classList.toggle('is-waiting', st.state === 'queued');
      }
      const set = (sel, text) => { const el = box.querySelector(sel); if (el) el.textContent = text; };
      set('.yt-audio-pct', v.pct);
      set('.yt-audio-eta', v.pct && v.eta ? ` · ${v.eta}` : v.eta);
      set('.yt-audio-dur', v.dur ? ` · ${v.dur}` : '');
      const reason = box.querySelector('[data-part="failed"] .yt-audio-reason');
      if (reason) reason.textContent = v.reason;
    }
  }
  function say(msg, kind) {
    for (const box of boxes) {
      const el = box.querySelector('.yt-audio-status');
      if (!el) continue;
      if (window.Forms) Forms.status(el, msg, kind); else el.textContent = msg;
    }
  }

  // ---- Asking again --------------------------------------------------------
  let timer = null, delay = 0, busy = false;
  async function fetchStatus() {
    const res = await fetch(`/api/songs/${variantId}/audio/youtube/status`, { headers: { Accept: 'application/json' } });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    return res.json();
  }
  function schedule(prev, cur) {
    clearTimeout(timer); timer = null;
    if (!active(cur)) { delay = 0; return; }
    delay = nextDelay(prev, cur, delay);
    timer = setTimeout(tick, delay * 1000);
  }
  async function tick() {
    timer = null;
    if (document.hidden || busy) return; // asked again when shown
    busy = true;
    let cur = null;
    try { cur = await fetchStatus(); } catch (e) { cur = null; }
    busy = false;
    if (!cur) {
      delay = Math.min(MAX_DELAY, Math.max(delay, 5) * 2);
      timer = setTimeout(tick, delay * 1000);
      return;
    }
    update(cur);
  }
  function update(cur) {
    const prev = status;
    status = cur;
    paint(cur);
    // Ready now: the player (audio-player.js), the Mixer, the lyrics'
    // lines take the new audio.
    if (cur.state === 'ready' && prev && active(prev)) document.dispatchEvent(new CustomEvent('songbooks:audio-changed'));
    schedule(prev, cur);
  }
  function askNow() { clearTimeout(timer); timer = null; delay = 0; tick(); }
  document.addEventListener('visibilitychange', () => { if (!document.hidden && active(status) && !timer) askNow(); });
  document.addEventListener('songbooks:reference-saved', () => askNow());

  // ---- An editor's ways --------------------------------------------------
  async function queue(btn) {
    if (window.Forms) Forms.busy(btn, true);
    say('');
    try {
      const res = await fetch(`/api/songs/${variantId}/audio/youtube`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
      if (!res.ok) { say(tr('Couldn’t ask for the audio. Try again.'), 'error'); return; }
      update(await res.json());
    } catch (e) {
      say(tr('Couldn’t reach the server. Check your connection and try again.'), 'error');
    } finally {
      if (window.Forms) Forms.busy(btn, false);
    }
  }
  for (const box of boxes) {
    box.querySelectorAll('[data-yt-retry], [data-yt-again]').forEach((b) => b.addEventListener('click', () => queue(b)));
    box.querySelector('[data-yt-remove]')?.addEventListener('click', async (e) => {
      const btn = e.currentTarget;
      if (!confirm(tr('Remove the audio taken from this video? The video stays, but the click, sync, parts and melody need the audio. This can’t be undone.'))) return;
      if (window.Forms) Forms.busy(btn, true);
      try {
        const res = await fetch(`/api/songs/${variantId}/audio/youtube`, { method: 'DELETE' });
        if (!res.ok) { say(tr('Couldn’t remove the audio. Try again.'), 'error'); return; }
        update({ state: 'removed', canEdit: true });
        document.dispatchEvent(new CustomEvent('songbooks:audio-changed'));
      } catch (err) {
        say(tr('Couldn’t reach the server. Check your connection and try again.'), 'error');
      } finally {
        if (window.Forms) Forms.busy(btn, false);
      }
    });
    // The page's recording upload (audio-player.js, #audio-band-file).
    box.querySelectorAll('[data-yt-upload]').forEach((b) => b.addEventListener('click', () => {
      const input = document.getElementById('audio-band-file');
      if (input) { input.click(); return; }
      document.querySelector('#mode-segmented button[data-value="reference"]')?.click();
    }));
  }

  paint(status);
  schedule(null, status);
})(typeof window !== 'undefined' ? window : globalThis);

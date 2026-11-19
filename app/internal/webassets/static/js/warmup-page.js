// warmup-page.js — an exercise's page (warmups.html): the coach's
// recording in its written key, to listen to; the exercise written out in
// every key of the student's path, played on the piano to train with;
// their singing checked against its notes (pitch-check.js); a session's
// next exercise. The coach picks whose keys, sets a path, records.
(function () {
  const W = window.songbooksWarmup;
  const page = document.querySelector('.warmup-practice');
  const dataEl = document.getElementById('warmup-data');
  if (!page || !W || !dataEl) return;
  const data = JSON.parse(dataEl.textContent);
  const ex = data.exercise;
  const cid = page.dataset.community;
  const rhythm = ex.kind === 'rhythm';
  const p = ex.pattern;

  // ---- The keys -----------------------------------------------------------
  const OCT_KEY = `songbooks:warmup-octave:${ex.id}`;
  let octave = 0;
  try { octave = Number(localStorage.getItem(OCT_KEY)) || 0; } catch (e) { /* none */ }
  // a student's path; the coach with no student picked hears a usual one
  const waypoints = data.waypoints || (data.studentId ? W.defaultPath(p, data.low, data.high) : [0, 4, 0]);
  const keys = rhythm ? Array(Math.max(1, Number(p.repeats) || 4)).fill(0) : W.expandPath(waypoints, p.step);

  let built = null;
  // alphaTab scrolls the bar being played into view: below the player card
  const card = page.querySelector('.warmup-player');
  const player = W.player(page.querySelector('.warmup-score-canvas'), { scrollOffsetY: () => -(card.offsetHeight + 16) });
  const pathText = page.querySelector('.warmup-path-text');
  const progress = page.querySelector('.warmup-progress');
  function load() {
    built = W.build(p, keys, { octave, title: ex.name, voiceName: t('Voice'), pianoName: t('Piano') });
    player.load(built.xml);
    const r = built.repeats;
    const turn = r.reduce((best, x, i) => (x.key > r[best].key ? i : best), 0);
    const mins = Math.floor(built.duration / 60), secs = Math.round(built.duration % 60);
    pathText.textContent = rhythm
      ? tn(r.length, '%d repeat', '%d repeats') + ' · ' + t('%d min %d s', mins, secs)
      : t('Keys: %s', [r[0], r[turn], r[r.length - 1]].filter((x, i, a) => i === 0 || x !== a[i - 1]).map((x) => W.label(x.root)).join(' → '))
        + ' · ' + tn(r.length, '%d repeat', '%d repeats') + ' · ' + t('%d min %d s', mins, secs);
    progress.innerHTML = r.map(() => '<i></i>').join('');
    if (checker) checker.refresh();
  }
  page.querySelectorAll('.warmup-octave button').forEach((b) => {
    if (rhythm) { b.hidden = true; return; }
    b.addEventListener('click', () => {
      octave = Math.max(-24, Math.min(24, octave + Number(b.dataset.octave)));
      try { localStorage.setItem(OCT_KEY, String(octave)); } catch (e) { /* this page only */ }
      load();
    });
  });

  // ---- Playing ------------------------------------------------------------
  const play = page.querySelector('.warmup-play'), stop = page.querySelector('.warmup-stop');
  const api = player.api;
  api.playerReady.on(() => { play.disabled = false; stop.disabled = false; });
  api.scoreLoaded.on(() => { applyGuide(); });
  play.addEventListener('click', () => { W.audioSession('playback'); api.countInVolume = 1; if (rhythm) api.metronomeVolume = 1; api.playPause(); });
  stop.addEventListener('click', () => api.stop());
  page.querySelector('.warmup-speed').addEventListener('change', (e) => { api.playbackSpeed = Number(e.target.value) || 1; });
  const guide = page.querySelector('.warmup-guide input');
  function applyGuide() {
    if (!api.score || !api.score.tracks.length) return;
    api.changeTrackVolume([api.score.tracks[0]], guide.checked ? 0.8 : 0);
  }
  guide.addEventListener('change', applyGuide);
  const playIcon = play.innerHTML;
  const pauseIcon = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.5" height="12" rx="1"/><rect x="11.5" y="4" width="3.5" height="12" rx="1"/></svg>';
  api.playerStateChanged.on((e) => {
    const on = e.state === 1;
    play.innerHTML = on ? pauseIcon : playIcon;
    play.setAttribute('aria-label', on ? t('Pause') : t('Train'));
    play.classList.toggle('is-playing', on);
  });
  // Where it is: the key sung now, the repeat, the time; the repeats done.
  const keyEl = page.querySelector('.warmup-key'), repEl = page.querySelector('.warmup-rep'), timeEl = page.querySelector('.warmup-time');
  const clock2 = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
  setInterval(() => {
    if (!built) return;
    const c = player.clock();
    let i = built.repeats.findIndex((r) => c.time < r.end);
    if (i < 0) i = built.repeats.length - 1;
    const r = built.repeats[i];
    keyEl.textContent = W.label(r.root);
    repEl.textContent = t('repeat %d of %d', i + 1, built.repeats.length);
    timeEl.textContent = `${clock2(c.time)} / ${clock2(built.duration)}`;
    [...progress.children].forEach((d, k) => { d.className = k < i ? 'is-done' : k === i && (c.playing || c.time > 0) ? 'is-now' : ''; });
  }, 200);

  // ---- The coach's recording: listened to, never trained with -------------
  const listen = page.querySelector('.warmup-listen');
  const demoURL = `/communities/${cid}/warmups/${ex.id}/demo`;
  let demo = null;
  if (ex.hasDemo) {
    listen.hidden = false;
    listen.querySelector('span').textContent = t('Listen to %s', data.coach || t('the coach'));
    listen.addEventListener('click', () => {
      if (!demo) demo = new Audio(`${demoURL}?v=${encodeURIComponent(ex.demoAt || '')}`);
      if (demo.paused) { api.stop(); demo.currentTime = 0; demo.play(); } else demo.pause();
    });
  }
  if (data.isCoach) coachTools();

  function coachTools() {
    page.querySelector('.warmup-student')?.addEventListener('change', (e) => {
      const u = new URL(location.href);
      if (Number(e.target.value)) u.searchParams.set('student', e.target.value); else u.searchParams.delete('student');
      location.href = u.toString();
    });
    const exApi = `/api/communities/${cid}/exercises/${ex.id}`;
    // The path of the student shown.
    const pf = page.querySelector('.warmup-path-form');
    if (pf && data.studentId && !rhythm) {
      pf.hidden = false;
      pf.waypoints.value = waypoints.map((w) => (w > 0 ? '+' + w : String(w))).join(', ');
      const save = (w) => fetch(`${exApi}/path`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ personId: data.studentId, waypoints: w }) })
        .then((r) => { if (!r.ok) throw new Error(); location.reload(); }).catch(() => alert(t('Couldn’t save.')));
      pf.addEventListener('submit', (e) => {
        e.preventDefault();
        const w = pf.waypoints.value.split(/[,\s]+/).filter(Boolean).map(Number).filter(Number.isFinite);
        if (!w.length) return;
        save(w);
      });
      pf.querySelector('.warmup-path-default').addEventListener('click', () => save([]));
    }
    // Recording the demo: the written key once, the piano under, the
    // microphone as it is.
    const rec = page.querySelector('.warmup-record'), recStatus = page.querySelector('.warmup-record-status');
    const remove = page.querySelector('.warmup-demo-remove');
    if (ex.hasDemo) remove.hidden = false;
    remove.addEventListener('click', () => {
      if (!confirm(t('Remove your recording of this exercise?'))) return;
      fetch(`${exApi}/demo`, { method: 'DELETE' }).then(() => location.reload());
    });
    let recorder = null;
    rec.addEventListener('click', async () => {
      if (recorder) { recorder.stop(); return; }
      let stream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
      } catch (e) { recStatus.textContent = t('The microphone isn’t allowed: allow it for this site, then try again.'); return; }
      // the written key, once: the score reloaded for it
      const once = W.build(p, rhythm ? [0, 0] : [0], { title: ex.name, voiceName: t('Voice'), pianoName: t('Piano') });
      player.load(once.xml);
      await new Promise((res) => { const h = () => { api.scoreLoaded.off(h); setTimeout(res, 300); }; api.scoreLoaded.on(h); });
      const chunks = [];
      recorder = new MediaRecorder(stream);
      recorder.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data); };
      recorder.onstop = async () => {
        stream.getTracks().forEach((tr) => tr.stop());
        api.stop();
        rec.textContent = t('Record my demo');
        const blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' });
        recorder = null;
        recStatus.textContent = t('Saving…');
        const r = await fetch(`${exApi}/demo`, { method: 'PUT', headers: { 'Content-Type': (blob.type || 'audio/webm').split(';')[0] }, body: blob });
        recStatus.textContent = r.ok ? t('Saved') : t('Couldn’t save.');
        if (r.ok) setTimeout(() => location.reload(), 500);
      };
      const finished = () => { api.playerFinished.off(finished); if (recorder && recorder.state === 'recording') setTimeout(() => recorder && recorder.stop(), 800); };
      api.playerFinished.on(finished);
      recorder.start();
      rec.textContent = t('Stop and keep');
      recStatus.textContent = t('Recording: sing over the piano, it stops at the end.');
      api.countInVolume = 1;
      api.play();
    });
  }

  // ---- Checking: against the exercise's own notes -------------------------
  let checker = null;
  const pc = window.songbooksPitchCheck;
  if (pc) {
    const student = !data.isCoach && data.studentId;
    checker = pc.create({
      mount: (panel) => document.getElementById('warmup-check').append(panel),
      clock: () => player.clock(),
      visible: () => !!built,
      notesKey: () => `${octave}:${keys.join(',')}`,
      loadNotes: async () => built.notes.map((n, i, all) => ({ start: n.start, end: n.end, midi: n.midi, afterRest: rhythm || i === 0 || all[i - 1].end < n.start - 0.05 || all[i - 1].repeat !== n.repeat })),
      loading: () => t('Preparing the exercise…'),
      loadingAt: () => t('Preparing the exercise…'),
      loadFailed: () => t('Couldn’t prepare the exercise.'),
      ready: () => t('Sing along: headphones on. Press Train.'),
      key: () => 0,
      seek: (tm) => { api.timePosition = Math.max(0, tm) * 1000; },
      info: { settings: data.settings, name: data.name, coach: data.coach, takes: [], level: ex.level || '' },
      loadInfo: student ? async () => { const r = await fetch(`/api/practice/exercise-progress?exerciseId=${ex.id}`); return r.status === 200 ? r.json() : null; } : null,
      postTake: student ? async (body) => {
        const r = await fetch('/api/practice/exercise-take', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(Object.assign({ exerciseId: ex.id }, body)) });
        const got = r.status === 200 ? await r.json() : null;
        if (got && session) session.took(body, got);
        return got;
      } : null,
      anyPitch: rhythm,
    });
  }

  // ---- A session: the exercises one after another, the stars added up -----
  const session = data.session ? sessionBar(data.session) : null;
  function sessionBar(s) {
    const bar = page.querySelector('.warmup-session-bar');
    const at = s.exerciseIds.indexOf(ex.id);
    const KEY = `songbooks:warmup-session:${s.id}`;
    let run = {};
    try { run = JSON.parse(sessionStorage.getItem(KEY) || '{}'); } catch (e) { /* new run */ }
    if (at === 0 && !run.started) run = { started: Date.now(), stars: {} };
    const save = () => { try { sessionStorage.setItem(KEY, JSON.stringify(run)); } catch (e) { /* this page only */ } };
    save();
    const total = () => Object.values(run.stars || {}).reduce((a, b) => a + b, 0);
    const render = () => {
      const next = s.exerciseIds[at + 1];
      bar.innerHTML = `<strong>${escapeHTML(s.name)}</strong> <span class="hint">${t('exercise %d of %d', at + 1, s.exerciseIds.length)}</span>
        <span class="warmup-session-stars">★ ${total()} / ${3 * s.exerciseIds.length}</span>
        ${next ? `<a class="warmup-session-next" href="/communities/${cid}/warmups/${next}?session=${s.id}">${t('Next: %s', escapeHTML(s.names[next] || ''))} →</a>` : `<span class="warmup-session-done">${t('Last one!')}</span>`}`;
      bar.hidden = false;
    };
    render();
    return {
      took: (body) => {
        const score = rhythm ? body.rhythm : body.pitch;
        const stars = score >= 70 ? 3 : score >= 52 ? 2 : score >= 28 ? 1 : 0;
        run.stars = run.stars || {};
        run.stars[ex.id] = Math.max(run.stars[ex.id] || 0, stars);
        save();
        render();
      },
    };
  }
  function escapeHTML(x) { return String(x).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c])); }

  load();
  // for the page's tests and the curious: what's played, and where it is
  window.songbooksWarmupPage = { built: () => built, clock: () => player.clock() };
})();

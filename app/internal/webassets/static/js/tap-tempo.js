// tap-tempo.js — tap the beat, read the tempo: the song page's "Tap tempo"
// (the ⋯ menu and the phone's Morceau sheet, play.html #tap-tempo-dialog)
// and the editor's Tap button by its Tempo field (song.js
// renderHeaderFields). The arithmetic is plain functions, tested by
// internal/webassets/jstest/tap-tempo.test.mjs:
//   - the tempo is the average of the last taps (up to 8, so 7 beats),
//     in whole bpm, from the second tap on;
//   - 2 seconds without a tap and the next one starts over;
//   - compared with the song's own tempo ("tempo:" in its header).
// The click track and the metronome keep their own tap (clock.js
// tapDetermine), which steers a clock that's playing.
(function (root) {
  const IDLE_MS = 2000;
  const KEEP = 8;
  const MIN_BPM = 20;
  const MAX_BPM = 400;

  // bpmOf: the average beat of a run of tap times (ms), or null under two.
  function bpmOf(taps) {
    if (!taps || taps.length < 2) return null;
    const span = taps[taps.length - 1] - taps[0];
    if (!(span > 0)) return null;
    return Math.round((60000 * (taps.length - 1)) / span);
  }

  // create: a tapper. tap(now) adds a tap and gives the tempo so far
  // (null on the first tap of a run); a tap more than idleMs after the
  // last one starts a new run.
  function create(opts) {
    const idleMs = (opts && opts.idleMs) || IDLE_MS;
    const keep = Math.max(2, (opts && opts.keep) || KEEP);
    let taps = [];
    return {
      tap(now) {
        if (taps.length && now - taps[taps.length - 1] > idleMs) taps = [];
        taps.push(now);
        if (taps.length > keep) taps = taps.slice(taps.length - keep);
        return bpmOf(taps);
      },
      reset() { taps = []; },
      count() { return taps.length; },
      // The next tap will start over.
      idle(now) { return !taps.length || now - taps[taps.length - 1] > idleMs; },
    };
  }

  // A tempo worth saving to a song.
  const usable = (bpm) => Number.isFinite(bpm) && bpm >= MIN_BPM && bpm <= MAX_BPM;

  // songTempo: the song header's "tempo:" as a number, or null ("", or
  // not a number).
  function songTempo(value) {
    const n = parseInt(String(value == null ? '' : value).trim(), 10);
    return Number.isFinite(n) && n > 0 ? n : null;
  }

  // compare: the tapped tempo against the song's: {song, diff} with diff
  // > 0 faster, < 0 slower, 0 the same; null without both.
  function compare(bpm, song) {
    const s = songTempo(song);
    if (bpm == null || s == null) return null;
    return { song: s, diff: Math.round(bpm) - s };
  }

  const api = { IDLE_MS, KEEP, MIN_BPM, MAX_BPM, bpmOf, create, usable, songTempo, compare };
  if (typeof module !== 'undefined' && module.exports) { module.exports = api; return; }
  root.SongbooksTapTempo = api;
  const tr = (s, ...a) => (typeof root.t === 'function' ? root.t(s, ...a) : s);
  const TU = () => root.SongbooksTempoUnits;

  // ---- Saving the song's tempo --------------------------------------------
  // The song's metre, as the page has it (#transport).
  function pageSig() {
    const d = (document.getElementById('transport') || {}).dataset || {};
    return { num: d.timeNum || '4', den: d.timeDen || '4' };
  }
  // The page follows a saved tempo: the header, the follow-along clock,
  // and whoever listens to 'songbooks:tempo-saved' (the player's bar grid
  // and click, the Sync sheet, the editor).
  function tempoSaved(value, version) {
    const sig = pageSig();
    const head = document.querySelector('.topbar-tempo');
    if (head) head.textContent = TU() ? TU().text(value, sig.num, sig.den, { short: true }) : value + ' bpm';
    document.getElementById('transport')?.setAttribute('data-bpm', String(value));
    // The song's version the Sync sheet's chart fixes are saved against.
    if (version) document.getElementById('editor')?.setAttribute('data-version', String(version));
    const dlg = document.getElementById('tap-tempo-dialog');
    if (dlg) dlg.dataset.songTempo = String(value);
    root.songbooksFollowClock?.setBpm(value);
    document.dispatchEvent(new CustomEvent('songbooks:tempo-saved', { detail: { tempo: value, version } }));
  }
  // saveSong: the song's "tempo:" set to value (in its beat: dotted
  // quarters in 6/8), as any edit of the song is (PUT …/meta: who may
  // edit it, The Big Songbook's review, a revision). {saved, version}
  // once saved, and the page follows; {pending, message} when the edit
  // waits for review (nothing changes meanwhile); {error} else.
  async function saveSong(variantId, value) {
    let res;
    try {
      res = await fetch(`/api/songs/${variantId}/meta`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: 'tempo', value: String(value) }),
      });
    } catch (e) {
      return { error: tr("Couldn't save the tempo. Try again.") };
    }
    const data = await res.json().catch(() => ({}));
    if (res.status === 409) return { error: tr('The song changed meanwhile: reload the page, then try again.') };
    if (!res.ok) return { error: (res.status === 403 || res.status === 422) && data.error ? data.error : tr("Couldn't save the tempo. Try again.") };
    if (data.pending) return { pending: true, message: data.message || tr('Sent for review: the song changes once it’s approved.'), url: data.url };
    tempoSaved(value, data.version);
    return { saved: true, version: data.version };
  }
  api.saveSong = saveSong;

  // ---- The song page's panel ------------------------------------------------
  const dialog = document.getElementById('tap-tempo-dialog');
  if (!dialog || typeof dialog.showModal !== 'function') return;
  const pad = document.getElementById('tap-tempo-pad');
  const out = document.getElementById('tap-tempo-bpm');
  const cmp = document.getElementById('tap-tempo-compare');
  const useBtn = document.getElementById('tap-tempo-use');
  const status = document.getElementById('tap-tempo-status');
  const tapper = create();
  let bpm = null; // as tapped, in the note picked
  let opener = null;
  // A compound metre: the note tapped is picked (a dotted quarter, a
  // quarter, an eighth); the song keeps its beat.
  const sig = pageSig();
  let unit = 'beat';
  const unitPick = TU() && TU().picker(sig.num, sig.den, (id) => { unit = id; tapper.reset(); bpm = null; render(); });
  if (unitPick) {
    unit = unitPick.value;
    out.parentElement?.querySelector('span')?.replaceWith(unitPick);
  }
  // The tapped tempo in the song's beat, whole.
  const inBeat = () => (bpm == null ? null : Math.round(TU() ? TU().toBeat(bpm, unit, sig.num, sig.den) : bpm));
  const said = (v) => (TU() ? TU().text(v, sig.num, sig.den) : tr('%d bpm', v));

  function render() {
    out.textContent = bpm == null ? '–' : String(bpm);
    const song = songTempo(dialog.dataset.songTempo);
    const c = compare(inBeat(), dialog.dataset.songTempo);
    const v = inBeat();
    if (song == null) cmp.textContent = v != null && unitPick ? said(v) : tr('The song has no tempo yet.');
    else if (unitPick) cmp.textContent = !c ? tr('The song: %s', said(song)) : c.diff === 0 ? tr('%s: the song’s tempo', said(v)) : tr('%s; the song: %s', said(v), said(song));
    else if (!c) cmp.textContent = tr('The song: %d bpm', song);
    else if (c.diff === 0) cmp.textContent = tr('The song: %d bpm, the same', song);
    else if (c.diff > 0) cmp.textContent = tr('The song: %d bpm, %d faster here', song, c.diff);
    else cmp.textContent = tr('The song: %d bpm, %d slower here', song, -c.diff);
    if (useBtn) useBtn.disabled = !usable(inBeat()) || (c != null && c.diff === 0);
  }

  let lastTapAt = -Infinity;
  function tap() {
    const now = performance.now();
    lastTapAt = now;
    const v = tapper.tap(now);
    if (v != null) bpm = v;
    else bpm = null; // the first tap of a run
    if (status) status.textContent = '';
    pad.classList.remove('is-hit');
    void pad.offsetWidth; // restart the flash
    pad.classList.add('is-hit');
    render();
  }
  // On the finger's touch, not its lift: the beat is when it lands.
  pad.addEventListener('pointerdown', (e) => { if (e.button === 0) { e.preventDefault(); tap(); } });
  // Enter on the pad (a pointer's click was already counted).
  pad.addEventListener('click', () => { if (performance.now() - lastTapAt > 400) tap(); });
  dialog.addEventListener('keydown', (e) => {
    // Its keys stay in it (song.js plays on Space, steps sections on ↑/↓).
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const k = e.key;
    const onOther = e.target instanceof Element && e.target !== pad && e.target.closest('button, a, input, select, textarea');
    if (k === 't' || k === 'T' || ((k === ' ' || k === 'Spacebar') && !onOther)) {
      e.preventDefault();
      if (!e.repeat) tap();
    }
  });
  // Space's keyup would click the focused pad once more.
  dialog.addEventListener('keyup', (e) => { if ((e.key === ' ' || e.key === 'Spacebar') && e.target === pad) e.preventDefault(); });

  useBtn?.addEventListener('click', async () => {
    const value = inBeat();
    if (!usable(value)) return;
    useBtn.disabled = true;
    status.className = 'hint form-status';
    status.textContent = tr('Saving…');
    const r = await saveSong(dialog.dataset.variantId, value);
    if (r.error) {
      status.className = 'hint form-status is-error';
      status.textContent = r.error;
      render();
      return;
    }
    status.className = 'hint form-status is-ok';
    status.textContent = r.pending ? r.message : unitPick ? tr('Saved: the song is at %s.', said(value)) : tr('Saved: the song is at %d bpm.', value);
    render();
  });
  document.querySelectorAll('[data-tap-tempo-open]').forEach((b) => b.addEventListener('click', () => {
    const sheet = b.closest('.stack-sheet');
    opener = b.closest('[role="menu"]') ? document.getElementById('song-more-btn') : sheet ? document.getElementById(sheet.id + '-btn') : b;
    tapper.reset();
    bpm = null;
    if (status) status.textContent = '';
    render();
    dialog.showModal();
    pad.focus();
  }));
  dialog.addEventListener('click', (e) => { if (e.target === dialog) dialog.close(); });
  dialog.addEventListener('close', () => { if (opener && opener.offsetParent !== null) opener.focus(); });
})(typeof window !== 'undefined' ? window : globalThis);

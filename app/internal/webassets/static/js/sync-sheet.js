// The Sync sheet (play.html #sync-sheet): placing the chart's sections on
// the recording by ear, on a phone as well as a desktop.
//
// Play, and tap the big button on the first beat of each section as it
// goes by: the tap sets that section's start and moves on to the next,
// never seeking (Soundslice's T, Capo's "record downbeats", Transcribe!'s
// markers). Each tap snaps to the nearest downbeat the player detects or
// to the voice coming in (the recording's vocals part) within 0.15 s;
// what's left over after snapping is learnt as a fixed tap delay. Then
// per section: ▶ from 2 s before, ±0.1 s, ±1 bar, ✓ (right as it is),
// "redo from here". The chart, the counter and the click follow the
// unsaved sync as it's edited (songbooksPlayer.setPreview); Save writes
// it in one request (PUT …/markers), each section the person placed or
// checked as "set", the others staying estimates. Undo goes back step by
// step; earlier saved syncs can be taken back from their history.
//
// "Suggest" asks both engines — the server's (where the singing starts,
// the import wizard's) and the player's (the downbeats) — and shows each
// section's suggestion, both when they disagree by more than a bar, to
// hear (▶), take (✓) or leave (✗); nothing is applied on its own. The
// wizard's proposal and the Mixer's Auto-align arrive the same way. A
// section is flagged when the voice doesn't agree with it: a sung
// section the voice comes into bars later, or not at all, singing over
// one without lyrics, a start past the end of the recording.
//
// It works on the player's recording (audio-player.js,
// window.songbooksPlayer); opened from the player's sync badge, or by
// the import wizard with its proposal (songbooksSyncSheet.open).
(function () {
  const sheet = document.getElementById('sync-sheet');
  const P = window.songbooksPlayer;
  if (!sheet || !P) return;
  const $ = (id) => document.getElementById(id);
  const rowsEl = $('sync-rows');
  const tapBtn = $('sync-tap');
  const tapWhat = $('sync-tap-what');
  const playBtn = $('sync-play');
  const timeEl = $('sync-time');
  const nowEl = $('sync-now');
  const warnEl = $('sync-warn');
  const chip = $('sync-state-chip');
  const saveBtn = $('sync-save');
  const undoBtn = $('sync-undo');
  const statusEl = $('sync-status');
  const snapBox = $('sync-snap');
  const clickBtn = $('sync-click');
  const historyBtn = $('sync-history-btn');
  const historyEl = $('sync-history');
  const iconPlay = playBtn.innerHTML;
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.5" height="12" rx="1"/><rect x="11.5" y="4" width="3.5" height="12" rx="1"/></svg>';

  const SNAP = 0.15; // seconds: a tap this close to a downbeat or the voice lands on it
  let rows = []; // [{index, label, startBar, barCount, t, origin}] in chart order
  let cursor = 0; // the row the next tap sets (and whose tools are open)
  let todoFrom = -1; // "redo from here": rows from it on are to be tapped again
  let history = []; // undo snapshots
  let dirty = false;
  let cat = null;
  let voice = []; // the voice's phrase starts
  let phrases = []; // the voice's phrases, [start, end]
  let voiceLoaded = false;
  let beats = []; // detected downbeats
  let lags = []; // tap − snapped target, seconds: the tap delay learnt
  let timer = null;
  // Automatic suggestions, per section index: [{t, src}] — the server's
  // proposal ("voice": placed where the singing starts), the player's
  // detection ("beat": the downbeats), or one handed in (the wizard's,
  // the Mixer's Auto-align). Each is accepted or rejected, never applied
  // on its own.
  let sugg = {};
  let rejected = new Set();
  let suggNotes = [];

  const say = (msg, kind) => { if (window.Forms) Forms.status(statusEl, msg, kind); else statusEl.textContent = msg; };
  function fmt(t) {
    if (t === null || !isFinite(t)) return '–';
    const s = Math.max(0, t), m = Math.floor(s / 60);
    return `${m}:${(s - m * 60).toFixed(2).padStart(5, "0")}`;
  }
  const isOpen = () => !sheet.hidden;

  // ---- The rows ------------------------------------------------------------
  function load(markers) {
    cat = P.category();
    const secs = P.sections();
    const count = {};
    secs.forEach((s) => { count[s.label] = (count[s.label] || 0) + 1; });
    const seen = {};
    rows = secs.map((s) => {
      const m = markers.find((x) => x.sectionIndex === s.index);
      // "Verse ×10" is no help: number a repeated name that has none.
      seen[s.label] = (seen[s.label] || 0) + 1;
      const label = count[s.label] > 1 && !/\d\s*$/.test(s.label) ? `${s.label} ${seen[s.label]}` : s.label;
      return { index: s.index, label, startBar: s.startBar, barCount: s.barCount, hasLyrics: !!s.hasLyrics, t: m ? m.timeSeconds : null, origin: m ? (m.origin || 'set') : null };
    });
    cursor = 0;
    todoFrom = -1;
  }

  // ---- Suggestions and checks -------------------------------------------------
  function addSuggestions(list, src) {
    const dur = P.duration();
    for (const m of list) {
      // Past the end of the recording: the chart has more than it plays.
      if (m.timeSeconds === null || !isFinite(m.timeSeconds) || (dur && m.timeSeconds >= dur - 0.05)) continue;
      (sugg[m.sectionIndex] = sugg[m.sectionIndex] || []).push({ t: m.timeSeconds, src });
    }
  }
  // What's still to decide for row i: the suggestions that differ from
  // its time by more than a snap, two engines that agree within a bar
  // shown once (the voice's first).
  function pending(i) {
    const r = rows[i];
    if (rejected.has(r.index)) return [];
    const order = { voice: 0, given: 1, beat: 2 };
    const list = (sugg[r.index] || []).slice().sort((a, b) => order[a.src] - order[b.src]);
    const out = [];
    const near = (a, b) => Math.abs(a - b) < barLen(i) * 0.9;
    for (const s of list) {
      // One that agrees with the row as it is: nothing to decide (and an
      // engine within a bar of it says the same).
      if (r.t !== null && Math.abs(s.t - r.t) <= SNAP) continue;
      if (out.some((o) => near(o.t, s.t))) continue;
      out.push(s);
    }
    return out.filter((s) => r.t === null || !list.some((o) => o !== s && Math.abs(o.t - r.t) <= SNAP && near(o.t, s.t)));
  }
  function accept(i, s) {
    change(() => {
      const r = rows[i];
      r.t = s.t;
      r.origin = 'set';
      delete sugg[r.index];
    });
  }
  function acceptAll() {
    change(() => {
      rows.forEach((r, i) => {
        const p = pending(i);
        if (p.length) { r.t = p[0].t; r.origin = 'set'; }
        delete sugg[r.index];
      });
      // Keep them in order: a start that lands before the previous one
      // can't be right.
      for (let i = 1; i < rows.length; i++) {
        const prev = rows.slice(0, i).reverse().find((x) => x.t !== null);
        if (rows[i].t !== null && prev && rows[i].t <= prev.t) rows[i].t = null;
      }
    });
  }
  function reject(i) {
    rejected.add(rows[i].index);
    render();
  }
  // What the recording says against row i's start: the voice coming in
  // much later than a sung section starts (the review's "verse placed 15
  // bars before the singing"), no voice at all in it, singing over a
  // section without lyrics, a start past the end of the recording.
  function flags(i) {
    const r = rows[i];
    if (r.t === null) return [];
    const out = [];
    const dur = P.duration();
    if (dur && r.t >= dur - 0.05) return [t('after the end of the recording')];
    if (!voiceLoaded || !phrases.length) return out;
    const next = rows.slice(i + 1).find((x) => x.t !== null);
    const len = barLen(i);
    const end = next ? next.t : Math.min(dur || Infinity, r.t + r.barCount * len);
    if (end - r.t < 2 * len) return out;
    const sung = phrases.reduce((s, p) => s + Math.max(0, Math.min(end, p[1]) - Math.max(r.t, p[0])), 0);
    if (r.hasLyrics) {
      const first = voice.find((o) => o >= r.t - 0.3);
      if (first === undefined || first >= end || sung < 0.5) out.push(t('no voice in this section'));
      else if ((first - r.t) / len >= 3) out.push(t('the voice comes in %d bars later', Math.round((first - r.t) / len)));
    } else if (sung / (end - r.t) > 0.6) {
      out.push(t('the voice sings here, but the chart has no lyrics'));
    }
    return out;
  }
  async function suggest() {
    const btn = $('sync-suggest-btn');
    window.Forms?.busy?.(btn, true);
    say(t('Listening to the recording… (a few seconds)'));
    const forCat = cat;
    // The player's: the downbeats, at the chart's tempo.
    const bars = P.detectBars();
    if (bars) addSuggestions(rows.map((r) => ({ sectionIndex: r.index, timeSeconds: bars[r.startBar] ?? null })).filter((m) => m.timeSeconds !== null && m.timeSeconds < P.duration()), 'beat');
    // The server's: where the singing starts (the import wizard's).
    try {
      const lang = encodeURIComponent(document.documentElement.lang || '');
      const res = await fetch(`/api/songs/${P.variantId}/audio/${forCat}/analyze?lang=${lang}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
      if (res.ok && forCat === cat) {
        const p = await res.json();
        addSuggestions((p.sections || []).map((s) => ({ sectionIndex: s.index, timeSeconds: s.time })), 'voice');
        suggNotes = p.notes || [];
      }
    } catch (e) { /* the player's alone */ }
    window.Forms?.busy?.(btn, false);
    rejected = new Set();
    const n = rows.filter((_, i) => pending(i).length).length;
    say(n ? tn(n, '%d section has a suggestion', '%d sections have a suggestion') : t('The suggestions agree with the sync as it is.'), 'ok');
    render();
  }
  const snapshot = () => JSON.stringify({ rows, cursor, todoFrom, sugg });
  function change(fn) {
    history.push(snapshot());
    if (history.length > 200) history.shift();
    fn();
    dirty = true;
    afterChange();
  }
  function undo() {
    const s = history.pop();
    if (!s) return;
    ({ rows, cursor, todoFrom, sugg } = JSON.parse(s));
    dirty = true;
    afterChange();
  }
  function markers() {
    return rows.filter((r) => r.t !== null).map((r) => ({ sectionIndex: r.index, timeSeconds: Math.round(r.t * 1000) / 1000, origin: r.origin || 'set' }));
  }
  function afterChange() {
    P.setPreview(markers());
    render();
  }

  // A bar's length around row i: from its own span when the next start
  // is known, else the chart's tempo.
  function barLen(i) {
    const r = rows[i], next = rows.slice(i + 1).find((x) => x.t !== null);
    if (r && r.t !== null && next && next.startBar > r.startBar) return (next.t - r.t) / (next.startBar - r.startBar);
    return P.bpm > 0 ? (60 / P.bpm) * P.beatsPerBar : 2;
  }
  function rowBpm(i) {
    const r = rows[i], next = rows.slice(i + 1).find((x) => x.t !== null);
    if (!r || r.t === null || !next || next.t <= r.t) return 0;
    return (60 * P.beatsPerBar * (next.startBar - r.startBar)) / (next.t - r.t);
  }
  const offTempo = (v) => P.bpm > 0 && v > 0 && Math.abs(v / P.bpm - 1) > 0.08;

  // ---- Tapping ---------------------------------------------------------------
  function snap(x) {
    if (!snapBox.checked) return { t: x, snapped: false };
    let best = null;
    for (const c of voice.concat(beats)) {
      const d = Math.abs(c - x);
      if (d <= SNAP && (!best || d < Math.abs(best - x))) best = c;
    }
    return best === null ? { t: x, snapped: false } : { t: best, snapped: true };
  }
  // The tap delay: half the snapped taps came this much late or more.
  function lag() {
    if (lags.length < 3) return 0;
    const s = lags.slice().sort((a, b) => a - b);
    return Math.max(0, Math.min(0.2, s[s.length >> 1]));
  }
  function tap() {
    if (cursor >= rows.length) return;
    const raw = P.audio.currentTime;
    const s = snap(raw);
    const snapped = s.snapped;
    let at = s.t;
    if (snapped) lags.push(raw - at);
    else at = raw - lag();
    const prev = rows.slice(0, cursor).reverse().find((r) => r.t !== null);
    if (prev && at <= prev.t + 0.2) {
      say(t('That’s before %s: go back a little, or redo from there.', prev.label), 'error');
      return;
    }
    change(() => {
      const r = rows[cursor];
      const was = r.t;
      r.t = at;
      r.origin = 'set';
      // Later estimates move along (the whole rest is usually late or
      // early by the same amount); later starts placed by a person stay,
      // unless the tap went past them.
      const d = was === null ? 0 : at - was;
      for (let j = cursor + 1; j < rows.length; j++) {
        const q = rows[j];
        if (q.t === null) continue;
        if (q.origin !== 'set' || (todoFrom >= 0 && j >= todoFrom)) q.t += d;
        const before = rows.slice(0, j).reverse().find((x) => x.t !== null);
        if (before && q.t <= before.t + 0.2) q.t = before.t + Math.max(0.2, (q.startBar - before.startBar) * barLen(j - 1));
      }
      cursor++;
      if (todoFrom >= 0 && cursor >= rows.length) todoFrom = -1;
    });
    say(snapped ? t('%s at %s (snapped)', rows[cursor - 1].label, fmt(at)) : t('%s at %s', rows[cursor - 1].label, fmt(at)), 'ok');
    flash();
  }
  function flash() {
    tapBtn.classList.remove('flash');
    void tapBtn.offsetWidth;
    tapBtn.classList.add('flash');
  }
  function nudge(i, d) {
    const r = rows[i];
    if (!r || r.t === null) return;
    const prev = rows.slice(0, i).reverse().find((x) => x.t !== null);
    const next = rows.slice(i + 1).find((x) => x.t !== null);
    let to = r.t + d;
    if (prev) to = Math.max(prev.t + 0.2, to);
    if (next) to = Math.min(next.t - 0.2, to);
    to = Math.max(-1, Math.min(P.duration(), to));
    if (Math.abs(to - r.t) < 1e-4) return;
    change(() => { r.t = to; r.origin = 'set'; });
  }
  function confirmRow(i) {
    const r = rows[i];
    if (!r || r.t === null || r.origin === 'set') return;
    change(() => { r.origin = 'set'; if (i === cursor) cursor++; });
  }
  function redoFrom(i) {
    change(() => { cursor = i; todoFrom = i; });
    const from = rows[i].t !== null ? rows[i].t : (rows.slice(0, i).reverse().find((x) => x.t !== null) || {}).t;
    P.seek(Math.max(0, (from ?? 0) - 4));
    if (!P.isPlaying()) P.play();
  }
  function audition(i) {
    const r = rows[i];
    if (!r || r.t === null) return;
    P.seek(Math.max(0, r.t - 2));
    if (!P.isPlaying()) P.play();
  }
  function select(i) {
    cursor = Math.max(0, Math.min(rows.length - 1, i));
    render();
    rowsEl.children[cursor]?.scrollIntoView({ block: 'nearest' });
  }

  // ---- Drawing -----------------------------------------------------------------
  function render() {
    const tempos = rows.map((_, i) => rowBpm(i));
    rowsEl.replaceChildren(...rows.map((r, i) => {
      const li = document.createElement('li');
      li.className = `sync-row ${r.t === null ? 'is-unset' : r.origin === 'set' ? 'is-set' : 'is-auto'}${i === cursor ? ' is-cursor' : ''}${todoFrom >= 0 && i >= todoFrom && i >= cursor ? ' is-todo' : ''}`;
      const main = document.createElement('button');
      main.type = 'button';
      main.className = 'sync-row-main';
      main.setAttribute('aria-current', i === cursor ? 'true' : 'false');
      const bpmOff = offTempo(tempos[i]);
      main.innerHTML = '<span class="sync-row-label"></span><span class="sync-row-time"></span><span class="sync-row-origin"></span><span class="sync-row-bpm"></span>';
      main.querySelector('.sync-row-label').textContent = r.label;
      main.querySelector('.sync-row-time').textContent = fmt(r.t);
      main.querySelector('.sync-row-origin').textContent = r.t === null ? t('sync|not placed') : r.origin === 'set' ? t('sync|set') : t('sync|estimated');
      const b = main.querySelector('.sync-row-bpm');
      if (tempos[i]) {
        b.textContent = t('%d bpm', Math.round(tempos[i]));
        b.classList.toggle('warn', bpmOff);
        if (bpmOff) b.title = t('the chart’s %d bars here make the counter run at %d bpm; the chart says %d', r.barCount, Math.round(tempos[i]), Math.round(P.bpm));
      }
      main.addEventListener('click', () => select(i));
      li.appendChild(main);
      const fl = flags(i);
      if (fl.length) {
        li.classList.add('is-flagged');
        const p = document.createElement('p');
        p.className = 'sync-row-flag';
        p.textContent = fl.join(' · ');
        li.appendChild(p);
      }
      for (const s of pending(i)) li.appendChild(suggestion(i, s));
      if (i === cursor) li.appendChild(tools(i));
      return li;
    }));
    const waiting = rows.filter((_, i) => pending(i).length).length;
    $('sync-accept-all').hidden = !waiting;
    $('sync-suggest-count').textContent = waiting ? tn(waiting, '%d suggestion to check', '%d suggestions to check') : '';
    const notes = $('sync-suggest-notes');
    notes.hidden = !(waiting && suggNotes.length);
    notes.textContent = suggNotes.join(' ');
    const r = rows[cursor];
    tapWhat.textContent = r ? t('Start of %s', r.label) : t('Every section is placed');
    tapBtn.disabled = !r;
    undoBtn.disabled = !history.length;
    saveBtn.disabled = !dirty;
    const set = rows.filter((x) => x.t !== null && x.origin === 'set').length, placed = rows.filter((x) => x.t !== null).length;
    chip.textContent = placed && set === placed ? t('Sync set') : placed ? t('%d of %d checked', set, rows.length) : t('Not synced');
    chip.dataset.state = placed && set === placed ? 'set' : 'auto';
    const off = tempos.filter(offTempo);
    warnEl.hidden = !off.length;
    if (off.length) {
      // The player's figure (bar-weighted), the one its badge gives.
      const sorted = tempos.filter(Boolean).sort((a, b) => a - b);
      const mid = Math.round(P.syncTempos().median || sorted[sorted.length >> 1]);
      $('sync-warn-sum').textContent = t('The counter isn’t at the chart’s tempo: %d bpm, not %d', mid, Math.round(P.bpm));
      $('sync-warn-text').textContent = t('The counter runs at about %d bpm between these starts, the chart says %d: where a section is marked, the chart has more or fewer bars than the recording plays. Count its bars while listening and fix the chart.', mid, Math.round(P.bpm));
    }
  }
  // One suggestion for row i: its time and where it comes from, ▶ to
  // hear it, ✓ to take it, ✗ to leave the row as it is.
  function suggestion(i, s) {
    const r = rows[i];
    const div = document.createElement('div');
    div.className = 'sync-suggest';
    const what = document.createElement('span');
    const from = { voice: t('where the singing starts'), beat: t('from the downbeats'), given: t('proposed') }[s.src];
    const d = r.t === null ? '' : ` (${s.t > r.t ? '+' : '−'}${Math.abs(s.t - r.t).toFixed(1)} s)`;
    what.textContent = t('Suggested: %s, %s', fmt(s.t) + d, from);
    const btn = (cls, text, label, fn) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = `sync-btn ${cls}`;
      b.textContent = text;
      b.setAttribute('aria-label', label);
      b.addEventListener('click', fn);
      return b;
    };
    div.append(what,
      btn('', '▶', t('play the suggestion for %s from 2 seconds before', r.label), () => { P.seek(Math.max(0, s.t - 2)); if (!P.isPlaying()) P.play(); }),
      btn('ok', '✓', t('take the suggestion for %s', r.label), () => accept(i, s)),
      btn('no', '✗', t('keep %s as it is', r.label), () => reject(i)));
    return div;
  }
  function tools(i) {
    const r = rows[i];
    const div = document.createElement('div');
    div.className = 'sync-row-tools';
    const btn = (act, text, label, disabled) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'sync-btn';
      b.dataset.act = act;
      b.textContent = text;
      if (label) b.setAttribute('aria-label', label);
      b.disabled = !!disabled;
      return b;
    };
    const none = r.t === null;
    div.append(
      btn('audition', t('▶ 2 s before'), t('play %s from 2 seconds before', r.label), none),
      btn('bar-', t('−1 bar'), t('one bar earlier'), none),
      btn('ms-', t('−0.1 s'), t('0.1 second earlier'), none),
      btn('ms+', t('+0.1 s'), t('0.1 second later'), none),
      btn('bar+', t('+1 bar'), t('one bar later'), none),
      btn('ok', t('✓ Right'), t('%s starts right there', r.label), none || r.origin === 'set'),
      btn('redo', t('Redo from here'), t('tap again from %s on', r.label)),
    );
    div.addEventListener('click', (e) => {
      const b = e.target.closest('button[data-act]');
      if (!b) return;
      const act = b.dataset.act;
      if (act === 'audition') audition(i);
      else if (act === 'ms-') nudge(i, -0.1);
      else if (act === 'ms+') nudge(i, 0.1);
      else if (act === 'bar-') nudge(i, -barLen(Math.max(0, i - 1)));
      else if (act === 'bar+') nudge(i, barLen(i));
      else if (act === 'ok') confirmRow(i);
      else if (act === 'redo') redoFrom(i);
    });
    return div;
  }
  function tick() {
    const at = P.audio.currentTime;
    timeEl.textContent = fmt(at);
    let cur = null;
    rows.forEach((r) => { if (r.t !== null && r.t <= at + 0.02) cur = r; });
    nowEl.textContent = cur ? cur.label : '';
    const playing = P.isPlaying();
    if (playBtn.dataset.state !== String(playing)) { playBtn.innerHTML = playing ? iconPause : iconPlay; playBtn.dataset.state = String(playing); }
    clickBtn.setAttribute('aria-pressed', String(!!P.click()));
    clickBtn.classList.toggle('active', !!P.click());
    sheet.querySelectorAll('#sync-speed button').forEach((b) => b.classList.toggle('active', Math.abs(+b.dataset.v - P.speed()) < 0.01));
  }

  // ---- Snap targets ------------------------------------------------------------
  async function loadTargets() {
    voice = [];
    phrases = [];
    voiceLoaded = false;
    beats = [];
    const forCat = cat;
    try {
      const res = await fetch(`/api/songs/${P.variantId}/audio/${forCat}/voice`);
      if (res.ok && forCat === cat) {
        const c = await res.json();
        voice = c.onsets || [];
        phrases = c.phrases || [];
        voiceLoaded = true;
        if (isOpen()) render();
      }
    } catch (e) { /* snapping to the beat only */ }
    // The detection takes a moment on a long song: after the sheet shows.
    setTimeout(() => { if (forCat === cat) beats = P.detectBars() || []; }, 50);
  }

  // ---- Saving, history -----------------------------------------------------------
  async function save() {
    window.Forms?.busy?.(saveBtn, true);
    const ok = await P.saveMarkers(markers(), cat);
    window.Forms?.busy?.(saveBtn, false);
    if (ok) {
      dirty = false;
      say(t('Saved: the chart follows this sync.'), 'ok');
      render();
    } else {
      say(t("Couldn't save. Try again."), 'error');
    }
  }
  async function showHistory() {
    const open = historyEl.hidden;
    historyEl.hidden = !open;
    historyBtn.setAttribute('aria-expanded', String(open));
    if (!open) return;
    historyEl.innerHTML = `<li class="hint">${t('Loading…')}</li>`;
    let list = [];
    try {
      const res = await fetch(`/api/songs/${P.variantId}/audio/${cat}/markers/history`);
      if (res.ok) list = (await res.json()).history || [];
    } catch (e) { /* shown empty */ }
    if (!list.length) { historyEl.innerHTML = `<li class="hint">${t('No earlier sync saved.')}</li>`; return; }
    historyEl.replaceChildren(...list.map((h) => {
      const li = document.createElement('li');
      const when = new Date(h.savedAt);
      const what = document.createElement('span');
      what.textContent = `${when.toLocaleString(document.documentElement.lang || undefined, { dateStyle: 'medium', timeStyle: 'short' })} · ${tn(h.markers.length, '%d section', '%d sections')}`;
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'audio-pill-btn';
      b.textContent = t('Go back to this one');
      b.addEventListener('click', () => {
        change(() => { const keep = rows; load(h.markers); rows.forEach((r, i) => { r.label = keep[i]?.label || r.label; }); });
        historyEl.hidden = true;
        historyBtn.setAttribute('aria-expanded', 'false');
        say(t('Earlier sync loaded: save to keep it.'), 'ok');
      });
      li.append(what, b);
      return li;
    }));
  }

  // ---- Opening, closing ----------------------------------------------------------
  let lastFocus = null;
  async function open(opts = {}) {
    if (opts.category && opts.category !== P.category()) P.selectCategory(opts.category);
    sheet.hidden = false;
    document.documentElement.classList.add('sync-sheet-open');
    lastFocus = document.activeElement;
    statusEl.textContent = '';
    historyEl.hidden = true;
    // Wait for the recording (a category just switched to decodes first).
    for (let i = 0; !P.ready() && i < 100; i++) {
      say(t('Loading the recording…'));
      await new Promise((r) => setTimeout(r, 100));
    }
    if (!P.ready()) { say(t("The recording didn't load."), 'error'); return; }
    statusEl.textContent = '';
    history = [];
    lags = [];
    load(P.syncMarkers());
    dirty = false;
    // A proposal handed in (the wizard's, Auto-align's) is a set of
    // suggestions to accept or reject, not a new sync.
    sugg = {};
    rejected = new Set();
    suggNotes = opts.notes || [];
    if (opts.proposal) addSuggestions(opts.proposal, opts.source || 'given');
    // Start where the sync still needs a person.
    const first = rows.findIndex((r) => r.origin !== 'set');
    cursor = first >= 0 ? first : 0;
    render();
    loadTargets();
    clearInterval(timer);
    timer = setInterval(tick, 50);
    tick();
    tapBtn.focus({ preventScroll: true });
    rowsEl.children[cursor]?.scrollIntoView({ block: 'nearest' });
  }
  function close() {
    if (dirty && !window.confirm(t('Leave without saving the sync?'))) return;
    dirty = false;
    sheet.hidden = true;
    document.documentElement.classList.remove('sync-sheet-open');
    clearInterval(timer);
    P.setPreview(null);
    lastFocus?.focus?.({ preventScroll: true });
  }

  // ---- Wiring --------------------------------------------------------------------
  // A tap counts when the finger goes down, not when it lifts.
  tapBtn.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    tap();
  });
  tapBtn.addEventListener('click', (e) => { if (e.detail === 0) tap(); }); // Enter / Space on it
  playBtn.addEventListener('click', () => (P.isPlaying() ? P.pause() : P.play()));
  $('sync-back').addEventListener('click', () => P.seek(Math.max(0, P.audio.currentTime - 5)));
  clickBtn.addEventListener('click', () => { P.toggleClick(); tick(); });
  $('sync-speed').addEventListener('click', (e) => { const b = e.target.closest('button[data-v]'); if (b) { P.setSpeed(+b.dataset.v); tick(); } });
  undoBtn.addEventListener('click', undo);
  saveBtn.addEventListener('click', save);
  historyBtn.addEventListener('click', showHistory);
  $('sync-suggest-btn').addEventListener('click', suggest);
  $('sync-accept-all').addEventListener('click', acceptAll);
  sheet.querySelectorAll('[data-sync-close]').forEach((el) => el.addEventListener('click', close));
  // Keys while it's open, before the player's own (window, capture).
  window.addEventListener('keydown', (e) => {
    if (!isOpen() || e.target.closest?.('input, select, textarea, [contenteditable]')) return;
    let handled = true;
    const k = e.key;
    if ((e.metaKey || e.ctrlKey) && (k === 'z' || k === 'Z')) undo();
    else if (e.metaKey || e.ctrlKey || e.altKey) handled = false;
    else if (k === 'Escape') close();
    else if (k === ' ' || k === 'Spacebar') { if (e.target.closest?.('button') && e.target !== tapBtn) handled = false; else (P.isPlaying() ? P.pause() : P.play()); }
    else if (k === 't' || k === 'T' || (k === 'Enter' && !e.target.closest?.('button'))) tap();
    else if (k === 'ArrowLeft') nudge(cursor, e.shiftKey ? -barLen(Math.max(0, cursor - 1)) : -0.1);
    else if (k === 'ArrowRight') nudge(cursor, e.shiftKey ? barLen(cursor) : 0.1);
    else if (k === 'ArrowUp') select(cursor - 1);
    else if (k === 'ArrowDown') select(cursor + 1);
    else if (k === 'k' || k === 'K') { P.toggleClick(); tick(); }
    else if (k === 'z' || k === 'Z') undo();
    else handled = false;
    if (handled) { e.preventDefault(); e.stopImmediatePropagation(); }
  }, true);
  window.addEventListener('beforeunload', (e) => { if (isOpen() && dirty) { e.preventDefault(); e.returnValue = ''; } });

  window.songbooksSyncSheet = { open, close, isOpen };
})();

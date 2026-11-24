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
// Over it all, the recording's waveform (sync-wave.js): zoomed from the
// whole song to a few beats, scrolled, following the playhead, with the
// sections, bar lines, bar numbers and chords on it, the analysis' beats
// faintly, its suggestions and chord checks where they apply, and each
// section's start a marker to drag (snapping to a downbeat, a beat or
// the voice; Alt to place it freely), the bars between following.
//
// It works on the player's recording (audio-player.js,
// window.songbooksPlayer); opened from the player's sync badge, or by
// the import wizard with its proposal (songbooksSyncSheet.open).
(function () {
  const sheet = document.getElementById('sync-sheet');
  const P = window.songbooksPlayer;
  if (!sheet || !P) return;
  const $ = (id) => document.getElementById(id);
  const WM = window.SyncWaveMath;
  const rowsEl = $('sync-rows');
  const tapBtn = $('sync-tap');
  const tapWhat = $('sync-tap-what');
  const hereLabel = $('sync-here-label');
  const hereMeta = $('sync-here-meta');
  const hereMore = $('sync-here-more');
  const followBtn = $('sync-follow');
  const playBtn = $('sync-play');
  const timeEl = $('sync-time');
  const nowEl = $('sync-now');
  const warnEl = $('sync-warn-box');
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

  // tn with more values than the count.
  const tnx = (n, one, other, ...args) => {
    const single = window.SB_LANG === 'fr' ? n === 0 || n === 1 : n === 1;
    return t(single ? one : other, n, ...args);
  };
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
    const order = { analysis: 0, voice: 1, given: 2, beat: 3 };
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
  // A suggestion is taken only between the starts around it: one that
  // lands past the next start would run the grid backwards.
  function accept(i, s) {
    const r = WM.acceptPicks(rows, { [i]: s.t }, { strict: true });
    if (r.refused.length) {
      const prev = rows.slice(0, i).reverse().find((x) => x.t !== null);
      const next = rows.slice(i + 1).find((x) => x.t !== null);
      const before = prev && s.t <= prev.t + 0.2;
      const other = before ? prev : next;
      say(!other ? t('That suggestion is out of order.')
        : before ? t('That suggestion comes before %s’s start: move that one first, or leave it.', other.label)
          : t('That suggestion comes after %s’s start: move that one first, or leave it.', other.label), 'error');
      return;
    }
    change(() => {
      rows = rows.map((x, k) => ({ ...x, t: r.rows[k].t, origin: r.rows[k].origin }));
      delete sugg[rows[i].index];
    });
  }
  // Every pending suggestion, in order: a start set by ear never goes;
  // a suggestion that would land past one is left out instead.
  function acceptAll() {
    const picks = {};
    rows.forEach((r, i) => { const p = pending(i); if (p.length) picks[i] = p[0].t; });
    const r = WM.acceptPicks(rows, picks);
    change(() => {
      rows = rows.map((x, k) => ({ ...x, t: r.rows[k].t, origin: r.rows[k].origin }));
      Object.keys(picks).forEach((i) => { delete sugg[rows[i].index]; });
    });
    if (r.refused.length) say(tn(r.refused.length, '%d suggestion was left out: it didn’t fit between the starts set by ear.', '%d suggestions were left out: they didn’t fit between the starts set by ear.'), 'error');
  }
  function reject(i) {
    history.push(snapshot());
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
        addSuggestions((p.sections || []).map((s) => ({ sectionIndex: s.index, timeSeconds: s.time })), p.downbeats ? 'analysis' : 'voice');
        suggNotes = p.notes || [];
      }
    } catch (e) { /* the player's alone */ }
    window.Forms?.busy?.(btn, false);
    rejected = new Set();
    const n = rows.filter((_, i) => pending(i).length).length;
    say(n ? tn(n, '%d section has a suggestion', '%d sections have a suggestion') : t('The suggestions agree with the sync as it is.'), 'ok');
    render();
  }
  const snapshot = () => JSON.stringify({ rows, cursor, todoFrom, sugg, rejected: [...rejected] });
  function restore(s) {
    let rej;
    ({ rows, cursor, todoFrom, sugg, rejected: rej } = JSON.parse(s));
    rejected = new Set(rej || []);
  }
  // Unsaved: the starts aren't the ones last saved (or loaded).
  let savedMarks = '[]';
  const markSaved = () => { savedMarks = JSON.stringify(markers()); dirty = false; };
  const updateDirty = () => { dirty = JSON.stringify(markers()) !== savedMarks; };
  function change(fn) {
    history.push(snapshot());
    if (history.length > 200) history.shift();
    fn();
    updateDirty();
    afterChange();
  }
  function undo() {
    const s = history.pop();
    if (!s) return;
    restore(s);
    updateDirty();
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
  // is known, the last one placed from the span before it (±1 bar on it
  // follows the recording, not a chart tempo that's off), else the
  // chart's tempo (sync-wave-math.js barLength).
  const chartBar = () => (P.bpm > 0 ? (60 / P.bpm) * P.beatsPerBar : 2);
  const barLen = (i) => WM.barLength(rows, i, chartBar());
  // The tempo the counter runs at between the starts, in the chart's
  // beat (a dotted quarter in 6/8), and whether it's off the chart's.
  const offTempo = (v) => WM.offTempo(v, P.bpm);
  // A bpm said in the chart's beat: "♩. = 46 (♪ = 138)" in 6/8.
  const bpmText = (v, o) => (P.bpmText ? P.bpmText(v, o) : t('%d bpm', Math.round(v)));

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
    if (!P.isPlaying()) play();
  }
  function audition(i) {
    const r = rows[i];
    if (!r || r.t === null) return;
    P.seek(Math.max(0, r.t - 2));
    if (!P.isPlaying()) play();
  }
  // The section the next tap sets; the waveform shows its start.
  function select(i, fromWave) {
    cursor = Math.max(0, Math.min(rows.length - 1, i));
    render();
    // A phone scrolls the card and the list as one: the card (and its
    // tap button) stays in sight; a wide screen scrolls the list to it.
    if (window.matchMedia?.('(max-width: 720px)').matches) $('sync-here').scrollIntoView({ block: 'nearest' });
    else rowsEl.children[cursor]?.scrollIntoView({ block: 'nearest' });
    if (wave && !fromWave && rows[cursor]) wave.reveal(rows[cursor].t);
  }

  // ---- Drawing -----------------------------------------------------------------
  // The list: one line per section, its state as a dot, its start, and
  // what needs a look (a suggestion, a flag, a tempo or bar count off);
  // the section the next tap sets gets the card beside it, with its tools.
  // Rebuilding the list and the card mustn't drop a keyboard's focus:
  // every button made here has a data-focus key, and the one focused
  // before is focused again (or, gone or disabled, the cursor's row).
  function keepFocus(fn) {
    const a = document.activeElement;
    const key = a && sheet.contains(a) && a.dataset ? a.dataset.focus : null;
    fn();
    if (!key || (document.activeElement && document.activeElement !== document.body && sheet.contains(document.activeElement))) return;
    let b = sheet.querySelector(`[data-focus="${CSS.escape(key)}"]`);
    if (!b || b.disabled || b.closest('[hidden]')) b = key.startsWith('ana') ? anaEl?.querySelector('summary') : rowsEl.querySelector(`[data-focus="row:${cursor}"]`);
    b?.focus({ preventScroll: true });
  }
  function render() { keepFocus(renderNow); }
  function renderNow() {
    const tempos = WM.rowTempos(rows, P.beatsPerBar);
    rowsEl.replaceChildren(...rows.map((r, i) => {
      const li = document.createElement('li');
      const state = r.t === null ? 'unset' : r.origin === 'set' ? 'set' : 'auto';
      li.className = `sync-row is-${state}${i === cursor ? ' is-cursor' : ''}${todoFrom >= 0 && i >= todoFrom && i >= cursor ? ' is-todo' : ''}`;
      const main = document.createElement('button');
      main.type = 'button';
      main.className = 'sync-row-main';
      main.dataset.focus = `row:${i}`;
      main.setAttribute('aria-current', i === cursor ? 'true' : 'false');
      main.innerHTML = '<i class="sync-row-dot"></i><span class="sync-row-label"></span><span class="sync-row-notes"></span><span class="sync-row-time"></span>';
      main.querySelector('.sync-row-dot').title = stateText(r);
      main.querySelector('.sync-row-label').textContent = r.label;
      main.querySelector('.sync-row-time').textContent = fmt(r.t);
      main.setAttribute('aria-label', `${r.label}, ${fmt(r.t)}, ${stateText(r)}`);
      const notes = main.querySelector('.sync-row-notes');
      const note = (cls, text, title) => { const s = el('span', `sync-row-note ${cls}`, text); if (title) s.title = title; notes.append(s); };
      if (pending(i).length) note('sugg', '●', t('a suggestion to check'));
      const fl = flags(i);
      if (fl.length) { li.classList.add('is-flagged'); note('flag', '⚑', fl.join(' · ')); }
      const as = anaSection(r.index);
      if (as && !as.last && as.played && as.played !== as.written) note('bars', t('%d → %d bars', as.written, as.played), t('written %d · played %d', as.written, as.played));
      if (offTempo(tempos[i])) note('bpm', bpmText(tempos[i], { short: true }), t('the chart’s %d bars here make the counter run at %s; the chart says %s', r.barCount, bpmText(tempos[i]), bpmText(P.bpm)));
      main.addEventListener('click', () => select(i));
      li.appendChild(main);
      return li;
    }));
    renderHere(tempos);
    const waiting = rows.filter((_, i) => pending(i).length).length;
    $('sync-accept-all').hidden = !waiting;
    $('sync-suggest-count').textContent = waiting ? tn(waiting, '%d suggestion to check', '%d suggestions to check') : '';
    const notesEl = $('sync-suggest-notes');
    notesEl.hidden = !(waiting && suggNotes.length);
    notesEl.textContent = suggNotes.join(' ');
    undoBtn.disabled = !history.length;
    saveBtn.disabled = !dirty || gpOwnSync();
    $('sync-gp-note').hidden = !gpOwnSync();
    // Where you are: how many starts are checked by ear, how many to go.
    const set = rows.filter((x) => x.t !== null && x.origin === 'set').length;
    const placed = rows.filter((x) => x.t !== null).length;
    chip.textContent = set === rows.length && rows.length ? t('Sync set')
      : placed ? t('%d of %d set · %d to check', set, rows.length, rows.length - set) : t('Not synced');
    chip.dataset.state = set === rows.length && rows.length ? 'set' : 'auto';
    const off = tempos.filter(offTempo);
    warnEl.hidden = !off.length;
    if (off.length) {
      // The figure most bars run at (bar-weighted, as the player's badge),
      // over the starts as they are now, saved or not.
      const mid = WM.medianTempo(rows, P.beatsPerBar);
      const fix = WM.tempoFix(rows, P.beatsPerBar, P.bpm);
      $('sync-warn-sum').textContent = t('The counter isn’t at the chart’s tempo: %s, not %s', bpmText(mid), bpmText(P.bpm));
      $('sync-warn-text').textContent = t('Between these starts the counter runs at about %s, the chart says %s. If the recording plays at about %s throughout, the chart’s tempo is off: set it to that. If it plays at the chart’s tempo, the chart has more or fewer bars than the recording plays where a section is marked: count its bars while listening and fix the chart.', bpmText(mid), bpmText(P.bpm), bpmText(mid, { short: true }));
      const use = $('sync-warn-use');
      use.hidden = !fix;
      use.textContent = t('Use %s as the chart’s tempo', bpmText(fix, { short: true }));
      use.dataset.bpm = String(fix);
      use.disabled = tempoBusy;
    }
    renderTempo();
    waveChanged();
  }
  const stateText = (r) => (r.t === null ? t('sync|not placed') : r.origin === 'set' ? t('sync|set') : t('sync|estimated'));
  // The card: the section the next tap sets, its start, its tools, what
  // the recording says about it and the suggestions for it.
  function renderHere(tempos) {
    const r = rows[cursor];
    tapWhat.textContent = r ? t('Start of %s', r.label) : t('Every section is placed');
    tapBtn.disabled = !r;
    hereLabel.textContent = r ? r.label : t('Every section is placed');
    hereMeta.textContent = r ? [fmt(r.t), stateText(r), t('bar %s', P.barLabel(r.startBar))].join(' · ') : '';
    $('sync-here').dataset.state = !r ? '' : r.t === null ? 'unset' : r.origin === 'set' ? 'set' : 'auto';
    if (!r) { hereMore.replaceChildren(); return; }
    // What to decide first: the suggestions; then the tools.
    const out = pending(cursor).map((s) => suggestion(cursor, s));
    out.push(tools(cursor));
    const fl = flags(cursor);
    if (fl.length) out.push(el('p', 'sync-row-flag', fl.join(' · ')));
    const as = anaSection(r.index);
    if (as && !as.last && as.played && as.played !== as.written) out.push(el('p', 'sync-row-bars', t('written %d · played %d', as.written, as.played)));
    if (offTempo(tempos[cursor])) out.push(el('p', 'sync-row-bpm', t('the chart’s %d bars here make the counter run at %s; the chart says %s', r.barCount, bpmText(tempos[cursor]), bpmText(P.bpm))));
    hereMore.replaceChildren(...out);
  }
  // One suggestion for row i: its time and where it comes from, ▶ to
  // hear it, ✓ to take it, ✗ to leave the row as it is.
  // On the waveform's chips.
  const srcShort = (src) => ({ analysis: t('sync|analysis'), voice: t('sync|voice'), beat: t('sync|downbeat'), given: t('sync|proposed') }[src]);
  const srcText = (src) =>({ analysis: t('on the recording’s downbeat'), voice: t('where the singing starts'), beat: t('from the downbeats'), given: t('proposed') }[src]);
  function suggestion(i, s) {
    const r = rows[i];
    const div = document.createElement('div');
    div.className = 'sync-suggest';
    const what = document.createElement('span');
    const d = r.t === null ? '' : ` (${s.t > r.t ? '+' : '−'}${Math.abs(s.t - r.t).toFixed(1)} s)`;
    what.textContent = t('Suggested: %s, %s', fmt(s.t) + d, srcText(s.src));
    const btn = (cls, text, label, fn) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = `sync-btn ${cls}`;
      b.dataset.focus = `sugg:${i}:${s.src}:${cls || 'play'}`;
      b.textContent = text;
      b.setAttribute('aria-label', label);
      b.title = label;
      b.addEventListener('click', fn);
      return b;
    };
    div.append(what,
      btn('', '▶', t('play the suggestion for %s from 2 seconds before', r.label), () => { P.seek(Math.max(0, s.t - 2)); if (!P.isPlaying()) play(); }),
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
      b.dataset.focus = `tool:${act}`;
      b.textContent = text;
      if (label) { b.setAttribute('aria-label', label); b.title = label; }
      b.disabled = !!disabled;
      return b;
    };
    const none = r.t === null;
    div.append(
      btn('audition', t('▶ 2 s before'), t('play %s from 2 seconds before', r.label), none),
      btn('bar-', t('−1 bar'), t('one bar earlier (Shift ←)'), none),
      btn('ms-', t('−0.1 s'), t('0.1 second earlier (←)'), none),
      btn('ms+', t('+0.1 s'), t('0.1 second later (→)'), none),
      btn('bar+', t('+1 bar'), t('one bar later (Shift →)'), none),
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

  // ---- The waveform --------------------------------------------------------------
  // What it draws, rebuilt when the rows or the analysis change; while a
  // marker is dragged, the bars between the starts are worked out here
  // (the player's grid follows on release).
  let wave = null;
  let waveData = null;
  let chordBars = []; // [{bar, name, first}] from the chart, by performed bar
  let peaksFor = null; // the samples the peaks were built from
  let dragFrom = null; // the drag's snapshot, for undo
  let dragLocal = false;
  let lastGhost = { key: '', at: 0 };
  const waveTargets = { downbeat: [], beat: [], voice: [], suggestion: [] };
  function localBars() {
    const secs = P.sections();
    const marks = rows.filter((r) => r.t !== null).map((r) => ({ start: r.startBar, t: r.t }));
    return WM.barTimes(marks, P.bpm > 0 ? (60 / P.bpm) * P.beatsPerBar : 0, P.duration()).slice(0, Math.max(2, secs.reduce((m, s) => Math.max(m, s.startBar + s.barCount), 0) + 64));
  }
  function waveChanged() {
    if (!wave) return;
    const p = ana && ana.proposal;
    const bars = dragLocal ? localBars() : P.barTimes();
    const sectionsOut = rows.map((r, i) => {
      const as = anaSection(r.index);
      const badge = as && !as.last && as.played && as.played !== as.written ? t('%d → %d bars', as.written, as.played) : '';
      const end = bars.length > r.startBar + r.barCount ? bars[r.startBar + r.barCount] : null;
      return { i, label: r.label, t: r.t, startBar: r.startBar, state: r.t === null ? 'unset' : r.origin === 'set' ? 'set' : 'auto', cursor: i === cursor, badge, end };
    });
    // Where each chip was last drawn stays its own until the next frame
    // (a second click on it may come first).
    const boxes = new Map(((waveData && waveData.ghosts) || []).map((g) => [g.key, g._box]));
    const ghosts = [];
    rows.forEach((r, i) => {
      for (const s of pending(i)) {
        const key = `${i}:${s.t}`;
        ghosts.push({ i, t: s.t, s, key, label: `${r.label} · ${srcShort(s.src)}`, _box: boxes.get(key) || null });
      }
    });
    const fixes = ((p && p.chordFixes) || []).map((f) => ({ t: f.time, label: `${f.chart} → ${f.heard}`, key: fixKey(f) }));
    const beatList = (p && p.beats && p.beats.length ? p.beats : []);
    const downs = (p && p.downbeats && p.downbeats.length ? p.downbeats : beats) || [];
    waveTargets.downbeat = downs;
    waveTargets.beat = beatList;
    waveTargets.voice = voice;
    waveTargets.suggestion = ghosts.map((g) => g.t).sort((a, b) => a - b);
    waveData = {
      bars, sections: sectionsOut, chords: chordBars, beats: beatList, downbeats: downs, voice,
      ghosts, fixes, chartBars: P.totalBars(), pickup: P.hasPickup(), leadIn: P.leadIn ? P.leadIn() : (P.hasPickup() ? 1 : 0), barLabel: P.barLabel,
    };
    wave.invalidate();
  }
  function ensureWave() {
    if (wave || !window.SongbooksSyncWave) return;
    wave = window.SongbooksSyncWave.create({
      canvas: $('sync-wave-canvas'),
      overview: $('sync-wave-overview'),
      now: () => P.audio.currentTime,
      playing: () => P.isPlaying(),
      duration: () => P.duration(),
      isOpen,
      data: () => waveData || { bars: [], sections: [], chords: [], beats: [], downbeats: [], voice: [], ghosts: [], fixes: [], chartBars: 0, barLabel: String },
      seek: (tm) => P.seek(Math.max(0, tm)),
      select: (i) => select(i, true),
      dragStart: (i) => { dragFrom = snapshot(); dragLocal = true; setLive(false); select(i, true); },
      dragMove: (i, tm, o) => dragTo(i, tm, o),
      dragEnd: (i, cancelled) => dragDone(i, cancelled),
      pickGhost: (g) => {
        // Once: that section, its suggestion in the card; twice: taken.
        const now = Date.now();
        if (lastGhost.key === g.key && now - lastGhost.at < 600) { accept(g.i, g.s); lastGhost = { key: '', at: 0 }; return; }
        lastGhost = { key: g.key, at: now };
        select(g.i);
      },
      pickFix: (f) => showFix(f.key),
      onView: (_, on) => { followBtn.setAttribute('aria-pressed', String(on)); followBtn.classList.toggle('active', on); },
    });
  }
  function dragTo(i, raw, o) {
    const r = rows[i];
    if (!r) return;
    let at = raw;
    let kind = null;
    if (!o.free && snapBox.checked) ({ t: at, kind } = WM.snap(raw, waveTargets, o.pxPerSec));
    const prev = rows.slice(0, i).reverse().find((x) => x.t !== null);
    const next = rows.slice(i + 1).find((x) => x.t !== null);
    at = WM.between(at, prev ? prev.t : null, next ? next.t : null, P.duration());
    r.t = at;
    r.origin = 'set';
    const snapText = { downbeat: t('on a downbeat'), beat: t('on a beat'), voice: t('where the voice comes in'), suggestion: t('on the suggestion') }[kind];
    say(snapText ? `${r.label} ${fmt(at)} · ${snapText}` : `${r.label} ${fmt(at)}`);
    hereMeta.textContent = [fmt(r.t), stateText(r), t('bar %s', P.barLabel(r.startBar))].join(' · ');
    waveChanged();
  }
  function dragDone(i, cancelled) {
    const before = dragFrom;
    dragFrom = null;
    dragLocal = false;
    if (!before) return;
    setLive(true);
    if (cancelled || before === snapshot()) {
      restore(before);
      render();
      return;
    }
    history.push(before);
    if (history.length > 200) history.shift();
    updateDirty();
    afterChange();
    const r = rows[i];
    if (r) say(`${r.label} ${fmt(r.t)}`, 'ok');
  }
  // While a marker is dragged, the card and the status don't announce
  // every position: only where it's let go.
  function setLive(on) {
    $('sync-here').setAttribute('aria-live', on ? 'polite' : 'off');
    statusEl.setAttribute('aria-live', on ? 'polite' : 'off');
  }
  // A chord check flagged on the waveform: open the analysis there.
  function showFix(key) {
    if (!anaEl) return;
    anaEl.open = true;
    const li = anaBody.querySelector(`[data-fix="${CSS.escape(key)}"]`);
    if (li) {
      li.scrollIntoView({ block: 'nearest' });
      li.classList.remove('flash');
      void li.offsetWidth;
      li.classList.add('flash');
    }
  }
  // The chart's chords, by performed bar (the Mixer's timeline: every
  // section in the order played, its chords per bar).
  async function loadChords() {
    chordBars = [];
    try {
      const res = await fetch(`/api/songs/${P.variantId}/mixer`);
      if (!res.ok) return;
      const m = await res.json();
      for (const s of m.sections || []) {
        (s.chords || []).forEach((bar, j) => {
          (bar || []).forEach((c, k) => chordBars.push({ bar: s.startBar + j + (c.pos || 0), name: c.name, first: j === 0 && k === 0 }));
        });
      }
      chordBars.sort((a, b) => a.bar - b.bar);
      waveChanged();
    } catch (e) { /* drawn without chords */ }
  }
  // The recording's peaks, built once per recording (sync-wave-math.js).
  function loadPeaks() {
    const s = P.samples();
    if (!wave || !s || s === peaksFor) return;
    peaksFor = s;
    wave.setPeaks(WM.buildPeaks(s.x, s.sr));
  }
  function play() {
    if (wave) wave.setFollow(true);
    P.play();
  }
  let lastTickAt = -1;
  function tick() {
    const at = P.audio.currentTime;
    timeEl.textContent = fmt(at);
    let cur = null;
    rows.forEach((r) => { if (r.t !== null && r.t <= at + 0.02) cur = r; });
    // Where you are: the section and the bar in it.
    const bar = waveData && waveData.bars.length > 1 ? WM.barAt(waveData.bars, at) : null;
    nowEl.textContent = cur ? (bar !== null && bar >= cur.startBar ? t('%s · bar %d', cur.label, Math.floor(bar) - cur.startBar + 1) : cur.label) : '';
    const playing = P.isPlaying();
    if (wave) {
      // Another recording (a category switched, a part swapped in): its
      // peaks when they're there, none meanwhile — never the last one's.
      const s = P.samples();
      if (s !== peaksFor) {
        if (s) loadPeaks();
        else if (peaksFor) { peaksFor = null; wave.setPeaks(null); }
      }
      if (at !== lastTickAt || playing) wave.invalidate();
    }
    if (detectWanted && P.samples() && P.samples() !== beatsFor) scheduleDetect();
    lastTickAt = at;
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
  }
  // The player's own downbeats, for snapping, only when the analysis has
  // none: it takes a moment on a long song, so once the analysis has
  // answered, when the page is idle, and again for another recording.
  let detectWanted = false;
  let beatsFor = null; // the samples the downbeats were found in
  let detectTimer = null;
  const hasAnaBeats = () => !!(ana && ana.proposal && ana.proposal.downbeats && ana.proposal.downbeats.length);
  function scheduleDetect() {
    if (detectTimer || hasAnaBeats()) return;
    const forCat = cat;
    const run = () => {
      detectTimer = null;
      if (!isOpen() || forCat !== cat || hasAnaBeats()) return;
      const s = P.samples();
      if (!s || s === beatsFor) return;
      beatsFor = s;
      beats = P.detectBars() || [];
      waveChanged();
    };
    detectTimer = window.requestIdleCallback ? requestIdleCallback(run, { timeout: 1500 }) : setTimeout(run, 200);
  }

  // ---- The chart's tempo -----------------------------------------------------------
  // What the counter goes by outside the starts (before the first, after
  // the last), what ±1 bar moves on a row it can't measure, and what the
  // "off tempo" checks compare with: the chart's "tempo:". Typed — in a
  // compound metre in the note picked (tempo-units.js), the chart keeping
  // its beat — tapped while the recording plays, or the counter's own
  // taken from the warning in one click. Saved as any edit of the song is
  // (tap-tempo.js saveSong: PUT …/meta, its rights, The Big Songbook's
  // review, a revision); the player, the counter, the click and this
  // sheet follow at once ('songbooks:tempo-saved'), the sync as it is.
  const tempoInput = $('sync-tempo-input');
  const tempoSaveBtn = $('sync-tempo-save');
  const TU = window.SongbooksTempoUnits;
  let tempoUnit = 'beat';
  let tempoEdited = false; // the field holds what the person typed or tapped
  let tempoBusy = false;
  let tempoTapper = null;
  const unitPick = TU && TU.picker(P.timeNum, P.timeDen, (id, prev) => {
    tempoUnit = id;
    // The figure typed stays the same tempo, said in the new note.
    const v = parseFloat(tempoInput.value);
    if (v > 0) tempoInput.value = String(Math.round(TU.fromBeat(TU.toBeat(v, prev, P.timeNum, P.timeDen), id, P.timeNum, P.timeDen)));
    tempoTapper?.reset();
    renderTempo();
  });
  if (unitPick) {
    tempoUnit = unitPick.value;
    $('sync-tempo-unit').replaceChildren(unitPick);
  }
  // The field's tempo in the chart's beat, whole; null when unusable.
  function typedTempo() {
    const v = parseFloat(tempoInput.value);
    if (!(v > 0)) return null;
    const beat = Math.round(TU ? TU.toBeat(v, tempoUnit, P.timeNum, P.timeDen) : v);
    return beat >= 20 && beat <= 400 ? beat : null;
  }
  const inUnit = (bpm) => Math.round(TU ? TU.fromBeat(bpm, tempoUnit, P.timeNum, P.timeDen) : bpm);
  function renderTempo() {
    if (!tempoEdited && document.activeElement !== tempoInput) tempoInput.value = P.bpm > 0 ? String(inUnit(P.bpm)) : '';
    const v = typedTempo();
    tempoSaveBtn.disabled = tempoBusy || v === null || v === Math.round(P.bpm);
    tempoInput.disabled = tempoBusy;
    const mid = WM.medianTempo(rows, P.beatsPerBar);
    $('sync-tempo-now').textContent = mid ? t('The counter runs at %s between the starts.', bpmText(mid)) : '';
  }
  const editorDirty = () => document.documentElement.classList.contains('is-dirty') || !!(window.songbooksEditorDirty && window.songbooksEditorDirty());
  async function saveTempo(value) {
    const TT = window.SongbooksTapTempo;
    if (tempoBusy || !(value > 0) || !TT || !TT.saveSong) return;
    if (editorDirty()) { say(t('Save or discard your edits to the song first.'), 'error'); return; }
    tempoBusy = true;
    render();
    say(t('Saving…'));
    const r = await TT.saveSong(P.variantId, value);
    tempoBusy = false;
    if (r.error) say(r.error, 'error');
    else if (r.pending) say(r.message, 'ok');
    else {
      tempoEdited = false;
      say(t('The chart’s tempo is now %s: the counter follows it.', bpmText(value)), 'ok');
    }
    render();
  }
  tempoInput.addEventListener('input', () => { tempoEdited = true; renderTempo(); });
  tempoInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); const v = typedTempo(); if (v !== null && v !== Math.round(P.bpm)) saveTempo(v); }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); tempoEdited = false; tempoInput.blur(); renderTempo(); }
  });
  tempoInput.addEventListener('blur', () => { if (!tempoInput.value.trim()) tempoEdited = false; renderTempo(); });
  tempoSaveBtn.addEventListener('click', () => { const v = typedTempo(); if (v !== null) saveTempo(v); });
  // Tap: the beat tapped (in the note picked) fills the field, from the
  // second tap (tap-tempo.js' arithmetic); set to save it.
  const tempoTap = $('sync-tempo-tap');
  let tempoTappedAt = -Infinity;
  function tapTempo() {
    const TT = window.SongbooksTapTempo;
    if (!TT) return;
    tempoTapper = tempoTapper || TT.create();
    tempoTappedAt = performance.now();
    const v = tempoTapper.tap(tempoTappedAt);
    tempoTap.classList.remove('flash');
    void tempoTap.offsetWidth;
    tempoTap.classList.add('flash');
    if (v === null || !TT.usable(v)) return;
    tempoInput.value = String(v);
    tempoEdited = true;
    renderTempo();
  }
  tempoTap.addEventListener('pointerdown', (e) => { if (e.button === 0) { e.preventDefault(); tapTempo(); } });
  tempoTap.addEventListener('click', () => { if (performance.now() - tempoTappedAt > 400) tapTempo(); });
  // The warning's one click: the counter's tempo as the chart's.
  $('sync-warn-use').addEventListener('click', (e) => { const v = +e.currentTarget.dataset.bpm; if (v > 0) saveTempo(v); });
  // The tempo saved (here, Tap tempo, the editor): the counter's bars
  // outside the starts and the checks follow; the downbeats found by ear
  // start from it, found again; the analysis' proposals weigh it.
  document.addEventListener('songbooks:tempo-saved', () => {
    if (!isOpen()) return;
    tempoEdited = false;
    beatsFor = null;
    render();
    loadAnalysis();
  });

  // ---- Saving, history -----------------------------------------------------------
  // A Guitar Pro file's own sync places every bar: section starts placed
  // here don't change what the chart follows.
  const gpOwnSync = () => cat === 'gp' && ((P.meta() || {}).barSyncPoints || []).length > 1;
  async function save() {
    if (gpOwnSync()) return;
    window.Forms?.busy?.(saveBtn, true);
    const ok = await P.saveMarkers(markers(), cat);
    window.Forms?.busy?.(saveBtn, false);
    if (ok) {
      markSaved();
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

  // ---- The recording's own analysis ------------------------------------------------
  // Its beats, bars and chords (the stems worker's analysis stage), laid
  // on the chart by the server (POST …/analysis): a sync to take in one
  // tap (every section on its downbeat, every bar placed, saved as
  // estimates), one-tap fixes for when the beat grid is wrong (×2, ÷2, the
  // downbeat a beat off, "bar 1 is here"), and the chart's proposals —
  // the tempo and metre, an intro it lacks, sections whose bar count
  // isn't what's played, chords the recording contradicts — reviewed as
  // the lines they change before being saved as a revision.
  const anaEl = $('sync-analysis');
  const anaBody = $('sync-analysis-body');
  let ana = null; // the last response
  let hints = { tempoFactor: 0, shiftBeats: 0, bar1At: 0 };
  let picks = { tempo: false, time: false, intro: false, sections: new Set(), chords: new Set() };
  let review = null; // {changes} while reviewing
  let anaBusy = false;
  const pct = (x) => Math.round((x || 0) * 100);
  const fixKey = (f) => `${f.defSection}:${f.measure}:${f.slot}`;
  async function loadAnalysis() {
    const forCat = cat;
    if (!anaEl) return;
    try {
      const res = await fetch(`/api/songs/${P.variantId}/audio/${forCat}/analysis?lang=${encodeURIComponent(document.documentElement.lang || '')}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ hints }),
      });
      if (!res.ok || forCat !== cat) return;
      ana = await res.json();
    } catch (e) { return; }
    const p = ana.proposal;
    if (p) {
      // Snap taps to the analysis' downbeats rather than the player's guess.
      if (p.downbeats && p.downbeats.length) beats = p.downbeats;
      // Picks that no longer exist go.
      const secs = new Set((p.sections || []).filter((s) => s.grid).map((s) => s.index));
      picks.sections.forEach((i) => { if (!secs.has(i)) picks.sections.delete(i); });
      const keys = new Set((p.chordFixes || []).map(fixKey));
      picks.chords.forEach((k) => { if (!keys.has(k)) picks.chords.delete(k); });
      if (!p.intro) picks.intro = false;
      if (!p.proposeTempo) picks.tempo = false;
      if (!p.proposeTime) picks.time = false;
    }
    renderAnalysis();
    render();
  }
  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function anaBtn(text, title, fn, cls = 'sync-btn') {
    const b = el('button', cls, text);
    b.type = 'button';
    b.dataset.focus = `ana:${text}`;
    if (title) { b.title = title; b.setAttribute('aria-label', title); }
    b.addEventListener('click', fn);
    return b;
  }
  // How long ago, roughly: "3 min", "2 h".
  function ago(iso) {
    const s = (Date.now() - Date.parse(iso)) / 1000;
    if (!isFinite(s) || s < 0) return '';
    if (s < 90) return t('just now');
    if (s < 5400) return t('%d min ago', Math.round(s / 60));
    if (s < 129600) return t('%d h ago', Math.round(s / 3600));
    return t('%d days ago', Math.round(s / 86400));
  }
  // The analysis' tempo, counted in the beat of the metre it heard.
  const anaBpm = (v, sig, short) => (TU ? TU.text(v, sig, undefined, { short }) : t('%d bpm', Math.round(v)));
  // Why the analysis is less sure than its chords' fit says.
  const whyText = (p) => ({
    'too-long': t('the chart has far more bars than the recording plays'),
    bars: t('the chart’s bars and the recording’s are far apart'),
    section: t('a section’s bar count is far off'),
    unplaced: t('many of the chart’s bars aren’t heard'),
  }[p.why] || '');
  const listen = (tm) => anaBtn('▶', t('listen from 2 seconds before'), () => { P.seek(Math.max(0, tm - 2)); if (!P.isPlaying()) play(); }, 'sync-btn sync-ana-play');
  function renderAnalysis() { keepFocus(renderAnalysisNow); }
  // The header's chip: the analysis' state where a phone sees it, a tap
  // away from its panel (under the list there).
  function renderAnaChip(state, text) {
    const c = $('sync-ana-chip');
    if (!c) return;
    c.hidden = !ana;
    c.dataset.state = state;
    c.querySelector('span').textContent = text;
  }
  function renderAnalysisNow() {
    if (!anaEl) return;
    anaEl.hidden = !ana;
    if (!ana) { renderAnaChip('none', ''); return; }
    const dot = $('sync-analysis-dot'), sum = $('sync-analysis-sum');
    const p = ana.proposal;
    if (!p) {
      dot.dataset.state = 'none';
      const waiting = ana.stemStatus && ana.stemStatus !== 'done';
      const since = ana.since ? ago(ana.since) : '';
      sum.textContent = ana.status === 'failed' ? t('analysis failed')
        : waiting ? t('after the split into parts')
          : ana.status === 'running' ? t('being analysed…')
            : ana.status === 'queued' ? (ana.ahead ? tn(ana.ahead, 'queued, %d ahead', 'queued, %d ahead') : t('queued'))
              : t('not analysed yet');
      const msg = ana.status === 'failed' ? t('The recording couldn’t be analysed.')
        : waiting ? t('The recording is analysed (its beats, bars and chords) once it’s split into its parts.')
          : ana.status === 'running' ? t('The recording is being analysed (its beats, bars and chords) since %s: a minute or two.', since || t('just now'))
            : ana.status === 'queued' ? (ana.ahead ? tnx(ana.ahead, 'The recording waits for its analysis behind %d other one (queued %s).', 'The recording waits for its analysis behind %d others (queued %s).', since || t('just now')) : t('The recording waits for its analysis (queued %s).', since || t('just now')))
              : t('The recording hasn’t been analysed yet.');
      const kids = [el('p', 'hint', msg)];
      if (ana.status === 'failed' && ana.error) kids.push(el('p', 'hint sync-ana-error', ana.error.split('\n').filter(Boolean).pop() || ''));
      if (!waiting && ana.status !== 'queued' && ana.status !== 'running') kids.push(anaBtn(ana.status === 'failed' ? t('Listen again') : t('Analyse the recording'), '', queueAnalysis, 'audio-pill-btn'));
      anaBody.replaceChildren(...kids);
      renderAnaChip('none', sum.textContent);
      return;
    }
    dot.dataset.state = p.state;
    $('sync-analysis-title').textContent = t('Analysed');
    const why = whyText(p);
    sum.textContent = p.state === 'unsure'
      ? (why || t('the beat is unclear'))
      : t('%s · %s · bar 1 at %s · %s sure', anaBpm(p.tempo, p.timeSig, true), p.timeSig, fmt(p.firstDownbeat), `${pct(p.confidence)}%`);
    renderAnaChip(p.state, p.state === 'good' ? t('analysis|good') : p.state === 'check' ? t('analysis|to check') : t('analysis|unsure'));
    if (review) { renderReview(); return; }
    const out = [];
    // Why to trust it, or not: the chart's bars against the recording's.
    if (p.writtenBars || p.chartBars) {
      const totals = p.writtenBars ? t('chart %d bars · recording %d bars', p.writtenBars, p.playedBars) : tn(p.chartBars, 'chart %d bar', 'chart %d bars');
      out.push(el('p', `sync-ana-totals${why ? ' is-off' : ''}`, why ? `${totals} · ${why}` : totals));
    }
    // The beat grid's one-tap fixes: ×2 or ÷2 first when the bars say
    // the recording is counted in another octave.
    const fixes = el('div', 'sync-ana-fixes');
    const tf = hints.tempoFactor || p.autoTempoFactor || 0;
    const x2 = anaBtn(t('×2'), t('the beat is twice as fast'), () => setHint({ tempoFactor: tf && tf < 1 ? 1 : 2 }));
    const d2 = anaBtn(t('÷2'), t('the beat is half as fast'), () => setHint({ tempoFactor: tf > 1 ? 1 : 0.5 }));
    x2.setAttribute('aria-pressed', String(tf === 2));
    d2.setAttribute('aria-pressed', String(tf > 0 && tf < 1));
    if (p.suggestTempoFactor === 2) x2.classList.add('is-suggested');
    if (p.suggestTempoFactor === 0.5) d2.classList.add('is-suggested');
    fixes.append(
      ...(p.suggestTempoFactor === 0.5 ? [d2, x2] : [x2, d2]),
      anaBtn(t('−1 beat'), t('the downbeat is one beat earlier'), () => setHint({ shiftBeats: hints.shiftBeats - 1 })),
      anaBtn(t('+1 beat'), t('the downbeat is one beat later'), () => setHint({ shiftBeats: hints.shiftBeats + 1 })),
      anaBtn(t('Bar 1 here'), t('the chart’s first bar starts where the recording is now'), () => setHint({ bar1At: Math.max(0.01, P.audio.currentTime) })),
    );
    if ((hints.tempoFactor && hints.tempoFactor !== 1) || hints.shiftBeats || hints.bar1At || p.autoTempoFactor) {
      // "As detected" is the beats as the tracker heard them, without the
      // reading the analysis took on its own either (tempoFactor 1).
      fixes.append(anaBtn(t('As detected'), t('forget these fixes'), () => { hints = { tempoFactor: p.autoTempoFactor ? 1 : 0, shiftBeats: 0, bar1At: 0 }; loadAnalysis(); }, 'sync-btn sync-ana-reset'));
    }
    out.push(fixes);
    (p.notes || []).forEach((n) => out.push(el('p', 'hint sync-ana-note', n)));
    // The sync, in one tap (never over a Guitar Pro file's own, nor when
    // the analysis is unsure): the starts set by ear stay.
    if (!gpOwnSync() && cat !== 'gp' && p.state !== 'unsure') {
      const setN = rows.filter((r) => r.t !== null && r.origin === 'set').length;
      const use = el('div', 'sync-ana-use');
      const btn = anaBtn(setN ? t('Place the others') : t('Use this sync'),
        setN ? tn(setN, 'the %d start set by ear stays; the others on their downbeats and every bar placed, as estimates to check by ear', 'the %d starts set by ear stay; the others on their downbeats and every bar placed, as estimates to check by ear') : t('every section on its downbeat and every bar placed, saved as estimates to check by ear'),
        useSync, 'primary');
      btn.disabled = setN === rows.length && rows.length > 0;
      use.append(btn, el('span', 'hint', btn.disabled ? t('every start is set by ear') : tn((p.barPoints || []).length, '%d bar placed', '%d bars placed')));
      out.push(use);
    }
    // The chart's proposals.
    const props = el('ul', 'sync-ana-props');
    const item = (checked, onchange, text, extra, at, key) => {
      const li = el('li', 'sync-ana-prop');
      if (key) li.dataset.fix = key;
      const lab = el('label');
      const box = el('input');
      box.type = 'checkbox';
      box.dataset.focus = `ana-pick:${key || text}`;
      box.checked = checked;
      box.addEventListener('change', () => { onchange(box.checked); renderAnalysis(); });
      lab.append(box, el('span', '', text));
      li.append(lab);
      if (at !== undefined && at !== null && at >= 0) li.append(listen(at));
      if (extra) li.append(el('code', 'sync-ana-grid', extra));
      props.append(li);
    };
    if (p.proposeTempo) item(picks.tempo, (v) => { picks.tempo = v; }, t('Tempo: %s → %s', +p.chartTempo > 0 ? bpmText(+p.chartTempo) : t('none'), bpmText(p.proposeTempo)));
    if (p.proposeTime) item(picks.time, (v) => { picks.time = v; }, t('Time signature: %s → %s', p.chartTimeSig || t('none'), p.proposeTime));
    if (p.intro) item(picks.intro, (v) => { picks.intro = v; }, tn(p.intro.bars, 'Add an intro of %d bar before the first section', 'Add an intro of %d bars before the first section'), p.intro.grid, p.firstDownbeat);
    for (const s of p.sections || []) {
      if (!s.grid) continue;
      item(picks.sections.has(s.index), (v) => { if (v) picks.sections.add(s.index); else picks.sections.delete(s.index); },
        t('%s: written %d · played %d bars: use the played ones', p.labels[s.index] || s.label, s.written, s.played), s.grid, s.start);
    }
    for (const f of p.chordFixes || []) {
      const k = fixKey(f);
      const alt = f.alternatives && f.alternatives.length ? ` ${t('(or %s)', f.alternatives.join(', '))}` : '';
      item(picks.chords.has(k), (v) => { if (v) picks.chords.add(k); else picks.chords.delete(k); },
        t('%s, bar %d: the chart says %s, the recording plays %s (%s sure)', p.labels[f.section] || '', f.secBar, f.chart, f.heard, `${pct(f.confidence)}%`) + alt, '', f.time, k);
    }
    if (props.children.length) {
      out.push(el('p', 'sync-ana-props-head', t('The chart, against the recording')));
      out.push(props);
      const n = picks.sections.size + picks.chords.size + (picks.tempo ? 1 : 0) + (picks.time ? 1 : 0) + (picks.intro ? 1 : 0);
      const row = el('div', 'sync-ana-use');
      const rv = anaBtn(n ? tn(n, 'Review %d change', 'Review %d changes') : t('Review the changes'), t('see the lines of the chart this changes before saving them'), startReview, 'audio-pill-btn');
      rv.disabled = !n;
      row.append(rv);
      out.push(row);
    } else {
      out.push(el('p', 'hint', p.suggestTempoFactor || p.why ? t('No change to the chart is proposed until its bars and the recording’s agree.') : t('The chart’s bars and chords agree with the recording.')));
    }
    anaBody.replaceChildren(...out);
  }
  function setHint(h) {
    hints = { ...hints, ...h };
    if (h.tempoFactor !== undefined || h.shiftBeats !== undefined) hints.bar1At = h.bar1At || 0;
    loadAnalysis();
  }
  async function queueAnalysis() {
    try {
      await fetch(`/api/songs/${P.variantId}/audio/${cat}/analysis/queue`, { method: 'POST' });
    } catch (e) { /* shown as before */ }
    loadAnalysis();
  }
  function fixBody() {
    return { tempo: picks.tempo, time: picks.time, intro: picks.intro, sections: [...picks.sections], chords: [...picks.chords] };
  }
  async function apply(body) {
    const res = await fetch(`/api/songs/${P.variantId}/audio/${cat}/analysis/apply?lang=${encodeURIComponent(document.documentElement.lang || '')}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ hints, ...body }),
    });
    if (!res.ok) throw new Error(String(res.status));
    return res.json();
  }
  // The sync as the analysis places it, saved as estimates; the rows then
  // show it, to check by ear and confirm.
  // The starts set by ear stay (the server keeps them); what it replaces
  // can be undone (Undo, then Save) or taken back from the History.
  async function useSync(e) {
    const btn = e.currentTarget;
    if (anaBusy) return;
    if (dirty && !window.confirm(t('Replace your unsaved changes with the recording’s sync?'))) return;
    anaBusy = true;
    window.Forms?.busy?.(btn, true);
    try {
      const r = await apply({ sync: true, fix: {} });
      const before = snapshot();
      const keep = rows;
      load(r.markers || []);
      rows.forEach((x, i) => { x.label = keep[i]?.label || x.label; });
      history.push(before);
      markSaved();
      // The player follows the sync saved (markers and every bar).
      await P.refreshSync?.(cat);
      afterChange();
      P.setPreview(null);
      say(r.kept ? tn(r.kept, 'The recording’s sync is saved as an estimate, the %d start set by ear kept: check the others by ear, then confirm each (✓).', 'The recording’s sync is saved as an estimate, the %d starts set by ear kept: check the others by ear, then confirm each (✓).')
        : t('The recording’s sync is saved, as an estimate: check it by ear, then confirm each start (✓).'), 'ok');
    } catch (err) {
      say(t("Couldn't save. Try again."), 'error');
    }
    window.Forms?.busy?.(btn, false);
    anaBusy = false;
  }
  async function startReview() {
    try {
      const r = await apply({ fix: fixBody(), dryRun: true });
      review = { changes: r.changes || [] };
    } catch (e) {
      say(t("Couldn't prepare the changes. Try again."), 'error');
      return;
    }
    renderAnalysis();
  }
  function renderReview() {
    const out = [el('p', 'sync-ana-props-head', t('These lines of the chart change:'))];
    const list = el('ul', 'sync-ana-diff');
    for (const c of review.changes) {
      const li = el('li');
      li.append(el('strong', '', c.where));
      if (c.before) li.append(el('code', 'sync-ana-before', c.before));
      li.append(el('code', 'sync-ana-after', c.after || t('(removed)')));
      list.append(li);
    }
    out.push(list);
    const resync = el('label', 'audio-check');
    const box = el('input');
    box.type = 'checkbox';
    box.checked = true;
    box.id = 'sync-ana-resync';
    resync.append(box, document.createTextNode(` ${t('and place the sync from the recording')}`));
    out.push(resync);
    const row = el('div', 'sync-ana-use');
    row.append(
      anaBtn(t('Save as a revision'), t('save the chart with these changes, like an edit'), saveReview, 'primary'),
      anaBtn(t('Back'), '', () => { review = null; renderAnalysis(); }, 'audio-pill-btn'),
    );
    out.push(row);
    anaBody.replaceChildren(...out);
  }
  async function saveReview(e) {
    const editor = document.getElementById('editor');
    if (editorDirty()) {
      say(t('Save or discard your edits to the song first.'), 'error');
      return;
    }
    const btn = e.currentTarget;
    window.Forms?.busy?.(btn, true);
    try {
      const r = await apply({ fix: fixBody(), sync: !!$('sync-ana-resync')?.checked, version: editor ? editor.dataset.version : '' });
      if (r.saved || r.proposal) {
        // The page reloads with the new chart, and the sheet opens again.
        say(t('Saved. Reloading the song…'), 'ok');
        try { sessionStorage.setItem('songbooks:reopen-sync', cat); } catch (err) { /* reopen by hand */ }
        dirty = false;
        window.location.reload();
        return;
      }
      window.Forms?.busy?.(btn, false);
      review = null;
      renderAnalysis();
      say(t('Nothing to save: the chart already says this.'), 'ok');
    } catch (err) {
      window.Forms?.busy?.(btn, false);
      say(t('Couldn’t save: the song changed meanwhile? Reload and try again.'), 'error');
    }
  }
  // A row's section, as the analysis has it.
  function anaSection(index) {
    const p = ana && ana.proposal;
    return p ? (p.sections || []).find((s) => s.index === index) : null;
  }

  // ---- Opening, closing ----------------------------------------------------------
  let lastFocus = null;
  let openToken = 0; // a newer open, or a close, ends an open still waiting
  async function open(opts = {}) {
    const token = ++openToken;
    const want = opts.category || null;
    if (want && want !== P.category()) P.selectCategory(want);
    sheet.hidden = false;
    document.documentElement.classList.add('sync-sheet-open');
    lastFocus = document.activeElement;
    statusEl.textContent = '';
    historyEl.hidden = true;
    clearInterval(timer);
    // Wait for the recording: a category just switched to decodes first,
    // and one just added (the wizard's download) comes with the player's
    // next look at the song's recordings. Never another one's.
    const here = () => P.ready() && (!want || P.category() === want);
    for (let i = 0; !here() && i < 300; i++) {
      say(t('Loading the recording…'));
      if (want && P.category() !== want && P.categories().includes(want)) P.selectCategory(want);
      await new Promise((r) => setTimeout(r, 100));
      if (token !== openToken || !isOpen()) return;
    }
    if (!here()) {
      rows = [];
      rowsEl.replaceChildren();
      say(t("The recording didn’t load: close this and try again in a moment."), 'error');
      return;
    }
    statusEl.textContent = '';
    history = [];
    lags = [];
    tempoEdited = false;
    load(P.syncMarkers());
    markSaved();
    beatsFor = null;
    detectWanted = false;
    ana = null;
    review = null;
    hints = { tempoFactor: 0, shiftBeats: 0, bar1At: 0 };
    picks = { tempo: false, time: false, intro: false, sections: new Set(), chords: new Set() };
    renderAnalysis();
    // A proposal handed in (the wizard's, Auto-align's) is a set of
    // suggestions to accept or reject, not a new sync.
    sugg = {};
    rejected = new Set();
    suggNotes = opts.notes || [];
    if (opts.proposal) addSuggestions(opts.proposal, opts.source || 'given');
    // Start where the sync still needs a person.
    const first = rows.findIndex((r) => r.origin !== 'set');
    cursor = first >= 0 ? first : 0;
    // The waveform: the whole recording first, following the playhead.
    ensureWave();
    if (wave) {
      wave.restyle();
      wave.resize();
      wave.setView(WM.fit(P.duration()));
      wave.setFollow(true);
      loadPeaks();
    }
    // A wide screen has a column for the analysis: open.
    if (anaEl && window.matchMedia?.('(min-width: 1101px)').matches) anaEl.open = true;
    render();
    renderVoice();
    loadTargets();
    loadAnalysis().finally(() => {
      if (token !== openToken) return;
      detectWanted = !hasAnaBeats();
      scheduleDetect();
    });
    loadChords();
    clearInterval(timer);
    timer = setInterval(tick, 50);
    tick();
    tapBtn.focus({ preventScroll: true });
    if (!window.matchMedia?.('(max-width: 720px)').matches) rowsEl.children[cursor]?.scrollIntoView({ block: 'nearest' });
  }
  function close() {
    if (dirty && !window.confirm(t('Leave without saving the sync?'))) return;
    openToken++;
    dirty = false;
    sheet.hidden = true;
    document.documentElement.classList.remove('sync-sheet-open');
    clearInterval(timer);
    detectWanted = false;
    P.setPreview(null);
    lastFocus?.focus?.({ preventScroll: true });
  }
  // With the voice turned down the player plays the accompaniment: the
  // sung starts are placed without hearing the singer. Say so, with the
  // voice a tap away.
  function renderVoice() {
    const b = $('sync-voice');
    if (!b) return;
    const off = !!P.voiceDown?.();
    b.hidden = !off;
  }

  // ---- Wiring --------------------------------------------------------------------
  // A tap counts when the finger goes down, not when it lifts.
  tapBtn.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    tap();
  });
  tapBtn.addEventListener('click', (e) => { if (e.detail === 0) tap(); }); // Enter / Space on it
  playBtn.addEventListener('click', () => (P.isPlaying() ? P.pause() : play()));
  $('sync-back').addEventListener('click', () => P.seek(Math.max(0, P.audio.currentTime - 5)));
  // The very start of the recording, whatever the section or the lead.
  $('sync-start')?.addEventListener('click', () => P.seek(0));
  clickBtn.addEventListener('click', () => { P.toggleClick(); tick(); });
  $('sync-speed').addEventListener('click', (e) => { const b = e.target.closest('button[data-v]'); if (b) { P.setSpeed(+b.dataset.v); tick(); } });
  undoBtn.addEventListener('click', undo);
  saveBtn.addEventListener('click', save);
  historyBtn.addEventListener('click', showHistory);
  $('sync-suggest-btn').addEventListener('click', suggest);
  $('sync-accept-all').addEventListener('click', acceptAll);
  sheet.querySelectorAll('[data-sync-close]').forEach((el) => el.addEventListener('click', close));
  $('sync-zoom-in').addEventListener('click', () => wave && wave.zoom(2));
  $('sync-zoom-out').addEventListener('click', () => wave && wave.zoom(0.5));
  $('sync-zoom-fit').addEventListener('click', () => wave && wave.fit());
  followBtn.addEventListener('click', () => wave && wave.setFollow(!wave.following()));
  snapBox.addEventListener('change', () => snapBox.closest('label')?.classList.toggle('active', snapBox.checked));
  snapBox.closest('label')?.classList.toggle('active', snapBox.checked);
  // A theme switched while it's open: the waveform's colours follow.
  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener?.('change', () => wave && wave.restyle());
  const themed = new MutationObserver(() => wave && isOpen() && wave.restyle());
  themed.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  themed.observe(document.body, { attributes: true, attributeFilter: ['data-theme'] });
  $('sync-voice')?.addEventListener('click', () => { P.toggleVoice?.(); renderVoice(); });
  document.addEventListener('songbooks:voice-changed', () => { if (isOpen()) renderVoice(); });
  $('sync-ana-chip')?.addEventListener('click', () => {
    if (!anaEl) return;
    anaEl.open = true;
    anaEl.scrollIntoView({ block: 'start', behavior: 'smooth' });
    anaEl.querySelector('summary')?.focus({ preventScroll: true });
  });
  // Typing goes to a text field; a checkbox (Snap, a proposal) doesn't
  // take the sheet's keys away.
  const typing = (el) => !!el.closest?.('select, textarea, [contenteditable]') || (el.tagName === 'INPUT' && !['checkbox', 'radio', 'button', 'range'].includes(el.type));
  // Enter and Space are a control's own on a button, a summary, a link, a
  // checkbox, the waveform: elsewhere they tap and play.
  const ownsKeys = (el) => el !== tapBtn && !!el.closest?.('button, summary, a[href], input, canvas, [role="button"]');
  function focusables() {
    return [...sheet.querySelectorAll('button, [href], input, select, textarea, summary, [tabindex]:not([tabindex="-1"])')]
      .filter((x) => !x.disabled && !x.closest('[hidden]') && x.getClientRects().length && !(x.closest('details:not([open])') && x.tagName !== 'SUMMARY' && !x.closest('summary')));
  }
  // Keys while it's open, before the player's own (window, capture).
  window.addEventListener('keydown', (e) => {
    if (!isOpen() || typing(e.target)) return;
    let handled = true;
    const k = e.key;
    if (k === 'Tab') {
      // A modal dialog: Tab stays inside it.
      const f = focusables();
      if (!f.length) return;
      const i = f.indexOf(document.activeElement);
      if (e.shiftKey && i <= 0) f[f.length - 1].focus();
      else if (!e.shiftKey && (i === f.length - 1 || i < 0)) f[0].focus();
      else return;
      e.preventDefault();
      return;
    }
    if ((e.metaKey || e.ctrlKey) && (k === 'z' || k === 'Z')) undo();
    else if (e.metaKey || e.ctrlKey || e.altKey) handled = false;
    else if (k === 'Escape' && !historyEl.hidden) { historyEl.hidden = true; historyBtn.setAttribute('aria-expanded', 'false'); historyBtn.focus(); }
    else if (k === 'Escape') close();
    else if (k === ' ' || k === 'Spacebar') { if (ownsKeys(e.target)) handled = false; else (P.isPlaying() ? P.pause() : play()); }
    else if (k === 'Enter') { if (ownsKeys(e.target) || e.target === tapBtn) handled = false; else tap(); }
    else if (k === 't' || k === 'T') tap();
    // The playhead: Home, the very start; ← → on the waveform step it.
    // Elsewhere ← → nudge the section picked (as before).
    else if (k === 'Home') P.seek(0);
    else if (k === 'End') P.seek(Math.max(0, P.duration() - 5));
    else if ((k === 'ArrowLeft' || k === 'ArrowRight') && e.target && e.target.id === 'sync-wave-canvas') {
      const step = (e.shiftKey ? 5 : 1) * (k === 'ArrowLeft' ? -1 : 1);
      P.seek(Math.max(0, Math.min(P.duration(), P.audio.currentTime + step)));
    }
    else if (k === 'ArrowLeft') nudge(cursor, e.shiftKey ? -barLen(Math.max(0, cursor - 1)) : -0.1);
    else if (k === 'ArrowRight') nudge(cursor, e.shiftKey ? barLen(cursor) : 0.1);
    else if (k === 'ArrowUp') select(cursor - 1);
    else if (k === 'ArrowDown') select(cursor + 1);
    else if (k === 'k' || k === 'K') { P.toggleClick(); tick(); }
    else if (k === 'z' || k === 'Z') undo();
    else if (wave && (k === '+' || k === '=')) wave.zoom(2);
    else if (wave && (k === '-' || k === '_')) wave.zoom(0.5);
    else if (wave && k === '0') wave.fit();
    else if (wave && (k === 'f' || k === 'F')) wave.setFollow(!wave.following());
    else handled = false;
    if (handled) { e.preventDefault(); e.stopImmediatePropagation(); }
  }, true);
  window.addEventListener('beforeunload', (e) => { if (isOpen() && dirty) { e.preventDefault(); e.returnValue = ''; } });

  window.songbooksSyncSheet = { open, close, isOpen };
  // Reopened after its chart fixes were saved (the page reloads with the
  // new chart).
  try {
    const again = sessionStorage.getItem('songbooks:reopen-sync');
    if (again) {
      sessionStorage.removeItem('songbooks:reopen-sync');
      const go = () => open({ category: again });
      if (P.ready()) go(); else setTimeout(go, 300);
    }
  } catch (e) { /* not reopened */ }
})();

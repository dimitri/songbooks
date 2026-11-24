// Guitar Pro import wizard (GP Import tab, play.html). Parses a .gp/
// .gp3/.gp4/.gp5/.gpx file entirely client-side, using the vendored
// alphaTab library's own importer (already shipped for web rendering —
// see alphatab-tab.js) rather than a hand-rolled Go parser: it already
// handles every GP format version, including the bend-curve/repeat/
// alternate-ending encodings a from-scratch parser would have to
// reverse-engineer from scratch. Confirmed against real files from a
// real GuitarPro lesson library this session: alphaTab.importer.
// ScoreLoader.loadScoreFromBytes() parses GP8's zip+XML container
// cleanly, exposes the embedded backing-track mp3 verbatim
// (score.backingTrack.rawAudioFile) and real per-bar audio sync
// (masterBar.syncPoints[].syncPointValue.millisecondOffset) — no manual
// zip/XML parsing needed at all.
//
// Score source written for an imported section is NOT LilyPond-subset
// text — it's the section's internal/tab.Riff encoded as plain JSON,
// decoded by the same field names Go's json package would produce (see
// tab.DecodeScoreSource/DecodeRiffJSON). A GP file already carries the
// exact fret/string a human chose for every note; re-deriving it through
// ParseScore's own assignFrets (pitch -> lowest-available-fret) could
// silently pick a different fingering than the one actually tabbed out.
//
// GP string numbering is the OPPOSITE of this app's own convention:
// GP's note.string is 1 = LOWEST (thickest) string; this app's
// Note.String is 1 = HIGHEST, matching real LilyPond/TabStaff (see
// internal/tab/tab.go's Riff.Tuning comment). Confirmed empirically this
// session (a file's very first note: GP string=1, fret=0, realValue=40
// — open low E) — every note's string is remapped on the way in
// (numStrings + 1 - gpString).
//
// A 3-step wizard (file -> align bars -> import), not a single form:
// the embedded backing track routinely starts with a count-in or
// silent bars the chart has no bar for, so "where does our bar 0
// actually start in this file" needs a real answer before anything
// saves, not a guessed number.
//
// The align step is an Audacity-3-style timeline — one continuous
// waveform with a labeled-region lane below it — not a grid of per-bar
// notation cards (an earlier version of this file did that; a direct
// review found it hard to scan and disconnected from the actual
// problem, which is a LISTENING problem, not a reading-notation one).
// Section boundaries are SHARED between adjacent sections (the end of
// one section IS the start of the next), so there's one draggable
// handle per boundary, not independent start/end handles per section —
// simpler, and it can't produce gaps or overlaps. The bar grid overlaid
// on the waveform comes from the GP file's own real per-bar sync
// points, never from audio analysis: a from-scratch energy-flux beat
// detector was prototyped and tested against a real file with known
// ground-truth bar times this session, and only matched the true grid
// on 16% of its detected onsets (worse, it reported an identical,
// clearly-bogus tempo on two unrelated songs) — nowhere near reliable
// enough to drive bar placement. GP's own sync points are real,
// human-authored-and-synced data; there's no reason to approximate them
// with an unreliable DSP guess when they're already right there.
(function () {
  const panel = document.getElementById('gpimport-panel');
  if (!panel) return;
  const variantId = panel.dataset.variantId;

  const fileInput = document.getElementById('gpimport-file');
  const fileSourceNote = document.getElementById('gpimport-file-source');
  const status = document.getElementById('gpimport-status');
  const trackRow = document.getElementById('gpimport-track-row');
  const trackSelect = document.getElementById('gpimport-track');
  const mappingBody = document.getElementById('gpimport-mapping');
  const mappingTable = document.getElementById('gpimport-mapping-table');
  const importBtn = document.getElementById('gpimport-run');
  const includeAudio = document.getElementById('gpimport-include-audio');
  const audioRow = document.getElementById('gpimport-audio-row');
  const summary = document.getElementById('gpimport-summary');
  const previewHolder = document.getElementById('gpimport-preview');
  const headerHolder = document.getElementById('gpimport-header');
  const deriveRow = document.getElementById('gpimport-derive-row');
  const deriveBtn = document.getElementById('gpimport-derive-chords');

  const searchTitleInput = document.getElementById('gpimport-search-title');
  const searchArtistInput = document.getElementById('gpimport-search-artist');
  const searchBtn = document.getElementById('gpimport-search-btn');
  const searchResults = document.getElementById('gpimport-search-results');
  const searchSkip = document.getElementById('gpimport-search-skip');

  const contentPreviewEl = document.getElementById('gpimport-content-preview');
  const useMarkersRow = document.getElementById('gpimport-use-markers-row');
  const useMarkersBtn = document.getElementById('gpimport-use-markers');

  const scrollWrap = document.getElementById('gpimport-scroll');
  const timeline = document.getElementById('gpimport-timeline');
  const waveformCanvas = document.getElementById('gpimport-waveform');
  const bargridEl = document.getElementById('gpimport-bargrid');
  const labeltrackEl = document.getElementById('gpimport-labeltrack');
  const boundariesEl = document.getElementById('gpimport-boundaries');
  const playheadEl = document.getElementById('gpimport-playhead');
  const zoomInBtn = document.getElementById('gpimport-zoom-in');
  const zoomOutBtn = document.getElementById('gpimport-zoom-out');
  const zoomLabel = document.getElementById('gpimport-zoom-label');
  const playBtn = document.getElementById('gpimport-play');
  const timeLabel = document.getElementById('gpimport-time-label');

  const steps = {
    fit: document.getElementById('gpimport-step-fit'),
    search: document.getElementById('gpimport-step-search'),
    file: document.getElementById('gpimport-step-file'),
    align: document.getElementById('gpimport-step-align'),
    check: document.getElementById('gpimport-step-check'),
    import: document.getElementById('gpimport-step-import'),
  };
  const step1Next = document.getElementById('gpimport-step1-next');
  const step2Back = document.getElementById('gpimport-step2-back');
  const step2Next = document.getElementById('gpimport-step2-next');
  const step3Back = document.getElementById('gpimport-step3-back');

  // Step 1: what the file is for. "beside": the full score in the song's
  // Score tab, the chart still the song's main view; "whole": the file
  // is the song ("content: gp"), it opens on the score; both are checked
  // against the chart (step 3, "Check": where the chart starts in the
  // file, bar counts, chords, tempo, time, key). "parts": the file's bars
  // are lined up with the song's sections, one "gp[Track]:" line each.
  const FITS = ['beside', 'parts', 'whole'];
  let fit = FITS.includes(panel.dataset.songShape) ? panel.dataset.songShape : 'whole';
  const lining = () => fit === 'parts';
  const fitRadios = [...panel.querySelectorAll('input[name="gpimport-fit"]')];
  fitRadios.forEach((r) => { r.checked = r.value === fit; r.addEventListener('change', () => { if (r.checked) { fit = r.value; applyFit(); } }); });
  const stepper = document.getElementById('gpimport-stepper');
  const ORDER = ['fit', 'file', 'align', 'import'];
  function applyFit() {
    step1Next.textContent = lining() ? t('Next: line up the sections →') : t('Next: check it against the chart →');
    trackRow.hidden = !lining() || !score || score.tracks.length < 2;
    deriveRow.hidden = !lining() || !score || ourSections.length > 0;
    // The third step lines the file up (inside the chart), or checks it
    // against the chart (next to it, or the whole song).
    const third = stepper.querySelector('[data-step="align"]');
    third.textContent = lining() ? third.dataset.parts : third.dataset.check;
  }
  const SLOT = { search: 'file', check: 'align' };
  function goToStep(name, scroll = true) {
    Object.entries(steps).forEach(([k, el]) => { el.hidden = k !== name; });
    const at = ORDER.indexOf(SLOT[name] || name);
    stepper.querySelectorAll('li').forEach((li) => {
      const i = ORDER.indexOf(li.dataset.step);
      li.classList.toggle('current', i === at);
      li.classList.toggle('done', i < at);
    });
    if (scroll) panel.scrollIntoView({ block: 'nearest' });
  }
  document.getElementById('gpimport-fit-next').addEventListener('click', () => { applyFit(); goToStep(score ? 'file' : 'search'); });
  panel.querySelectorAll('.gpimport-back').forEach((b) => b.addEventListener('click', () => goToStep(b.dataset.back)));
  goToStep('fit', false);
  applyFit();

  // ---- Step 0: search GProTab.net (handleImportGPFind, handlers_import.go)
  // for a Guitar Pro file, as an alternative to picking one already on
  // disk. Title/artist are prefilled from this song's own header so the
  // common case ("find a file for THIS song") is a single click; both
  // stay editable for anything else. See that handler's own doc comment
  // for why GProTab.net rather than Ultimate Guitar.
  searchTitleInput.value = panel.dataset.songTitle || '';
  searchArtistInput.value = panel.dataset.songArtist || '';

  async function runSearch() {
    const title = searchTitleInput.value.trim();
    // `required` (checked by forms.js before submit) lets only-spaces through.
    if (!title) { Forms.fieldError(searchTitleInput, t('Enter the song title to search for.')); searchTitleInput.focus(); return; }
    Forms.busy(searchBtn, true);
    searchResults.innerHTML = '';
    setStatus(t('Searching GProTab.net…'));
    try {
      const qs = new URLSearchParams({ title, artist: searchArtistInput.value.trim() });
      const res = await fetch(`/api/import/gp/find?${qs}`);
      if (!res.ok) throw new Error(await res.text());
      const { candidates } = await res.json();
      if (!candidates || !candidates.length) {
        setStatus(t('Nothing on GProTab.net for that title. Try a different spelling, or choose a file from your computer.'), 'err');
        return;
      }
      setStatus(tn(candidates.length, 'Found %d file.', 'Found %d files.'), 'ok');
      renderSearchResults(candidates);
    } catch (e) {
      setStatus(t('Couldn’t search GProTab.net just now. Try again, or choose a file from your computer.'), 'err');
    } finally {
      Forms.busy(searchBtn, false);
    }
  }

  function renderSearchResults(candidates) {
    searchResults.innerHTML = '';
    const table = document.createElement('table');
    table.className = 'gpimport-mapping-table';
    table.innerHTML = '<thead><tr>' + ['Title', 'Artist', 'Format', 'Size', 'Rating'].map((h) => `<th>${escapeHTML(t(h))}</th>`).join('') + '<th></th></tr></thead>';
    const tbody = document.createElement('tbody');
    for (const c of candidates) {
      const tr = document.createElement('tr');
      const sizeText = c.SizeKB ? `${c.SizeKB.toFixed(1)} kb` : '—';
      const ratingText = c.Votes ? `${c.Rating.toFixed(1)}/5 (${c.Votes})` : '—';
      tr.innerHTML = `
        <td>${escapeHTML(c.Title)}</td>
        <td>${escapeHTML(c.Artist)}</td>
        <td>${escapeHTML(c.Format || '?')}</td>
        <td>${sizeText}</td>
        <td>${ratingText}</td>
        <td class="gpimport-result-actions"><button type="button" class="gpimport-preview-result">${escapeHTML(t('Preview'))}</button> <button type="button" class="primary gpimport-use-result">${escapeHTML(t('Use this'))}</button></td>`;
      tr.querySelector('.gpimport-use-result').addEventListener('click', () => useSearchResult(c));
      tr.querySelector('.gpimport-preview-result').addEventListener('click', (e) => openPreview(c, e.currentTarget));
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    searchResults.appendChild(table);
  }

  function escapeHTML(s) {
    return (s || '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  // One download per result: a file previewed and then chosen isn't
  // fetched again.
  const fetched = new Map(); // url -> Promise<{ buf, filename, type }>
  function fetchResult(c) {
    if (!fetched.has(c.URL)) {
      fetched.set(c.URL, (async () => {
        const res = await fetch(`/api/import/gp/fetch?url=${encodeURIComponent(c.URL)}`);
        if (!res.ok) throw new Error(await res.text());
        const buf = await res.arrayBuffer();
        let filename = 'import.gp';
        const cd = res.headers.get('Content-Disposition');
        const m = cd && /filename="([^"]+)"/.exec(cd);
        if (m) filename = m[1];
        return { buf, filename, type: res.headers.get('Content-Type') || 'application/octet-stream' };
      })().catch((e) => { fetched.delete(c.URL); throw e; }));
    }
    return fetched.get(c.URL);
  }

  async function useSearchResult(c) {
    setStatus(t('Downloading “%s” from GProTab.net…', c.Title));
    try {
      const { buf, filename, type } = await fetchResult(c);
      fileSourceNote.hidden = false;
      fileSourceNote.innerHTML = `${escapeHTML(t('From GProTab.net:'))} <a href="${escapeHTML(c.URL)}" target="_blank" rel="noopener">${escapeHTML(c.Title)} — ${escapeHTML(c.Artist)}</a>`;
      goToStep('file');
      // A real File, as from the file picker: the import step uploads it
      // (FormData.append wants a Blob).
      await loadFile(new File([buf], filename, { type }));
    } catch (e) {
      setStatus(t('Couldn’t download that file. Try another result, or choose a file from your computer.'), 'err');
    }
  }

  // ---- A search result's preview: before choosing one of the files found,
  // look at it — its whole score, played with alphaTab's synthesizer and
  // its cursor; its tempo, time, key, bars, tracks and sections next to the
  // song's; how it lines up with the chart (the check step's comparison,
  // from where it lines up best). Native <dialog>: Esc closes it, focus
  // stays in it and goes back to the result's Preview button.
  const prev = document.getElementById('gpprev');
  let prevApi = null;
  let prevFor = null;   // { c, opener }
  let prevSeq = 0;
  function closePreview() {
    if (prev.open) prev.close();
  }
  prev.addEventListener('close', () => {
    // The preview's sound stops with it.
    if (prevApi) { try { prevApi.stop(); prevApi.destroy(); } catch (e) { /* gone */ } prevApi = null; }
    prev.querySelector('.gpprev-score').replaceChildren();
    prevSeq++;
    const back = prevFor && prevFor.opener;
    prevFor = null;
    // After the browser's own focus restoring, which may pick another.
    if (back && back.isConnected) setTimeout(() => back.focus(), 0);
  });
  prev.querySelector('.gpprev-x').addEventListener('click', closePreview);
  prev.querySelector('.gpprev-close').addEventListener('click', closePreview);
  prev.addEventListener('click', (e) => { if (e.target === prev) closePreview(); }); // the backdrop
  // Esc closes the preview only, not the editor it was opened from.
  prev.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    closePreview();
  });
  prev.querySelector('.gpprev-use').addEventListener('click', () => {
    const c = prevFor && prevFor.c;
    closePreview();
    if (c) useSearchResult(c);
  });

  async function openPreview(c, opener) {
    const seq = ++prevSeq;
    if (prevApi) { try { prevApi.stop(); prevApi.destroy(); } catch (e) { /* gone */ } prevApi = null; }
    prevFor = { c, opener };
    const q = (sel) => prev.querySelector(sel);
    q('#gpprev-title').textContent = c.Title || t('Guitar Pro file');
    q('.gpprev-sub').textContent = [c.Artist, c.Format].filter(Boolean).join(' · ');
    q('.gpprev-status').textContent = t('Downloading “%s” from GProTab.net…', c.Title);
    q('.gpprev-fit').textContent = '';
    q('.gpprev-fit').className = 'gpprev-fit';
    q('.gpprev-facts').hidden = true;
    q('.gpprev-align-box').hidden = true;
    q('.gpprev-player').hidden = true;
    q('.gpprev-use').disabled = true;
    q('.gpprev-score').replaceChildren();
    if (!prev.open) prev.showModal();
    let file, facts;
    try {
      file = await fetchResult(c);
      if (seq !== prevSeq) return;
      facts = GPFacts.extract(alphaTab.importer.ScoreLoader.loadScoreFromBytes(new Uint8Array(file.buf), new alphaTab.Settings()));
    } catch (e) {
      if (seq === prevSeq) q('.gpprev-status').textContent = t('Couldn’t open this file. Try another result.');
      return;
    }
    q('.gpprev-status').textContent = '';
    q('.gpprev-use').disabled = false;
    drawPreviewScore(new Uint8Array(file.buf), seq);
    try {
      const raw = await (await fetch(`/api/songs/${variantId}/raw`)).json();
      let out = await postCheck(facts, Number(headerValue(raw.text || '', 'gp_start')) || 1);
      // Compared where it lines up best, as the check step's first fix.
      const fix = GPAlign.fixes(out.alignment)[0];
      if (fix) out = await postCheck(facts, fix.start);
      if (seq !== prevSeq) return;
      fillPreviewFacts(raw.text || '', facts, out);
    } catch (e) {
      if (seq === prevSeq) q('.gpprev-status').textContent = t('Couldn’t check the file against the chart: %s', e.message);
    }
  }

  // The file's facts next to the song's, a difference marked.
  function fillPreviewFacts(text, facts, out) {
    const al = out.alignment;
    const tbody = prev.querySelector('.gpprev-facts tbody');
    tbody.replaceChildren();
    const row = (label, mine, theirs, differs) => {
      const tr = document.createElement('tr');
      if (differs) tr.className = 'is-diff';
      for (const [v, tag] of [[label, 'th'], [mine, 'td'], [theirs, 'td']]) {
        const c = document.createElement(tag);
        c.textContent = v || '—';
        tr.appendChild(c);
      }
      tbody.appendChild(tr);
    };
    const songTempo = headerValue(text, 'tempo');
    const songTime = headerValue(text, 'time') || '4/4';
    // The file's tempo counts quarters; said, and compared, in the beat
    // of its own metre (a dotted quarter in 6/8), as the song's is.
    const TU = window.SongbooksTempoUnits;
    const ft = (facts.time || [])[0];
    const fileTempo = facts.tempo && TU && ft ? Math.round(facts.tempo / TU.beatQuarters(ft.num, ft.den)) : facts.tempo;
    const tempoSaid = (v, num, den) => (TU && TU.compound(num, den) ? TU.text(+v, num, den, { short: true }) : t('%s bpm', v));
    row(t('Tempo'), songTempo ? tempoSaid(songTempo, ...songTime.split('/')) : t('none (120)'), fileTempo ? tempoSaid(fileTempo, ft ? ft.num : 4, ft ? ft.den : 4) : '', !!fileTempo && String(fileTempo) !== (songTempo || '120'));
    const times = (facts.time || []).map((x) => (x.bar > 1 ? t('%s from bar %d', `${x.num}/${x.den}`, x.bar) : `${x.num}/${x.den}`)).join(', ');
    row(t('Time signature'), headerValue(text, 'time') || t('none (4/4)'), times, !!facts.time.length && `${facts.time[0].num}/${facts.time[0].den}` !== songTime || facts.time.length > 1);
    const fk = GPAlign.keyName((facts.keys || [])[0]);
    const sk = headerValue(text, 'key');
    row(t('Key'), sk || t('key|none'), fk || t('not set (C)'), !!fk && fk.toLowerCase() !== sk.toLowerCase());
    const played = (facts.played || []).length;
    row(t('Bars'), al.chartBars ? tn(al.chartBars, '%d bar', '%d bars') : t('none yet'), played === facts.bars ? tn(played, '%d bar', '%d bars') : t('%s (%d written)', tn(played, '%d bar', '%d bars'), facts.bars), !!al.chartBars && al.chartBars !== played - (al.start - 1));
    row(t('Tracks'), '', (facts.tracks || []).map((x) => x.name || t('Track %d', x.n)).join(', '), false);
    const secs = al.sections.length;
    row(t('Sections'), secs ? tn(secs, '%d section', '%d sections') : t('none yet'), (facts.markers || []).length ? `${tn(facts.markers.length, '%d section', '%d sections')}: ${facts.markers.map((m) => m.text).join(', ')}` : t('no section markers'), secs > 0 && (facts.markers || []).length !== secs);
    prev.querySelector('.gpprev-facts').hidden = false;

    const fit = prev.querySelector('.gpprev-fit');
    const left = GPAlign.remaining(al, out.header, null, out.items);
    if (!al.sections.length) {
      fit.textContent = t('The song has no bars yet to compare the file with.');
    } else if (!left.total) {
      fit.className = 'gpprev-fit is-ok';
      fit.textContent = al.start > 1 ? t('Lines up with the chart, from the file’s bar %d.', al.start) : t('Lines up with the chart.');
    } else {
      fit.className = 'gpprev-fit is-off';
      const ok = al.sections.filter((r) => r.status === 'ok').length;
      fit.textContent = (al.start > 1 ? t('From the file’s bar %d: ', al.start) : '') + t('%s of %s line up; %s.', String(ok), tn(al.sections.length, '%d section', '%d sections'), tn(left.total, '%d difference', '%d differences'));
    }
    const box = prev.querySelector('.gpprev-align-box');
    box.hidden = !al.sections.length;
    let picked = Math.max(0, al.sections.findIndex((r) => r.status !== 'ok' && !(r.status === 'shifted' && !r.own)));
    const draw = () => drawAlignment(prev.querySelector('.gpprev-align'), al, { picked, onPick: (i) => { picked = i; draw(); } });
    draw();
  }

  // The whole score, every track to pick, played with the synthesizer.
  function drawPreviewScore(bytes, seq) {
    const holder = prev.querySelector('.gpprev-score');
    const player = prev.querySelector('.gpprev-player');
    const play = prev.querySelector('.gpprev-play');
    const chips = prev.querySelector('.gpprev-tracks');
    const time = prev.querySelector('.gpprev-time');
    chips.replaceChildren();
    time.textContent = '';
    play.disabled = true;
    const playIcon = play.dataset.icon || (play.dataset.icon = play.innerHTML);
    play.innerHTML = playIcon;
    const sc = alphaTab.importer.ScoreLoader.loadScoreFromBytes(bytes, new alphaTab.Settings());
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.display.layoutMode = alphaTab.LayoutMode.Page;
    s.player.enablePlayer = true;
    s.player.enableCursor = true;
    s.player.enableUserInteraction = true;
    // The file's own subset of the soundfont (gp-soundfont.js).
    const soundFontURL = window.GPSoundFont ? GPSoundFont.player(s, sc) : (s.player.soundFont = '/static/js/vendor/alphatab/soundfont/sonivox.sf2');
    s.player.scrollElement = prev.querySelector('.gpprev-body');
    if (alphaTab.PlayerMode) s.player.playerMode = alphaTab.PlayerMode.EnabledSynthesizer;
    for (const n of ['ScoreCopyright']) if (alphaTab.NotationElement[n] !== undefined) s.notation.elements.set(alphaTab.NotationElement[n], false);
    let shown = new Set(sc.tracks.length <= 3 ? sc.tracks.map((x, i) => i) : [0]);
    const draw = (tries) => {
      if (seq !== prevSeq) return;
      if (!holder.clientWidth && tries > 0) { requestAnimationFrame(() => draw(tries - 1)); return; }
      const api = new alphaTab.AlphaTabApi(holder, s);
      prevApi = api;
      if (window.GPSoundFont) GPSoundFont.attach(api, soundFontURL);
      api.renderScore(sc, [...shown].sort((a, b) => a - b));
      api.playerReady.on(() => { play.disabled = false; });
      api.playerStateChanged.on((e) => {
        const on = e.state === 1;
        play.innerHTML = on ? '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.5" height="12" rx="1"/><rect x="11.5" y="4" width="3.5" height="12" rx="1"/></svg>' : playIcon;
        play.setAttribute('aria-label', on ? t('Pause') : t('Play'));
        play.classList.toggle('active', on);
        if (on) document.dispatchEvent(new CustomEvent('songbooks:pause-reference'));
      });
      const fmt = (ms) => { const x = Math.max(0, Math.round(ms / 1000)); return Math.floor(x / 60) + ':' + String(x % 60).padStart(2, '0'); };
      api.playerPositionChanged.on((e) => { time.textContent = fmt(e.currentTime) + ' / ' + fmt(e.endTime); });
    };
    play.onclick = () => { if (prevApi) prevApi.playPause(); };
    sc.tracks.forEach((tr, i) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'gp-track-chip';
      b.textContent = tr.name || t('Track %d', i + 1);
      b.setAttribute('aria-pressed', String(shown.has(i)));
      b.addEventListener('click', () => {
        if (shown.has(i) && shown.size > 1) shown.delete(i); else shown.add(i);
        b.setAttribute('aria-pressed', String(shown.has(i)));
        if (prevApi) {
          const surface = holder.querySelector('.at-surface');
          if (surface) surface.replaceChildren();
          prevApi.renderScore(sc, [...shown].sort((a, b) => a - b));
        }
      });
      chips.appendChild(b);
    });
    player.hidden = false;
    // Each track at its level in the file (gp-levels.js), before alphaTab
    // makes its sound: a Guitar Pro 8 file would otherwise play silent.
    const levels = window.GPLevels ? GPLevels.read(bytes).then((l) => { GPLevels.apply(sc, l); }) : Promise.resolve();
    levels.catch(() => {}).finally(() => draw(60));
  }

  document.getElementById('gpimport-search-form').addEventListener('submit', (e) => { e.preventDefault(); runSearch(); });
  searchSkip.addEventListener('click', () => { fileSourceNote.hidden = true; goToStep('file'); });

  let score = null;
  let rawBytes = null;  // the file's bytes, re-parsed for each drawing
  let rawFile = null;   // the originally-selected .gp/.gp3/.gp4/.gp5/.gpx File — kept so the
                         // import step can also upload the source file itself for download,
                         // alongside the data it extracts from it (see uploadSourceFile)
  let ourSections = []; // [{index, label, barCount, startBar}]
  let realIdx = [];     // real-bar-position -> GP-authored masterBar index
  let barTimes = [];    // real-bar-position -> seconds (interpolated where GP has no sync point for it)
  let syncedPositions = new Set(); // real-bar-positions that carry a REAL GP sync point (buildBarTimes) — every other position in barTimes is interpolated/estimated, not authored data
  let audioEl = null;   // <audio>, source = a Blob URL of the embedded backing track
  let audioDuration = 0;
  let boundaries = [];  // length ourSections.length+1, each a real-bar-position (float, fractional between bars while dragging is in progress is never stored — always snapped to an integer bar)
  let pxPerSec = 20;
  const SECTION_COLORS = ['#af4e39', '#3d7a8a', '#7a5ca8', '#2f8f5b', '#b8860b', '#c0507e'];

  function setStatus(msg, cls) {
    status.textContent = msg;
    status.className = 'hint' + (cls ? ' ' + cls : '');
  }

  // ---- The order the file's bars are played in, as positions into
  // score.masterBars: GPFacts.played (gp-facts.js), the same expansion the
  // server counts a "gp[...]" line's bars with.
  function expandGPBars(masterBars) {
    return GPFacts.played(masterBars).map((n) => n - 1);
  }

  // ---- Real-bar -> seconds. Two layers:
  //
  // 1. A NOMINAL timeline, walked across the real (as-played) bar
  //    sequence using the file's own tempo automations and each bar's
  //    own time signature (beats = numerator * 4/denominator, duration
  //    = beats * 60/tempo) — the same arithmetic any notation program
  //    uses to estimate playback time absent real audio analysis.
  // 2. GP's own real sync points, where present, override the nominal
  //    shape locally: interpolated between adjacent known points
  //    (captures actual human tempo drift a nominal estimate can't),
  //    and used to anchor (shift) the nominal timeline everywhere else.
  //
  // Needed because real sync-point DENSITY varies a lot file to file —
  // confirmed this session: short lesson riffs had one sync point
  // nearly every bar, but a real band's own working chart (Mottainai
  // Structure SD.gp, 45 bars) had exactly ONE. The earlier version of
  // this function only used sync points (extrapolating from the
  // nearest pair's own rate), which degenerates badly below 2 points —
  // with exactly 1, every OTHER bar silently stayed at time 0, pinning
  // the whole bar grid to the left edge. A tempo-aware nominal estimate
  // underneath fixes that for every file, not just sparse ones.
  //
  // Returns realIdx.length + 1 entries, not realIdx.length: the extra
  // synthetic entry at index realIdx.length is "one bar past the last
  // real bar," needed so the LAST boundary (the end of the last mapped
  // section) can actually reach every real bar in the file, including
  // the final one — a boundary is an exclusive end position, so without
  // this a file's last bar could never be included in any section's
  // count (confirmed live: a 13-real-bar file could only ever reach 12
  // mapped bars).
  // Positions are tracked in quarter beats rather than bar indexes: a GP
  // sync point can sit part-way through a bar (its ratioPosition — e.g.
  // ¾ of the way into bar 7 in Florent Passamonti's BB Box lesson), and an
  // anacrusis first bar lasts only as long as its notes, so counting it
  // as a whole bar pushes every later bar a pickup-length late.
  function barBeats(mb) {
    // A free-time bar (mb.isFreeTime — a rubato intro/outro with no
    // fixed tempo, confirmed present in a real file this session)
    // has no well-defined duration to estimate from time signature
    // and tempo alone; a 0-length guess would stack several of them
    // at the same x position on the timeline, so fall back to one
    // nominal 4-beat bar's worth of time instead — still wrong, but
    // spread out and draggable rather than invisible.
    if (mb.isFreeTime) return 4;
    if (mb.isAnacrusis && mb.calculateDuration) return mb.calculateDuration() / 960 || 4;
    return (mb.timeSignatureNumerator || 4) * (4 / (mb.timeSignatureDenominator || 4)) || 4;
  }

  function buildBarTimes() {
    const n = realIdx.length;
    const beatAt = new Array(n + 1).fill(0);  // real-bar-position -> quarter beats from the start
    const nominal = new Array(n + 1).fill(0); // ... -> seconds at the file's own tempo map
    const secPerBeat = [];
    let tempo = score.tempo || 120;
    for (let pos = 0; pos < n; pos++) {
      const mb = score.masterBars[realIdx[pos]];
      if (mb.tempoAutomations && mb.tempoAutomations.length) tempo = mb.tempoAutomations[0].value || tempo;
      secPerBeat[pos] = 60 / tempo;
      beatAt[pos + 1] = beatAt[pos] + barBeats(mb);
      nominal[pos + 1] = nominal[pos] + barBeats(mb) * secPerBeat[pos];
    }
    const nominalAt = (beat) => {
      let pos = 0;
      while (pos < n - 1 && beatAt[pos + 1] <= beat) pos++;
      return nominal[pos] + (beat - beatAt[pos]) * (secPerBeat[pos] || 0.5);
    };

    // Every sync point of every pass through a repeated bar: GP numbers
    // the passes (barOccurence), realIdx lists the bar once per pass.
    const known = []; // [{beat, t}]
    const seen = new Map();
    syncedPositions = new Set();
    realIdx.forEach((gpBarIdx, pos) => {
      const occ = seen.get(gpBarIdx) || 0;
      seen.set(gpBarIdx, occ + 1);
      for (const sp of score.masterBars[gpBarIdx].syncPoints || []) {
        if ((sp.syncPointValue.barOccurence || 0) !== occ) continue;
        const ratio = sp.ratioPosition || 0;
        known.push({ beat: beatAt[pos] + ratio * (beatAt[pos + 1] - beatAt[pos]), t: sp.syncPointValue.millisecondOffset / 1000 });
        if (ratio === 0) syncedPositions.add(pos);
      }
    });
    known.sort((a, b) => a.beat - b.beat);
    if (known.length === 0) return nominal;

    // Between two sync points GP plays at a steady tempo, so bars are
    // linear in beats there; before the first / after the last, shift the
    // nominal timeline's own shape so it passes exactly through that point
    // rather than extrapolating a single borrowed interval's rate.
    const first = known[0], last = known[known.length - 1];
    return beatAt.map((beat) => {
      if (beat <= first.beat) return nominalAt(beat) + first.t - nominalAt(first.beat);
      if (beat >= last.beat) return nominalAt(beat) + last.t - nominalAt(last.beat);
      let i = 0;
      while (known[i + 1].beat < beat) i++;
      const a = known[i], b = known[i + 1];
      return b.beat > a.beat ? a.t + ((beat - a.beat) / (b.beat - a.beat)) * (b.t - a.t) : a.t;
    });
  }

  function nearestBarForTime(t) {
    let lo = 0, hi = barTimes.length - 1;
    if (t <= barTimes[0]) return 0;
    if (t >= barTimes[hi]) return hi;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (barTimes[mid] < t) lo = mid + 1; else hi = mid;
    }
    if (lo > 0 && Math.abs(barTimes[lo - 1] - t) < Math.abs(barTimes[lo] - t)) return lo - 1;
    return lo;
  }

  async function loadFile(file) {
    setStatus(t('Reading the file…'));
    try {
      const buf = new Uint8Array(await file.arrayBuffer());
      score = alphaTab.importer.ScoreLoader.loadScoreFromBytes(buf);
      rawFile = file;
      rawBytes = buf;
    } catch (e) {
      setStatus(t('Couldn’t read this file. Check it’s a Guitar Pro file (.gp, .gp3, .gp4, .gp5 or .gpx).'), 'err');
      score = null;
      return;
    }
    trackSelect.innerHTML = '';
    score.tracks.forEach((tr, i) => {
      const opt = document.createElement('option');
      opt.value = i;
      opt.textContent = tr.name || t('Track %d', i + 1);
      trackSelect.appendChild(opt);
    });
    trackRow.hidden = !lining() || score.tracks.length < 2;
    factsCache = null;
    check = null;
    realIdx = expandGPBars(score.masterBars);
    barTimes = buildBarTimes();
    step1Next.disabled = false;
    const res = await fetch(`/api/songs/${variantId}/structure`);
    if (!res.ok) { setStatus(t('Couldn’t load this song’s sections. Reload the page and try again.'), 'err'); return; }
    const data = await res.json();
    ourSections = (data.sections || []).filter(s => s.barCount > 0);
    boundaries = [0];
    ourSections.forEach(sec => boundaries.push(Math.min(realIdx.length, boundaries[boundaries.length - 1] + sec.barCount)));
    deriveRow.hidden = !lining() || ourSections.length > 0;
    setStatus(t('Read “%s”: %d track(s), %d bar(s), %d section(s) to match up.', score.title || file.name, score.tracks.length, realIdx.length, ourSections.length), 'ok');
  }

  trackSelect.addEventListener('change', () => { if (steps.align.hidden === false) prepareTimeline(); });
  fileInput.addEventListener('change', () => { if (fileInput.files[0]) loadFile(fileInput.files[0]); });

  // ---- Timeline setup: decode the embedded backing track once, build
  // an <audio> element from it for playback/seeking, then lay out.
  async function prepareTimeline() {
    if (!score) return;
    renderNotationPreview();
    setUpSectionMarkers();
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) {
      scrollWrap.hidden = true;
      setStatus(t('This file has no embedded backing track — nothing to align against. Sections will map in their own default order; the import still writes note data for each, just without audio sync. Use the notation preview above to see where sections actually change.'));
      renderMappingTableOnly();
      return;
    }
    scrollWrap.hidden = false;
    if (!audioEl) {
      const bytes = score.backingTrack.rawAudioFile;
      const blob = new Blob([bytes], { type: 'audio/mpeg' });
      audioEl = new Audio(URL.createObjectURL(blob));
      audioEl.addEventListener('loadedmetadata', () => { audioDuration = audioEl.duration; layout(); });
      audioEl.addEventListener('timeupdate', () => {
        playheadEl.style.left = (audioEl.currentTime * pxPerSec) + 'px';
        timeLabel.textContent = `${fmtTime(audioEl.currentTime)} / ${fmtTime(audioDuration)}`;
      });
      audioEl.addEventListener('ended', () => { playBtn.textContent = t('▶ Play'); });
    }
    layout();
  }

  function fmtTime(t) {
    if (!isFinite(t)) return '0:00';
    const m = Math.floor(t / 60), s = Math.floor(t % 60);
    return `${m}:${String(s).padStart(2, '0')}`;
  }

  // ---- Notation previews, straight from the file: the whole track while
  // aligning (step 3), each section's bars before importing (step 4).
  // alphaTab draws Guitar Pro's own bend curves cleanly since 1.8 (all
  // 296 bent notes of the Florent lessons checked); the earlier detour
  // through this app's own alphaTeX is gone with the Riff format.
  // drawFileBars draws bars of one track straight from the file (a fresh
  // parse each time: alphaTab's model is changed by what it draws), from
  // written bar `from` (0-based), `count` bars or all of them.
  function drawFileBars(container, trackIndex, from, count) {
    const sc = alphaTab.importer.ScoreLoader.loadScoreFromBytes(rawBytes, new alphaTab.Settings());
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.player.enablePlayer = false;
    s.display.startBar = from + 1;
    if (count) s.display.barCount = count;
    for (const n of ['ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright', 'GuitarTuning', 'TrackNames', 'ChordDiagrams']) {
      if (alphaTab.NotationElement[n] !== undefined) s.notation.elements.set(alphaTab.NotationElement[n], false);
    }
    // alphaTab skips a container that has no width yet (a step that was
    // just un-hidden): wait for it to be laid out.
    const draw = (tries) => {
      if (!container.clientWidth && tries > 0) { requestAnimationFrame(() => draw(tries - 1)); return; }
      const api = new alphaTab.AlphaTabApi(container, s);
      api.renderScore(sc, [trackIndex]);
      container._alphaTabApi = api;
    };
    draw(60);
  }

  async function renderNotationPreview() {
    contentPreviewEl.innerHTML = '';
    delete contentPreviewEl.dataset.tex;
    contentPreviewEl.className = 'gpimport-file-preview';
    try {
      drawFileBars(contentPreviewEl, Number(trackSelect.value) || 0, 0, 0);
      wireContentPreviewClicks();
    } catch (e) {
      contentPreviewEl.textContent = t('Preview failed: %s', e.message);
    }
  }

  // render() (alphatab-tab.js) stashes its AlphaTabApi on the container
  // as container._alphaTabApi once construction succeeds — but it's
  // still async underneath (a rAF, possibly a retry or two), so it isn't
  // there yet the instant render() returns. Polling briefly for it
  // rather than assuming either "immediately" or "never" is the same
  // call this wizard already makes elsewhere (waiting for a container to
  // actually have content before trusting it).
  function wireContentPreviewClicks(attempt) {
    attempt = attempt || 0;
    const api = contentPreviewEl._alphaTabApi;
    if (!api) {
      if (attempt < 40) setTimeout(() => wireContentPreviewClicks(attempt + 1), 250);
      return;
    }
    // beatMouseDown's own payload shape varies by how it's reached
    // (direct API call vs. the DOM event this ALSO fires under the same
    // name) — confirmed live only that a Beat object is reachable one
    // way or the other, not which; checking both rather than assuming
    // is cheap and avoids silently never firing.
    api.beatMouseDown.on(e => {
      const beat = e?.beat || e;
      const written = beat?.voice?.bar?.index; // a written bar of the file: jump to where it's first played
      if (written == null) return;
      const pos = realIdx.indexOf(written);
      if (pos >= 0) jumpTimelineTo(pos);
    });
  }

  // jumpTimelineTo seeks playback (when there's audio to seek) and/or
  // scrolls the waveform/bar-grid timeline to center on a real-bar
  // position — the other half of "click a bar in the real notation
  // instead of guessing from the waveform": clicking there is now a
  // real way to GET somewhere on the timeline, not just look at notes.
  function jumpTimelineTo(pos) {
    if (audioEl && barTimes[pos] != null) { audioEl.currentTime = barTimes[pos]; }
    if (!scrollWrap.hidden && barTimes[pos] != null) {
      scrollWrap.scrollLeft = Math.max(0, barTimes[pos] * pxPerSec - scrollWrap.clientWidth / 2);
    }
  }

  // ---- GP's own section markers (Guitar Pro's real "rehearsal mark"
  // concept — alphaTab exposes it as masterBar.section/isSectionStart):
  // when the file's own author already named real sections, that's
  // actual data, not a guess by ear — offering it as a one-click
  // boundary-setting shortcut beats re-discovering the same thing by
  // dragging against a waveform.
  function gpSectionMarkers() {
    const out = [];
    realIdx.forEach((gpBarIdx, pos) => {
      const mb = score.masterBars[gpBarIdx];
      if (mb.section) out.push({ pos, text: mb.section.text || mb.section.marker || '' });
    });
    return out;
  }

  function setUpSectionMarkers() {
    const markers = gpSectionMarkers();
    useMarkersRow.hidden = markers.length < 2;
    useMarkersBtn.onclick = () => {
      // Exactly ourSections.length + 1 boundaries are needed (one past
      // each section's own start, plus the final end) — the file may
      // carry more or fewer real markers than this song has sections
      // (an intro/outro the file's author marked that this song's own
      // section list doesn't split out separately, say), so this maps
      // the first N markers straight across and leaves any remaining
      // sections at their current (default sequential) positions rather
      // than guessing — still real progress, not an all-or-nothing deal.
      const n = Math.min(markers.length, boundaries.length - 2);
      for (let i = 0; i < n; i++) boundaries[i + 1] = markers[i].pos;
      renderBarGrid();
      renderLabelTrack();
      renderBoundaries();
      renderMappingTableOnly();
      setStatus(t('Placed %d section start(s) from the file’s own markers.', n), 'ok');
    };
  }

  function drawWaveform() {
    const ctx = waveformCanvas.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    const w = Math.max(timeline.clientWidth, 1), h = waveformCanvas.clientHeight;
    waveformCanvas.width = w * dpr; waveformCanvas.height = h * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = '#9fe870';
    const mid = h / 2;
    // Real peak data when decodeAudioData has run (best-effort, kept
    // simple — a flat silhouette if decoding hasn't resolved yet is a
    // harmless placeholder, not a correctness issue).
    const peaks = waveformCanvas._peaks;
    if (peaks) {
      for (let x = 0; x < w; x++) {
        const i = Math.floor((x / w) * peaks.length);
        const amp = (peaks[i] || 0) * mid * 0.95;
        ctx.fillRect(x, mid - amp, 1, amp * 2);
      }
    }
  }

  async function decodePeaks() {
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) return;
    try {
      const bytes = score.backingTrack.rawAudioFile;
      const ctx = new (window.AudioContext || window.webkitAudioContext)();
      const ab = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
      const audioBuffer = await ctx.decodeAudioData(window.songbooksDecodable ? window.songbooksDecodable(ab) : ab);
      ctx.close();
      const channel = audioBuffer.getChannelData(0);
      const buckets = 2000;
      const perBucket = Math.floor(channel.length / buckets) || 1;
      const peaks = new Float32Array(buckets);
      for (let i = 0; i < buckets; i++) {
        let max = 0;
        const start = i * perBucket, end = Math.min(start + perBucket, channel.length);
        for (let j = start; j < end; j++) { const v = Math.abs(channel[j]); if (v > max) max = v; }
        peaks[i] = max;
      }
      waveformCanvas._peaks = peaks;
      drawWaveform();
    } catch (e) { /* leave the flat placeholder — playback/alignment still work without a waveform picture */ }
  }

  // ---- Bar grid: real per-bar ticks when zoomed in enough to read
  // them, else one collapsed block per section (the chart's own
  // "multi-rest" idea, applied to the timeline instead of the staff).
  const TICK_THRESHOLD_PX = 10; // below this px-per-bar, stop drawing individual ticks
  // Guitar Pro's own tempo-map/sync editor marks three distinct things on
  // its ruler: a solid tick at every measure its own sync data actually
  // anchors, a lighter tick at every measure it only interpolated
  // between two anchors (or extrapolated past the last one), and a
  // beat subdivision within each measure — not just one tick per bar.
  // This mirrors that directly: .synced vs plain .beat1 is REAL file
  // data vs this wizard's own estimate (syncedPositions, buildBarTimes),
  // and .beatN below is new — reusing a class this file already defined
  // but never actually drew a tick for.
  function renderBarGrid() {
    bargridEl.innerHTML = '';
    if (!barTimes.length) return;
    const avgPxPerBar = (barTimes[barTimes.length - 1] - barTimes[0]) / barTimes.length * pxPerSec;
    if (avgPxPerBar >= TICK_THRESHOLD_PX) {
      realIdx.forEach((gpBarIdx, pos) => {
        const x = barTimes[pos] * pxPerSec;
        const mb = score.masterBars[gpBarIdx];
        const line = document.createElement('div');
        line.className = 'beat1' + (syncedPositions.has(pos) ? ' synced' : '');
        line.title = syncedPositions.has(pos)
          ? t('Real sync point from this file')
          : t('Estimated — no sync point here, interpolated from nearby ones');
        line.style.left = x + 'px';
        bargridEl.appendChild(line);
        // Beat subdivisions within this bar — interior ticks only (the
        // bar's own beat1 line above already marks the first beat), evenly
        // spaced across this bar's real duration (next bar's time minus
        // this one's): the file's sync points are per-BAR, never finer,
        // so a beat tick is this wizard's own even split, same honest
        // estimate groupMeasures/buildBarTimes already makes for anything
        // between two known points, not real sub-bar sync data.
        const barDur = (barTimes[pos + 1] ?? x / pxPerSec) - barTimes[pos];
        const beatsPerBar = mb.timeSignatureNumerator || 4;
        for (let b = 1; b < beatsPerBar; b++) {
          const bx = x + (barDur * b / beatsPerBar) * pxPerSec;
          const btick = document.createElement('div');
          btick.className = 'beatN';
          btick.style.left = bx + 'px';
          bargridEl.appendChild(btick);
        }
        const num = document.createElement('div');
        num.className = 'barnum';
        num.style.left = (x + 2) + 'px';
        num.textContent = pos;
        bargridEl.appendChild(num);
        // The file's own section marker, when it has one — a real
        // reference point ("the author called this bar X"), not a
        // guess from the waveform shape alone.
        if (mb.section) {
          const tag = document.createElement('div');
          tag.className = 'gpimport-gp-marker';
          tag.style.left = x + 'px';
          tag.textContent = mb.section.text || mb.section.marker || '•';
          bargridEl.appendChild(tag);
        }
      });
    } else {
      // Collapsed: one tinted block per CURRENT section mapping (not
      // per original GP bar) so the zoomed-out view already shows the
      // boundaries the user is placing, not an unrelated GP grouping.
      for (let i = 0; i < boundaries.length - 1; i++) {
        const block = document.createElement('div');
        block.className = 'collapsed-block';
        block.style.left = (barTimes[boundaries[i]] * pxPerSec) + 'px';
        block.style.width = ((barTimes[boundaries[i + 1]] - barTimes[boundaries[i]]) * pxPerSec) + 'px';
        bargridEl.appendChild(block);
      }
    }
  }

  function sectionBarCount(i) { return boundaries[i + 1] - boundaries[i]; }

  function renderLabelTrack() {
    labeltrackEl.innerHTML = '';
    ourSections.forEach((sec, i) => {
      const x0 = barTimes[boundaries[i]] * pxPerSec;
      const x1 = barTimes[boundaries[i + 1]] * pxPerSec;
      const el = document.createElement('div');
      const mapped = sectionBarCount(i);
      el.className = 'gpimport-section-block' + (mapped !== sec.barCount ? ' mismatch' : '');
      el.style.left = x0 + 'px';
      el.style.width = Math.max(2, x1 - x0) + 'px';
      el.style.background = mapped === sec.barCount ? '' : '';
      el.textContent = t('%s (%d/%d bars)', sec.label, mapped, sec.barCount);
      el.title = mapped === sec.barCount ? t('Matches this section’s real bar count') : t('This section needs %d bars — currently mapped to %d', sec.barCount, mapped);
      labeltrackEl.appendChild(el);
    });
  }

  let draggingBoundary = -1;
  function renderBoundaries() {
    boundariesEl.innerHTML = '';
    boundaries.forEach((pos, i) => {
      const handle = document.createElement('div');
      handle.className = 'gpimport-boundary' + (i === draggingBoundary ? ' dragging' : '');
      handle.style.left = (barTimes[pos] * pxPerSec) + 'px';
      handle.title = i === 0 ? t('Drag past any count-in/lead-in — this is the song’s bar 0')
        : i === boundaries.length - 1 ? t('End of the last mapped section')
        : t('Boundary between “%s” and “%s”', ourSections[i - 1].label, ourSections[i].label);
      handle.addEventListener('mousedown', ev => startDrag(ev, i));
      boundariesEl.appendChild(handle);
    });
  }

  function startDrag(ev, i) {
    ev.preventDefault();
    draggingBoundary = i;
    renderBoundaries();
    const onMove = e => {
      const rect = timeline.getBoundingClientRect();
      const x = e.clientX - rect.left + scrollWrap.scrollLeft;
      const t = Math.max(0, x / pxPerSec);
      let pos = nearestBarForTime(t);
      const lo = i === 0 ? 0 : boundaries[i - 1] + 1;
      const hi = i === boundaries.length - 1 ? realIdx.length : boundaries[i + 1] - 1;
      pos = Math.max(lo, Math.min(hi, pos));
      boundaries[i] = pos;
      renderBarGrid();
      renderLabelTrack();
      renderBoundaries();
    };
    const onUp = () => {
      draggingBoundary = -1;
      renderBoundaries();
      renderMappingTableOnly();
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }

  timeline.addEventListener('click', e => {
    if (e.target.closest('.gpimport-boundary') || !audioEl) return;
    const rect = timeline.getBoundingClientRect();
    const x = e.clientX - rect.left + scrollWrap.scrollLeft;
    audioEl.currentTime = Math.max(0, x / pxPerSec);
  });

  function layout() {
    const duration = audioDuration || (barTimes[barTimes.length - 1] || 60) + 5;
    const widthPx = duration * pxPerSec;
    timeline.style.width = widthPx + 'px';
    zoomLabel.textContent = Math.round(pxPerSec / 20 * 100) + '%';
    drawWaveform();
    renderBarGrid();
    renderLabelTrack();
    renderBoundaries();
    renderMappingTableOnly();
  }

  zoomInBtn.addEventListener('click', () => { pxPerSec = Math.min(300, pxPerSec * 1.6); layout(); });
  zoomOutBtn.addEventListener('click', () => { pxPerSec = Math.max(2, pxPerSec / 1.6); layout(); });
  playBtn.addEventListener('click', () => {
    if (!audioEl) return;
    if (audioEl.paused) { audioEl.play(); playBtn.textContent = t('❚❚ Pause'); }
    else { audioEl.pause(); playBtn.textContent = t('▶ Play'); }
  });

  function renderMappingTableOnly() {
    if (!ourSections.length) { mappingTable.hidden = true; return; }
    // Rebuilt after every drag, zoom or marker click: keep the sections
    // the user unticked unticked, or a partial re-import would silently
    // overwrite them.
    const prev = [...mappingBody.querySelectorAll('.gpimport-include')].map((c) => c.checked);
    mappingBody.innerHTML = '';
    ourSections.forEach((sec, i) => {
      const mapped = sectionBarCount(i);
      const tr = document.createElement('tr');
      tr.style.borderLeft = `4px solid ${SECTION_COLORS[i % SECTION_COLORS.length]}`;
      tr.innerHTML = `
        <td><input type="checkbox" class="gpimport-include" aria-label="${escapeHTML(t('Import %s', sec.label))}"${prev.length === ourSections.length && !prev[i] ? '' : ' checked'}></td>
        <td>${sec.label}</td>
        <td>${sec.barCount}</td>
        <td${mapped !== sec.barCount ? ' style="color:#b8860b;font-weight:600"' : ''}>${mapped}</td>
        <td></td>`;
      mappingBody.appendChild(tr);
    });
    mappingTable.hidden = false;
  }

  // ---- Chord-grid derivation: GP's own chord diagrams -> a "grid:"
  // line, for a song that has no chords typed in yet. A GP beat that
  // carries a chord diagram has a chordId into staff.chords (a real,
  // human-authored chord name the GP author attached — e.g. "E7" — not
  // re-derived by guessing from the notes, which would be unreliable
  // and could silently pick the wrong chord). Only the FIRST chord-
  // bearing beat in a bar sets that bar's chord — a mid-bar chord change
  // isn't representable in this app's one-chord-per-bar grid grammar
  // (same granularity the chart's own grid: line always has). A GP
  // author only ever stamps a diagram on the bar where the chord
  // CHANGES, confirmed live against a real course file — most bars
  // carry no diagram at all and mean "keep sustaining," not "unknown."
  // Both that case and an explicit repeat of the same name become "."
  // (grid's own sustain token), exactly how a hand-typed grid already
  // works.
  // Guitar Pro's chord names, in this app's chord notation: GP writes
  // major sevenths "7M"/"M7" and altered tensions as "/9+" or "/9#" —
  // and this app reads any "/" as a slash chord, so "F#7/9+" would show
  // as F#7 over a bass note "9+". A "/" followed by a note stays a bass.
  function normalizeChordName(name) {
    let n = String(name || '').replace(/\s+/g, '');
    n = n.replace(/(?:7M|M7|Maj7|MAJ7)/, 'maj7');
    n = n.replace(/\/?\(?([#+b-])?(5|9|11|13)([#+b-])?\)?/g, (m, pre, deg, post) => {
      const alt = pre || post;
      if (!alt) return m.startsWith('/') && /^[A-G]/.test(m.slice(1)) ? m : m;
      return (alt === '+' || alt === '#' ? '#' : 'b') + deg;
    });
    return n;
  }

  function extractChordGrid(track, realBarStart, barCount) {
    const staff = track.staves[0];
    const chords = staff.chords;
    const tokens = [];
    let lastChord = null;
    for (let r = realBarStart; r < realBarStart + barCount; r++) {
      const gpBarIndex = realIdx[r];
      if (gpBarIndex === undefined) break;
      const voice = staff.bars[gpBarIndex].voices[0];
      // Every chord change in the bar, with where it falls (0..1).
      const changes = [];
      if (voice && chords) {
        const len = voice.bar.masterBar.calculateDuration() || 3840;
        for (const b of voice.beats) {
          if (b.chordId == null || !chords.has(b.chordId)) continue;
          const name = normalizeChordName(chords.get(b.chordId).name);
          if (!changes.length || changes[changes.length - 1].name !== name) changes.push({ name, at: b.playbackStart / len });
        }
      }
      if (changes.length > 1) {
        // Two chords in the bar: half each, as the grid's slots split it.
        const first = changes[0].at > 0.01 ? (lastChord || '?') : changes[0].name;
        const second = changes[changes.length - 1].name;
        tokens.push(`${first === lastChord && changes[0].at > 0.01 ? '.' : first} ${second}`);
        lastChord = second;
      } else if (changes.length === 1) {
        const c = changes[0].name;
        tokens.push(c === lastChord ? '.' : c);
        lastChord = c;
      } else if (lastChord) {
        // No chord diagram on THIS bar is the normal case, not a gap —
        // confirmed live against a real course file: a GP author only
        // ever stamps a diagram on the bar where the chord CHANGES, not
        // on every bar it's sustained over. Same "." sustain token as an
        // explicit repeat gets, just reached by absence instead of a
        // matching name.
        tokens.push('.');
      } else {
        // No chord has been seen AT ALL yet (the song's very first
        // bar(s), before any diagram) — nothing to sustain. "?" keeps
        // the bar count right and is visibly wrong at a glance, for a
        // quick manual fix, instead of guessing a chord out of thin air.
        tokens.push('?');
      }
    }
    return tokens;
  }


  // Writes a brand-new "[Song]" section from the derived chord grid —
  // for a song with no sections/chords yet (see gpimport-derive-row in
  // play.html). Preserves whatever header lines (title/artist/key/...)
  // already exist; only the section body is new. Re-fetches this
  // song's structure afterward so the normal align/import steps below
  // see the section that was just created, same as if a human had
  // typed it in by hand.
  async function deriveAndCreateSection() {
    deriveBtn.disabled = true;
    try {
      setStatus(t('Making a chord grid from this file…'));
      const track = score.tracks[Number(trackSelect.value) || 0];
      const tokens = extractChordGrid(track, 0, realIdx.length);
      const rawRes = await fetch(`/api/songs/${variantId}/raw`);
      if (!rawRes.ok) throw new Error(t('could not load this song’s current source'));
      const raw = await rawRes.json();
      const body = raw.text.replace(/\s*$/, '');
      const grid = `[Song]\ngrid: | ${tokens.join(' | ')} |\n`;
      const putRes = await fetch(`/api/songs/${variantId}/raw`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: body ? body + '\n\n' + grid : grid, version: raw.version }),
      });
      if (!putRes.ok) throw new Error(t('saving the chord grid failed: %s', await putRes.text()));
      const res = await fetch(`/api/songs/${variantId}/structure`);
      const data = await res.json();
      ourSections = (data.sections || []).filter(s => s.barCount > 0);
      boundaries = [0];
      ourSections.forEach(sec => boundaries.push(Math.min(realIdx.length, boundaries[boundaries.length - 1] + sec.barCount)));
      deriveRow.hidden = true;
      setStatus(t('Made a chord grid from the file’s chord diagrams: %d section(s) ready to match up.', ourSections.length), 'ok');
    } catch (e) {
      setStatus(t('Couldn’t make the chord grid: %s', e.message), 'err');
    } finally {
      deriveBtn.disabled = false;
    }
  }
  deriveBtn.addEventListener('click', deriveAndCreateSection);

  // ---- Per-section extraction: GP real-bar range -> our Riff JSON ----
  // The written bars (1-based) section i plays, as "gp[...]" ranges:
  // "9-12" or "1-4, 9-12". Repeats inside them are played as written.
  function writtenRanges(i) {
    const bars = [...new Set(realIdx.slice(boundaries[i], boundaries[i + 1]))].map((x) => x + 1).sort((x, y) => x - y);
    const ranges = [];
    for (const n of bars) {
      const last = ranges[ranges.length - 1];
      if (last && n === last[1] + 1) last[1] = n; else ranges.push([n, n]);
    }
    return ranges;
  }
  function rangesSpec(ranges) {
    return ranges.map(([a, b]) => (a === b ? String(a) : `${a}-${b}`)).join(', ');
  }
  // The name a "gp[...]" line uses for a track: its name, or "#N" when it
  // has none or another track has the same.
  function trackLabel(index) {
    const name = (score.tracks[index].name || '').trim();
    const same = score.tracks.filter((t) => (t.name || '').trim().toLowerCase() === name.toLowerCase()).length;
    return name && same === 1 ? name : `#${index + 1}`;
  }

  async function uploadSourceFile() {
    if (!rawFile) return;
    const form = new FormData();
    form.append('file', rawFile, rawFile.name);
    form.append('facts', JSON.stringify(GPFacts.extract(alphaTab.importer.ScoreLoader.loadScoreFromBytes(rawBytes, new alphaTab.Settings()))));
    const res = await fetch(`/api/songs/${variantId}/gp-file`, { method: 'PUT', body: form });
    if (!res.ok) throw new Error(t('source file upload failed: %s', await res.text()));
    await fetch(`/api/songs/${variantId}/meta`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: 'gp_source', value: rawFile.name }),
    });
  }

  // Writes the section's "gp[Track]: bars" line (handlePutSectionGP,
  // gpview.go), replacing one for the same track and the notes an earlier
  // import copied into the text for it.
  async function putGPRef(sectionIndex, track, spec) {
    const res = await fetch(`/api/songs/${variantId}/sections/${sectionIndex}/gp`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ track, spec }),
    });
    if (!res.ok) throw new Error(t('section %d: %s', sectionIndex, await res.text()));
  }

  // Sync points are keyed on the SAME boundaries the score import just
  // used — not a separately-tracked global shift (the earlier version
  // of this file had exactly that bug: a shift field decoupled from
  // whatever a user had manually marked, uploading audio sync for a
  // different bar mapping than the one actually saved).
  async function importAudioAndSync() {
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) return;
    const bytes = score.backingTrack.rawAudioFile;
    const form = new FormData();
    form.append('file', new Blob([bytes], { type: 'audio/mpeg' }), 'gp-backing-track.mp3');
    const upRes = await fetch(`/api/songs/${variantId}/audio/gp`, { method: 'PUT', body: form });
    if (!upRes.ok) throw new Error(t('audio upload failed: %s', await upRes.text()));

    const points = [];
    for (let pos = boundaries[0]; pos < realIdx.length; pos++) {
      const ourBar = pos - boundaries[0];
      let t = barTimes[pos];
      // Songbooks times a pickup as a whole bar with its notes at the end
      // (the Mixer pads it with leading rests), so bar 0 starts a full
      // bar before bar 1, not where the anacrusis notes start.
      const mb = score.masterBars[realIdx[pos]];
      if (mb.isAnacrusis && barTimes[pos + 1] != null) {
        const full = (mb.timeSignatureNumerator || 4) * (4 / (mb.timeSignatureDenominator || 4));
        const short = barBeats(mb);
        if (short > 0 && short < full) t = barTimes[pos + 1] - (barTimes[pos + 1] - t) * (full / short);
      }
      points.push({ barIndex: ourBar, timeSeconds: t });
    }
    if (points.length) {
      const spRes = await fetch(`/api/songs/${variantId}/audio/gp/sync-points`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ points }),
      });
      if (!spRes.ok) throw new Error(t('sync points failed: %s', await spRes.text()));
    }
  }

  step1Next.addEventListener('click', async () => {
    if (!lining()) {
      goToStep('check');
      await prepareHeader(checkHeaderEl);
      await runCheck();
      return;
    }
    goToStep('align');
    await prepareTimeline();
    decodePeaks();
  });
  step2Back.addEventListener('click', () => { if (audioEl) audioEl.pause(); goToStep('file'); });
  step2Next.addEventListener('click', async () => {
    if (audioEl) audioEl.pause();
    const rows = [...mappingBody.querySelectorAll('tr')].filter(tr => tr.querySelector('.gpimport-include').checked);
    summary.textContent = rows.length
      ? t('Importing %d section(s): %s.', rows.length, rows.map(tr => tr.children[1].textContent).join(', '))
      : t('No sections selected — nothing will be imported.');
    previewHolder.hidden = false;
    audioRow.hidden = !(score.backingTrack && score.backingTrack.rawAudioFile);
    goToStep('import');
    await prepareHeader();
    await renderPreview();
  });

  // ---- Step 3 for a file next to the chart (or the whole song): the check
  // Before anything is saved, the file is compared with the chart: the
  // server lays the chart's sections over the file's bars from where the
  // chart starts in it (POST …/gp-check, gpalign.go — the comparison drift
  // makes once the file is kept, so both agree), and this draws it: two
  // timelines on one bar axis, a row per section, the chords of the one
  // picked side by side, and its bars in the file. Fixes the app can make
  // are one tap (where the chart starts; the header facts above); the
  // rest is said in plain words.
  const checkHeaderEl = document.getElementById('gpimport-check-header');
  const checkSummaryEl = document.getElementById('gpimport-check-summary');
  const startInput = document.getElementById('gpimport-start');
  const startNote = document.getElementById('gpimport-start-note');
  const fixesEl = document.getElementById('gpimport-fixes');
  const alignHost = document.getElementById('gpimport-align');
  const checkNotation = document.getElementById('gpimport-check-notation');
  const detailBars = document.getElementById('gpimport-detail-bars');
  const detailTrack = document.getElementById('gpimport-detail-track');
  const detailNotation = document.getElementById('gpimport-detail-notation');
  let factsCache = null;
  let check = null;       // the last POST …/gp-check answer: { header, alignment }
  let checkStart = 1;     // the file's bar (as played) the chart starts on
  let checkPicked = -1;   // the section shown in detail
  let checkSeq = 0;
  const fileFacts = () => factsCache || (factsCache = GPFacts.extract(score));

  // t() with a count picking the form, the other values after it.
  function tc(n, one, other, ...args) {
    const single = window.SB_LANG === 'fr' ? (n === 0 || n === 1) : n === 1;
    return t(single ? one : other, ...args);
  }

  async function postCheck(facts, start) {
    const res = await fetch(`/api/songs/${variantId}/gp-check`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ facts, start }),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
  }

  async function runCheck(start) {
    if (!score) return;
    const seq = ++checkSeq;
    try {
      if (start === undefined) {
        // The song's own "gp_start:", when it has one (a file replaced).
        const raw = await (await fetch(`/api/songs/${variantId}/raw`)).json();
        start = Number(headerValue(raw.text || '', 'gp_start')) || 1;
      }
      const out = await postCheck(fileFacts(), start);
      if (seq !== checkSeq) return;
      check = out;
      checkStart = out.alignment.start;
      startInput.value = checkStart;
      startInput.max = out.alignment.fileBars || 1;
      const first = out.alignment.sections.findIndex((r) => r.status !== 'ok' && !(r.status === 'shifted' && !r.own));
      // The first section left to look at, unless one was picked and is
      // still there to look at.
      const picked = out.alignment.sections[checkPicked];
      if (!picked || (picked.status === 'ok' && first >= 0)) checkPicked = Math.max(0, first);
      renderCheck();
    } catch (e) {
      setStatus(t('Couldn’t check the file against the chart: %s', e.message), 'err');
    }
  }

  function takenFacts() {
    return new Set(headerChoices.filter((c) => c.input.checked || c.input.disabled).map((c) => c.key));
  }

  function renderCheckSummary() {
    if (!check || !checkSummaryEl) return;
    const al = check.alignment;
    const left = GPAlign.remaining(al, check.header, takenFacts(), check.items);
    checkSummaryEl.className = 'gpimport-check-summary ' + (left.total ? 'is-off' : 'is-ok');
    if (!al.sections.length) {
      checkSummaryEl.textContent = left.header
        ? tc(left.header, 'The song has no bars yet to check against: %d difference in its header.', 'The song has no bars yet to check against: %d differences in its header.', left.header)
        : t('The song has no bars yet to check against; its header agrees with the file.');
    } else if (!left.total) {
      checkSummaryEl.textContent = t('The file and the chart line up: %s, %s.', tn(al.sections.length, '%d section', '%d sections'), tn(al.chartBars, '%d bar', '%d bars'));
    } else {
      checkSummaryEl.textContent = tc(left.total, '%d difference left. Fix it below, or import anyway: the song will show it as out of step with the file.', '%d differences left. Fix them below, or import anyway: the song will show them as out of step with the file.', left.total);
    }
  }

  function renderCheck() {
    const al = check.alignment;
    renderCheckSummary();
    const notes = [];
    if (checkStart > 1) notes.push(tn(checkStart - 1, '%d bar of the file before the chart starts.', '%d bars of the file before the chart starts.'));
    if (al.fileEmpty) notes.push(tn(al.fileEmpty, 'The file opens with %d silent bar.', 'The file opens with %d silent bars.'));
    if (al.filePickup !== al.chartPickup) notes.push(al.filePickup ? t('The file starts with a pickup bar, the chart doesn’t.') : t('The chart starts with a pickup bar, the file doesn’t.'));
    startNote.textContent = notes.join(' ');
    fixesEl.replaceChildren();
    for (const f of GPAlign.fixes(al)) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'gpimport-fix';
      const first = al.sections[0];
      b.textContent = f.why === 'marker' ? t('Start at bar %d: the chart’s %s on the file’s “%s”', f.start, first.label, first.match.text)
        : f.why === 'chords' ? t('Start at bar %d, where the chords line up best', f.start)
        : f.why === 'empty' ? tc(f.start - 1, 'Start at bar %d, after the %d silent bar', 'Start at bar %d, after the %d silent bars', f.start, f.start - 1)
        : t('Start at the file’s first bar');
      b.addEventListener('click', () => runCheck(f.start));
      fixesEl.appendChild(b);
    }
    drawAlignment(alignHost, al, {
      picked: checkPicked,
      onPick: (i) => { checkPicked = i; renderCheck(); },
      onLineUp: (row) => runCheck(GPAlign.startFor(checkStart, row, al.fileBars)),
    });
    drawCheckNotation();
  }

  // The bars of the picked section in the file, as Guitar Pro numbers them,
  // in one track (the one with the chord names, else the first).
  // A section paired with one of the file's draws that one, all its bars.
  function drawCheckNotation() {
    const row = check && check.alignment.sections[checkPicked];
    const written = row && ((row.match && row.match.written) || row.written);
    checkNotation.hidden = !written || !written.length;
    if (checkNotation.hidden) return;
    if (!detailTrack.options.length) {
      score.tracks.forEach((tr, i) => detailTrack.add(new Option(tr.name || t('Track %d', i + 1), i)));
      const ct = fileFacts().chordTrack;
      detailTrack.value = String(ct ? ct - 1 : 0);
      detailTrack.closest('label').hidden = score.tracks.length < 2;
    }
    detailBars.textContent = t('%s in the file: bars %s', row.match ? `“${row.match.text}”` : row.label, written.map(([a, b]) => (a === b ? a : `${a}–${b}`)).join(', '));
    detailNotation.replaceChildren();
    for (const [a, b] of written) {
      const mount = document.createElement('div');
      detailNotation.appendChild(mount);
      try { drawFileBars(mount, Number(detailTrack.value) || 0, a - 1, b - a + 1); } catch (e) { mount.textContent = t('Preview failed: %s', e.message); }
    }
  }
  detailTrack.addEventListener('change', drawCheckNotation);
  document.getElementById('gpimport-start-down').addEventListener('click', () => runCheck(GPAlign.clampStart(checkStart - 1, check && check.alignment.fileBars)));
  document.getElementById('gpimport-start-up').addEventListener('click', () => runCheck(GPAlign.clampStart(checkStart + 1, check && check.alignment.fileBars)));
  startInput.addEventListener('change', () => runCheck(GPAlign.clampStart(startInput.value, check && check.alignment.fileBars)));

  // What a section's row says: how it sits on the file, and what to do
  // when the app can't do it.
  function rowStatus(row, al, k) {
    const m = row.match;
    switch (row.status) {
      case 'ok': return { text: t('Lines up'), cls: 'ok' };
      case 'missing': return { text: row.fileBars > 0 ? tn(row.fileBars, 'The file ends %d bar into it.', 'The file ends %d bars into it.') : t('The file ends before it.'), advice: t('Start the chart earlier in the file, or add the missing bars in Guitar Pro.'), cls: 'off' };
      case 'bars': {
        const n = Math.abs(m.bars - row.bars);
        const text = t('%s: chart %s, file %s', row.label, tn(row.bars, '%d bar', '%d bars'), tn(m.bars, '%d bar', '%d bars'));
        let advice;
        if (m.bars > row.bars && m.bars % row.bars === 0) advice = t('The file plays “%s” %d times: add a repeat to the chart’s %s, or edit the file in Guitar Pro.', m.text, m.bars / row.bars, row.label);
        else if (m.bars > row.bars) advice = tc(n, 'The file’s “%s” has %d more bar: add it to the chart’s %s, or edit the file in Guitar Pro.', 'The file’s “%s” has %d more bars: add them to the chart’s %s, or edit the file in Guitar Pro.', m.text, n, row.label);
        else advice = tc(n, 'The file’s “%s” has %d bar less: take it out of the chart’s %s, or edit the file in Guitar Pro.', 'The file’s “%s” has %d bars less: take them out of the chart’s %s, or edit the file in Guitar Pro.', m.text, n, row.label);
        return { text, advice, cls: 'off' };
      }
      case 'shifted': {
        const n = Math.abs(row.shift);
        if (!row.own) {
          let cause = '';
          for (let j = k - 1; j >= 0; j--) if (al.sections[j].status === 'bars' || (al.sections[j].status === 'shifted' && al.sections[j].own)) { cause = al.sections[j].label; break; }
          return { text: cause ? t('Moved along by %s', cause) : t('Moved along by the sections before'), cls: 'follow' };
        }
        return { text: row.shift > 0 ? tc(n, 'The file’s “%s” starts %d bar later', 'The file’s “%s” starts %d bars later', m.text, n) : tc(n, 'The file’s “%s” starts %d bar earlier', 'The file’s “%s” starts %d bars earlier', m.text, n), lineUp: true, cls: 'off' };
      }
      case 'chords': return { text: tn(row.chordDiff.length, '%d bar with other chords', '%d bars with other chords'), advice: t('Fine if the chart simplifies them; after the import, “Use the file’s” takes the file’s.'), cls: 'chords' };
      case 'time': return { text: t('Time signature: %s here, %s in the file', row.chartTime, row.fileTime), cls: 'off' };
    }
    return { text: '', cls: '' };
  }

  // drawAlignment draws a check's alignment into host: the two timelines,
  // a row per section and the chords of the picked one. Also the search
  // results' preview (compact: no line-up buttons).
  function drawAlignment(host, al, opts = {}) {
    host.replaceChildren();
    const mk = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
    if (!al.sections.length) return;
    const L = GPAlign.layout(al);
    const scroll = mk('div', 'gpimport-twin-scroll');
    const twin = mk('div', 'gpimport-twin');
    twin.style.minWidth = Math.round(L.total * 7 + 64) + 'px';
    const lane = (name, label) => {
      const r = mk('div', 'gpimport-twin-row');
      r.dataset.row = name;
      r.append(mk('span', 'gpimport-twin-name', label));
      const l = mk('div', 'gpimport-twin-lane');
      r.append(l);
      twin.append(r);
      return l;
    };
    const chartLane = lane('chart', t('Chart'));
    const fileLane = lane('file', t('File'));
    const place = (e, x, w) => { e.style.left = (x * 100) + '%'; e.style.width = `calc(${w * 100}% - 2px)`; };
    L.chart.forEach((c) => {
      const row = al.sections[c.i];
      const b = mk(opts.onPick ? 'button' : 'div', 'gpimport-twin-block is-' + (row.status === 'shifted' && !row.own ? 'follow' : row.status) + (c.i === opts.picked ? ' is-picked' : ''), c.label);
      if (opts.onPick) { b.type = 'button'; b.addEventListener('click', () => opts.onPick(c.i)); }
      b.title = `${c.label} · ${rowStatus(row, al, c.i).text}`;
      place(b, c.x, c.w);
      chartLane.append(b);
    });
    L.file.forEach((f) => {
      // A lead-in too short for its name keeps it in its tooltip.
      const b = mk('div', 'gpimport-twin-block' + (f.lead ? ' is-lead' : ''), f.lead ? (f.bars * 7 >= 40 ? t('lead-in') : '') : f.label);
      b.title = f.lead ? tn(f.bars, '%d bar before the first section', '%d bars before the first section') : `${f.label || t('The file')} · ${tn(f.bars, '%d bar', '%d bars')} · ${t('bar %d', f.bar || f.from)}`;
      place(b, f.x, f.w);
      fileLane.append(b);
    });
    // Where the chart starts in the file.
    for (const l of [chartLane, fileLane]) {
      const mark = mk('div', 'gpimport-twin-start');
      mark.style.left = (L.lead / L.total * 100) + '%';
      l.append(mark);
    }
    scroll.append(twin);
    host.append(scroll);

    const table = mk('table', 'gpimport-mapping-table gpimport-check-table');
    const head = mk('thead');
    head.innerHTML = `<tr><th>${escapeHTML(t('Section'))}</th><th>${escapeHTML(t('Chart'))}</th><th>${escapeHTML(t('File'))}</th><th></th></tr>`;
    const body = mk('tbody');
    al.sections.forEach((row, k) => {
      const st = rowStatus(row, al, k);
      const tr = mk('tr', 'is-' + st.cls + (k === opts.picked ? ' is-picked' : ''));
      const name = mk('td');
      if (opts.onPick) {
        const pick = mk('button', 'link-btn', row.label);
        pick.type = 'button';
        pick.addEventListener('click', () => opts.onPick(k));
        name.append(pick);
      } else name.textContent = row.label;
      const fileCell = row.match ? `${tn(row.match.bars, '%d bar', '%d bars')} · “${row.match.text}”` : row.fileBars ? tn(row.fileBars, '%d bar', '%d bars') : '—';
      const what = mk('td', 'gpimport-check-what');
      what.append(mk('span', 'gpimport-check-status', st.text));
      if (st.advice) what.append(mk('span', 'gpimport-check-advice', st.advice));
      if (st.lineUp && opts.onLineUp) {
        const b = mk('button', 'gpimport-fix', t('Line up here'));
        b.type = 'button';
        b.addEventListener('click', () => opts.onLineUp(row));
        what.append(b);
      }
      tr.append(name, mk('td', '', tn(row.bars, '%d bar', '%d bars')), mk('td', '', fileCell), what);
      body.append(tr);
    });
    table.append(head, body);
    host.append(table);

    // The picked section's chords, bar by bar: the chart's over the file's.
    const row = al.sections[opts.picked];
    if (!row) return;
    const box = mk('div', 'gpimport-chords');
    box.append(mk('h4', '', t('%s, bar by bar', row.label)));
    const grid = mk('div', 'gpimport-chords-grid');
    grid.style.setProperty('--bars', row.bars);
    const diff = new Set(row.chordDiff || []);
    const cell = (text, cls) => mk('span', 'gpimport-chord' + (cls ? ' ' + cls : ''), text || '·');
    grid.append(mk('span', 'gpimport-chords-name', t('Chart')));
    row.chart.forEach((c, b) => grid.append(cell(c, diff.has(b) ? 'is-diff' : '')));
    grid.append(mk('span', 'gpimport-chords-name', t('File')));
    row.file.forEach((c, b) => grid.append(cell(b < row.fileBars ? c : '', diff.has(b) ? 'is-diff' : b >= row.fileBars ? 'is-none' : '')));
    const wrap = mk('div', 'gpimport-chords-scroll');
    wrap.append(grid);
    box.append(wrap);
    if (!al.hasChords) box.append(mk('p', 'hint', t('The file names no chords: only the bars are compared.')));
    host.append(box);
  }

  document.getElementById('gpimport-check-next').addEventListener('click', () => {
    const mbs = score.masterBars.length;
    const what = [tn(score.tracks.length, '%d track', '%d tracks'), score.tracks.map((tr) => tr.name || t('untitled')).join(', '), tn(mbs, '%d bar', '%d bars')];
    let text = fit === 'beside'
      ? t('The song keeps opening on its chart; this file goes in its Score tab: %s (%s), %s.', ...what)
      : t('The song will open on this file’s score: %s (%s), %s. Its sections, chords and lyrics stay as they are, as extras.', ...what);
    if (checkStart > 1) text += ' ' + t('The chart starts at the file’s bar %d.', checkStart);
    const left = check ? GPAlign.remaining(check.alignment, check.header, takenFacts(), check.items).total : 0;
    if (left) text += ' ' + tn(left, '%d difference with the file is left: the song will show it.', '%d differences with the file are left: the song will show them.');
    summary.textContent = text;
    previewHolder.replaceChildren();
    previewHolder.hidden = true;
    audioRow.hidden = !(score.backingTrack && score.backingTrack.rawAudioFile);
    goToStep('import');
  });

  // ---- Song header: the file's tempo, time signature and swing ------
  // A Guitar Pro file knows all three; a song created by hand or from
  // another import usually doesn't, and the chart then falls back to
  // 4/4 at 120 — every bar's length, the bar grid and the Mixer's
  // playback go wrong with it. The import step offers each value the
  // song doesn't already have, checked by default; unchecking keeps the
  // song's own (a deliberate edit wins).
  let headerChoices = []; // [{key, label, gp, song, input}]
  let diagramChoices = []; // [{name, fact, input}]
  function gpHeader() {
    const mbs = score.masterBars;
    const full = mbs.find((m) => !m.isAnacrusis) || mbs[0];
    const feels = mbs.map((m) => m.tripletFeel);
    const most = (v) => feels.filter((f) => f === v).length;
    // alphaTab's TripletFeel: 2 = triplet 8th (a shuffle), 1 = triplet 16th.
    const swing = most(2) > feels.length / 2 ? '8' : most(1) > feels.length / 2 ? '16' : '';
    const keys = fileFacts().keys || [];
    // The file's tempo counts quarter notes; the song's, its beat: a
    // dotted quarter in 6/8, 9/8, 12/8 (tempo-units.js), 92 quarters
    // there are "tempo: 61".
    const q = window.SongbooksTempoUnits ? window.SongbooksTempoUnits.beatQuarters(full.timeSignatureNumerator, full.timeSignatureDenominator) : 1;
    return {
      tempo: String(Math.round((score.tempo || 120) / q)),
      time: `${full.timeSignatureNumerator}/${full.timeSignatureDenominator}`,
      swing,
      key: GPAlign.keyName(keys[0]),
    };
  }
  function headerValue(text, key) {
    const m = new RegExp(`^${key}:[ \\t]*(.*)$`, 'm').exec(text.split(/\n\[/)[0]);
    return m ? m[1].trim() : '';
  }
  // Sets (or with an empty value removes) one header line, keeping every
  // other line as written; a new line goes after the existing header.
  function setHeader(text, key, value) {
    const lines = text.split('\n');
    let end = lines.findIndex((l) => /^\[/.test(l));
    if (end < 0) end = lines.length;
    const at = lines.slice(0, end).findIndex((l) => new RegExp(`^${key}:`).test(l));
    if (at >= 0) {
      if (value) lines[at] = `${key}: ${value}`; else lines.splice(at, 1);
      return lines.join('\n');
    }
    if (!value) return text;
    let last = -1;
    for (let i = 0; i < end; i++) if (/^[a-z-]+:/.test(lines[i])) last = i;
    lines.splice(last + 1, 0, `${key}: ${value}`);
    return lines.join('\n');
  }
  async function prepareHeader(holder = headerHolder) {
    headerChoices = [];
    if (!holder || !score) return;
    headerHolder.innerHTML = '';
    if (checkHeaderEl) checkHeaderEl.innerHTML = '';
    let text = '';
    try { text = (await (await fetch(`/api/songs/${variantId}/raw`)).json()).text || ''; } catch (e) { return; }
    const gp = gpHeader();
    const rows = [
      { key: 'tempo', label: t('Tempo'), show: (v) => (!v ? t('none (120)') : window.SongbooksTempoUnits && window.SongbooksTempoUnits.compound(gp.time) ? window.SongbooksTempoUnits.text(+v, gp.time) : t('%s bpm', v)) },
      { key: 'time', label: t('Time signature'), show: (v) => v || t('none (4/4)') },
      { key: 'swing', label: t('Swing'), show: (v) => (v === '8' ? t('swung eighths') : v === '16' ? t('swung sixteenths') : t('straight')) },
      // A file left in C major says nothing about the key.
      ...(gp.key ? [{ key: 'key', label: t('Key'), show: (v) => v || t('key|none') }] : []),
    ];
    const list = document.createElement('div');
    list.className = 'gpimport-header-list';
    const reimport = !!document.querySelector('.gpimport-reimport-note');
    for (const r of rows) {
      const song = headerValue(text, r.key);
      const same = r.key === 'key' ? song.toLowerCase() === gp.key.toLowerCase() : song === gp[r.key];
      const p = document.createElement('p');
      const label = document.createElement('label');
      const input = document.createElement('input');
      input.type = 'checkbox';
      // Re-importing a song that came from Guitar Pro keeps the header
      // values the song already has (they may have been set on purpose
      // since): only empty ones are filled in unless ticked.
      input.checked = !(reimport && song);
      input.disabled = same;
      label.append(input, ' ' + t('%s: %s', r.label, r.show(gp[r.key])));
      p.appendChild(label);
      const was = document.createElement('span');
      was.className = 'hint';
      was.textContent = ' ' + (same ? t('(already in the song)') : t('(song has %s)', r.show(song)));
      p.appendChild(was);
      input.addEventListener('change', () => { renderPreview(); renderCheckSummary(); });
      list.appendChild(p);
      headerChoices.push({ key: r.key, gp: gp[r.key], song, input });
    }
    const h = document.createElement('p');
    h.className = 'hint';
    h.textContent = t('From the Guitar Pro file — checked values are written to the song header:');
    holder.append(h, list);

    // The file's chord diagrams: new ones ticked, ones that differ from
    // the song's not (the song's may be on purpose).
    diagramChoices = [];
    const have = new Map();
    for (const m of text.split(/\n\[/)[0].matchAll(/^chord:\s*(\S+)\s+(\S+)/gm)) have.set(m[1], m[2]);
    const facts = GPFacts.extract(score);
    const seen = new Set();
    const dl = document.createElement('div');
    dl.className = 'gpimport-header-list';
    for (const d of facts.diagrams) {
      const name = normalizeChordName(d.name);
      const fold = name.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase();
      if (seen.has(fold)) continue;
      seen.add(fold);
      const p = document.createElement('p');
      const label = document.createElement('label');
      const input = document.createElement('input');
      input.type = 'checkbox';
      input.checked = !have.has(name);
      label.append(input, ' ' + t('Chord diagram %s', name));
      p.appendChild(label);
      if (have.has(name)) {
        const was = document.createElement('span');
        was.className = 'hint';
        was.textContent = ' ' + t('(the song has its own: %s)', have.get(name));
        p.appendChild(was);
      }
      dl.appendChild(p);
      diagramChoices.push({ name, fact: 'diagram:' + fold, input });
    }
    if (diagramChoices.length) holder.appendChild(dl);
  }
  // The header the song will have after the import.
  function chosenHeader(key) {
    const c = headerChoices.find((x) => x.key === key);
    return c ? (c.input.checked ? c.gp : c.song) : '';
  }
  async function writeHeader(extra = {}) {
    const todo = headerChoices.filter((c) => c.input.checked && !c.input.disabled);
    if (!todo.length && !Object.keys(extra).length) return;
    const raw = await (await fetch(`/api/songs/${variantId}/raw`)).json();
    let text = raw.text;
    for (const c of todo) text = setHeader(text, c.key, c.gp);
    for (const [k, v] of Object.entries(extra)) text = setHeader(text, k, v);
    const res = await fetch(`/api/songs/${variantId}/raw`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text, version: raw.version }),
    });
    if (!res.ok) throw new Error(t('saving the song header failed: %s', await res.text()));
  }

  // ---- Preview: real notation for exactly the sections currently
  // checked, before the one commit step — see handleGPImportPreview's
  // own doc comment (handlers_import.go) for why this is a server round
  // trip (reusing tab.ToAlphaTex) rather than a second alphaTeX
  // generator written in JS. One alphaTab mount per included section,
  // same .staff-line.alphatab-row/data-tex convention every other
  // TabOnly row in this app already uses, so alphatab-tab.js's own
  // render() handles it identically — nothing GP-import-specific in how
  // it actually draws.
  async function renderPreview() {
    previewHolder.innerHTML = '';
    if (!score) return;
    const ti = Number(trackSelect.value) || 0;
    const checks = [...mappingBody.querySelectorAll('.gpimport-include')];
    for (let i = 0; i < ourSections.length; i++) {
      if (!checks[i] || !checks[i].checked) continue;
      const ranges = writtenRanges(i);
      const row = document.createElement('div');
      row.className = 'gpimport-preview-row';
      const p = document.createElement('p');
      p.className = 'hint';
      p.textContent = `${ourSections[i].label}: gp[${trackLabel(ti)}]: ${rangesSpec(ranges)}`;
      row.appendChild(p);
      previewHolder.appendChild(row);
      for (const [a, b] of ranges) {
        const mount = document.createElement('div');
        row.appendChild(mount);
        try { drawFileBars(mount, ti, a - 1, b - a + 1); } catch (e) { p.textContent += ' — ' + t('preview failed: %s', e.message); }
      }
    }
  }
  step3Back.addEventListener('click', () => goToStep(lining() ? 'align' : 'check'));

  // The file's chord diagrams ticked in the header step become the song's.
  async function takeDiagrams() {
    for (const c of diagramChoices.filter((x) => x.input.checked)) {
      setStatus(t('Chord diagram %s…', c.name));
      await fetch(`/api/songs/${variantId}/gp-drift/take`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fact: c.fact }),
      });
    }
  }

  importBtn.addEventListener('click', async () => {
    if (!score) { setStatus(t('Choose a Guitar Pro file first.'), 'err'); return; }
    const ti = Number(trackSelect.value) || 0;
    const label = trackLabel(ti);
    const checks = [...mappingBody.querySelectorAll('.gpimport-include')];
    Forms.busy(importBtn, true);
    try {
      // The file first: the "gp[...]" lines point into it.
      setStatus(t('Keeping the Guitar Pro file with the song…'));
      await uploadSourceFile();
      setStatus(t('Writing the song header…'));
      // "content: gp" makes the song open on the score: set for the whole
      // song, taken off a song that had it when the chart is to stay its
      // main view.
      const extra = fit === 'whole' ? { content: 'gp' } : panel.dataset.gpOnly ? { content: '' } : {};
      // Where the chart starts in the file (the check step): "gp_start:"
      // when past its first bar.
      if (!lining()) extra.gp_start = checkStart > 1 ? String(checkStart) : '';
      await writeHeader(extra);
      if (!lining()) {
        if (includeAudio.checked) {
          setStatus(t('Uploading the backing track and bar sync…'));
          // The chart's bar 0 is the file's bar checkStart.
          boundaries = [checkStart - 1];
          await importAudioAndSync();
        }
        await takeDiagrams();
        setStatus(fit === 'beside'
          ? t('Imported: the file is in the song’s Score tab, and the song still opens on its chart. Reload the page to see it.')
          : t('Imported: the song now opens on the file’s score. Reload the page to see it.'), 'ok');
        return;
      }
      for (let i = 0; i < ourSections.length; i++) {
        if (!checks[i] || !checks[i].checked) continue;
        const sec = ourSections[i];
        setStatus(t('Section %d of %d: %s…', i + 1, ourSections.length, sec.label));
        await putGPRef(sec.index, label, rangesSpec(writtenRanges(i)));
      }
      if (includeAudio.checked) {
        setStatus(t('Uploading the backing track and bar sync…'));
        await importAudioAndSync();
      }
      await takeDiagrams();
      setStatus(t('Imported. Reload the page to see it on the chart.'), 'ok');
    } catch (e) {
      setStatus(t('The import stopped part-way: %s', e.message), 'err');
    } finally {
      Forms.busy(importBtn, false);
    }
  });
})();

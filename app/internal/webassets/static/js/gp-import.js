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
  const summary = document.getElementById('gpimport-summary');
  const previewHolder = document.getElementById('gpimport-preview');
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
    search: document.getElementById('gpimport-step-search'),
    file: document.getElementById('gpimport-step-file'),
    align: document.getElementById('gpimport-step-align'),
    import: document.getElementById('gpimport-step-import'),
  };
  const step1Next = document.getElementById('gpimport-step1-next');
  const step2Back = document.getElementById('gpimport-step2-back');
  const step2Next = document.getElementById('gpimport-step2-next');
  const step3Back = document.getElementById('gpimport-step3-back');

  function goToStep(name) {
    Object.entries(steps).forEach(([k, el]) => { el.hidden = k !== name; });
  }

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
    if (!title) { setStatus('enter a song title to search for', 'err'); return; }
    searchBtn.disabled = true;
    searchResults.innerHTML = '';
    setStatus('searching GProTab.net…');
    try {
      const qs = new URLSearchParams({ title, artist: searchArtistInput.value.trim() });
      const res = await fetch(`/api/import/gp/find?${qs}`);
      if (!res.ok) throw new Error(await res.text());
      const { candidates } = await res.json();
      if (!candidates || !candidates.length) {
        setStatus('no results on GProTab.net for that title — try a different spelling, or choose a file by hand below.', 'err');
        return;
      }
      setStatus(`found ${candidates.length} result(s).`, 'ok');
      renderSearchResults(candidates);
    } catch (e) {
      setStatus('search failed: ' + e.message, 'err');
    } finally {
      searchBtn.disabled = false;
    }
  }

  function renderSearchResults(candidates) {
    searchResults.innerHTML = '';
    const table = document.createElement('table');
    table.className = 'gpimport-mapping-table';
    table.innerHTML = '<thead><tr><th>Title</th><th>Artist</th><th>Format</th><th>Size</th><th>Rating</th><th></th></tr></thead>';
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
        <td><button type="button" class="primary gpimport-use-result">Use this</button></td>`;
      tr.querySelector('.gpimport-use-result').addEventListener('click', () => useSearchResult(c));
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    searchResults.appendChild(table);
  }

  function escapeHTML(s) {
    return (s || '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  async function useSearchResult(c) {
    setStatus(`fetching "${c.Title}" from GProTab.net…`);
    try {
      const res = await fetch(`/api/import/gp/fetch?url=${encodeURIComponent(c.URL)}`);
      if (!res.ok) throw new Error(await res.text());
      const buf = await res.arrayBuffer();
      let filename = 'import.gp';
      const cd = res.headers.get('Content-Disposition');
      const m = cd && /filename="([^"]+)"/.exec(cd);
      if (m) filename = m[1];
      fileSourceNote.hidden = false;
      fileSourceNote.innerHTML = `From GProTab.net: <a href="${escapeHTML(c.URL)}" target="_blank" rel="noopener">${escapeHTML(c.Title)} — ${escapeHTML(c.Artist)}</a>`;
      goToStep('file');
      await loadFile({ name: filename, arrayBuffer: async () => buf });
    } catch (e) {
      setStatus('fetching that file failed: ' + e.message, 'err');
    }
  }

  searchBtn.addEventListener('click', runSearch);
  searchSkip.addEventListener('click', () => { fileSourceNote.hidden = true; goToStep('file'); });

  let score = null;
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

  // ---- GP's own "real played bar count" expansion, mirroring
  // realBarCount (handlers_pages.go) on the GP side — a flat array
  // mapping each REAL (as-played) bar position to the GP-AUTHORED
  // masterBar index it plays. Repeat-free files (every file checked
  // this session) just come back as the identity mapping; a "|: :|"
  // bracket replays its bars repeatCount times (2 if unmarked); an
  // alternate ending only plays on the pass(es) its own bitmask names.
  // Not exhaustively tested against every possible multi-ending shape —
  // same pragmatic scope realBarCount itself documents on our side.
  function expandGPBars(masterBars) {
    const real = [];
    let i = 0;
    while (i < masterBars.length) {
      if (masterBars[i].isRepeatStart || !masterBars[i].isRepeatEnd) {
        let j = i;
        const openedAt = i;
        while (j < masterBars.length && !masterBars[j].isRepeatEnd) j++;
        if (j < masterBars.length && masterBars[j].isRepeatEnd) {
          const count = Math.max(2, masterBars[j].repeatCount || 2);
          for (let pass = 1; pass <= count; pass++) {
            for (let k = openedAt; k <= j; k++) {
              const mb = masterBars[k];
              if (mb.alternateEndings && !(mb.alternateEndings & (1 << (pass - 1)))) continue;
              real.push(k);
            }
          }
          i = j + 1;
          continue;
        }
      }
      real.push(i);
      i++;
    }
    return real;
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
    setStatus('parsing…');
    try {
      const buf = new Uint8Array(await file.arrayBuffer());
      score = alphaTab.importer.ScoreLoader.loadScoreFromBytes(buf);
      rawFile = file;
    } catch (e) {
      setStatus('could not parse this file: ' + e.message, 'err');
      score = null;
      return;
    }
    trackSelect.innerHTML = '';
    score.tracks.forEach((t, i) => {
      const opt = document.createElement('option');
      opt.value = i;
      opt.textContent = t.name || `Track ${i + 1}`;
      trackSelect.appendChild(opt);
    });
    trackRow.hidden = score.tracks.length < 2;
    realIdx = expandGPBars(score.masterBars);
    barTimes = buildBarTimes();
    step1Next.disabled = false;
    const res = await fetch(`/api/songs/${variantId}/structure`);
    if (!res.ok) { setStatus('could not load this song\'s sections', 'err'); return; }
    const data = await res.json();
    ourSections = (data.sections || []).filter(s => s.barCount > 0);
    boundaries = [0];
    ourSections.forEach(sec => boundaries.push(Math.min(realIdx.length, boundaries[boundaries.length - 1] + sec.barCount)));
    deriveRow.hidden = ourSections.length > 0;
    setStatus(`parsed "${score.title || file.name}" — ${score.tracks.length} track(s), ${realIdx.length} real bar(s), ${ourSections.length} section(s) to map.`, 'ok');
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
      setStatus('This file has no embedded backing track — nothing to align against. Sections will map in their own default order; the import still writes note data for each, just without audio sync. Use the notation preview above to see where sections actually change.');
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
      audioEl.addEventListener('ended', () => { playBtn.textContent = '▶ Play'; });
    }
    layout();
  }

  function fmtTime(t) {
    if (!isFinite(t)) return '0:00';
    const m = Math.floor(t / 60), s = Math.floor(t % 60);
    return `${m}:${String(s).padStart(2, '0')}`;
  }

  // ---- Real notation preview: the GP file's OWN content, rendered by
  // alphaTab's own renderScore straight from the already-parsed Score
  // object (score, from loadFile's ScoreLoader call) — never through
  // this app's own Riff/alphaTeX conversion (extractRiff/tab.ToAlphaTex,
  // used by renderPreview below for the FINAL extracted sections). The
  // two previews answer different questions: this one is "what does
  // this file actually contain, so I can align it" (step 3, before any
  // extraction has happened); that one is "what did we just extract for
  // THIS section" (step 4, after alignment). Rebuilt (not reused) on
  // every track change, since a different track is different notation
  // entirely, not a restyle of the same one.
  // Renders via THIS app's own extractRiff -> tab.ToAlphaTex round trip
  // (the exact same /api/gp-import/preview call and alphaTeX-rendering
  // path the step-4 per-section preview below already uses, proven live
  // against this exact file) rather than alphaTab's own renderScore
  // straight off the raw parsed Score object. A first version did that
  // instead — simpler in principle, no server round trip — and it
  // reliably produced garbled "MNaN,NaN" SVG paths on a real file with
  // bends: alphaTab's native bend-curve rendering off raw GP curve data
  // is a code path nothing else in this app has ever exercised (every
  // OTHER riff shown anywhere here is always OUR OWN re-encoded
  // BendSemitones/BendRelease form, via extractRiff, never the file's
  // raw native curve points) — going through that same re-encoding here
  // sidesteps whatever's actually broken there, rather than fighting it.
  //
  // Mounted via window.AlphaTabTab.render (alphatab-tab.js) — the SAME
  // data-tex/.staff-line.alphatab-row convention every other TabOnly row
  // in this app uses — rather than a hand-rolled AlphaTabApi construction
  // here. A hand-rolled version (tried first) hit the exact "skipped
  // rendering because of width=0" race this app has hit before in every
  // OTHER context a row can appear in right as its own [hidden] toggle
  // clears: alphatab-tab.js's own render() already has the full retry/
  // watchdog machinery proven against that (resizeGuard, pollGuard, an
  // 8-second construction-time watchdog) — reusing it beats re-fighting
  // the same race with a bare single requestAnimationFrame. The one
  // thing given up by not holding the AlphaTabApi instance directly is
  // click-a-bar-to-jump (beatMouseDown) — left for a follow-up rather
  // than trading away reliability to get it in this pass.
  async function renderNotationPreview() {
    contentPreviewEl.innerHTML = '';
    delete contentPreviewEl.dataset.tex;
    contentPreviewEl.className = 'staff-line alphatab-row';
    const trackIndex = Number(trackSelect.value) || 0;
    const track = score.tracks[trackIndex];
    try {
      const riff = extractRiff(track, 0, realIdx.length);
      const res = await fetch('/api/gp-import/preview', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ riff: JSON.stringify(riff) }),
      });
      if (!res.ok) throw new Error(await res.text());
      const { alphaTex } = await res.json();
      contentPreviewEl.dataset.tex = alphaTex;
      if (window.AlphaTabTab) window.AlphaTabTab.render(contentPreviewEl);
      wireContentPreviewClicks();
    } catch (e) {
      contentPreviewEl.textContent = 'Preview failed: ' + e.message;
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
      const pos = beat?.voice?.bar?.index; // index WITHIN this alphaTeX doc — same as realIdx's own "pos", since extractRiff walked the real bars in that same order, one beat per real bar's position
      if (pos == null) return;
      jumpTimelineTo(pos);
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
      setStatus(`set ${n} boundary/boundaries from this file's own section markers.`, 'ok');
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
      const audioBuffer = await ctx.decodeAudioData(ab);
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
          ? 'Real sync point from this file'
          : 'Estimated — no sync point here, interpolated from nearby ones';
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
      el.textContent = `${sec.label} (${mapped}/${sec.barCount} bars)`;
      el.title = mapped === sec.barCount ? 'Matches this section\'s real bar count' : `This section needs ${sec.barCount} bars — currently mapped to ${mapped}`;
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
      handle.title = i === 0 ? 'Drag past any count-in/lead-in — this is the song\'s bar 0'
        : i === boundaries.length - 1 ? 'End of the last mapped section'
        : `Boundary between "${ourSections[i - 1].label}" and "${ourSections[i].label}"`;
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
    if (audioEl.paused) { audioEl.play(); playBtn.textContent = '❚❚ Pause'; }
    else { audioEl.pause(); playBtn.textContent = '▶ Play'; }
  });

  function renderMappingTableOnly() {
    if (!ourSections.length) { mappingTable.hidden = true; return; }
    mappingBody.innerHTML = '';
    ourSections.forEach((sec, i) => {
      const mapped = sectionBarCount(i);
      const tr = document.createElement('tr');
      tr.style.borderLeft = `4px solid ${SECTION_COLORS[i % SECTION_COLORS.length]}`;
      tr.innerHTML = `
        <td><input type="checkbox" class="gpimport-include" checked></td>
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
  function extractChordGrid(track, realBarStart, barCount) {
    const staff = track.staves[0];
    const chords = staff.chords;
    const tokens = [];
    let lastChord = null;
    for (let r = realBarStart; r < realBarStart + barCount; r++) {
      const gpBarIndex = realIdx[r];
      if (gpBarIndex === undefined) break;
      const voice = staff.bars[gpBarIndex].voices[0];
      let barChord = null;
      if (voice && chords) {
        for (const b of voice.beats) {
          if (b.chordId != null && chords.has(b.chordId)) { barChord = chords.get(b.chordId).name; break; }
        }
      }
      if (barChord) {
        tokens.push(barChord === lastChord ? '.' : barChord);
        lastChord = barChord;
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
      setStatus('deriving chord grid from this file…');
      const track = score.tracks[Number(trackSelect.value) || 0];
      const tokens = extractChordGrid(track, 0, realIdx.length);
      const rawRes = await fetch(`/api/songs/${variantId}/raw`);
      if (!rawRes.ok) throw new Error('could not load this song\'s current source');
      const raw = await rawRes.json();
      const body = raw.text.replace(/\s*$/, '');
      const grid = `[Song]\ngrid: | ${tokens.join(' | ')} |\n`;
      const putRes = await fetch(`/api/songs/${variantId}/raw`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: body ? body + '\n\n' + grid : grid, version: raw.version }),
      });
      if (!putRes.ok) throw new Error('saving the chord grid failed: ' + (await putRes.text()));
      const res = await fetch(`/api/songs/${variantId}/structure`);
      const data = await res.json();
      ourSections = (data.sections || []).filter(s => s.barCount > 0);
      boundaries = [0];
      ourSections.forEach(sec => boundaries.push(Math.min(realIdx.length, boundaries[boundaries.length - 1] + sec.barCount)));
      deriveRow.hidden = true;
      setStatus(`created a chord grid from this file's own chord diagrams — ${ourSections.length} section(s) ready to map.`, 'ok');
    } catch (e) {
      setStatus('deriving chord grid failed: ' + e.message, 'err');
    } finally {
      deriveBtn.disabled = false;
    }
  }
  deriveBtn.addEventListener('click', deriveAndCreateSection);

  // ---- Per-section extraction: GP real-bar range -> our Riff JSON ----
  function extractRiff(track, realBarStart, barCount) {
    const staff = track.staves[0];
    const numStrings = staff.tuning.length;
    const bars = [];
    for (let r = realBarStart; r < realBarStart + barCount; r++) {
      const gpBarIndex = realIdx[r];
      if (gpBarIndex === undefined) break;
      const gpBar = staff.bars[gpBarIndex];
      const voice = gpBar.voices[0];
      const isSoleRest = voice && voice.beats.length === 1 && voice.beats[0].isRest;
      const beats = (voice ? voice.beats : []).map(b => {
        // A bar whose ONLY event is a rest is, by definition, a full
        // bar of silence — but real GP files routinely encode that rest
        // with whatever short duration (often a quarter) regardless of
        // the actual time signature, since it doesn't affect playback
        // and GP's own alphaTab-side isFullBarRest only special-cases a
        // LITERAL Whole-duration rest (confirmed against the vendored
        // bundle: isFullBarRest = isRest && 1 beat && duration===Whole).
        // Taking the encoded duration at face value here would print a
        // single quarter-rest glyph sitting in an otherwise-empty bar
        // instead of the full-bar rest mark the bar obviously means —
        // confirmed live against a real imported file (bars with
        // duration=4/dots=0, isFullBarRest false, in a 4/4 bar that's
        // musically just "rest here").
        // A tuplet (a triplet is 3 in the time of 2) keeps its ratio and a
        // grace note takes no time: without them a bar of triplet eighths,
        // or one with grace notes, reads as overfull.
        const tuplet = (beat) => {
          if (b.tupletNumerator > 1 && b.tupletDenominator > 0) { beat.TupletNum = b.tupletNumerator; beat.TupletDen = b.tupletDenominator; }
          if (b.graceType) beat.Grace = true;
          return beat;
        };
        if (b.isRest) return tuplet({ Rest: true, Duration: isSoleRest ? '1' : String(b.duration) + '.'.repeat(b.dots) });
        const notes = b.notes.map(n => ({
          MIDI: n.realValue,
          String: numStrings + 1 - n.string,
          Fret: n.fret,
          Tied: !!n.isTieDestination,
        }));
        const beat = { Duration: String(b.duration) + '.'.repeat(b.dots), Notes: notes };
        if (b.notes.some(n => n.isHammerPullOrigin)) beat.HammerPull = true;
        if (b.notes.some(n => n.isPalmMute)) beat.PalmMute = true;
        if (b.notes.length === 1 && b.notes[0].slideOutType) beat._slideOut = true;
        const bendNote = b.notes.find(n => n.hasBend && n.bendPoints && n.bendPoints.length > 1);
        if (bendNote) {
          const pts = bendNote.bendPoints;
          const maxVal = Math.max(...pts.map(p => p.value));
          const lastVal = pts[pts.length - 1].value;
          // alphaTab's BendPoint.value is documented in quarter-tone
          // units (1 = a quarter tone = 0.5 semitone) — not independently
          // re-verified against a known "full step" example this
          // session; flagged here so a visibly-wrong bend width on a
          // real import is easy to trace back to this one conversion.
          beat.BendSemitones = maxVal * 0.5;
          if (lastVal < maxVal) beat.BendRelease = true;
        }
        return tuplet(beat);
      });
      bars.push({ Beats: beats });
    }
    for (let bi = 0; bi < bars.length; bi++) {
      for (let i = 0; i < bars[bi].Beats.length; i++) {
        const prev = i > 0 ? bars[bi].Beats[i - 1] : (bi > 0 ? bars[bi - 1].Beats[bars[bi - 1].Beats.length - 1] : null);
        if (prev && prev.Notes && prev.Notes.length === 1 && prev._slideOut) {
          bars[bi].Beats[i].HasSlideFrom = true;
          bars[bi].Beats[i].SlideFromFret = prev.Notes[0].Fret;
        }
      }
    }
    for (const bar of bars) for (const beat of bar.Beats) delete beat._slideOut;
    return { Tuning: staff.tuning.slice(), MinimumFret: 0, Bars: bars };
  }

  // Keeps the ORIGINAL file downloadable (see internal/store/gpfile.go) —
  // separate from importAudioAndSync below, which only ever extracts and
  // uploads the backing track audio GP embeds, never the source file
  // itself. Best-effort: a failure here shouldn't block the data import
  // that's the actual point of this wizard.
  async function uploadSourceFile() {
    if (!rawFile) return;
    const form = new FormData();
    form.append('file', rawFile, rawFile.name);
    const res = await fetch(`/api/songs/${variantId}/gp-file`, { method: 'PUT', body: form });
    if (!res.ok) throw new Error('source file upload failed: ' + (await res.text()));
  }

  // Writes straight into the section's own score[label]: field in the
  // song's raw text (handlePutSectionScore, handlers_api.go) — the same
  // storage a hand-written score[Drum]: ... line uses, not a separate
  // per-section database row. label is the GP file's own track name
  // (falling back to "Guitar", same default prepareTimeline/trackSelect
  // use when a GP track carries no name of its own); it's a display/key
  // name only — the imported riff already carries its own exact
  // Tuning/fret choices (extractRiff), so nothing here depends on the
  // label itself being "guitar"/"bass"/"drum" the way a hand-typed
  // score[label]: shortcut body would (tab.ParseVoice dispatches a JSON
  // riff straight to tab.DecodeRiffJSON, bypassing that lookup).
  async function putScore(sectionIndex, label, riff) {
    const res = await fetch(`/api/songs/${variantId}/sections/${sectionIndex}/score/${encodeURIComponent(label)}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ body: JSON.stringify(riff) }),
    });
    if (!res.ok) throw new Error(`section ${sectionIndex}: ` + (await res.text()));
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
    if (!upRes.ok) throw new Error('audio upload failed: ' + (await upRes.text()));

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
      if (!spRes.ok) throw new Error('sync points failed: ' + (await spRes.text()));
    }
  }

  step1Next.addEventListener('click', async () => {
    goToStep('align');
    await prepareTimeline();
    decodePeaks();
  });
  step2Back.addEventListener('click', () => { if (audioEl) audioEl.pause(); goToStep('file'); });
  step2Next.addEventListener('click', async () => {
    if (audioEl) audioEl.pause();
    const rows = [...mappingBody.querySelectorAll('tr')].filter(tr => tr.querySelector('.gpimport-include').checked);
    summary.textContent = rows.length
      ? `Importing ${rows.length} section(s): ${rows.map(tr => tr.children[1].textContent).join(', ')}.`
      : 'No sections selected — nothing will be imported.';
    goToStep('import');
    await renderPreview();
  });

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
    const track = score.tracks[Number(trackSelect.value) || 0];
    const checks = [...mappingBody.querySelectorAll('.gpimport-include')];
    for (let i = 0; i < ourSections.length; i++) {
      if (!checks[i] || !checks[i].checked) continue;
      const sec = ourSections[i];
      const riff = extractRiff(track, boundaries[i], sectionBarCount(i));
      const row = document.createElement('div');
      row.className = 'gpimport-preview-row';
      row.innerHTML = `<p class="hint">${escapeHTML(sec.label)}</p><div class="staff-line alphatab-row"></div>`;
      previewHolder.appendChild(row);
      const mount = row.querySelector('.alphatab-row');
      try {
        const res = await fetch('/api/gp-import/preview', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ riff: JSON.stringify(riff) }),
        });
        if (!res.ok) throw new Error(await res.text());
        const { alphaTex } = await res.json();
        mount.dataset.tex = alphaTex;
        if (window.AlphaTabTab) window.AlphaTabTab.render(mount);
      } catch (e) {
        row.querySelector('.hint').textContent += ` — preview failed: ${e.message}`;
      }
    }
  }
  step3Back.addEventListener('click', () => goToStep('align'));

  importBtn.addEventListener('click', async () => {
    if (!score) { setStatus('parse a file first', 'err'); return; }
    const track = score.tracks[Number(trackSelect.value) || 0];
    const label = track.name || 'Guitar';
    const checks = [...mappingBody.querySelectorAll('.gpimport-include')];
    importBtn.disabled = true;
    try {
      for (let i = 0; i < ourSections.length; i++) {
        if (!checks[i] || !checks[i].checked) continue;
        const sec = ourSections[i];
        setStatus(`importing section ${i + 1}/${ourSections.length}: ${sec.label}…`);
        const riff = extractRiff(track, boundaries[i], sectionBarCount(i));
        await putScore(sec.index, label, riff);
      }
      if (includeAudio.checked) {
        setStatus('uploading backing track + sync points…');
        await importAudioAndSync();
      }
      setStatus('uploading the Guitar Pro file itself (for download)…');
      await uploadSourceFile();
      setStatus('imported. Reload the Chart tab to see it.', 'ok');
    } catch (e) {
      setStatus('import failed: ' + e.message, 'err');
    } finally {
      importBtn.disabled = false;
    }
  });
})();

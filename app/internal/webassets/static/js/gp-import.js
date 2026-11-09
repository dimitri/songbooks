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
// saves, not a guessed number. The align step's bar strip renders
// EVERY real bar through this app's own actual pipeline (POST
// .../scores/{i}/preview -> tab.DecodeScoreSource -> tab.ToAlphaTex ->
// alphaTab) — never a hand-drawn stand-in — and a section row's "Mark
// in preview…" button arms it so the next bar clicked sets that
// section's start directly, the same "select, then click where"
// interaction the audio-marker UI already uses.
(function () {
  const panel = document.getElementById('gpimport-panel');
  if (!panel) return;
  const variantId = panel.dataset.variantId;

  const fileInput = document.getElementById('gpimport-file');
  const status = document.getElementById('gpimport-status');
  const trackRow = document.getElementById('gpimport-track-row');
  const trackSelect = document.getElementById('gpimport-track');
  const shiftInput = document.getElementById('gpimport-shift');
  const mappingBody = document.getElementById('gpimport-mapping');
  const mappingTable = document.getElementById('gpimport-mapping-table');
  const previewEl = document.getElementById('gpimport-preview');
  const importBtn = document.getElementById('gpimport-run');
  const includeAudio = document.getElementById('gpimport-include-audio');
  const summary = document.getElementById('gpimport-summary');

  const steps = {
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

  let score = null;
  let ourSections = []; // [{index, label, barCount, startBar}]

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
        // Find the matching repeat end (or just this one bar, if none).
        let j = i;
        let openedAt = masterBars[i].isRepeatStart ? i : i;
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

  async function loadFile(file) {
    setStatus('parsing…');
    try {
      const buf = new Uint8Array(await file.arrayBuffer());
      score = alphaTab.importer.ScoreLoader.loadScoreFromBytes(buf);
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
    setStatus(`parsed "${score.title || file.name}" — ${score.tracks.length} track(s), ${score.masterBars.length} bar(s). Loading this song's sections…`);
    step1Next.disabled = false;
    await loadOurSections();
  }

  async function loadOurSections() {
    const res = await fetch(`/api/songs/${variantId}/structure`);
    if (!res.ok) { setStatus('could not load this song\'s sections', 'err'); return; }
    const data = await res.json();
    ourSections = data.sections || [];
    renderMapping();
  }

  // A fixed, distinct color per mapped section — cycles if there are
  // more sections than colors. Used to tint both the mapping table row
  // and that section's assigned bars in the strip below, so "which bars
  // did I just assign to the Verse" reads at a glance instead of
  // requiring the user to cross-reference bar numbers by hand.
  const SECTION_COLORS = ['#af4e39', '#3d7a8a', '#7a5ca8', '#2f8f5b', '#b8860b', '#c0507e'];
  function sectionColor(i) { return SECTION_COLORS[i % SECTION_COLORS.length]; }

  let armedRow = null; // the <tr> currently waiting for a bar click, or null

  function renderMapping() {
    if (!score || !ourSections.length) {
      mappingTable.hidden = true;
      return;
    }
    const shift = parseInt(shiftInput.value, 10) || 0;
    mappingBody.innerHTML = '';
    ourSections.forEach((sec, colorIdx) => {
      const tr = document.createElement('tr');
      tr.dataset.sectionIndex = sec.index;
      tr.dataset.barCount = sec.barCount;
      tr.style.borderLeft = `4px solid ${sectionColor(colorIdx)}`;
      const suggested = sec.startBar + shift;
      tr.innerHTML = `
        <td><input type="checkbox" class="gpimport-include" checked ${sec.barCount ? '' : 'disabled'}></td>
        <td>${sec.label}</td>
        <td>${sec.barCount}</td>
        <td><input type="number" class="gpimport-start" value="${suggested}" min="0" style="width:4rem"></td>
        <td><button type="button" class="gpimport-mark">Mark in preview&hellip;</button></td>`;
      mappingBody.appendChild(tr);
    });
    mappingTable.hidden = false;
    highlightBarStrip();
  }

  mappingBody.addEventListener('click', e => {
    const btn = e.target.closest('.gpimport-mark');
    if (!btn) return;
    const tr = btn.closest('tr');
    if (armedRow === tr) {
      armedRow = null;
      btn.textContent = 'Mark in preview…';
    } else {
      mappingBody.querySelectorAll('.gpimport-mark').forEach(b => b.textContent = 'Mark in preview…');
      armedRow = tr;
      btn.textContent = 'Click a bar below ↓';
    }
    highlightBarStrip();
  });
  mappingBody.addEventListener('input', e => {
    if (e.target.classList.contains('gpimport-start')) highlightBarStrip();
  });

  // ---- The bar strip: this song's ACTUAL rendering code (POST
  // .../scores/{i}/preview -> tab.DecodeScoreSource -> tab.ToAlphaTex ->
  // alphaTab), one real bar at a time, as a row of small clickable
  // cards — not a hand-drawn stand-in for what the import would
  // produce. Clicking a card while a section row is "armed" (its own
  // "Mark in preview…" button) sets that section's GP start bar
  // directly — the actual lead-in/offset problem this wizard exists for
  // is "where does the chart's bar 0 really start in this recording,"
  // which is far easier to answer by pointing at the real notation than
  // by guessing a bar NUMBER. The sectionIndex in the preview URL is
  // arbitrary (parsing doesn't depend on which section it's "for") —
  // the first real section is reused for every ad-hoc preview here.
  let barStripCache = null; // {tex: [...], realCount}
  async function renderBarStrip() {
    if (!score) return;
    const track = score.tracks[Number(trackSelect.value) || 0];
    const realIdx = expandGPBars(score.masterBars);
    const cap = 120; // real lesson/riff files are short; a longer file just previews its first 120 real bars
    const count = Math.min(realIdx.length, cap);
    previewEl.innerHTML = '<p class="hint">rendering preview…</p>';
    const cards = [];
    for (let r = 0; r < count; r++) {
      const riff = extractRiff(track, r, 1);
      let tex = null;
      try {
        const res = await fetch(`/api/songs/${variantId}/scores/${ourSections[0].index}/preview`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ lySource: JSON.stringify(riff) }),
        });
        const data = await res.json();
        if (data.ok) tex = data.alphaTex;
      } catch (e) { /* leave this one bar unrendered */ }
      cards.push({ bar: r, tex });
    }
    barStripCache = cards;
    previewEl.innerHTML = '';
    cards.forEach(({ bar, tex }) => {
      const card = document.createElement('div');
      card.className = 'gpimport-bar-card';
      card.dataset.bar = bar;
      card.innerHTML = `<div class="gpimport-bar-label">${bar}</div>`;
      if (tex) {
        const row = document.createElement('div');
        row.className = 'alphatab-row';
        row.dataset.tex = tex;
        card.appendChild(row);
        window.AlphaTabTab?.render(row);
      }
      card.addEventListener('click', () => {
        if (!armedRow) return;
        armedRow.querySelector('.gpimport-start').value = bar;
        armedRow.querySelector('.gpimport-mark').textContent = 'Mark in preview…';
        armedRow = null;
        highlightBarStrip();
      });
      previewEl.appendChild(card);
    });
    highlightBarStrip();
    if (realIdx.length > cap) {
      const note = document.createElement('p');
      note.className = 'hint';
      note.textContent = `(showing the first ${cap} of ${realIdx.length} bars — set a start bar beyond this range by typing it directly.)`;
      previewEl.appendChild(note);
    }
  }

  // Tints each bar card by whichever section(s) currently claim it
  // (per the mapping table's own start-bar + bar-count), and gives the
  // armed row's target range a dashed outline as a live preview of
  // where the NEXT click would land.
  function highlightBarStrip() {
    if (!previewEl) return;
    previewEl.querySelectorAll('.gpimport-bar-card').forEach(card => {
      card.style.background = '';
      card.style.outline = '';
      card.title = '';
    });
    [...mappingBody.querySelectorAll('tr')].forEach((tr, colorIdx) => {
      const start = parseInt(tr.querySelector('.gpimport-start').value, 10);
      const count = Number(tr.dataset.barCount);
      if (isNaN(start) || !count) return;
      const label = tr.children[1].textContent;
      for (let b = start; b < start + count; b++) {
        const card = previewEl.querySelector(`.gpimport-bar-card[data-bar="${b}"]`);
        if (!card) continue;
        card.style.background = sectionColor(colorIdx) + '33';
        card.title = label;
        if (tr === armedRow) card.style.outline = `2px dashed ${sectionColor(colorIdx)}`;
      }
    });
  }

  shiftInput.addEventListener('input', renderMapping);
  fileInput.addEventListener('change', () => { if (fileInput.files[0]) loadFile(fileInput.files[0]); });
  trackSelect.addEventListener('change', () => { renderMapping(); renderBarStrip(); });

  // ---- Per-section extraction: GP real-bar range -> our Riff JSON ----
  function extractRiff(track, realBarStart, barCount) {
    const staff = track.staves[0];
    const numStrings = staff.tuning.length;
    const realIdx = expandGPBars(score.masterBars);
    const bars = [];
    for (let r = realBarStart; r < realBarStart + barCount; r++) {
      const gpBarIndex = realIdx[r];
      if (gpBarIndex === undefined) break; // ran past the end of the GP file
      const gpBar = staff.bars[gpBarIndex];
      const voice = gpBar.voices[0];
      const beats = (voice ? voice.beats : []).map(b => {
        if (b.isRest) return { Rest: true, Duration: String(b.duration) + '.'.repeat(b.dots) };
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
        return beat;
      });
      bars.push({ Beats: beats });
    }
    // Slide-out on GP note N becomes HasSlideFrom on beat N+1 in this
    // app's own model (a slide is described from the NEXT beat's point
    // of view — see Beat.HasSlideFrom's doc comment).
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

  async function putScore(sectionIndex, riff) {
    const res = await fetch(`/api/songs/${variantId}/scores/${sectionIndex}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ lySource: JSON.stringify(riff) }),
    });
    if (!res.ok) throw new Error(`section ${sectionIndex}: ` + (await res.text()));
  }

  async function importAudioAndSync(realBarStart0, shift) {
    if (!score.backingTrack || !score.backingTrack.rawAudioFile) return;
    const bytes = score.backingTrack.rawAudioFile;
    const form = new FormData();
    form.append('file', new Blob([bytes], { type: 'audio/mpeg' }), 'gp-backing-track.mp3');
    const upRes = await fetch(`/api/songs/${variantId}/audio/gp`, { method: 'PUT', body: form });
    if (!upRes.ok) throw new Error('audio upload failed: ' + (await upRes.text()));

    const realIdx = expandGPBars(score.masterBars);
    const points = [];
    for (let r = 0; r < realIdx.length; r++) {
      const gpBar = score.masterBars[realIdx[r]];
      const sp = gpBar.syncPoints && gpBar.syncPoints[0];
      if (!sp) continue;
      const ourBar = r - shift; // GP real-bar r corresponds to our song's real bar (r - shift)
      if (ourBar < 0) continue;
      points.push({ barIndex: ourBar, timeSeconds: sp.syncPointValue.millisecondOffset / 1000 });
    }
    if (points.length) {
      const spRes = await fetch(`/api/songs/${variantId}/audio/gp/sync-points`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ points }),
      });
      if (!spRes.ok) throw new Error('sync points failed: ' + (await spRes.text()));
    }
  }

  step1Next.addEventListener('click', () => { goToStep('align'); renderBarStrip(); });
  step2Back.addEventListener('click', () => goToStep('file'));
  step2Next.addEventListener('click', () => {
    const rows = [...mappingBody.querySelectorAll('tr')].filter(tr => tr.querySelector('.gpimport-include').checked);
    summary.textContent = rows.length
      ? `Importing ${rows.length} section(s): ${rows.map(tr => tr.children[1].textContent).join(', ')}.`
      : 'No sections selected — nothing will be imported.';
    goToStep('import');
  });
  step3Back.addEventListener('click', () => goToStep('align'));

  importBtn.addEventListener('click', async () => {
    if (!score) { setStatus('parse a file first', 'err'); return; }
    const track = score.tracks[Number(trackSelect.value) || 0];
    const shift = parseInt(shiftInput.value, 10) || 0;
    const rows = [...mappingBody.querySelectorAll('tr')].filter(tr => tr.querySelector('.gpimport-include').checked);
    importBtn.disabled = true;
    try {
      for (const tr of rows) {
        const sectionIndex = Number(tr.dataset.sectionIndex);
        const barCount = Number(tr.dataset.barCount);
        const startBar = parseInt(tr.querySelector('.gpimport-start').value, 10) || 0;
        setStatus(`importing section ${sectionIndex + 1}/${rows.length}…`);
        const riff = extractRiff(track, startBar, barCount);
        await putScore(sectionIndex, riff);
      }
      if (includeAudio.checked) {
        setStatus('uploading backing track + sync points…');
        await importAudioAndSync(0, shift);
      }
      setStatus(`imported ${rows.length} section(s). Reload the Chart tab to see them — each section still needs a "score:" line in Source pointing at it.`, 'ok');
    } catch (e) {
      setStatus('import failed: ' + e.message, 'err');
    } finally {
      importBtn.disabled = false;
    }
  });
})();

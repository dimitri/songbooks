// Scores tab: per-section LilyPond-subset score editor (note-level
// tab/staff source, see /docs/song-format#score) plus a chord-diagram
// builder for "chord:" header lines (see internal/chorddiagram). Lazily
// initialized the first time the Scores tab is opened (song.js's
// tab-switch handler calls window.ScoreEditor.init()) — most songs have
// no score: yet, so there's no reason to fetch anything at page load.
(function () {
  let initialized = false;

  function init() {
    if (initialized) return;
    initialized = true;
    initScoreSections();
    initChordDiagramEditor();
  }

  // ---- Per-section score source -------------------------------------------
  function initScoreSections() {
    const panel = document.getElementById('scores-panel');
    const variantId = panel?.dataset.variantId;
    if (!panel || !variantId) return;

    panel.querySelectorAll('.score-section').forEach(section => {
      const sectionIndex = section.dataset.sectionIndex;
      const textarea = section.querySelector('.score-source');
      const preview = section.querySelector('.score-section-preview');
      const status = section.querySelector('.score-status');
      let loaded = false;
      let previewTimer = null;

      async function loadOnce() {
        if (loaded) return;
        loaded = true;
        try {
          const res = await fetch(`/api/songs/${variantId}/scores/${sectionIndex}`);
          if (res.ok) {
            const data = await res.json();
            textarea.value = data.lySource || '';
            renderLivePreview();
          }
          // 404 (no score saved yet) just leaves the textarea at its
          // placeholder — not an error state worth surfacing.
        } catch (e) { /* leave textarea empty; user can still type+save */ }
      }

      async function renderLivePreview() {
        const src = textarea.value.trim();
        if (!src) { preview.innerHTML = ''; status.textContent = ''; status.className = 'score-status hint'; return; }
        try {
          const res = await fetch(`/api/songs/${variantId}/scores/${sectionIndex}/preview`, {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ lySource: src }),
          });
          const data = await res.json();
          if (!data.ok) {
            preview.innerHTML = '';
            status.textContent = data.error || "couldn't parse this as a score";
            status.className = 'score-status err';
            return;
          }
          status.textContent = '';
          status.className = 'score-status hint';
          const row = document.createElement('div');
          row.className = 'alphatab-row';
          row.dataset.tex = data.alphaTex;
          preview.innerHTML = '';
          preview.appendChild(row);
          window.AlphaTabTab?.render(row);
        } catch (e) { /* leave last good preview in place */ }
      }

      section.addEventListener('toggle', () => { if (section.open) loadOnce(); });
      textarea.addEventListener('input', () => {
        clearTimeout(previewTimer);
        previewTimer = setTimeout(renderLivePreview, 400);
      });

      section.querySelector('.score-save').addEventListener('click', async () => {
        status.textContent = 'saving…'; status.className = 'score-status';
        try {
          const res = await fetch(`/api/songs/${variantId}/scores/${sectionIndex}`, {
            method: 'PUT', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ lySource: textarea.value }),
          });
          if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            status.textContent = err.error || 'save failed';
            status.className = 'score-status err';
            return;
          }
          status.textContent = 'saved — reload the Chart tab to see it in place';
          status.className = 'score-status ok';
        } catch (e) {
          status.textContent = 'save failed';
          status.className = 'score-status err';
        }
      });

      section.querySelector('.score-delete').addEventListener('click', async () => {
        if (!confirm('Remove this section’s score source?')) return;
        try {
          await fetch(`/api/songs/${variantId}/scores/${sectionIndex}`, { method: 'DELETE' });
          textarea.value = '';
          preview.innerHTML = '';
          status.textContent = 'removed';
          status.className = 'score-status ok';
        } catch (e) {
          status.textContent = 'remove failed';
          status.className = 'score-status err';
        }
      });
    });
  }

  // ---- Chord diagram builder -----------------------------------------------
  // Builds a "[BaseFret:]StringSpec" (internal/chorddiagram.ParseSpec
  // grammar, byte-compatible with legacy \gtab) from a clickable grid.
  // v1 scope: a barre is inferred only from a contiguous run of STRINGS
  // sharing the exact same fret (encoded as "(111)") — the rarer shape
  // where a barre row carries extra, differently-fretted dots on top of
  // it (real example: \gtab{G}{7:XX(3121)}) isn't buildable from the
  // grid yet; that line can still be hand-edited in Source afterward.
  const FRET_ROWS = 4;

  function initChordDiagramEditor() {
    const nameInput = document.getElementById('cde-name');
    const baseFretInput = document.getElementById('cde-base-fret');
    const numStringsInput = document.getElementById('cde-num-strings');
    const grid = document.getElementById('cde-grid');
    const preview = document.getElementById('cde-preview');
    const specOut = document.getElementById('cde-spec');
    const insertBtn = document.getElementById('cde-insert');
    const status = document.getElementById('cde-status');
    if (!grid) return;

    // One state per string, left (lowest-pitched) to right: 'X', 'O', or
    // a fret row number (1-based within the visible FRET_ROWS window).
    let states = Array(6).fill('O');

    function buildGrid() {
      const n = Math.max(4, Math.min(6, parseInt(numStringsInput.value, 10) || 6));
      states = states.slice(0, n);
      while (states.length < n) states.push('O');
      grid.innerHTML = '';
      for (let c = 0; c < n; c++) {
        const col = document.createElement('div');
        col.className = 'cde-col';
        ['X', 'O', ...Array.from({ length: FRET_ROWS }, (_, i) => String(i + 1))].forEach(val => {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.textContent = val;
          btn.className = 'cde-cell' + (states[c] === val ? ' active' : '');
          btn.addEventListener('click', () => { states[c] = val; buildGrid(); update(); });
          col.appendChild(btn);
        });
        grid.appendChild(col);
      }
    }

    function buildSpec() {
      const baseFret = Math.max(1, parseInt(baseFretInput.value, 10) || 1);
      let tokens = '';
      for (let i = 0; i < states.length;) {
        const v = states[i];
        if (v === 'X' || v === 'O') { tokens += v; i++; continue; }
        let j = i;
        while (j < states.length && states[j] === v) j++;
        const runLen = j - i;
        tokens += runLen >= 2 ? '(' + v.repeat(runLen) : v;
        if (runLen >= 2) tokens += ')';
        i = j;
      }
      return (baseFret > 1 ? baseFret + ':' : '') + tokens;
    }

    let previewTimer = null;
    function update() {
      const spec = buildSpec();
      specOut.textContent = `chord: ${nameInput.value.trim() || 'Chord'} ${spec}`;
      clearTimeout(previewTimer);
      previewTimer = setTimeout(async () => {
        try {
          const res = await fetch('/api/chord-diagrams/preview', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: nameInput.value.trim() || 'Chord', spec }),
          });
          const data = await res.json();
          preview.innerHTML = data.svg || '';
        } catch (e) { /* leave last good preview in place */ }
      }, 200);
    }

    numStringsInput.addEventListener('input', () => { buildGrid(); update(); });
    baseFretInput.addEventListener('input', update);
    nameInput.addEventListener('input', update);

    insertBtn.addEventListener('click', () => {
      const textarea = document.getElementById('raw-text');
      if (!textarea) return;
      const line = `chord: ${nameInput.value.trim() || 'Chord'} ${buildSpec()}\n`;
      const start = textarea.selectionStart ?? textarea.value.length;
      const end = textarea.selectionEnd ?? textarea.value.length;
      textarea.value = textarea.value.slice(0, start) + line + textarea.value.slice(end);
      textarea.dispatchEvent(new Event('input'));
      status.textContent = 'inserted into Source — switch tabs to see it';
      status.className = 'hint ok';
    });

    buildGrid();
    update();
  }

  window.ScoreEditor = { init };
})();

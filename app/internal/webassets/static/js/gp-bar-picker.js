// The bar picker: choose, on a map of the song's Guitar Pro file, which
// bars each section shows — what a "gp[Track]: bars" line says, without
// typing it (docs/gp-file-model.md §3.1). Opened from the editor drawer's
// Chart tab, from a section on the song page, and from a selection in the
// file editor.
//
// The map is a strip of the file's bars (number, chord, markers, repeat
// signs), with a band under the bars each section already uses. Drag to
// choose bars, shift-click to extend, Ctrl/⌘-click to add a second range,
// or drag the selection's handles. It checks the bars play as many bars
// as the section has, previews them with alphaTab, and offers shortcuts:
// the same bars as an earlier section of the same name, the bars right
// after the previous section, or every section from the file's markers.
//
// GPBarPicker.open({ variantId, sections, current, onApply, onRemove })
//   sections: [{ index, label, barCount, gp: [{ track, spec }] }] —
//             barCount is null for a section whose bars come from the file
//   onApply({ index, track, spec }) and onRemove({ index, track }) may be
//   async; the picker keeps its own copy of the sections' lines up to date.
(function () {
  if (typeof alphaTab === 'undefined' || typeof GPFacts === 'undefined' || !window.GPFile) return;

  const cache = {};
  async function loadFile(variantId) {
    if (!cache[variantId]) {
      cache[variantId] = GPFile.loadBytes(variantId).then((bytes) => {
        const score = GPFile.parse(bytes);
        return { bytes, score, facts: GPFacts.extract(score) };
      });
    }
    return cache[variantId];
  }

  const fold = (s) => (s || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().trim();
  const baseLabel = (s) => fold(s).replace(/\s*\d+$/, '');
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };

  function rangesSpec(ranges, notation) {
    const out = ranges.map(([a, b]) => (a === b ? String(a) : `${a}-${b}`)).join(', ');
    return out + (notation ? ' ' + notation : '');
  }
  function normalize(ranges) {
    const bars = [...new Set(ranges.flatMap(([a, b]) => { const r = []; for (let x = Math.min(a, b); x <= Math.max(a, b); x++) r.push(x); return r; }))].sort((x, y) => x - y);
    const out = [];
    for (const n of bars) {
      const last = out[out.length - 1];
      if (last && n === last[1] + 1) last[1] = n; else out.push([n, n]);
    }
    return out;
  }
  const playedCount = (facts, ranges) => facts.played.filter((b) => ranges.some(([a, c]) => b >= a && b <= c)).length;
  const sameTrack = (a, b) => fold(a) === fold(b);

  async function open(opts) {
    const { variantId } = opts;
    const sections = opts.sections.map((s) => ({ ...s, gp: (s.gp || []).map((g) => ({ ...g })) }));
    let file;
    try { file = await loadFile(variantId); } catch (e) { alert(e.message); return; }
    const { facts } = file;
    const tracks = facts.tracks;
    const trackLabel = (i) => {
      const name = (tracks[i].name || '').trim();
      return name && tracks.filter((t) => fold(t.name) === fold(name)).length === 1 ? name : `#${i + 1}`;
    };

    const st = { cur: opts.current || 0, track: '', notation: '', ranges: [], cellW: 46, changed: false };

    // ---- Layout -------------------------------------------------------------
    const overlay = el('div', 'gpbp-overlay');
    const dlg = el('div', 'gpbp');
    dlg.setAttribute('role', 'dialog');
    dlg.setAttribute('aria-label', 'Bars from the Guitar Pro file');
    overlay.appendChild(dlg);
    const head = el('div', 'gpbp-head');
    head.append(el('h2', '', 'Bars from the Guitar Pro file'));
    const closeBtn = el('button', 'gpbp-x', '×');
    closeBtn.type = 'button';
    closeBtn.setAttribute('aria-label', 'Close');
    head.appendChild(closeBtn);
    const secRow = el('div', 'gpbp-sections');
    secRow.setAttribute('role', 'tablist');
    const stripTools = el('div', 'gpbp-striptools');
    const stripWrap = el('div', 'gpbp-stripwrap');
    const strip = el('div', 'gpbp-strip');
    const bands = el('div', 'gpbp-bands');
    stripWrap.append(strip, bands);
    const pick = el('div', 'gpbp-pick');
    const preview = el('div', 'gpbp-preview');
    const foot = el('div', 'gpbp-foot');
    dlg.append(head, secRow, stripTools, stripWrap, pick, preview, foot);
    document.body.appendChild(overlay);
    document.body.classList.add('gpbp-open');

    // ---- Sections -----------------------------------------------------------
    const cur = () => sections[st.cur];
    function refOf(sec, track) { return (sec.gp || []).find((g) => sameTrack(g.track, track)); }
    function loadSelection() {
      const sec = cur();
      const ref = refOf(sec, st.track) || (sec.gp || [])[0];
      if (ref && !refOf(sec, st.track)) st.track = ref.track;
      const p = ref ? GPFacts.parseRef(ref.spec) : null;
      st.ranges = p && !p.error ? p.ranges : [];
      st.notation = p && !p.error ? p.notation : '';
    }
    function renderSections() {
      secRow.replaceChildren();
      sections.forEach((s, i) => {
        const b = el('button', 'gpbp-sec');
        b.type = 'button';
        b.setAttribute('role', 'tab');
        b.setAttribute('aria-selected', i === st.cur ? 'true' : 'false');
        if (!(s.gp || []).length) b.classList.add('none');
        b.append(el('span', '', s.label), el('span', 'n', s.barCount == null ? 'file' : String(s.barCount)));
        b.addEventListener('click', () => { st.cur = i; loadSelection(); render(); });
        secRow.appendChild(b);
      });
    }

    // ---- The strip ----------------------------------------------------------
    const marker = (n) => (facts.markers.find((m) => m.bar === n) || {}).text || '';
    const rep = (n) => facts.repeats.find((r) => r.bar === n) || {};
    function chordOf(n) {
      const c = (facts.chords[n - 1] || [])[0];
      return c ? c.name : '';
    }
    const cells = [];
    function buildStrip() {
      strip.replaceChildren();
      for (let n = 1; n <= facts.bars; n++) {
        const c = el('div', 'gpbp-cell');
        c.dataset.bar = n;
        const r = rep(n);
        if (r.start) c.classList.add('rs');
        if (r.count) c.classList.add('re');
        const m = marker(n);
        if (m) { const mk = el('span', 'mk', m); c.appendChild(mk); }
        c.append(el('span', 'b', String(n)), el('span', 'ch', chordOf(n)));
        if (r.endings) { let e = []; for (let k = 0; k < 8; k++) if (r.endings & (1 << k)) e.push(k + 1); c.appendChild(el('span', 've', e.join(',') + '.')); }
        if (r.count) c.appendChild(el('span', 'rc', '×' + r.count));
        strip.appendChild(c);
        cells[n] = c;
      }
      const lh = el('span', 'gpbp-handle l');
      const rh = el('span', 'gpbp-handle r');
      lh.title = rh.title = 'Drag to change';
      strip.append(lh, rh);
      strip.style.setProperty('--cw', st.cellW + 'px');
    }
    function renderStrip() {
      const inSel = (n) => st.ranges.some(([a, b]) => n >= a && n <= b);
      for (let n = 1; n <= facts.bars; n++) cells[n].classList.toggle('sel', inSel(n));
      const [lh, rh] = strip.querySelectorAll('.gpbp-handle');
      if (st.ranges.length) {
        const first = st.ranges[0][0], last = st.ranges[st.ranges.length - 1][1];
        lh.hidden = rh.hidden = false;
        lh.style.left = cells[first].offsetLeft - 4 + 'px';
        rh.style.left = cells[last].offsetLeft + cells[last].offsetWidth - 3 + 'px';
      } else lh.hidden = rh.hidden = true;
      // Bands: every other section's bars for this track (or every track).
      bands.replaceChildren();
      const lanes = [];
      sections.forEach((s, i) => {
        if (i === st.cur) return;
        for (const g of s.gp || []) {
          if (st.track && g.track && !sameTrack(g.track, st.track)) continue;
          const p = GPFacts.parseRef(g.spec);
          if (p.error) continue;
          for (const [a, b] of p.ranges) {
            let lane = lanes.findIndex((end) => end < a);
            if (lane < 0) { lane = lanes.length; lanes.push(0); }
            lanes[lane] = b;
            const band = el('div', 'gpbp-band', s.label);
            band.title = `${s.label}: ${g.track ? 'gp[' + g.track + ']' : 'gp'} ${g.spec}`;
            band.style.left = cells[Math.min(a, facts.bars)].offsetLeft + 'px';
            band.style.width = cells[Math.min(b, facts.bars)].offsetLeft + cells[Math.min(b, facts.bars)].offsetWidth - cells[Math.min(a, facts.bars)].offsetLeft + 'px';
            band.style.top = lane * 18 + 'px';
            band.addEventListener('click', () => { st.cur = i; loadSelection(); render(); });
            bands.appendChild(band);
          }
        }
      });
      if (st.ranges.length) {
        const [a] = st.ranges[0], b = st.ranges[st.ranges.length - 1][1];
        const band = el('div', 'gpbp-band cur', cur().label);
        band.style.left = cells[a].offsetLeft + 'px';
        band.style.width = cells[b].offsetLeft + cells[b].offsetWidth - cells[a].offsetLeft + 'px';
        band.style.top = lanes.length * 18 + 'px';
        bands.appendChild(band);
        lanes.push(0);
      }
      bands.style.height = Math.max(1, lanes.length) * 18 + 'px';
    }

    // Choosing bars with the pointer.
    let drag = null;
    const barAt = (e) => {
      const r = strip.getBoundingClientRect();
      const x = e.clientX - r.left + strip.scrollLeft;
      for (let n = 1; n <= facts.bars; n++) {
        const c = cells[n];
        if (x < c.offsetLeft + c.offsetWidth) return n;
      }
      return facts.bars;
    };
    strip.addEventListener('pointerdown', (e) => {
      const handle = e.target.closest('.gpbp-handle');
      const n = barAt(e);
      if (handle && st.ranges.length) {
        const fixed = handle.classList.contains('l') ? st.ranges[st.ranges.length - 1][1] : st.ranges[0][0];
        drag = { anchor: fixed, keep: [] };
      } else if (e.shiftKey && st.ranges.length) {
        const last = st.ranges[st.ranges.length - 1];
        drag = { anchor: last[0], keep: st.ranges.slice(0, -1) };
      } else if (e.metaKey || e.ctrlKey) {
        drag = { anchor: n, keep: st.ranges.slice() };
      } else {
        drag = { anchor: n, keep: [] };
      }
      strip.setPointerCapture(e.pointerId);
      move(e);
      e.preventDefault();
    });
    function move(e) {
      if (!drag) return;
      const n = barAt(e);
      st.ranges = normalize([...drag.keep, [Math.min(drag.anchor, n), Math.max(drag.anchor, n)]]);
      renderStrip();
      renderPick();
    }
    strip.addEventListener('pointermove', move);
    strip.addEventListener('pointerup', () => { if (drag) { drag = null; schedulePreview(); } });
    strip.addEventListener('pointercancel', () => { drag = null; });

    // ---- Track, notation, length check, shortcuts --------------------------
    function renderPick() {
      pick.replaceChildren();
      const row = el('div', 'gpbp-row');
      row.appendChild(el('span', 'gpbp-lbl', 'Track'));
      const opts = [['', 'All tracks'], ...tracks.map((t, i) => [trackLabel(i), t.name || `Track ${i + 1}`])];
      for (const [v, l] of opts) {
        const b = el('button', 'gpbp-chip' + (sameTrack(st.track, v) ? ' on' : ''), l);
        b.type = 'button';
        b.addEventListener('click', () => { st.track = v; const ref = refOf(cur(), v); if (ref) { const p = GPFacts.parseRef(ref.spec); if (!p.error) { st.ranges = p.ranges; st.notation = p.notation; } } render(); });
        row.appendChild(b);
      }
      const sel = el('select', 'gpbp-notation');
      for (const [v, l] of [['', 'As each reader reads it'], ['tab', 'Tab'], ['staff', 'Staff'], ['tab+staff', 'Staff and tab'], ['slash', 'Slashes']]) sel.add(new Option(l, v));
      sel.value = st.notation;
      sel.setAttribute('aria-label', 'Drawn as');
      sel.addEventListener('change', () => { st.notation = sel.value; renderPick(); schedulePreview(); });
      row.appendChild(sel);
      pick.appendChild(row);

      // How long the chosen bars play, against the section.
      const info = el('div', 'gpbp-row');
      const target = cur().barCount;
      const played = playedCount(facts, st.ranges);
      const check = el('span', 'gpbp-check');
      if (!st.ranges.length) { check.textContent = 'Choose bars on the strip: drag across them.'; check.classList.add('muted'); }
      else if (target == null) { check.textContent = `These bars play ${played} bars; the section takes its length from them.`; check.classList.add('ok'); }
      else if (played === target) { check.textContent = `Bars ${rangesSpec(st.ranges, '')} play ${played} bars, as the section has.`; check.classList.add('ok'); }
      else { check.textContent = `These bars play ${played} bars; the section has ${target}.`; check.classList.add('warn'); }
      info.appendChild(check);
      pick.appendChild(info);

      const tools = el('div', 'gpbp-row');
      const btn = (label, fn, title) => { const b = el('button', 'gpbp-ghost', label); b.type = 'button'; if (title) b.title = title; b.addEventListener('click', fn); tools.appendChild(b); return b; };
      // The same bars as an earlier section of the same name.
      const twin = sections.slice(0, st.cur).reverse().find((s) => baseLabel(s.label) === baseLabel(cur().label) && (s.gp || []).length);
      if (twin) btn(`Same as ${twin.label}`, () => { const g = refOf(twin, st.track) || twin.gp[0]; const p = GPFacts.parseRef(g.spec); st.track = g.track; st.ranges = p.ranges || []; st.notation = p.notation || ''; render(); });
      // Right after the previous section's bars.
      const prev = sections.slice(0, st.cur).reverse().find((s) => (s.gp || []).length);
      if (prev) btn(`Continue after ${prev.label}`, () => {
        const g = refOf(prev, st.track) || prev.gp[0];
        const p = GPFacts.parseRef(g.spec);
        if (p.error) return;
        const start = p.ranges[p.ranges.length - 1][1] + 1;
        if (start > facts.bars) return;
        const want = cur().barCount || 4;
        let end = start;
        while (end < facts.bars && playedCount(facts, [[start, end]]) < want) end++;
        st.track = g.track;
        st.ranges = [[start, end]];
        render();
      });
      if (facts.markers.length) btn('Match the file’s markers', matchMarkers, 'Every section without bars gets the bars of the marker with its name');
      if (tools.childElementCount) pick.appendChild(tools);
      const line = el('code', 'gpbp-line', st.ranges.length ? `${st.track ? 'gp[' + st.track + ']' : 'gp'}: ${rangesSpec(st.ranges, st.notation)}` : '');
      pick.appendChild(line);
    }

    // Every section without bars, from the marker with its name (or, for a
    // numbered one, the marker the same number of times).
    async function matchMarkers() {
      const marks = facts.markers.map((m, i) => ({ ...m, end: (facts.markers[i + 1] ? facts.markers[i + 1].bar : facts.bars + 1) - 1 }));
      const used = new Map();
      let done = 0;
      for (const s of sections) {
        if ((s.gp || []).length) continue;
        const base = baseLabel(s.label);
        const k = used.get(base) || 0;
        const hits = marks.filter((m) => baseLabel(m.text) === base);
        const m = hits[k] || hits[hits.length - 1];
        if (!m) continue;
        used.set(base, k + 1);
        await apply(s, st.track, rangesSpec([[m.bar, m.end]], st.notation));
        done++;
      }
      say(done ? `${done} section${done > 1 ? 's' : ''} matched to the file’s markers.` : 'No section without bars matches a marker’s name.');
      loadSelection();
      render();
    }

    // ---- Preview ------------------------------------------------------------
    let previewTimer = null, api = null;
    function schedulePreview() { clearTimeout(previewTimer); previewTimer = setTimeout(drawPreview, 250); }
    function drawPreview() {
      preview.replaceChildren();
      if (!st.ranges.length) return;
      const score = GPFile.parse(file.bytes);
      let tracksIdx = score.tracks.map((t, i) => i);
      if (st.track) { const i = GPFacts.findTrack(score.tracks, st.track); if (i >= 0) tracksIdx = [i]; }
      const n = st.notation;
      if (n) for (const i of tracksIdx) for (const sf of score.tracks[i].staves) if (!sf.isPercussion) { sf.showTablature = n === 'tab' || n === 'tab+staff'; sf.showStandardNotation = n === 'staff' || n === 'tab+staff'; sf.showSlash = n === 'slash'; }
      const [a] = st.ranges[0];
      const b = st.ranges[st.ranges.length - 1][1];
      const host = el('div', 'gpbp-preview-host');
      preview.appendChild(host);
      const s = new alphaTab.Settings();
      s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
      s.core.useWorkers = false;
      s.core.enableLazyLoading = false;
      s.player.enablePlayer = false;
      const ink = (name) => { const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(getComputedStyle(document.body).getPropertyValue(name).trim()); return m ? new alphaTab.model.Color(parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)) : null; };
      const fg = ink('--fg');
      if (fg) { const r = s.display.resources; r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = fg; r.staffLineColor = ink('--fg-muted') || fg; }
      s.display.startBar = a;
      s.display.barCount = Math.min(b - a + 1, 16);
      for (const e of ['ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright', 'GuitarTuning', 'ChordDiagrams', 'EffectTempo', 'EffectDynamics']) {
        if (alphaTab.NotationElement[e] !== undefined) s.notation.elements.set(alphaTab.NotationElement[e], false);
      }
      try { api = new alphaTab.AlphaTabApi(host, s); api.renderScore(score, tracksIdx); } catch (e) { host.textContent = 'Couldn’t draw these bars.'; }
    }

    // ---- Apply, remove, close ------------------------------------------------
    const status = el('span', 'gpbp-status');
    function say(msg) { status.textContent = msg; }
    async function apply(sec, track, spec) {
      await opts.onApply({ index: sec.index, track, spec });
      const g = refOf(sec, track);
      if (g) g.spec = spec; else sec.gp.push({ track, spec });
      st.changed = true;
    }
    const removeBtn = el('button', 'ghost-btn', 'Remove');
    removeBtn.type = 'button';
    removeBtn.addEventListener('click', async () => {
      const sec = cur();
      if (!refOf(sec, st.track)) return;
      await opts.onRemove({ index: sec.index, track: st.track });
      sec.gp = sec.gp.filter((g) => !sameTrack(g.track, st.track));
      st.changed = true;
      say(`${sec.label}: no bars from the file for ${st.track || 'all tracks'}.`);
      loadSelection();
      render();
    });
    const doneBtn = el('button', 'ghost-btn', 'Done');
    doneBtn.type = 'button';
    const useBtn = el('button', 'primary', 'Use these bars');
    useBtn.type = 'button';
    useBtn.addEventListener('click', async () => {
      if (!st.ranges.length) { say('Choose bars on the strip first.'); return; }
      const sec = cur();
      try { await apply(sec, st.track, rangesSpec(st.ranges, st.notation)); } catch (e) { say(e.message); return; }
      say(`${sec.label}: ${st.track ? 'gp[' + st.track + ']' : 'gp'}: ${rangesSpec(st.ranges, st.notation)}.`);
      // On to the next section without bars, starting where this one ends.
      const next = sections.findIndex((s, i) => i > st.cur && !(s.gp || []).length);
      if (next >= 0) {
        const end = st.ranges[st.ranges.length - 1][1];
        st.cur = next;
        loadSelection();
        if (!st.ranges.length && end < facts.bars) {
          const want = cur().barCount || 4;
          let e2 = end + 1;
          while (e2 < facts.bars && playedCount(facts, [[end + 1, e2]]) < want) e2++;
          st.ranges = [[end + 1, e2]];
        }
      }
      render();
    });
    foot.append(status, removeBtn, doneBtn, useBtn);

    // Zoom.
    const zoom = (d) => { st.cellW = Math.max(30, Math.min(90, st.cellW + d)); strip.style.setProperty('--cw', st.cellW + 'px'); requestAnimationFrame(renderStrip); };
    stripTools.append(el('span', 'gpbp-hint', 'Drag across bars · shift-click to extend · Ctrl/⌘-click for another range'));
    const zo = el('button', 'gpbp-ghost', '−'); zo.type = 'button'; zo.setAttribute('aria-label', 'Narrower bars'); zo.addEventListener('click', () => zoom(-8));
    const zi = el('button', 'gpbp-ghost', '+'); zi.type = 'button'; zi.setAttribute('aria-label', 'Wider bars'); zi.addEventListener('click', () => zoom(8));
    stripTools.append(zo, zi);

    function render() {
      renderSections();
      renderStrip();
      renderPick();
      removeBtn.hidden = !refOf(cur(), st.track);
      schedulePreview();
      const first = st.ranges.length ? cells[st.ranges[0][0]] : null;
      if (first) stripWrap.scrollLeft = Math.max(0, first.offsetLeft - 60);
    }

    function close() {
      overlay.remove();
      document.body.classList.remove('gpbp-open');
      document.removeEventListener('keydown', onKey, true);
      if (opts.onClose) opts.onClose(st.changed);
    }
    const onKey = (e) => { if (e.key === 'Escape' && !document.querySelector('.gpe-pop')) { e.preventDefault(); close(); } };
    document.addEventListener('keydown', onKey, true);
    closeBtn.addEventListener('click', close);
    doneBtn.addEventListener('click', close);
    overlay.addEventListener('pointerdown', (e) => { if (e.target === overlay) close(); });

    if (opts.track === null) {
      // Another track for this section: the first one it doesn't have yet.
      const used = (cur().gp || []).map((g) => fold(g.track));
      st.track = tracks.map((t, i) => trackLabel(i)).find((l) => !used.includes(fold(l))) || '';
    } else {
      if (opts.track !== undefined) st.track = opts.track;
      if (opts.ranges) st.ranges = normalize(opts.ranges); else loadSelection();
    }
    buildStrip();
    requestAnimationFrame(render);
  }

  window.GPBarPicker = { open, loadFile };
})();

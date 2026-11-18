// The bar picker: choose, on a map of the song's Guitar Pro file, which
// bars each section shows — what a "gp[Track]: bars" line says, without
// typing it (docs/gp-file-model.md §3.1). Opened from the editor drawer's
// Chart tab, from a section on the song page, and from a selection in the
// file editor.
//
// The map is the file itself, drawn by alphaTab on one long line (the
// chosen track, or every track, with its chords, markers and repeat
// signs), with a band under the bars each section already uses. Bars are
// chosen like a period on a calendar: click the first bar, then the last;
// "+ Another range" adds bars from elsewhere in the file. It checks the bars play as many bars
// as the section has, and offers shortcuts:
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

    const st = { cur: opts.current || 0, track: '', notation: '', ranges: [], scale: 0.9, changed: false, drawn: null };

    // ---- Layout -------------------------------------------------------------
    const overlay = el('div', 'gpbp-overlay');
    const dlg = el('div', 'gpbp');
    dlg.setAttribute('role', 'dialog');
    dlg.setAttribute('aria-label', t('Bars from the Guitar Pro file'));
    overlay.appendChild(dlg);
    const head = el('div', 'gpbp-head');
    head.append(el('h2', '', t('Bars from the Guitar Pro file')));
    const closeBtn = el('button', 'gpbp-x', '×');
    closeBtn.type = 'button';
    closeBtn.setAttribute('aria-label', t('Close'));
    head.appendChild(closeBtn);
    const secRow = el('div', 'gpbp-sections');
    secRow.setAttribute('role', 'tablist');
    const stripTools = el('div', 'gpbp-striptools');
    const stripWrap = el('div', 'gpbp-stripwrap');
    const strip = el('div', 'gpbp-strip');
    const ruler = el('div', 'gpbp-ruler');
    const scoreHost = el('div', 'gpbp-score');
    const over = el('div', 'gpbp-over');
    strip.append(ruler, scoreHost, over);
    const bands = el('div', 'gpbp-bands');
    stripWrap.append(strip, bands);
    const pick = el('div', 'gpbp-pick');
    const foot = el('div', 'gpbp-foot');
    dlg.append(head, secRow, stripTools, stripWrap, pick, foot);
    document.body.appendChild(overlay);
    document.body.classList.add('gpbp-open');

    // ---- Sections -----------------------------------------------------------
    const cur = () => sections[st.cur];
    function refOf(sec, track) { return (sec.gp || []).find((g) => sameTrack(g.track, track)); }
    function loadSelection() {
      st.pending = null;
      st.adding = false;
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
        b.append(el('span', '', s.label), el('span', 'n', s.barCount == null ? t('file') : String(s.barCount)));
        b.addEventListener('click', () => { st.cur = i; loadSelection(); render(); });
        secRow.appendChild(b);
      });
    }

    // ---- The strip: the file itself, drawn by alphaTab on one line -----------
    // The chosen track (or every track) in tab, staff or both, with its
    // chord names, markers and repeat signs; the selection, its handles and
    // the bar numbers are laid over it at the bars' real positions.
    let api = null;
    let rects = []; // bar (1-based) → { x, w } in the drawing
    const ink = (name) => { const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(getComputedStyle(document.body).getPropertyValue(name).trim()); return m ? new alphaTab.model.Color(parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)) : null; };
    function drawScore() {
      st.drawn = st.track + '|' + st.notation + '|' + st.scale;
      if (api) { try { api.destroy(); } catch (e) { /* gone */ } api = null; }
      scoreHost.replaceChildren();
      rects = [];
      over.replaceChildren();
      ruler.replaceChildren();
      strip.classList.add('loading');
      const score = GPFile.parse(file.bytes);
      for (const t of score.tracks) for (const sf of t.staves) if (sf.chords) for (const c of sf.chords.values()) if (!(c.strings || []).some((f) => f >= 0)) c.showDiagram = false;
      let idx = score.tracks.map((t, i) => i);
      if (st.track) { const i = GPFacts.findTrack(score.tracks, st.track); if (i >= 0) idx = [i]; }
      const n = st.notation;
      if (n) for (const i of idx) for (const sf of score.tracks[i].staves) if (!sf.isPercussion) { sf.showTablature = n === 'tab' || n === 'tab+staff'; sf.showStandardNotation = n === 'staff' || n === 'tab+staff'; sf.showSlash = n === 'slash'; }
      const s = new alphaTab.Settings();
      s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
      s.core.useWorkers = false;
      s.core.enableLazyLoading = false;
      s.player.enablePlayer = false;
      s.display.layoutMode = alphaTab.LayoutMode.Horizontal;
      s.display.scale = st.scale;
      s.notation.rhythmMode = alphaTab.TabRhythmMode.ShowWithBars;
      const hide = ['ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright', 'GuitarTuning', 'ChordDiagrams', 'EffectTempo', 'EffectDynamics', 'BarNumber'];
      if (idx.length === 1) hide.push('TrackNames');
      for (const e of hide) if (alphaTab.NotationElement[e] !== undefined) s.notation.elements.set(alphaTab.NotationElement[e], false);
      const fg = ink('--fg');
      if (fg) { const r = s.display.resources; r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = fg; r.staffLineColor = ink('--fg-muted') || fg; }
      // Chord names in the chart's own font, large enough to read at a
      // glance (alphaTab's default is a small italic serif).
      s.display.resources.elementFonts.set(alphaTab.NotationElement.EffectChordNames,
        new alphaTab.model.Font('EB Garamond', 19, alphaTab.model.FontStyle.Italic, alphaTab.model.FontWeight.Bold));
      try {
        api = new alphaTab.AlphaTabApi(scoreHost, s);
        api.postRenderFinished.on(measure);
        api.renderScore(score, idx);
      } catch (e) {
        scoreHost.textContent = t('Couldn’t draw the file.');
      }
    }
    // Where each bar is in the drawing, then the overlay over it.
    function measure() {
      const look = api && api.renderer.boundsLookup;
      const sys = look && look.staffSystems && look.staffSystems[0];
      if (!sys) return;
      rects = [];
      for (const mb of sys.bars) rects[mb.index + 1] = { x: mb.realBounds.x, w: mb.realBounds.w };
      strip.classList.remove('loading');
      scoreHost.querySelectorAll('svg text').forEach((t) => { if (/rendered by alphaTab/i.test(t.textContent)) t.remove(); });
      ruler.replaceChildren();
      for (let n = 1; n < rects.length; n++) {
        if (!rects[n]) continue;
        const num = el('span', 'gpbp-num', String(n));
        num.style.left = rects[n].x + 'px';
        ruler.appendChild(num);
      }
      strip.style.width = scoreHost.scrollWidth + 'px';
      renderStrip();
      scrollToSelection();
    }
    const rectOf = (n) => rects[Math.max(1, Math.min(n, rects.length - 1))];
    function span(a, b) { const r1 = rectOf(a), r2 = rectOf(b); return r1 && r2 ? { left: r1.x, width: r2.x + r2.w - r1.x } : null; }
    function scrollToSelection() {
      if (!st.ranges.length) return;
      const r = rectOf(st.ranges[0][0]);
      if (r) stripWrap.scrollLeft = Math.max(0, r.x - 80);
    }
    function renderStrip() {
      over.replaceChildren();
      if (rects.length) {
        for (const [a, b] of st.ranges) {
          const sp = span(a, b);
          if (!sp) continue;
          const box = el('div', 'gpbp-sel');
          Object.assign(box.style, { left: sp.left + 'px', width: sp.width + 'px' });
          over.appendChild(box);
        }
        // Between the two clicks: from the first bar to the one under the
        // pointer, as a calendar shows a period being chosen.
        if (st.pending) {
          const to = st.hover || st.pending.start;
          const sp = span(Math.min(st.pending.start, to), Math.max(st.pending.start, to));
          if (sp) {
            const box = el('div', 'gpbp-sel pending');
            Object.assign(box.style, { left: sp.left + 'px', width: sp.width + 'px' });
            over.appendChild(box);
          }
          const first = span(st.pending.start, st.pending.start);
          const mark = el('div', 'gpbp-start');
          Object.assign(mark.style, { left: first.left + 'px', width: first.width + 'px' });
          over.appendChild(mark);
        }
      }
      renderHint();
      // Bands: every other section's bars for this track (or every track).
      bands.replaceChildren();
      if (!rects.length) return;
      const lanes = [];
      const band = (label, a, b, cls, title, onClick) => {
        const sp = span(a, b);
        if (!sp) return;
        let lane = lanes.findIndex((end) => end < a);
        if (lane < 0) { lane = lanes.length; lanes.push(0); }
        lanes[lane] = b;
        const d = el('div', 'gpbp-band' + (cls ? ' ' + cls : ''), label);
        if (title) d.title = title;
        Object.assign(d.style, { left: sp.left + 'px', width: sp.width + 'px', top: lane * 18 + 'px' });
        if (onClick) d.addEventListener('click', onClick);
        bands.appendChild(d);
      };
      sections.forEach((sec, i) => {
        if (i === st.cur) return;
        for (const g of sec.gp || []) {
          if (st.track && g.track && !sameTrack(g.track, st.track)) continue;
          const pr = GPFacts.parseRef(g.spec);
          if (pr.error) continue;
          for (const [a, b] of pr.ranges) band(sec.label, a, b, '', `${sec.label}: ${g.track ? 'gp[' + g.track + ']' : 'gp'} ${g.spec}`, () => { st.cur = i; loadSelection(); render(); });
        }
      });
      for (const [a, b] of st.ranges) band(cur().label, a, b, 'cur');
      bands.style.height = Math.max(1, lanes.length) * 18 + 'px';
    }

    // Choosing bars like a period on a calendar: click the first bar, then
    // the last one. A new first click starts over, unless "Another range"
    // asked to add one.
    const barAt = (e) => {
      const x = e.clientX - over.getBoundingClientRect().left;
      for (let n = 1; n < rects.length; n++) if (rects[n] && x < rects[n].x + rects[n].w) return n;
      return Math.max(1, rects.length - 1);
    };
    over.addEventListener('click', (e) => {
      if (!rects.length) return;
      const n = barAt(e);
      if (!st.pending) {
        st.pending = { start: n, keep: st.adding ? st.ranges.slice() : [], before: st.ranges.slice() };
        if (!st.adding) st.ranges = [];
        st.hover = n;
      } else {
        const a2 = Math.min(st.pending.start, n), b2 = Math.max(st.pending.start, n);
        st.ranges = normalize([...st.pending.keep, [a2, b2]]);
        st.pending = null;
        st.adding = false;
      }
      renderStrip();
      renderPick();
    });
    over.addEventListener('pointermove', (e) => {
      if (!st.pending || !rects.length) return;
      const n = barAt(e);
      if (n !== st.hover) { st.hover = n; renderStrip(); }
    });
    function cancelPending() {
      if (!st.pending) return false;
      st.ranges = st.pending.before;
      st.pending = null;
      st.adding = false;
      renderStrip();
      renderPick();
      return true;
    }
    function renderHint() {
      if (st.pending) hint.textContent = t('From bar %d: now click the last bar. Esc to cancel.', st.pending.start);
      else if (st.adding) hint.textContent = t('Click the first bar of the other range.');
      else if (st.ranges.length) hint.textContent = t('Click a bar to choose again.');
      else hint.textContent = t('Click the first bar, then the last bar.');
    }

    // ---- Track, notation, length check, shortcuts --------------------------
    function renderPick() {
      pick.replaceChildren();
      const row = el('div', 'gpbp-row');
      row.appendChild(el('span', 'gpbp-lbl', t('Track')));
      const opts = [['', t('All tracks')], ...tracks.map((tr, i) => [trackLabel(i), tr.name || t('Track %d', i + 1)])];
      for (const [v, l] of opts) {
        const b = el('button', 'gpbp-chip' + (sameTrack(st.track, v) ? ' on' : ''), l);
        b.type = 'button';
        b.addEventListener('click', () => { st.track = v; const ref = refOf(cur(), v); if (ref) { const p = GPFacts.parseRef(ref.spec); if (!p.error) { st.ranges = p.ranges; st.notation = p.notation; } } render(); });
        row.appendChild(b);
      }
      const sel = el('select', 'gpbp-notation');
      for (const [v, l] of [['', t('As each reader reads it')], ['tab', t('Tab')], ['staff', t('notation|Staff')], ['tab+staff', t('Staff and tab')], ['slash', t('Slashes')]]) sel.add(new Option(l, v));
      sel.value = st.notation;
      sel.setAttribute('aria-label', t('Drawn as'));
      sel.addEventListener('change', () => { st.notation = sel.value; render(); });
      row.appendChild(sel);
      pick.appendChild(row);

      // How long the chosen bars play, against the section.
      const info = el('div', 'gpbp-row');
      const target = cur().barCount;
      const played = playedCount(facts, st.ranges);
      const check = el('span', 'gpbp-check');
      if (!st.ranges.length) { check.textContent = t('Choose bars on the strip: click the first bar, then the last.'); check.classList.add('muted'); }
      else if (target == null) { check.textContent = tn(played, 'These bars play %d bar; the section takes its length from them.', 'These bars play %d bars; the section takes its length from them.'); check.classList.add('ok'); }
      else if (played === target) { check.textContent = t(played === 1 ? 'Bars %s play %d bar, as the section has.' : 'Bars %s play %d bars, as the section has.', rangesSpec(st.ranges, ''), played); check.classList.add('ok'); }
      else { check.textContent = t(played === 1 ? 'These bars play %d bar; the section has %d.' : 'These bars play %d bars; the section has %d.', played, target); check.classList.add('warn'); }
      info.appendChild(check);
      pick.appendChild(info);

      const tools = el('div', 'gpbp-row');
      const btn = (label, fn, title) => { const b = el('button', 'gpbp-ghost', label); b.type = 'button'; if (title) b.title = title; b.addEventListener('click', fn); tools.appendChild(b); return b; };
      // The same bars as an earlier section of the same name.
      const twin = sections.slice(0, st.cur).reverse().find((s) => baseLabel(s.label) === baseLabel(cur().label) && (s.gp || []).length);
      if (twin) btn(t('Same as %s', twin.label), () => { const g = refOf(twin, st.track) || twin.gp[0]; const p = GPFacts.parseRef(g.spec); st.track = g.track; st.ranges = p.ranges || []; st.notation = p.notation || ''; render(); });
      // Right after the previous section's bars.
      const prev = sections.slice(0, st.cur).reverse().find((s) => (s.gp || []).length);
      if (prev) btn(t('Continue after %s', prev.label), () => {
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
      if (facts.markers.length) btn(t('Match the file’s markers'), matchMarkers, t('Every section without bars gets the bars of the marker with its name'));
      if (tools.childElementCount) pick.appendChild(tools);
      if (st.ranges.length) {
        const rr = el('div', 'gpbp-row');
        rr.appendChild(el('span', 'gpbp-lbl', t('Bars')));
        st.ranges.forEach(([a2, b2], k) => {
          const chip = el('span', 'gpbp-range', a2 === b2 ? String(a2) : `${a2}–${b2}`);
          if (st.ranges.length > 1) {
            const x = el('button', 'gpbp-range-x', '×');
            x.type = 'button';
            x.setAttribute('aria-label', t('Remove bars %d–%d', a2, b2));
            x.addEventListener('click', () => { st.ranges.splice(k, 1); render(); });
            chip.appendChild(x);
          }
          rr.appendChild(chip);
        });
        const more = el('button', 'gpbp-ghost' + (st.adding ? ' on' : ''), st.adding ? t('Click its first bar…') : t('+ Another range'));
        more.type = 'button';
        more.title = t('Add bars elsewhere in the file to this section');
        more.addEventListener('click', () => { st.adding = !st.adding; st.pending = null; renderStrip(); renderPick(); });
        rr.appendChild(more);
        pick.appendChild(rr);
      }
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
      say(done ? tn(done, '%d section matched to the file’s markers.', '%d sections matched to the file’s markers.') : t('No section without bars matches a marker’s name.'));
      loadSelection();
      render();
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
    const removeBtn = el('button', 'ghost-btn', t('Remove'));
    removeBtn.type = 'button';
    removeBtn.addEventListener('click', async () => {
      const sec = cur();
      if (!refOf(sec, st.track)) return;
      await opts.onRemove({ index: sec.index, track: st.track });
      sec.gp = sec.gp.filter((g) => !sameTrack(g.track, st.track));
      st.changed = true;
      say(st.track ? t('%s: no bars from the file for %s.', sec.label, st.track) : t('%s: no bars from the file for all tracks.', sec.label));
      loadSelection();
      render();
    });
    const doneBtn = el('button', 'ghost-btn', t('Done'));
    doneBtn.type = 'button';
    const useBtn = el('button', 'primary', t('Use these bars'));
    useBtn.type = 'button';
    useBtn.addEventListener('click', async () => {
      if (!st.ranges.length) { say(t('Choose bars on the strip first.')); return; }
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

    // Zoom: the drawing's scale.
    const zoom = (d) => { st.scale = Math.round(Math.max(0.5, Math.min(1.3, st.scale + d)) * 10) / 10; render(); };
    const hint = el('span', 'gpbp-hint');
    hint.setAttribute('aria-live', 'polite');
    stripTools.append(hint);
    const zo = el('button', 'gpbp-ghost', '−'); zo.type = 'button'; zo.setAttribute('aria-label', t('Smaller')); zo.addEventListener('click', () => zoom(-0.1));
    const zi = el('button', 'gpbp-ghost', '+'); zi.type = 'button'; zi.setAttribute('aria-label', t('Larger')); zi.addEventListener('click', () => zoom(0.1));
    stripTools.append(zo, zi);

    function render() {
      renderSections();
      renderPick();
      removeBtn.hidden = !refOf(cur(), st.track);
      // A different track, notation or scale is a new drawing.
      if (st.drawn !== st.track + '|' + st.notation + '|' + st.scale) drawScore();
      else { renderStrip(); scrollToSelection(); }
    }

    function close() {
      if (api) { try { api.destroy(); } catch (e) { /* gone */ } }
      overlay.remove();
      document.body.classList.remove('gpbp-open');
      document.removeEventListener('keydown', onKey, true);
      if (opts.onClose) opts.onClose(st.changed);
    }
    const onKey = (e) => {
      if (e.key !== 'Escape' || document.querySelector('.gpe-pop')) return;
      e.preventDefault();
      if (!cancelPending()) close();
    };
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
    render();
  }

  window.GPBarPicker = { open, loadFile };
})();

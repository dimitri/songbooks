// The Sync sheet's waveform (sync-sheet.js): the recording you can zoom
// from the whole song to a few beats and scroll, with the chart drawn on
// it — the sections, the bar lines and their numbers, the chords — the
// beats the recording's analysis found, faintly, and each section's
// start as a marker to drag. Guitar Pro's audio track, Soundslice's
// syncpoints, Ableton's warp markers (app/reports/sync-ui-review.md).
//
// Gestures: trackpad pinch, Ctrl/⌘ + wheel or a phone's two fingers zoom
// around the pointer; the wheel or a drag scrolls; a click or tap
// anywhere but a marker's handle seeks (on a section's band it also picks
// that section); the playhead line drags to scrub; a marker drags by its
// handles only — its name in the top band, its grip at the bottom —
// (snapping, Alt to place it freely), so a click on its line seeks like
// anywhere else; the overview strip under it shows the window and moves
// it. While playing, the view follows the playhead until you
// scroll or zoom yourself.
//
// Canvas, at the screen's pixel ratio; peaks at several resolutions
// (sync-wave-math.js) so a frame costs one pass over the pixel columns;
// drawn on requestAnimationFrame, only when something moved.
(function () {
  const M = window.SyncWaveMath;
  if (!M) return;

  // Lanes, in CSS pixels from the top: the sections' band, the bar
  // numbers, the chords; the waveform below; the markers' grips at the
  // bottom.
  const BAND = 20, NUMS = 14, CHORDS = 18, GRIP = 16;
  const TOP = BAND + NUMS + CHORDS;

  function create(o) {
    const cv = o.canvas, ov = o.overview;
    const cx = cv.getContext('2d'), ox = ov ? ov.getContext('2d') : null;
    let peaks = null;
    let view = { start: 0, span: 1 };
    let following = true;
    let W = 0, H = 0, OW = 0, OH = 0, dpr = 1;
    let colors = {};
    let raf = 0, dirty = true, lastNow = -1;
    let ovCache = null; // the overview's waveform, drawn once per size
    let drag = null; // {kind: 'marker'|'pan'|'pinch'|'overview', ...}
    const pointers = new Map();
    let hover = null; // the marker under the mouse

    const dur = () => o.duration() || 0;
    const pxPerSec = () => W / view.span;

    // A colour as the canvas takes it: a custom property can hold a
    // color-mix() a canvas can't read (it would keep the last fill), so
    // each is resolved by the page into rgb() through a probe's color.
    let probe = null;
    function resolve(value, fallback) {
      if (!value) return fallback;
      if (!probe) {
        probe = document.createElement('span');
        probe.style.cssText = 'position:absolute;width:0;height:0;overflow:hidden;visibility:hidden;pointer-events:none';
        probe.setAttribute('aria-hidden', 'true');
      }
      if (!probe.isConnected) cv.parentNode?.appendChild(probe);
      probe.style.color = '';
      probe.style.color = value;
      if (!probe.style.color) return fallback; // not a colour this browser knows
      const c = getComputedStyle(probe).color;
      return c || fallback;
    }
    function readColors() {
      const s = getComputedStyle(cv);
      const v = (n, d) => resolve(s.getPropertyValue(n).trim(), d);
      colors = {
        fg: v('--fg', '#241e18'), muted: v('--fg-muted', '#5f554a'), bg: v('--bg', '#f6f1e7'), raised: v('--bg-raised', '#fff'),
        accent: v('--accent', '#af4e39'), accentInk: v('--accent-ink', '#8a3d2c'), contrast: v('--accent-contrast', '#fff'),
        ok: v('--ok', '#685c23'), warn: v('--warn', '#8a5920'), danger: v('--danger', '#a52f2f'),
        wave: v('--sync-wave', v('--fg-muted', '#5f554a')), band: v('--sync-band', 'rgba(175,78,57,0.10)'), band2: v('--sync-band-alt', 'rgba(175,78,57,0.18)'),
      };
      ovCache = null;
    }
    function size() {
      dpr = window.devicePixelRatio || 1;
      W = cv.clientWidth; H = cv.clientHeight;
      cv.width = Math.round(W * dpr); cv.height = Math.round(H * dpr);
      if (ov) {
        OW = ov.clientWidth; OH = ov.clientHeight;
        ov.width = Math.round(OW * dpr); ov.height = Math.round(OH * dpr);
      }
      ovCache = null;
      invalidate();
    }
    const ro = new ResizeObserver(size);
    ro.observe(cv);
    if (ov) ro.observe(ov);

    function invalidate() {
      dirty = true;
      if (!raf) raf = requestAnimationFrame(frame);
    }
    // While playing, a frame each refresh; else only when asked.
    function frame() {
      raf = 0;
      const now = o.now();
      const playing = o.playing();
      if (playing && following && !drag) {
        const nv = M.follow(view, now, dur());
        if (nv.start !== view.start) { view = nv; dirty = true; }
      }
      if (now !== lastNow) dirty = true;
      if (dirty && W && H) draw(now);
      dirty = false;
      lastNow = now;
      if (playing && o.isOpen()) raf = requestAnimationFrame(frame);
    }
    function setView(v, byHand) {
      view = M.clampView(v, dur());
      if (byHand && o.playing()) following = false;
      o.onView && o.onView(view, following);
      invalidate();
    }

    // ---- Drawing ---------------------------------------------------------
    function draw(now) {
      const d = o.data();
      const c = cx;
      c.setTransform(dpr, 0, 0, dpr, 0, 0);
      c.clearRect(0, 0, W, H);
      const x = (t) => M.tToX(t, view, W);
      const t0 = view.start, t1 = view.start + view.span;
      const waveTop = TOP, waveBot = H - GRIP;
      const mid = (waveTop + waveBot) / 2, amp = (waveBot - waveTop) / 2 - 2;
      c.font = '600 11px system-ui, -apple-system, sans-serif';
      c.textBaseline = 'middle';

      // The sections' band, each from its start to the next one's.
      const secs = d.sections.filter((s) => s.t !== null);
      secs.forEach((s, k) => {
        const end = k + 1 < secs.length ? secs[k + 1].t : (s.end ?? dur());
        if (end < t0 || s.t > t1) return;
        const l = Math.max(0, x(s.t)), r = Math.min(W, x(end));
        c.fillStyle = s.cursor ? colors.band2 : (k % 2 ? colors.band : 'transparent');
        c.fillRect(l, 0, r - l, BAND);
        if (s.cursor) { c.fillStyle = colors.accent; c.fillRect(l, BAND - 2, r - l, 2); }
        // The name sticks to the left edge while its section is on screen.
        c.save();
        c.beginPath(); c.rect(l, 0, Math.max(0, r - l - 2), BAND); c.clip();
        c.fillStyle = colors.fg;
        c.font = '700 12px system-ui, -apple-system, sans-serif';
        const label = s.label;
        c.fillText(label, l + 6, BAND / 2 + 1);
        if (s.badge) {
          const lw = c.measureText(label).width;
          c.font = '700 10px system-ui, -apple-system, sans-serif';
          c.fillStyle = colors.warn;
          c.fillText(s.badge, l + 12 + lw, BAND / 2 + 1);
        }
        c.restore();
      });
      c.fillStyle = colors.muted;
      c.globalAlpha = 0.25;
      c.fillRect(0, BAND, W, 1);
      c.globalAlpha = 1;

      // The waveform.
      if (peaks) {
        const cols = M.columns(peaks, view, Math.floor(W));
        c.fillStyle = colors.wave;
        c.globalAlpha = 0.55;
        const step = cols.length > 900 ? 2 : 1;
        for (let i = 0; i < cols.length; i += step) {
          let p = cols[i];
          if (step > 1 && i + 1 < cols.length) p = Math.max(p, cols[i + 1]);
          const a = Math.max(0.5, p * amp);
          c.fillRect(i, mid - a, step === 1 ? 1 : 1.6, a * 2);
        }
        c.globalAlpha = 1;
      } else {
        c.fillStyle = colors.muted; c.globalAlpha = 0.3; c.fillRect(0, mid, W, 1); c.globalAlpha = 1;
      }

      // The analysis' beats (faint ticks) and downbeats (short, stronger),
      // under the chart's bar lines.
      const beatPx = d.beats.length > 1 ? (W * (d.beats[1] - d.beats[0])) / view.span : 0;
      if (beatPx >= 5) {
        c.fillStyle = colors.muted; c.globalAlpha = 0.28;
        for (const b of visible(d.beats, t0, t1)) c.fillRect(Math.round(x(b)), waveBot - 6, 1, 6);
        c.globalAlpha = 1;
      }
      const downPx = d.downbeats.length > 1 ? (W * (d.downbeats[1] - d.downbeats[0])) / view.span : 0;
      if (downPx >= 6) {
        c.fillStyle = colors.accent; c.globalAlpha = 0.45;
        for (const b of visible(d.downbeats, t0, t1)) c.fillRect(Math.round(x(b)) - 1, waveBot - 10, 2, 10);
        c.globalAlpha = 1;
      }
      // While a marker is dragged: the voice coming in, where it may snap.
      if (drag && drag.kind === 'marker' && d.voice.length) {
        c.fillStyle = colors.ok;
        for (const v of visible(d.voice, t0, t1)) { c.beginPath(); c.arc(x(v), waveTop + 4, 2.5, 0, 7); c.fill(); }
      }

      // The chart's bars: lines, every k-th numbered, the chords.
      const bars = d.bars;
      if (bars.length > 1) {
        const barPx = (W * ((bars[Math.min(bars.length - 1, 2)] - bars[1]) || 2)) / view.span;
        const every = M.labelEvery(barPx);
        const b0 = Math.max(0, Math.floor(M.barAt(bars, t0) ?? 0) - 1);
        const starts = new Set(secs.map((s) => s.startBar));
        for (let b = b0; b < bars.length && bars[b] <= t1; b++) {
          const t = bars[b];
          if (t < t0 - 0.001) continue;
          const px = Math.round(x(t)) + 0.5;
          if (!starts.has(b) && barPx >= 3) {
            c.strokeStyle = colors.fg; c.globalAlpha = barPx >= 14 ? 0.3 : 0.15;
            c.beginPath(); c.moveTo(px, BAND); c.lineTo(px, waveBot); c.stroke();
            c.globalAlpha = 1;
          }
          // Every few bars, by the bar's number (a lead-in reads 0, -1, …).
          const lead = d.leadIn != null ? d.leadIn : (d.pickup ? 1 : 0);
          if (b < d.chartBars && (((b - lead + 1) % every) + every) % every === 0) {
            c.fillStyle = colors.muted;
            c.font = '500 10px system-ui, -apple-system, sans-serif';
            c.fillText(d.barLabel(b), px + 3, BAND + NUMS / 2 + 1);
          }
        }
        // Chords where they change, never over each other.
        c.font = '700 12px system-ui, -apple-system, sans-serif';
        let lastEnd = -Infinity, last = '';
        for (const ch of d.chords) {
          const t = M.timeAt(bars, ch.bar);
          if (t === null || t > t1) { if (t !== null && t > t1) break; continue; }
          if (ch.name === last && !ch.first) continue;
          last = ch.name;
          if (t < t0) { lastEnd = -Infinity; continue; }
          const px = x(t) + 3;
          const w = c.measureText(ch.name).width;
          if (px < lastEnd + 4) continue;
          c.fillStyle = colors.accentInk;
          c.fillText(ch.name, px, BAND + NUMS + CHORDS / 2);
          lastEnd = px + w;
        }
      }

      // The recording's chord checks: a flag where the chart and the
      // recording disagree.
      // Zoomed out, only a flag; never over each other.
      let fixEnd = -Infinity;
      c.font = '700 11px system-ui, -apple-system, sans-serif';
      for (const f of d.fixes) {
        f._box = null;
        if (f.t < t0 || f.t > t1) continue;
        const px = x(f.t);
        if (px < fixEnd + 3) continue;
        const text = pxPerSec() >= 40 ? `⚑ ${f.label}` : '⚑';
        const w = c.measureText(text).width;
        c.fillStyle = colors.raised; c.globalAlpha = 0.8;
        c.fillRect(px + 1, waveTop + 1, w + 4, 15);
        c.globalAlpha = 1;
        c.fillStyle = colors.danger;
        c.fillText(text, px + 3, waveTop + 9);
        f._box = [px, waveTop, px + 6 + w, waveTop + 18];
        fixEnd = px + 6 + w;
      }

      // Suggestions: a dashed line and a chip to take or leave, chips
      // close together stacked.
      const chips = [];
      for (const g of d.ghosts) {
        g._box = null;
        if (g.t < t0 || g.t > t1) continue;
        const px = Math.round(x(g.t)) + 0.5;
        c.strokeStyle = colors.accent; c.lineWidth = 2; c.setLineDash([5, 4]);
        c.beginPath(); c.moveTo(px, BAND); c.lineTo(px, waveBot); c.stroke();
        c.setLineDash([]); c.lineWidth = 1;
        c.font = '700 11px system-ui, -apple-system, sans-serif';
        const tw = c.measureText(g.label).width + 12;
        const bx = px + tw + 6 > W ? px - tw - 4 : px + 4;
        let by = waveBot - 26;
        while (by > waveTop + 20 && chips.some((b) => bx < b[2] + 3 && bx + tw > b[0] - 3 && by < b[3] && by + 18 > b[1])) by -= 21;
        c.fillStyle = colors.accent;
        roundRect(c, bx, by, tw, 18, 5); c.fill();
        c.fillStyle = colors.contrast;
        c.fillText(g.label, bx + 6, by + 9.5);
        g._box = [bx, by, bx + tw, by + 18];
        chips.push(g._box);
      }

      // The sections' starts: the markers.
      for (const s of secs) {
        if (s.t < t0 - 1 || s.t > t1 + 1) continue;
        const px = Math.round(x(s.t)) + 0.5;
        const col = s.state === 'set' ? colors.ok : colors.warn;
        const strong = s.cursor || (hover && hover.i === s.i) || (drag && drag.i === s.i);
        c.strokeStyle = col; c.lineWidth = strong ? 3 : 2;
        c.beginPath(); c.moveTo(px, 0); c.lineTo(px, H); c.stroke();
        c.lineWidth = 1;
        // The grip.
        c.fillStyle = col;
        c.beginPath();
        c.moveTo(px - 7, H - GRIP); c.lineTo(px + 7, H - GRIP); c.lineTo(px + 7, H - 6); c.lineTo(px, H - 1); c.lineTo(px - 7, H - 6); c.closePath();
        c.fill();
        if (strong) { c.strokeStyle = colors.fg; c.stroke(); }
      }

      // The playhead: the text's colour, unlike any marker.
      if (now >= t0 && now <= t1) {
        c.fillStyle = colors.fg;
        c.fillRect(Math.round(x(now)) - 1, 0, 2, H);
      }
      drawOverview(now, d);
    }
    function drawOverview(now, d) {
      if (!ox || !OW || !OH) return;
      const D = dur();
      if (!D) return;
      const c = ox;
      c.setTransform(dpr, 0, 0, dpr, 0, 0);
      if (!ovCache || ovCache.w !== OW) {
        ovCache = { w: OW, cols: peaks ? M.columns(peaks, { start: 0, span: D }, Math.floor(OW)) : null };
      }
      c.clearRect(0, 0, OW, OH);
      const mid = OH / 2;
      if (ovCache.cols) {
        c.fillStyle = colors.wave; c.globalAlpha = 0.4;
        for (let i = 0; i < ovCache.cols.length; i += 2) { const a = Math.max(0.5, ovCache.cols[i] * (mid - 2)); c.fillRect(i, mid - a, 1.4, a * 2); }
        c.globalAlpha = 1;
      }
      for (const s of d.sections) {
        if (s.t === null) continue;
        c.fillStyle = s.state === 'set' ? colors.ok : colors.warn;
        c.fillRect(Math.round((s.t / D) * OW) - 1, 0, 2, OH);
      }
      // The window shown above, and the playhead.
      const l = (view.start / D) * OW, w = Math.max(4, (view.span / D) * OW);
      c.fillStyle = colors.fg; c.globalAlpha = 0.08; c.fillRect(l, 0, w, OH); c.globalAlpha = 1;
      c.strokeStyle = colors.fg; c.lineWidth = 1.5;
      roundRect(c, l + 0.75, 0.75, w - 1.5, OH - 1.5, 3); c.stroke();
      c.lineWidth = 1;
      c.fillStyle = colors.fg;
      c.fillRect(Math.round((now / D) * OW) - 1, 0, 2, OH);
    }
    function* visible(list, t0, t1) {
      if (!list || !list.length) return;
      let lo = 0, hi = list.length;
      while (lo < hi) { const m = (lo + hi) >> 1; if (list[m] < t0) lo = m + 1; else hi = m; }
      for (let i = lo; i < list.length && list[i] <= t1; i++) yield list[i];
    }
    function roundRect(c, x0, y0, w, h, r) {
      c.beginPath();
      c.moveTo(x0 + r, y0); c.arcTo(x0 + w, y0, x0 + w, y0 + h, r); c.arcTo(x0 + w, y0 + h, x0, y0 + h, r);
      c.arcTo(x0, y0 + h, x0, y0, r); c.arcTo(x0, y0, x0 + w, y0, r); c.closePath();
    }

    // ---- Pointing ----------------------------------------------------------
    const local = (e, el = cv) => { const r = el.getBoundingClientRect(); return { x: e.clientX - r.left, y: e.clientY - r.top }; };
    // The marker under p: near its line in the band or the grips' lane,
    // anywhere along it with a mouse.
    function markerAt(p, touch) {
      const d = o.data();
      const reach = touch ? 18 : 7;
      // By its handles only: the band at the top, the grip at the bottom.
      const inLane = p.y <= BAND || p.y >= H - GRIP - 4;
      if (!inLane) return null;
      let best = null, bd = Infinity;
      for (const s of d.sections) {
        if (s.t === null) continue;
        const dx = Math.abs(M.tToX(s.t, view, W) - p.x);
        if (dx <= (inLane ? reach + 3 : reach) && dx < bd) { bd = dx; best = s; }
      }
      return best;
    }
    const inBox = (b, p) => b && p.x >= b[0] && p.x <= b[2] && p.y >= b[1] && p.y <= b[3];
    function pickAt(p) {
      const d = o.data();
      const g = d.ghosts.find((x) => inBox(x._box, p));
      if (g) return { kind: 'ghost', g };
      const f = d.fixes.find((x) => inBox(x._box, p));
      if (f) return { kind: 'fix', f };
      return null;
    }
    // On the playhead line (outside the markers' handles): it drags.
    function onPlayhead(p, touch) {
      const now = o.now();
      if (!(now >= view.start && now <= view.start + view.span)) return false;
      return Math.abs(M.tToX(now, view, W) - p.x) <= (touch ? 14 : 5);
    }
    function sectionAtX(px) {
      const d = o.data();
      const t = M.xToT(px, view, W);
      let at = null;
      for (const s of d.sections) if (s.t !== null && s.t <= t) at = s;
      return at;
    }

    cv.addEventListener('pointerdown', (e) => {
      if (e.button !== undefined && e.button !== 0 && e.pointerType === 'mouse') return;
      try { cv.setPointerCapture(e.pointerId); } catch (err) { /* not an active pointer */ }
      const p = local(e);
      pointers.set(e.pointerId, p);
      if (pointers.size === 2) {
        // Two fingers: a pinch, whatever the first one started.
        if (drag && drag.kind === 'marker') o.dragEnd(drag.i, true);
        const [a, b] = [...pointers.values()];
        const mx = (a.x + b.x) / 2;
        drag = { kind: 'pinch', at: M.xToT(mx, view, W), span0: view.span, d0: Math.hypot(a.x - b.x, a.y - b.y) || 1 };
        following = o.playing() ? false : following;
        return;
      }
      const m = markerAt(p, e.pointerType !== 'mouse');
      if (m) {
        drag = { kind: 'marker', i: m.i, x0: p.x, moved: false };
        o.dragStart(m.i);
        e.preventDefault();
      } else if (onPlayhead(p, e.pointerType !== 'mouse')) {
        drag = { kind: 'scrub' };
        following = false;
        cv.style.cursor = 'col-resize';
      } else {
        drag = { kind: 'pan', x0: p.x, y0: p.y, start0: view.start, moved: false, pick: pickAt(p) };
      }
      invalidate();
    });
    cv.addEventListener('pointermove', (e) => {
      const p = local(e);
      if (pointers.has(e.pointerId)) pointers.set(e.pointerId, p);
      if (!drag) {
        if (e.pointerType === 'mouse') {
          const m = markerAt(p, false);
          const pk = !m && pickAt(p);
          cv.style.cursor = m ? 'ew-resize' : pk ? 'pointer' : onPlayhead(p, false) ? 'col-resize' : 'grab';
          if ((m && m.i) !== (hover && hover.i)) { hover = m; invalidate(); }
        }
        return;
      }
      if (drag.kind === 'pinch' && pointers.size >= 2) {
        const [a, b] = [...pointers.values()];
        const d1 = Math.hypot(a.x - b.x, a.y - b.y);
        setView(M.pinch(drag.at, drag.span0, drag.d0, d1, (a.x + b.x) / 2, W, dur()), true);
        return;
      }
      if (drag.kind === 'marker') {
        if (Math.abs(p.x - drag.x0) > 2) drag.moved = true;
        if (!drag.moved) return;
        drag.x = p.x;
        drag.free = e.altKey;
        o.dragMove(drag.i, M.xToT(Math.max(0, Math.min(W, p.x)), view, W), { free: e.altKey, pxPerSec: pxPerSec() });
        edgeScroll();
        invalidate();
        return;
      }
      if (drag.kind === 'scrub') {
        o.seek(Math.max(0, Math.min(dur(), M.xToT(Math.max(0, Math.min(W, p.x)), view, W))));
        invalidate();
        return;
      }
      if (drag.kind === 'pan') {
        if (!drag.moved && Math.hypot(p.x - drag.x0, p.y - drag.y0) > 4) { drag.moved = true; cv.style.cursor = 'grabbing'; }
        if (drag.moved) setView({ start: drag.start0 - ((p.x - drag.x0) / W) * view.span, span: view.span }, true);
      }
    });
    // A marker held near an edge: the view keeps scrolling, and the marker
    // along with it, until it's moved away or let go.
    let edgeFrame = 0;
    function edgeScroll() {
      if (edgeFrame) return;
      const step = () => {
        edgeFrame = 0;
        if (!drag || drag.kind !== 'marker' || !drag.moved || drag.x === undefined) return;
        const dir = drag.x < 24 ? -1 : drag.x > W - 24 ? 1 : 0;
        if (!dir) return;
        const before = view.start;
        setView({ start: view.start + dir * view.span * 0.015, span: view.span }, true);
        if (view.start === before) return; // at an end
        o.dragMove(drag.i, M.xToT(Math.max(0, Math.min(W, drag.x)), view, W), { free: drag.free, pxPerSec: pxPerSec() });
        invalidate();
        edgeFrame = requestAnimationFrame(step);
      };
      edgeFrame = requestAnimationFrame(step);
    }
    function end(e) {
      const p = local(e);
      pointers.delete(e.pointerId);
      if (!drag) return;
      const was = drag;
      if (was.kind === 'pinch') { if (pointers.size === 0) drag = null; return; }
      drag = null;
      if (was.kind === 'marker') {
        o.dragEnd(was.i, !was.moved || e.type === 'pointercancel');
        if (!was.moved) { o.select(was.i); }
      } else if (was.kind === 'pan' && !was.moved && e.type !== 'pointercancel') {
        if (was.pick && was.pick.kind === 'ghost') o.pickGhost(was.pick.g);
        else if (was.pick && was.pick.kind === 'fix') o.pickFix(was.pick.f);
        else {
          if (p.y <= BAND) { const s = sectionAtX(p.x); if (s) o.select(s.i); }
          o.seek(M.xToT(p.x, view, W));
        }
      }
      cv.style.cursor = '';
      invalidate();
    }
    cv.addEventListener('pointerup', end);
    cv.addEventListener('pointercancel', end);
    cv.addEventListener('pointerleave', () => { if (hover) { hover = null; invalidate(); } });

    // Wheel: Ctrl/⌘ (and a trackpad's pinch, which comes as Ctrl + wheel)
    // zooms around the pointer, otherwise it scrolls, either direction.
    // A plain vertical wheel scrolls the sheet (a mouse has no other way
    // over it); a sideways one, or Shift + wheel, scrolls the waveform.
    cv.addEventListener('wheel', (e) => {
      const p = local(e);
      const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? W : 1;
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        const f = Math.exp(-e.deltaY * unit * 0.01);
        setView(M.zoomAt(view, f, M.xToT(p.x, view, W), dur()), true);
        return;
      }
      const sideways = Math.abs(e.deltaX) > Math.abs(e.deltaY);
      if (!sideways && !e.shiftKey) return;
      e.preventDefault();
      setView(M.pan(view, (sideways ? e.deltaX : e.deltaY) * unit, W, dur()), true);
    }, { passive: false });
    // Safari's own trackpad pinch (on an iPhone, two fingers are already
    // the pointers' pinch: not applied twice).
    let g0 = null;
    cv.addEventListener('gesturestart', (e) => { e.preventDefault(); if (pointers.size >= 2) return; const p = local(e); g0 = { at: M.xToT(p.x, view, W), span: view.span, x: p.x }; });
    cv.addEventListener('gesturechange', (e) => {
      e.preventDefault();
      if (!g0 || pointers.size >= 2) return;
      const span = M.clampView({ start: 0, span: g0.span / (e.scale || 1) }, dur()).span;
      setView({ start: g0.at - (g0.x / W) * span, span }, true);
    });
    cv.addEventListener('gestureend', (e) => { e.preventDefault(); g0 = null; });

    // The overview: press or drag to move the window there.
    if (ov) {
      const moveTo = (e) => {
        const p = local(e, ov);
        const D = dur();
        setView({ start: (p.x / OW) * D - view.span / 2, span: view.span }, true);
      };
      ov.addEventListener('pointerdown', (e) => { try { ov.setPointerCapture(e.pointerId); } catch (err) { /* not an active pointer */ } drag = { kind: 'overview' }; moveTo(e); });
      ov.addEventListener('pointermove', (e) => { if (drag && drag.kind === 'overview') moveTo(e); });
      const stop = () => { if (drag && drag.kind === 'overview') drag = null; };
      ov.addEventListener('pointerup', stop);
      ov.addEventListener('pointercancel', stop);
      ov.addEventListener('wheel', (e) => {
        e.preventDefault();
        const dx = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
        setView({ start: view.start + (dx / Math.max(1, OW)) * dur() * 0.25, span: view.span }, true);
      }, { passive: false });
    }

    // ---- What the sheet asks ---------------------------------------------------
    // Zoom by factor around the playhead when it's on screen, else the middle.
    function zoom(factor) {
      const now = o.now();
      const at = now >= view.start && now <= view.start + view.span ? now : view.start + view.span / 2;
      setView(M.zoomAt(view, factor, at, dur()), true);
    }
    readColors();
    size();
    return {
      setPeaks(p) { peaks = p; ovCache = null; invalidate(); },
      invalidate,
      // Something the sheet changed: redraw, and keep following while playing.
      kick() { invalidate(); },
      zoom,
      fit() { setView(M.fit(dur()), true); },
      setView(v) { setView(v, false); },
      view: () => view,
      reveal(t) { if (t !== null && isFinite(t)) setView(M.reveal(view, t, dur()), false); },
      following: () => following,
      setFollow(on) { following = !!on; if (on) view = M.follow(view, o.now(), dur()); o.onView && o.onView(view, following); invalidate(); },
      restyle() { readColors(); invalidate(); },
      resize: size,
      dragging: () => !!(drag && drag.kind === 'marker'),
    };
  }
  window.SongbooksSyncWave = { create, LANES: { BAND, NUMS, CHORDS, GRIP } };
})();

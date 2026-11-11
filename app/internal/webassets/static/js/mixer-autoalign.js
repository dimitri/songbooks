// Auto-align for the Mixer (mixer.js): places every bar of the song in a
// reference recording, from the recording itself plus the song's score.
//
//   1. Onset envelope, 5 ms frames, two bands: a high-passed band (clicks,
//      snare, hats, picking) and a low-passed one (kick, bass).
//   2. Count-in: three or more evenly spaced, isolated clicks near the
//      song's tempo, before the band comes in (a jump in loudness). The
//      clicks give the recording's real tempo and its beat grid.
//   3. Downbeat: which beat of that grid bar 1 falls on (never on the
//      count-in itself) — the candidate whose bars best match the score's
//      note onsets and the chart's chords, plus the low band's accent on
//      bar starts. With no count-in the tempo comes from a beat comb and
//      the whole song is matched against the whole recording, 8 bars at a
//      time (so tempo drift doesn't break it): onsets for timing, chords
//      (pitch-class profile vs. each bar's chord) for where in the song.
//   4. Per-bar tracking: each bar is nudged onto the onsets around it,
//      following tempo drift, so a live performance stays aligned too.
//
// analyze({x, sr}, song) is pure: x is mono audio (any rate around 11 kHz
// is plenty), song is {tempo, barTicks: [N+1 tick positions, 960/quarter],
// onsets: [{tick, w}], barChords: [N] arrays of {pos, name}} (onsets and
// chords may be empty).
(function () {
  const FPS = 200;
  const TPQ = 960;

  function envelope(x, sr) {
    const hop = sr / FPS;
    const win = Math.max(64, Math.round(sr * 0.023));
    const n = Math.max(0, Math.floor((x.length - win) / hop));
    const a = Math.exp(-2 * Math.PI * 150 / sr);
    const cumHi = new Float64Array(x.length + 1);
    const cumLo = new Float64Array(x.length + 1);
    const cumAll = new Float64Array(x.length + 1);
    let prev = 0, lp = 0;
    for (let i = 0; i < x.length; i++) {
      const v = x[i];
      const h = v - 0.97 * prev;
      prev = v;
      lp = (1 - a) * v + a * lp;
      cumHi[i + 1] = cumHi[i] + h * h;
      cumLo[i + 1] = cumLo[i] + lp * lp;
      cumAll[i + 1] = cumAll[i] + v * v;
    }
    const eHi = new Float32Array(n), eLo = new Float32Array(n), rms = new Float32Array(n);
    for (let f = 0; f < n; f++) {
      const s = Math.round(f * hop), e = s + win;
      eHi[f] = Math.log(1e-10 + (cumHi[e] - cumHi[s]) / win);
      eLo[f] = Math.log(1e-10 + (cumLo[e] - cumLo[s]) / win);
      rms[f] = Math.sqrt((cumAll[e] - cumAll[s]) / win);
    }
    const lag = 3; // 15 ms: compare against a frame that doesn't overlap much
    const hi = new Float32Array(n), lo = new Float32Array(n);
    for (let f = lag; f < n; f++) {
      hi[f] = Math.max(0, eHi[f] - eHi[f - lag]);
      lo[f] = Math.max(0, eLo[f] - eLo[f - lag]);
    }
    const norm = (arr) => { let m = 0; for (const v of arr) m += v; m = m / (arr.length || 1) || 1; for (let i = 0; i < arr.length; i++) arr[i] /= m; };
    norm(hi); norm(lo);
    const o = new Float32Array(n);
    for (let f = 0; f < n; f++) o[f] = hi[f] + lo[f];
    return { o, om: maxFilter(o, 2), lom: maxFilter(lo, 3), rms, n };
  }

  function maxFilter(a, r) {
    const out = new Float32Array(a.length);
    for (let i = 0; i < a.length; i++) {
      let m = 0;
      for (let j = Math.max(0, i - r); j <= Math.min(a.length - 1, i + r); j++) if (a[j] > m) m = a[j];
      out[i] = m;
    }
    return out;
  }

  function percentile(arr, q) {
    const step = Math.max(1, Math.floor(arr.length / 4000));
    const s = [];
    for (let i = 0; i < arr.length; i += step) s.push(arr[i]);
    s.sort((p, r) => p - r);
    return s[Math.min(s.length - 1, Math.floor(q * s.length))] || 0;
  }

  const at = (arr, t) => { const f = Math.round(t * FPS); return f >= 0 && f < arr.length ? arr[f] : 0; };

  // Where the band comes in: the first moment the sound stays above a
  // sixth of the recording's typical playing level for 0.3 s without a
  // dip. A count-in click is loud too, but it's followed by near silence
  // before the next one; music isn't.
  function musicStart(env) {
    const w = Math.round(0.3 * FPS);
    const floor = percentile(env.rms, 0.6) / 6;
    let run = 0;
    for (let f = 0; f < env.n; f++) {
      run = env.rms[f] > floor ? run + 1 : 0;
      if (run >= w) return (f - w + 1) / FPS;
    }
    return 0;
  }

  function detectCountIn(env, beatSec, start) {
    const limit = Math.min(env.n, Math.round(start * FPS));
    if (limit < FPS) return null;
    const thr = Math.max(percentile(env.o, 0.95), 1.5);
    const peaks = [];
    for (let f = 2; f < limit - 2; f++) {
      const v = env.o[f];
      if (v > thr && v >= env.o[f - 1] && v >= env.o[f + 1] && v >= env.o[f - 2] && v >= env.o[f + 2]) {
        if (!peaks.length || f - peaks[peaks.length - 1] > 0.12 * FPS) peaks.push(f / FPS);
      }
    }
    // The run of evenly spaced clicks ending closest to the band's entry;
    // a click every two beats (a half-time "1 . 2 .") counts as two beats.
    let best = null;
    for (let end = peaks.length - 1; end >= 1; end--) {
      const beats = [0];
      const times = [peaks[end]];
      for (let i = end - 1; i >= 0; i--) {
        const d = times[0] - peaks[i];
        const k = Math.round(d / beatSec);
        if (k < 1 || k > 2 || Math.abs(d - k * beatSec) > 0.12 * beatSec) break;
        beats.unshift(beats[0] - k);
        times.unshift(peaks[i]);
      }
      if (times.length >= 3 && (!best || times.length > best.times.length)) best = { times, beats };
      if (best && best.times.length >= 4) break;
    }
    if (!best) return null;
    // Least squares: click time = phase + beat * period.
    const n = best.times.length;
    const mb = best.beats.reduce((s, v) => s + v, 0) / n;
    const mt = best.times.reduce((s, v) => s + v, 0) / n;
    let num = 0, den = 0;
    for (let i = 0; i < n; i++) { num += (best.beats[i] - mb) * (best.times[i] - mt); den += (best.beats[i] - mb) ** 2; }
    const period = den ? num / den : beatSec;
    if (period < 0.8 * beatSec || period > 1.25 * beatSec) return null;
    return { clicks: n, period, last: best.times[n - 1], phase: mt - mb * period };
  }

  // Beat period and phase by comb: the grid whose beats land on the most
  // onset energy, within ±6% of the song's notated tempo.
  function combFit(env, beatSec, from, to) {
    let best = { score: -1, period: beatSec, phase: 0 };
    for (let p = beatSec * 0.94; p <= beatSec * 1.06; p += beatSec * 0.002) {
      for (let ph = 0; ph < p; ph += 0.005) {
        let s = 0, c = 0;
        for (let t = from + ph; t < to; t += p) { s += at(env.om, t); c++; }
        s /= c || 1;
        if (s > best.score) best = { score: s, period: p, phase: from + ph };
      }
    }
    return best;
  }

  // ---- Chroma (pitch-class profile), 10 frames/s ------------------------
  const CFPS = 10;
  function fft(re, im) {
    const n = re.length;
    for (let i = 1, j = 0; i < n; i++) {
      let bit = n >> 1;
      for (; j & bit; bit >>= 1) j ^= bit;
      j ^= bit;
      if (i < j) { [re[i], re[j]] = [re[j], re[i]]; [im[i], im[j]] = [im[j], im[i]]; }
    }
    for (let len = 2; len <= n; len <<= 1) {
      const ang = -2 * Math.PI / len, wr = Math.cos(ang), wi = Math.sin(ang);
      for (let i = 0; i < n; i += len) {
        let cr = 1, ci = 0;
        for (let k = 0; k < len / 2; k++) {
          const a = i + k, b = a + len / 2;
          const tr = re[b] * cr - im[b] * ci, ti = re[b] * ci + im[b] * cr;
          re[b] = re[a] - tr; im[b] = im[a] - ti; re[a] += tr; im[a] += ti;
          const nr = cr * wr - ci * wi; ci = cr * wi + ci * wr; cr = nr;
        }
      }
    }
  }

  // Pitch-class profile: each semitone band from C2 to C6 averaged over
  // its own bins (a plain per-bin sum would let the widest, highest bands
  // — and any broadband noise in them — dominate), log-compressed, then
  // folded into 12 classes with each frame's mean removed, so only what
  // stands out harmonically counts.
  function chroma(x, sr) {
    const size = 4096, hop = sr / CFPS;
    const frames = Math.max(0, Math.floor((x.length - size) / hop));
    const out = new Array(frames);
    const LO = 36, HI = 84;
    const bandOf = new Int16Array(size / 2).fill(-1);
    const bandBins = new Int32Array(HI - LO + 1);
    for (let k = 1; k < size / 2; k++) {
      const midi = Math.round(69 + 12 * Math.log2((k * sr / size) / 440));
      if (midi >= LO && midi <= HI) { bandOf[k] = midi - LO; bandBins[midi - LO]++; }
    }
    const win = new Float32Array(size);
    for (let i = 0; i < size; i++) win[i] = 0.5 - 0.5 * Math.cos(2 * Math.PI * i / size);
    const re = new Float32Array(size), im = new Float32Array(size);
    const band = new Float32Array(HI - LO + 1);
    for (let fr = 0; fr < frames; fr++) {
      const s0 = Math.round(fr * hop);
      for (let i = 0; i < size; i++) { re[i] = x[s0 + i] * win[i]; im[i] = 0; }
      fft(re, im);
      band.fill(0);
      for (let k = 1; k < size / 2; k++) if (bandOf[k] >= 0) band[bandOf[k]] += Math.hypot(re[k], im[k]);
      const c = new Float32Array(12);
      for (let m = 0; m < band.length; m++) if (bandBins[m]) c[(LO + m) % 12] += Math.log1p(10 * band[m] / bandBins[m]);
      let mean = 0;
      for (let i = 0; i < 12; i++) mean += c[i] / 12;
      let norm = 0;
      for (let i = 0; i < 12; i++) { c[i] = Math.max(0, c[i] - mean); norm += c[i] * c[i]; }
      norm = Math.sqrt(norm) || 1;
      for (let i = 0; i < 12; i++) c[i] /= norm;
      out[fr] = c;
    }
    return out;
  }

  const ROOTS = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };
  // A chord symbol as the chart displays it (♭/♯/Δ/ø glyphs included) to a
  // normalized 12-bin template: root, third, fifth, a lighter seventh.
  function chordTemplate(name) {
    const m = /^([A-G])([b♭#♯]?)(.*)$/.exec(String(name).trim());
    if (!m) return null;
    let root = ROOTS[m[1]] + (m[2] === 'b' || m[2] === '♭' ? -1 : m[2] === '#' || m[2] === '♯' ? 1 : 0);
    root = (root + 12) % 12;
    const q = m[3].split('/')[0];
    const minor = /^(m(?!aj)|-|min)/.test(q);
    const dim = /dim|°|ø/.test(q);
    const aug = /aug|\+/.test(q);
    const t = new Float32Array(12);
    t[root] = 1;
    t[(root + (minor || dim ? 3 : 4)) % 12] = 0.8;
    t[(root + (dim ? 6 : aug ? 8 : 7)) % 12] = 0.8;
    if (/7|9|11|13/.test(q)) t[(root + (/maj|Δ/.test(q) ? 11 : dim && !/ø/.test(q) ? 9 : 10)) % 12] = 0.4;
    let n = 0;
    for (const v of t) n += v * v;
    n = Math.sqrt(n);
    for (let i = 0; i < 12; i++) t[i] /= n;
    return t;
  }

  // One template per bar: the bar's chords weighted by how long each
  // lasts in it, carrying the last chord through bars that don't change.
  function barTemplates(song) {
    const out = [];
    let last = null;
    const n = song.barTicks.length - 1;
    for (let b = 0; b < n; b++) {
      const chords = (song.barChords && song.barChords[b]) || [];
      const t = new Float32Array(12);
      let any = false;
      const segs = chords.length ? chords : last ? [{ pos: 0, tpl: last }] : [];
      segs.forEach((c, i) => {
        const tpl = c.tpl || chordTemplate(c.name);
        if (!tpl) return;
        const end = i + 1 < segs.length ? segs[i + 1].pos : 1;
        for (let k = 0; k < 12; k++) t[k] += tpl[k] * Math.max(0.05, end - c.pos);
        any = true;
        last = tpl;
      });
      out.push(any ? t : null);
    }
    return out;
  }

  function chromaScore(chr, tpls, times) {
    let s = 0, c = 0;
    for (let b = 0; b < tpls.length; b++) {
      if (!tpls[b] || times[b] === undefined || times[b + 1] === undefined) continue;
      const f0 = Math.max(0, Math.round(times[b] * CFPS) + 1), f1 = Math.min(chr.length, Math.round(times[b + 1] * CFPS));
      if (f1 <= f0) continue;
      const avg = new Float32Array(12);
      for (let f = f0; f < f1; f++) for (let k = 0; k < 12; k++) avg[k] += chr[f][k];
      let dot = 0, n = 0, tn = 0;
      for (let k = 0; k < 12; k++) { dot += avg[k] * tpls[b][k]; n += avg[k] * avg[k]; tn += tpls[b][k] * tpls[b][k]; }
      if (n > 0) { s += dot / Math.sqrt(n * tn); c++; }
    }
    return c ? s / c : 0;
  }

  function onsetsByBar(song) {
    const by = song.barTicks.slice(0, -1).map(() => []);
    for (const o of song.onsets) {
      let b = 0;
      while (b + 1 < song.barTicks.length - 1 && song.barTicks[b + 1] <= o.tick) b++;
      if (b < by.length) by[b].push(o);
    }
    return by;
  }

  function patternScore(env, song, t0, period, fromBar, toBar) {
    const { barTicks } = song;
    const t = (tick) => t0 + (tick / TPQ) * period;
    let s = 0, w = 0;
    for (let b = fromBar; b < Math.min(toBar, song.byBar.length); b++) {
      for (const o of song.byBar[b]) {
        s += o.w * at(env.om, t(o.tick));
        w += o.w;
      }
    }
    let acc = 0, bars = 0;
    for (let b = fromBar; b < Math.min(toBar, barTicks.length - 1); b++) { acc += at(env.lom, t(barTicks[b])); bars++; }
    return (w ? s / w : 0) + 0.5 * (bars ? acc / bars : 0);
  }

  function analyze(audio, song) {
    const env = envelope(audio.x, audio.sr);
    song = { ...song, byBar: onsetsByBar(song) };
    const tpls = barTemplates(song);
    const hasChords = tpls.some(Boolean);
    const chr = hasChords ? chroma(audio.x, audio.sr) : null;
    const ticksToSec = (tick, per) => (tick / TPQ) * per;
    // Score of "bar 0 starts at t0" over bars [a, b) at a steady period.
    const windowScore = (t0, per, a, b) => {
      let sc = patternScore(env, song, t0, per, a, b);
      if (chr) {
        const times = [];
        for (let k = a; k <= Math.min(b, song.barTicks.length - 1); k++) times[k] = t0 + ticksToSec(song.barTicks[k], per);
        sc += 2 * chromaScore(chr, tpls.slice(0, b).map((t, i) => (i >= a ? t : null)), times);
      }
      return sc;
    };
    const duration = env.n / FPS;
    const beatSec = 60 / (song.tempo || 120);
    const N = song.barTicks.length - 1;
    const firstFull = N > 1 && song.barTicks[1] - song.barTicks[0] < song.barTicks[2] - song.barTicks[1] ? 1 : 0;
    const start = musicStart(env);
    const countIn = detectCountIn(env, beatSec, start + 0.05);
    const probeBars = Math.min(N, 12);
    const candidates = [];
    let period, t0, how;

    if (countIn) {
      // Bar 1 lands on a beat of the click grid, normally the band's entry.
      period = countIn.period;
      const entryK = Math.round((start - countIn.phase) / period);
      // The downbeat of bar 1 comes after the last click, never on it.
      const lastK = Math.round((countIn.last - countIn.phase) / period);
      let best = -1;
      for (let k = Math.max(lastK + 1, entryK - 4); k <= entryK + 4; k++) {
        const cand = countIn.phase + k * period - (song.barTicks[firstFull] / TPQ) * period;
        const sc = windowScore(cand, period, 0, probeBars) + (k === entryK ? 0.5 : 0);
        if (sc > best) { best = sc; t0 = cand; }
      }
      how = `count-in of ${countIn.clicks} clicks`;
    } else {
      const fit = combFit(env, beatSec, start, Math.min(duration, start + 60));
      period = fit.period;
      // Every beat of the grid is a candidate start; the whole song is
      // scored 8 bars at a time, each window free to slide ±0.3 beat so
      // a drifting live tempo doesn't wash the match out.
      let best = -1;
      const slide = [];
      for (let d = -0.3; d <= 0.3001; d += 0.075) slide.push(d * period);
      for (let g = fit.phase; g < duration; g += period) {
        const cand = g - (song.barTicks[firstFull] / TPQ) * period;
        if (cand + ticksToSec(song.barTicks[Math.min(N, 8)], period) > duration) break;
        let total = 0, windows = 0;
        for (let a = 0; a < N; a += 8) {
          const b = Math.min(N, a + 8);
          if (cand + ticksToSec(song.barTicks[a], period) > duration) break;
          let w = -1;
          for (const d of slide) w = Math.max(w, windowScore(cand + d, period, a, b));
          total += w;
          windows++;
        }
        const sc = windows ? total / Math.ceil(N / 8) : 0;
        candidates.push({ t0: cand, score: sc });
        if (sc > best) { best = sc; t0 = cand; }
      }
      how = chr ? 'beat, downbeat and chord detection' : 'beat and downbeat detection';
    }

    // Per-bar tracking: start from the previous bar at the local tempo,
    // look ±15% of a beat around it, keep 80% of the correction.
    const times = new Array(N + 1);
    times[0] = t0;
    let p = period;
    for (let b = 0; b < N; b++) {
      const beats = (song.barTicks[b + 1] - song.barTicks[b]) / TPQ;
      if (b > 0) {
        const expected = times[b];
        if (expected < duration - 1) {
          let bestD = 0, bestS = patternScore(env, song, expected - (song.barTicks[b] / TPQ) * p, p, b, b + 1);
          const base = bestS;
          for (let d = -0.15 * p; d <= 0.15 * p; d += 1 / FPS) {
            const sc = patternScore(env, song, expected + d - (song.barTicks[b] / TPQ) * p, p, b, b + 1);
            if (sc > bestS) { bestS = sc; bestD = d; }
          }
          if (bestS > base * 1.1) times[b] = expected + 0.8 * bestD;
        }
        const back = Math.min(4, b);
        const span = (song.barTicks[b] - song.barTicks[b - back]) / TPQ;
        p = Math.max(period * 0.96, Math.min(period * 1.04, (times[b] - times[b - back]) / span));
      }
      times[b + 1] = times[b] + beats * p;
    }
    candidates.sort((x, y) => y.score - x.score);
    return { barTimes: times, period, bpm: 60 / period, countIn, musicStart: start, how, candidates: candidates.slice(0, 8) };
  }

  window.SongbooksAutoAlign = { analyze };
})();

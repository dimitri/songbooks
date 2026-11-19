// Lyrics only: which line of a section is being sung, from the voice.
//
// A chart has no time per line: song.js used to spread a section's lines
// evenly over its bars, which lights the wrong line during an
// instrumental lead-in, a long held note, or lines of unequal length,
// even when the section's own start is right. Here the lines go on the
// singer's phrases instead, from the recording's vocals part
// (GET /api/songs/{id}/audio/{category}/voice): the phrases starting in
// the section's time span are merged (shortest gap first) or split
// (longest phrase first) until there is one per line, and each line
// starts with its phrase. Without the voice (not split yet, no
// recording playing), song.js keeps the even spread.
//
// window.songbooksLyricLine(section, lineCount) → the line index, or null
// to fall back. place(phrases, n) is pure and exported for checking.
(function () {
  const player = document.getElementById('audio-player');
  const variantId = player && player.dataset.variantId;

  // n line starts from [start, end] phrases (sorted, non-empty).
  function place(phrases, n) {
    let ps = phrases.map((p) => [p[0], p[1]]);
    if (!ps.length || n < 1) return null;
    // More phrases than lines: a line holds the phrases with the shortest
    // rests between them.
    while (ps.length > n) {
      let k = 0, gap = Infinity;
      for (let i = 0; i + 1 < ps.length; i++) {
        const g = ps[i + 1][0] - ps[i][1];
        if (g < gap) { gap = g; k = i; }
      }
      ps.splice(k, 2, [ps[k][0], ps[k + 1][1]]);
    }
    // Fewer: the longest phrase holds two lines, sung one after the other.
    while (ps.length < n) {
      let k = 0;
      for (let i = 1; i < ps.length; i++) if (ps[i][1] - ps[i][0] > ps[k][1] - ps[k][0]) k = i;
      const [a, b] = ps[k];
      const mid = a + (b - a) / 2;
      ps.splice(k, 1, [a, mid], [mid, b]);
    }
    return ps.map((p) => p[0]);
  }

  let cues = null; // {cat, phrases}
  let loading = null;
  function load(cat) {
    if (!variantId || !cat || (cues && cues.cat === cat) || loading === cat) return;
    loading = cat;
    fetch(`/api/songs/${variantId}/audio/${cat}/voice`)
      .then((r) => (r.ok ? r.json() : null))
      .then((c) => { cues = { cat, phrases: (c && c.phrases) || [] }; })
      .catch(() => { cues = { cat, phrases: [] }; })
      .finally(() => { loading = null; });
  }
  document.addEventListener('songbooks:reference-playing', () => {
    const pos = window.songbooksReferencePosition && window.songbooksReferencePosition();
    if (pos && pos.category) load(pos.category);
  });

  const cache = new Map(); // key → line starts
  window.songbooksLyricLine = (sec, n) => {
    const pos = window.songbooksReferencePosition && window.songbooksReferencePosition();
    const at = window.songbooksReferenceTimeAtBar;
    if (!pos || pos.time === undefined || !at || !n) return null;
    if (!cues || cues.cat !== pos.category) { load(pos.category); return null; }
    const t0 = at(sec.startBar), t1 = at(sec.startBar + Math.max(1, sec.barCount));
    if (t0 === null || t1 === null || !(t1 > t0)) return null;
    const key = `${pos.category}|${sec.startBar}|${n}|${t0.toFixed(2)}|${t1.toFixed(2)}`;
    let starts = cache.get(key);
    if (starts === undefined) {
      // Phrases that start in the section (a sung pickup a little before
      // its first bar counts), cut at its end.
      const inside = cues.phrases.filter((p) => p[0] >= t0 - 0.6 && p[0] < t1 - 0.3).map((p) => [p[0], Math.min(p[1], t1)]);
      starts = place(inside, n);
      if (cache.size > 200) cache.clear();
      cache.set(key, starts);
    }
    if (!starts) return null;
    // A line lights as its first word comes, a moment early; before the
    // singing, the first one waits lit.
    let k = 0;
    for (let i = 0; i < starts.length; i++) if (starts[i] <= pos.time + 0.25) k = i;
    return k;
  };
  window.SongbooksLyricLines = { place };
})();

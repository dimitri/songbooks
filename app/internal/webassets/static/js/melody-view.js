// The song page's Melody view (docs/melody-sheet.md): the lead voice's
// notes, found in the recording's vocals part, in the song's bars, the
// lyrics laid under them — all estimated, so said, and the lines it's
// least sure of drawn lighter. The server writes it as alphaTex
// (GET /api/songs/{id}/melody, melody_sheet.go); alphaTab draws it on A4
// pages like the Score view (gp-view.js), loaded the first time the view
// is shown. Its cursor follows the song's player (the bar being played:
// the score's bars are the played bars, repeats written out); a click on
// a note plays from there. Editors move a line's lyrics a note earlier or
// later (PUT …/melody/lyrics-shift). Downloads: MusicXML (the server's),
// Guitar Pro (alphaTab's exporter, from the score as drawn).
(function () {
  const sheet = document.getElementById('sheet');
  const page = document.getElementById('melody-page');
  if (!sheet || !page) return;
  const variantId = page.dataset.variantId;
  const canEdit = page.dataset.canEdit === '1';
  const tr = (m, ...a) => (window.t ? window.t(m, ...a) : m.replace(/%[ds]/g, () => String(a.shift())));
  const $ = (sel) => page.querySelector(sel);
  const canvas = $('.melody-canvas');
  const cursor = $('.melody-cursor');
  const wait = $('.melody-wait');

  // { api, score, bars: [played bar per score bar], geo, data, offset }
  let view = null;
  let loading = null;
  let offset = 0;

  function loadAlphaTab() {
    if (typeof alphaTab !== 'undefined') return Promise.resolve();
    return new Promise((resolve, reject) => {
      const s = document.createElement('script');
      s.src = page.dataset.alphatab;
      s.onload = () => resolve();
      s.onerror = () => reject(new Error(tr('Could not load the score viewer.')));
      document.head.appendChild(s);
    });
  }

  const WAIT = {
    none: 'Melody: available once the song has a recording, split and synced.',
    split: 'Melody: available once the recording is split and synced.',
    notes: 'Melody: the recording’s voice is being read. Come back in a few minutes.',
    unsung: 'Melody: too little singing in this recording to write a melody.',
    sync: 'Melody: available once the recording is split and synced.',
  };
  function showWait(text) {
    wait.textContent = text;
    wait.hidden = !text;
    page.classList.toggle('is-waiting', !!text);
  }

  // The page's size (gp-view.js geometry): an A4 sheet at most 860 px
  // wide, the whole width on a phone; bars a line as leave room for the
  // words (alphaTab spaces the notes, not the syllables).
  function geometry() {
    const wrap = $('.melody-sheet-wrap');
    const avail = Math.max(280, wrap.clientWidth);
    const phone = avail < 600;
    const pageW = Math.min(avail, 860);
    const mx = Math.round(pageW * (phone ? 0.04 : 0.075));
    const my = Math.round(pageW * (phone ? 0.06 : 0.07));
    const scale = phone ? 0.85 : 1;
    const contentW = pageW - 2 * mx;
    const perRow = Math.max(1, Math.min(4, Math.floor(contentW / scale / (phone ? 160 : 215))));
    return { phone, pageW, pageH: Math.round((pageW * 297) / 210), mx, my, gap: phone ? 14 : 28, scale, perRow, contentW };
  }
  function applyGeometry(g) {
    const sh = $('.melody-sheet');
    sh.style.width = `${g.pageW}px`;
    sh.style.setProperty('--page-h', `${g.pageH}px`);
    canvas.style.marginLeft = `${g.mx}px`;
    canvas.style.width = `${g.contentW}px`;
  }

  // alphaTab's lines laid on pages, each on the page it fits on (as
  // gp-view.js paginate), what it measured moved with them.
  function paginate() {
    if (!view) return;
    const g = view.geo;
    const surface = canvas.querySelector('.at-surface');
    const lookup = view.api.renderer.boundsLookup;
    if (!surface || !lookup || lookup._paged) return;
    lookup._paged = true;
    surface.querySelectorAll('svg text').forEach((tx) => { if (/rendered by alphaTab/i.test(tx.textContent)) tx.closest('div')?.remove(); });
    const parts = [...surface.children]
      .filter((d) => d !== cursor)
      .map((div) => ({ div, top: parseFloat(div.style.top) || 0, h: parseFloat(div.style.height) || div.offsetHeight }))
      .filter((p) => p.h > 0)
      .sort((a, b) => a.top - b.top);
    const pageTop = (k) => k * (g.pageH + g.gap);
    let k = 0, y = g.my;
    const shifts = [];
    for (const p of parts) {
      if (y + p.h > pageTop(k) + g.pageH - g.my && y > pageTop(k) + g.my + 1) { k++; y = pageTop(k) + g.my; }
      shifts.push({ top: p.top, bottom: p.top + p.h, dy: y - p.top });
      p.div.style.top = `${y}px`;
      y += p.h;
    }
    const height = pageTop(k) + g.pageH;
    surface.style.height = `${height}px`;
    $('.melody-sheet').style.height = `${height}px`;
    $('.gp-pages').replaceChildren(...Array.from({ length: k + 1 }, (_, i) => {
      const d = document.createElement('div');
      d.className = 'gp-page';
      d.style.top = `${pageTop(i)}px`;
      return d;
    }));
    const dyAt = (y0) => (shifts.find((s) => y0 >= s.top - 1 && y0 < s.bottom + 1) || { dy: 0 }).dy;
    const seen = new Set();
    const shift = (o, dy) => {
      if (!o || typeof o !== 'object' || seen.has(o)) return;
      seen.add(o);
      for (const key of ['visualBounds', 'realBounds', 'lineAlignedBounds', 'noteHeadBounds']) {
        const b = o[key];
        if (b && !seen.has(b) && typeof b.y === 'number') { b.y += dy; seen.add(b); }
      }
      for (const key of ['bars', 'beats', 'notes']) if (Array.isArray(o[key])) o[key].forEach((c) => shift(c, dy));
    };
    for (const sys of lookup.staffSystems || []) {
      const b = sys.realBounds || sys.visualBounds;
      if (b) shift(sys, dyAt(b.y + Math.min(2, b.h / 2)));
    }
    view.lastBar = null;
  }

  // The score read from the server's alphaTex; the beats it's unsure of
  // drawn lighter (an unsure note's head, an unsure line's words).
  function readScore(data) {
    const imp = new alphaTab.importer.AlphaTexImporter();
    imp.initFromString(data.alphaTex, new alphaTab.Settings());
    const score = imp.readScore();
    const M = alphaTab.model;
    const pale = new M.Color(0xa8, 0x9e, 0x92);
    const bars = score.tracks[0].staves[0].bars;
    for (const m of data.marks || []) {
      const beat = bars[m.bar] && bars[m.bar].voices[0].beats[m.beat];
      if (!beat) continue;
      if (m.lowNote && M.NoteStyle) {
        for (const n of beat.notes) {
          n.style = n.style || new M.NoteStyle();
          n.style.colors.set(M.NoteSubElement.StandardNotationNoteHead, pale);
        }
      }
      if (m.lowLyric && M.BeatStyle) {
        beat.style = beat.style || new M.BeatStyle();
        beat.style.colors.set(M.BeatSubElement.Effects, pale);
        beat.style.colors.set(M.BeatSubElement.StandardNotationEffects, pale);
      }
    }
    return score;
  }

  function settings(g) {
    const s = new alphaTab.Settings();
    s.core.fontDirectory = '/static/js/vendor/alphatab/font/';
    s.core.useWorkers = false;
    s.core.enableLazyLoading = false;
    s.display.layoutMode = alphaTab.LayoutMode.Page;
    s.display.scale = g.scale;
    s.display.barsPerRow = g.perRow;
    s.display.padding = [0, 0, 0, 0];
    const ink = new alphaTab.model.Color(0x24, 0x1e, 0x18);
    const r = s.display.resources;
    r.staffLineColor = new alphaTab.model.Color(0x7a, 0x70, 0x64);
    r.mainGlyphColor = r.secondaryGlyphColor = r.scoreInfoColor = r.barSeparatorColor = ink;
    // The page has the song's title: none again on the score; and no
    // dynamics (alphaTab marks the first note f: nothing was measured).
    for (const n of ['EffectDynamics', 'ScoreTitle', 'ScoreSubTitle', 'ScoreArtist', 'ScoreAlbum', 'ScoreWords', 'ScoreMusic', 'ScoreWordsAndMusic', 'ScoreCopyright', 'TrackNames']) {
      if (alphaTab.NotationElement[n] !== undefined) s.notation.elements.set(alphaTab.NotationElement[n], false);
    }
    return s;
  }

  function draw(data) {
    const g = geometry();
    applyGeometry(g);
    const score = readScore(data);
    if (!view) {
      const api = new alphaTab.AlphaTabApi(canvas, settings(g));
      view = { api, geo: g };
      api.postRenderFinished.on(() => requestAnimationFrame(() => { paginate(); goToAskedBar(); }));
    } else if (g.perRow !== view.geo.perRow || g.scale !== view.geo.scale) {
      view.api.settings.display.barsPerRow = g.perRow;
      view.api.settings.display.scale = g.scale;
      view.api.updateSettings();
    }
    Object.assign(view, { score, bars: data.bars || [], geo: g, data, lastBar: null });
    view.api.renderScore(score, [0]);
    renderInfo(data);
    startFollowing();
  }

  function renderInfo(data) {
    const conf = $('.melody-conf');
    const bits = [];
    if (data.lines && data.lines.length) bits.push(tr('lyrics placed: %s', Math.round((data.confidence || 0) * 100) + '%'));
    if (data.syncSource === 'analysis') bits.push(tr('bars estimated from the recording, not synced'));
    else if (data.syncEstimated) bits.push(tr('sync estimated'));
    // The lyrics are held to the bars the sync gives: an estimate places
    // them only as well as it places the bars.
    if (data.syncSource === 'analysis' || data.syncEstimated) bits.push(tr('placement depends on the sync — save the sync for better results'));
    conf.textContent = bits.join(' · ');
    const xml = $('.melody-xml');
    if (xml) xml.href = `/api/songs/${variantId}/melody.musicxml${offset ? `?transpose=${offset}` : ''}`;
    if (canEdit) renderFixes(data);
  }

  // ---- Editors: a line's lyrics a note earlier or later -----------------

  function renderFixes(data) {
    const list = $('.melody-fix-lines');
    if (!list) return;
    list.replaceChildren(...(data.lines || []).map((l) => {
      const li = document.createElement('li');
      li.className = 'melody-fix-line' + (!l.matched || l.conf < 0.45 ? ' is-low' : '');
      const where = document.createElement('span');
      where.className = 'melody-fix-where';
      where.textContent = l.matched ? `${l.label} · ${tr('bar %d', l.bar + 1)}` : `${l.label} · ${tr('not placed')}`;
      const text = document.createElement('span');
      text.className = 'melody-fix-text';
      text.textContent = l.text;
      const btn = (d, label, title) => {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'icon-btn melody-fix-btn';
        b.textContent = label;
        b.title = title;
        b.setAttribute('aria-label', title);
        b.disabled = !l.matched;
        b.addEventListener('click', () => shiftLine(l, l.shift + d));
        return b;
      };
      const sh = document.createElement('span');
      sh.className = 'melody-fix-shift';
      sh.textContent = l.shift ? (l.shift > 0 ? `+${l.shift}` : String(l.shift)) : '';
      li.append(where, text, btn(-1, '◀', tr('a note earlier')), sh, btn(1, '▶', tr('a note later')));
      return li;
    }));
  }
  async function shiftLine(l, shift) {
    try {
      const res = await fetch(`/api/songs/${variantId}/melody/lyrics-shift${offset ? `?transpose=${offset}` : ''}`, {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ section: l.section, line: l.line, shift }),
      });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      if (data.state === 'ready') draw(data);
    } catch (e) {
      showWait(tr('Saving failed'));
    }
  }
  $('.melody-fix-toggle')?.addEventListener('click', (e) => {
    const box = $('.melody-fix');
    box.hidden = !box.hidden;
    e.currentTarget.setAttribute('aria-expanded', String(!box.hidden));
  });

  // ---- Guitar Pro download -------------------------------------------------

  $('.melody-gp')?.addEventListener('click', () => {
    if (!view || !view.data) return;
    const score = readScore(view.data);
    const bytes = new alphaTab.exporter.Gp7Exporter().export(score, new alphaTab.Settings());
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }));
    const title = (document.querySelector('h1')?.textContent || 'melody').trim().replace(/[\\/:*?"<>|]/g, '');
    a.download = `${title} - ${tr('melody')}.gp`;
    document.body.appendChild(a);
    a.click();
    setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
  });

  // ?bar=N (a songbook row's lowest or highest bar, row-range.js): that
  // played bar shown and marked, the player put there, once.
  let askedBar = (() => {
    const b = Number(new URLSearchParams(location.search).get('bar'));
    return Number.isInteger(b) && b >= 0 && new URLSearchParams(location.search).get('bar') !== null ? b : null;
  })();
  function goToAskedBar() {
    if (askedBar === null || !view) return;
    const i = view.bars.indexOf(askedBar);
    const lookup = view.api.renderer && view.api.renderer.boundsLookup;
    const mb = i >= 0 && lookup ? lookup.findMasterBarByIndex(i) : null;
    const r = mb && (mb.realBounds || mb.visualBounds);
    if (!r) return;
    const bar = askedBar;
    askedBar = null;
    cursor.hidden = false;
    cursor.style.left = `${canvas.offsetLeft + r.x}px`;
    cursor.style.top = `${canvas.offsetTop + r.y}px`;
    cursor.style.width = `${r.w}px`;
    cursor.style.height = `${r.h}px`;
    const top = canvas.getBoundingClientRect().top + r.y;
    window.scrollBy({ top: top - Math.max(80, window.innerHeight * 0.3) });
    if (window.songbooksPlayer && window.songbooksPlayer.seekBar) window.songbooksPlayer.seekBar(bar, false);
  }

  // ---- Following the player --------------------------------------------------

  let following = false;
  function startFollowing() {
    if (following) return;
    following = true;
    requestAnimationFrame(follow);
  }
  function follow() {
    if (!view || sheet.dataset.renderMode !== 'melody') { following = false; cursor.hidden = true; return; }
    requestAnimationFrame(follow);
    const pos = window.songbooksReferencePosition && window.songbooksReferencePosition();
    const bar = pos && pos.bar >= 0 ? Math.floor(pos.bar) : null;
    const i = bar === null ? -1 : view.bars.indexOf(bar);
    if (i === view.lastBar) return;
    view.lastBar = i;
    const lookup = view.api.renderer && view.api.renderer.boundsLookup;
    const mb = i >= 0 && lookup && lookup._paged ? lookup.findMasterBarByIndex(i) : null;
    const r = mb && (mb.realBounds || mb.visualBounds);
    if (!r) { cursor.hidden = true; return; }
    cursor.hidden = false;
    cursor.style.left = `${canvas.offsetLeft + r.x}px`;
    cursor.style.top = `${canvas.offsetTop + r.y}px`;
    cursor.style.width = `${r.w}px`;
    cursor.style.height = `${r.h}px`;
    if (pos && pos.playing) keepInSight(r);
  }
  function keepInSight(r) {
    const top = canvas.getBoundingClientRect().top + r.y, bottom = top + r.h;
    const dock = document.getElementById('audio-dock');
    const d = dock && dock.getBoundingClientRect();
    const floor = (d && d.height ? Math.min(window.innerHeight, d.top) : window.innerHeight) - 12;
    if (top < 56 || bottom > floor) window.scrollBy({ top: top - Math.max(64, window.innerHeight * 0.2), behavior: 'smooth' });
  }
  // A click on the score: the player goes to that beat's bar.
  canvas.addEventListener('click', (e) => {
    if (!view || !window.songbooksPlayer || !window.songbooksPlayer.seekBar) return;
    const lookup = view.api.renderer && view.api.renderer.boundsLookup;
    if (!lookup) return;
    const rect = canvas.getBoundingClientRect();
    const beat = lookup.getBeatAtPos(e.clientX - rect.left, e.clientY - rect.top);
    if (!beat) return;
    const i = beat.voice.bar.index;
    const played = view.bars[i];
    if (played === undefined) return;
    const mb = beat.voice.bar.masterBar;
    const full = (mb.timeSignatureNumerator * 3840) / mb.timeSignatureDenominator;
    window.songbooksPlayer.seekBar(played + Math.min(0.999, beat.playbackStart / full), false);
  });

  // ---- Loading -----------------------------------------------------------------

  async function load() {
    offset = window.songbooksChordOffset ? window.songbooksChordOffset() : 0;
    page.classList.add('is-loading');
    try {
      const res = await fetch(`/api/songs/${variantId}/melody${offset ? `?transpose=${offset}` : ''}`, { credentials: 'same-origin' });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      if (data.state !== 'ready') {
        showWait(tr(WAIT[data.state] || WAIT.sync));
        page.classList.add('is-empty');
        return;
      }
      await loadAlphaTab();
      page.classList.remove('is-empty');
      showWait('');
      draw(data);
    } catch (e) {
      showWait(e.message || tr('Could not load the melody.'));
    } finally {
      page.classList.remove('is-loading');
    }
  }
  function onMode() {
    if (sheet.dataset.renderMode !== 'melody') return;
    if (!loading) loading = load();
    else if (view) { relayout(); startFollowing(); }
  }
  function relayout() {
    if (!view || sheet.dataset.renderMode !== 'melody') return;
    const g = geometry();
    if (g.pageW === view.geo.pageW) return;
    draw(view.data);
  }
  new MutationObserver(onMode).observe(sheet, { attributes: true, attributeFilter: ['data-render-mode'] });
  let resizeTimer = null;
  window.addEventListener('resize', () => { clearTimeout(resizeTimer); resizeTimer = setTimeout(relayout, 150); });
  // The page's key moved (−/+, a coach's key, the player's Key): the notes
  // move with it.
  let keyTimer = null;
  document.addEventListener('songbooks:transpose', (e) => {
    const o = (e.detail && e.detail.offset) || 0;
    if (o === offset || !loading) return;
    clearTimeout(keyTimer);
    keyTimer = setTimeout(() => { loading = load(); }, 200);
  });
  onMode();

  window.MelodyView = { api: () => view && view.api, data: () => view && view.data, reload: () => (loading = load()) };
})();

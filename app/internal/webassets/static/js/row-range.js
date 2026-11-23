// A songbook row's melody (httpapi/row_voice.go, docs/coaching.md "The
// song's melody on songbook rows"): a song whose melody is analysed shows
// the bar where it first reaches its lowest note and the bar where it
// first reaches its highest, as sung, the syllables under them. The page
// draws the range as one bar; this fetches the two bars (small SVGs the
// server draws, cached there and here) for the rows as they come into
// view — never alphaTab per row: a songbook lists dozens. A tap on a bar
// opens the song's Melody view at that bar. With a voice to weigh it
// against (data-zone), the fit beside it follows the notes drawn.
(() => {
  const cache = new Map(); // url → Promise<data|null>
  const store = (() => { try { return window.sessionStorage; } catch (e) { return null; } })();
  const get = (url) => {
    if (cache.has(url)) return cache.get(url);
    let hit = null;
    try { hit = store && JSON.parse(store.getItem('rr:' + url) || 'null'); } catch (e) { hit = null; }
    const p = hit && hit.at > Date.now() - 300000 ? Promise.resolve(hit.data)
      : fetch(url, { credentials: 'same-origin' }).then((r) => (r.status === 200 ? r.json() : null)).catch(() => null)
        .then((data) => {
          try { if (store) store.setItem('rr:' + url, JSON.stringify({ at: Date.now(), data })); } catch (e) { /* full: no matter */ }
          return data;
        });
    cache.set(url, p);
    return p;
  };
  const tr = (m, ...a) => (window.t ? window.t(m, ...a) : m.replace(/%[ds]/g, () => String(a.shift())));

  // The song's page at a bar, in its Melody view: the row's own link
  // (its songbook, its key) when the row has one.
  function hrefFor(el, bar) {
    const row = el.closest('li, .gig-row, .cbook-song, .cbk-row') || el.parentElement;
    const a = el.closest('a[href*="/song/"]') || (row && row.querySelector('a[href*="/song/"]'));
    const id = (el.dataset.rowRange.match(/\/songs\/(\d+)\//) || [])[1];
    const u = new URL(a ? a.getAttribute('href') : `/song/${id}`, location.href);
    u.searchParams.set('view', 'melody');
    u.searchParams.set('bar', String(bar));
    return u.pathname + u.search;
  }

  function fit(box, low, high) {
    const z = (box.dataset.zone || '').split(',').map(Number);
    if (z.length < 2 || !z[0]) return;
    const up = high - z[1], down = z[0] - low;
    const parts = [];
    if (up > 0) parts.push(tr('%d high', up));
    if (down > 0) parts.push(tr('%d low', down));
    let f = box.querySelector('.row-voice-fit');
    if (!f) { f = document.createElement('span'); f.className = 'row-voice-fit'; box.append(f); }
    f.textContent = parts.length ? parts.join(' · ') : tr('fits');
    box.classList.toggle('is-out', parts.length > 0);
    box.classList.toggle('is-fit', parts.length === 0);
  }

  function draw(el, d) {
    if (!d || !d.bars || !d.bars.length) return;
    const wrap = document.createElement('span');
    wrap.className = 'rr-bars';
    wrap.setAttribute('role', 'group');
    wrap.setAttribute('aria-label', d.label);
    for (const b of d.bars) {
      const s = document.createElement('span');
      s.className = `rr-bar is-${b.kind}`;
      s.setAttribute('role', 'link');
      s.tabIndex = 0;
      s.dataset.href = hrefFor(el, b.bar);
      s.title = b.label;
      s.innerHTML = b.svg;
      wrap.append(s);
    }
    el.replaceChildren(wrap);
    el.classList.add('is-bars');
    const box = el.closest('.row-voice');
    if (box) fit(box, d.low, d.high);
  }

  // The rows in view (and a little ahead) only, once each.
  const seen = new WeakSet();
  const io = 'IntersectionObserver' in window ? new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      io.unobserve(e.target);
      const el = e.target;
      get(el.dataset.rowRange).then((d) => draw(el, d));
    }
  }, { rootMargin: '300px 0px' }) : null;
  function scan(root) {
    (root || document).querySelectorAll('[data-row-range]').forEach((el) => {
      if (seen.has(el)) return;
      seen.add(el);
      if (io) io.observe(el); else get(el.dataset.rowRange).then((d) => draw(el, d));
    });
  }
  scan();
  // Rows put in later (the coach's songbook filtered on a student swaps in
  // that student's).
  new MutationObserver((ms) => {
    for (const m of ms) for (const n of m.addedNodes) if (n.nodeType === 1) scan(n.parentElement || n);
  }).observe(document.body, { childList: true, subtree: true });

  // A bar opens the song there: a link of its own inside the row's link.
  const go = (e) => {
    const b = e.target.closest('.rr-bar[data-href]');
    if (!b) return false;
    e.preventDefault();
    e.stopPropagation();
    location.href = b.dataset.href;
    return true;
  };
  document.addEventListener('click', go, true);
  document.addEventListener('keydown', (e) => {
    if ((e.key === 'Enter' || e.key === ' ') && e.target.matches && e.target.matches('.rr-bar[data-href]')) go(e);
  }, true);
  window.RowRange = { scan };
})();

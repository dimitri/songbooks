// Service worker: the whole reason this exists is "wifi drops at the gig,
// the set still works." Everything — the song/project/setlist pages, their
// GET API calls, AND static assets — is network-first with a cache
// fallback, never cache-first. This app is under active development
// (CSS/JS change almost every round), and a plain cache-first strategy for
// /static/ served a hand-bumped CACHE_VERSION string that, in practice,
// never got bumped: a returning browser kept serving months-stale
// app.css/song.js forever, silently, with no error — which is exactly what
// made several rounds of real UI fixes look like they'd never landed.
// Network-first costs an extra round-trip per asset when online (fine —
// these are small local files), and still falls back to the cache when
// genuinely offline, which is the one thing this file actually exists for.
const CACHE_VERSION = 'songbooks-v2';
const CORE_ASSETS = [
  '/', '/manifest.json',
  '/static/css/app.css',
  '/static/js/song.js',
  '/static/js/clock.js',
  '/static/js/project.js',
  '/static/js/setlist.js',
  '/static/js/gig.js',
  '/static/fonts/EBGaramond.ttf',
  '/static/fonts/EBGaramond-Italic.ttf',
  '/static/icon.svg',
];

self.addEventListener('install', e => {
  e.waitUntil(
    caches.open(CACHE_VERSION).then(c => c.addAll(CORE_ASSETS)).catch(() => {})
  );
  self.skipWaiting();
});

self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys().then(keys => Promise.all(
      keys.filter(k => k !== CACHE_VERSION).map(k => caches.delete(k))
    )).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', e => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  e.respondWith(
    fetch(req).then(res => {
      if (res.ok) {
        const copy = res.clone();
        caches.open(CACHE_VERSION).then(c => c.put(req, copy));
      }
      return res;
    }).catch(() => caches.match(req).then(cached => cached || Response.error()))
  );
});

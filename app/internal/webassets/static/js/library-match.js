// "Already in the library" check for every new-song form (songbook
// wizard, band repertoire, community songbooks). As a title is typed it
// asks the server for songs with the same title (GET /api/catalog/match:
// accents, punctuation and a leading "The" ignored, artist only boosts)
// and lists up to five, each with one button that adds THAT song here
// instead of creating a duplicate. "None of these — create a new song"
// carries on with the form as before. No match: nothing shows, nothing
// changes.
//
//   const lm = LibraryMatch.attach({
//     title: input, artist: input,            // artist optional
//     host: element,                          // where the list goes
//     addLabel: 'Add to this songbook',
//     add: async (hit) => { ... },            // throw Error('message') on failure
//     isInList: (variantId) => bool,          // optional: already here?
//     openHref: (hit) => url,                 // optional: link for "already here" rows
//     onNone: () => { ... },                  // optional: after "None of these"
//   });
//   if (!(await lm.guard())) return;          // before creating: false = matches just shown
//   lm.check({host: otherEl});                // re-check now, e.g. after an import set the title
//   lm.reset();                               // form reopened
(function () {
  const MIN_CHARS = 3;
  const DEBOUNCE_MS = 350;
  // Set once /api/catalog/match answers 404 (a server without it), so
  // every later check goes straight to the older full-text search.
  let matchEndpointMissing = false;

  // Same folding as the server's matchNormSQL (internal/store/catalog_match.go),
  // used for the fallback search and to tell whether the title changed.
  function normalize(s) {
    return (s || '').toLowerCase()
      .replace(/œ/g, 'oe').replace(/æ/g, 'ae').replace(/ß/g, 's')
      .normalize('NFD').replace(/[̀-ͯ]/g, '')
      .replace(/[^a-z0-9]+/g, ' ').trim()
      .replace(/^(the|le|la|les|l) /, '');
  }

  // Fallback for a server without /api/catalog/match: the full-text
  // search over the title, then the same title rules as the server.
  async function searchFallback(title, artist, signal) {
    const res = await fetch(`/api/catalog/search?q=${encodeURIComponent(title)}`, { signal });
    if (!res.ok) throw new Error('search failed');
    const hits = await res.json();
    const t = normalize(title), a = normalize(artist);
    const scored = [];
    for (const h of hits) {
      const nt = normalize(h.title), na = normalize(h.artist);
      let ts = 0;
      if (nt === t) ts = 100;
      else if (nt.startsWith(t)) ts = 70;
      else if ((' ' + nt).includes(' ' + t)) ts = 45;
      else if (nt.length >= 4 && t.startsWith(nt + ' ')) ts = 40;
      if (!ts) continue;
      let as = 0;
      if (a && na) as = na === a ? 30 : ((' ' + na).includes(' ' + a) || (' ' + a).includes(' ' + na)) ? 15 : 0;
      scored.push({ hit: { ...h, exact: ts === 100 }, score: ts + as });
    }
    return scored.sort((x, y) => y.score - x.score).slice(0, 5).map((s) => s.hit);
  }

  async function fetchMatches(title, artist, signal) {
    if (!matchEndpointMissing) {
      const res = await fetch(`/api/catalog/match?title=${encodeURIComponent(title)}&artist=${encodeURIComponent(artist)}`, { signal });
      // A server without the route answers 404, or 200 with the home page
      // (GET / catches everything): either way, not JSON.
      const json = (res.headers.get('Content-Type') || '').includes('application/json');
      if (res.ok && json) return res.json();
      if (json) throw new Error('match failed');
      matchEndpointMissing = true;
    }
    return searchFallback(title, artist, signal);
  }

  // The line under a title: what tells two versions of a song apart.
  function details(hit) {
    const bits = [];
    if (hit.artist) bits.push(hit.artist);
    if (hit.key) bits.push(`Key ${hit.key}`);
    if (hit.visibility === 'user') bits.push('Only you can see it');
    else if (hit.madeBy) bits.push(`Shared by ${hit.madeBy}`);
    if (hit.bands) bits.push(hit.bands === 1 ? 'Played by 1 band' : `Played by ${hit.bands} bands`);
    else if (hit.songbooks) bits.push(hit.songbooks === 1 ? 'In 1 songbook' : `In ${hit.songbooks} songbooks`);
    return bits.join(' · ');
  }

  function attach(opts) {
    const titleInput = opts.title;
    const artistInput = opts.artist || null;
    if (!titleInput || !opts.host) return null;
    let host = opts.host;
    let timer = null;
    let controller = null;
    let shownKey = '';      // normalized title+artist the panel currently shows
    let shownCount = 0;
    let dismissed = false;  // "None of these" pressed; stays until the title is cleared or reset()
    let pending = null;     // the in-flight check's promise

    function key() {
      return normalize(titleInput.value) + '|' + normalize(artistInput ? artistInput.value : '');
    }

    function clear() {
      document.querySelectorAll('.library-match[data-owner="' + titleInput.id + '"]').forEach((el) => el.remove());
      shownKey = '';
      shownCount = 0;
    }

    function render(hits, k) {
      clear();
      if (!hits.length) return;
      shownKey = k;
      shownCount = hits.length;
      const panel = document.createElement('section');
      panel.className = 'library-match';
      panel.dataset.owner = titleInput.id;
      panel.setAttribute('aria-labelledby', titleInput.id + '-library-match-title');

      const heading = document.createElement('h3');
      heading.className = 'library-match-title';
      heading.id = titleInput.id + '-library-match-title';
      heading.textContent = 'Already in the library';
      const hint = document.createElement('p');
      hint.className = 'hint';
      hint.textContent = hits.length === 1
        ? 'This song is already here. Add it instead of starting from scratch.'
        : 'These songs are already here. Add one instead of starting from scratch.';
      panel.append(heading, hint);

      const list = document.createElement('div');
      list.className = 'attention-list';
      const status = document.createElement('p');
      status.className = 'form-status';
      status.setAttribute('role', 'status');

      for (const hit of hits) {
        const row = document.createElement('div');
        row.className = 'attention-row library-match-row';
        // Titles and artists are user-written: text only, never markup.
        const text = document.createElement('div');
        text.className = 'library-match-text';
        const t = document.createElement('div');
        t.className = 'attention-title';
        t.textContent = hit.title;
        text.appendChild(t);
        const d = details(hit);
        if (d) {
          const sub = document.createElement('div');
          sub.className = 'attention-subtitle';
          sub.textContent = d;
          text.appendChild(sub);
        }
        row.appendChild(text);

        if (opts.isInList && opts.isInList(hit.variantId)) {
          const here = document.createElement('a');
          here.className = 'chip-link';
          here.href = opts.openHref ? opts.openHref(hit) : `/song/${hit.variantId}`;
          here.textContent = 'Already here — open';
          row.appendChild(here);
        } else {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.textContent = opts.addLabel || 'Add this one';
          btn.setAttribute('aria-label', `${opts.addLabel || 'Add'}: ${hit.title}${hit.artist ? ', ' + hit.artist : ''}`);
          btn.addEventListener('click', async () => {
            Forms.status(status, '');
            Forms.busy(btn, true);
            try {
              await opts.add(hit);
            } catch (e) {
              Forms.status(status, (e && e.message) || `Couldn't add “${hit.title}”. Try again.`, 'error');
            } finally {
              Forms.busy(btn, false);
            }
          });
          row.appendChild(btn);
        }
        list.appendChild(row);
      }
      panel.append(list, status);

      const none = document.createElement('button');
      none.type = 'button';
      none.className = 'link-button library-match-none';
      none.textContent = 'None of these — create a new song';
      none.addEventListener('click', () => {
        dismissed = true;
        clear();
        if (opts.onNone) opts.onNone();
        else titleInput.focus();
      });
      panel.appendChild(none);
      host.appendChild(panel);
    }

    // Runs the check now; resolves to the number of matches on screen.
    function run() {
      clearTimeout(timer);
      if (controller) controller.abort();
      const title = titleInput.value.trim();
      const artist = artistInput ? artistInput.value.trim() : '';
      const k = key();
      if (!normalize(title)) dismissed = false;
      if (dismissed || normalize(title).length < MIN_CHARS) { clear(); return Promise.resolve(0); }
      if (k === shownKey) return Promise.resolve(shownCount);
      controller = new AbortController();
      const signal = controller.signal;
      pending = fetchMatches(title, artist, signal)
        .then((hits) => {
          if (signal.aborted) return shownCount;
          render(hits || [], k);
          return shownCount;
        })
        // A failed check never stands in the way of creating a song.
        .catch(() => (signal.aborted ? shownCount : 0));
      return pending;
    }

    function schedule() {
      // Typing again: the list goes back next to the fields.
      if (host !== opts.host) { clear(); host = opts.host; }
      clearTimeout(timer);
      timer = setTimeout(run, DEBOUNCE_MS);
    }
    titleInput.addEventListener('input', schedule);
    if (artistInput) artistInput.addEventListener('input', schedule);

    return {
      // Re-check now (optionally showing the list somewhere else, e.g.
      // next to an import that just filled in the title).
      check(o) {
        if (o && o.host && o.host !== host) { clear(); host = o.host; }
        return run();
      },
      // Before creating: true to go ahead. False only when matches were
      // not on screen yet and now are, so whoever typed fast and pressed
      // Create still gets asked once. Pressing Create with the list
      // already showing counts as "none of these".
      async guard() {
        if (dismissed) return true;
        const before = shownKey;
        const n = await run();
        if (n > 0 && shownKey !== before) {
          host.querySelector('.library-match')?.scrollIntoView({ block: 'nearest' });
          return false;
        }
        return true;
      },
      reset(o) {
        clearTimeout(timer);
        if (controller) controller.abort();
        dismissed = false;
        clear();
        if (o && o.host) host = o.host;
        else host = opts.host;
      },
      get dismissed() { return dismissed; },
    };
  }

  window.LibraryMatch = { attach, normalize };
})();

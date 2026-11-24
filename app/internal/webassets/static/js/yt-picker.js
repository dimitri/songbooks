// yt-picker.js — the YouTube picker: finding a song's video and using it.
// One component for every place that picks a video: the song's Reference
// view ("+ Reference", "Change the video"), the Import wizard's
// Recording step, Add / Create's reference video.
//
// A search prefilled with the song's title and artist runs over YouTube
// (GET /api/youtube/find, yt_picker.go: yt-dlp's search, no API key); a
// pasted link is read as that one video (oEmbed's title and channel).
// Each result shows its thumbnail, title, channel and length; picked, it
// plays in place (the preview) and gets the one primary action, "Use
// this video". What using it does is the caller's (onUse).
//
// YTPicker.mount(el, opts) → picker   in el (the "yt-picker" partial, or any element)
// YTPicker.create(opts) → picker      its own element (picker.el), to put anywhere
// YTPicker.open(opts) → Promise       in a dialog: the video used, or null
//
// opts (the partial's data-* give the defaults):
//   title, artist   the song: the query, and the search's ranking
//   current         the link in use: marked, its action off
//   useLabel        the action's words (default "Use this video")
//   onUse(c)        async; throws to say why it wasn't used, false to stay
//   extras(c)       a node beside each result (the wizard's stars)
//   order(list)     the results' order as shown (the wizard's "Starred first")
//   onResults(list) after each search
//
// picker: el, search(), setQuery(title, artist), setCurrent(url),
// render(), stop() (the preview off), reset(), focus(), candidates.
//
// The plain logic (links, the search's address, a result's line) is
// tested by internal/webassets/jstest/yt-picker.test.mjs.
(function (root) {
  const tr = (s, ...a) => (root.t ? root.t(s, ...a) : s.replace(/%[ds]/g, () => String(a.shift())));

  // ---- plain logic ----------------------------------------------------

  // A pasted link, rather than words to search for.
  const looksLink = (s) => /^(https?:\/\/|www\.|m\.youtube|youtu\.?be)/i.test(String(s || '').trim());

  // A YouTube video's id, from any of its addresses (watch?v=, youtu.be/,
  // shorts/, embed/, live/); null for anything else.
  function youtubeId(url) {
    let u;
    try { u = new URL(/^[a-z]+:\/\//i.test(url) ? url : 'https://' + url); } catch (e) { return null; }
    const host = u.hostname.toLowerCase().replace(/^(www|m|music)\./, '');
    let id = null;
    if (host === 'youtu.be') id = u.pathname.slice(1).split('/')[0];
    else if (host === 'youtube.com' || host === 'youtube-nocookie.com') {
      if (u.pathname === '/watch') id = u.searchParams.get('v');
      else {
        const m = u.pathname.match(/^\/(shorts|embed|live)\/([^/?]+)/);
        if (m) id = m[2];
      }
    }
    return id && /^[\w-]{6,20}$/.test(id) ? id : null;
  }

  const thumbOf = (id) => (id ? `https://i.ytimg.com/vi/${id}/mqdefault.jpg` : '');

  // "4:31", "1:02:05"; "" when unknown.
  function duration(secs) {
    const s = Math.round(Number(secs) || 0);
    if (s <= 0) return '';
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const ss = String(s % 60).padStart(2, '0');
    return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
  }

  // The query a song starts from: "Title Artist".
  const queryOf = (title, artist) => [title, artist].map((x) => String(x || '').trim()).filter(Boolean).join(' ');

  // The search's address: a link read as itself; the song's own query
  // with its title and artist apart (the ranking knows the artist's
  // channel); anything else typed as it is.
  function findURL(query, title, artist) {
    const q = String(query || '').trim();
    if (!q) return '';
    if (looksLink(q)) return '/api/youtube/find?' + new URLSearchParams({ url: q });
    if (title && q === queryOf(title, artist)) {
      const p = new URLSearchParams({ title: String(title).trim() });
      if (String(artist || '').trim()) p.set('artist', String(artist).trim());
      return '/api/youtube/find?' + p;
    }
    return '/api/youtube/find?' + new URLSearchParams({ q });
  }

  // Two links to the same video (or the same page).
  function sameVideo(a, b) {
    if (!a || !b) return false;
    const x = youtubeId(a);
    const y = youtubeId(b);
    if (x || y) return x === y;
    return String(a).trim() === String(b).trim();
  }

  // A result's line under its title: channel, length, what it is, in use.
  function metaParts(c, current) {
    return [
      c.channel ? c.channel + (c.official ? ' — ' + tr("the artist's channel") : '') : '',
      duration(c.duration),
      c.note ? tr(c.note) : '',
      sameVideo(c.url, current) ? tr('in use') : '',
    ].filter(Boolean);
  }

  const logic = { looksLink, youtubeId, thumbOf, duration, queryOf, findURL, sameVideo, metaParts };
  if (typeof module !== 'undefined' && module.exports) { module.exports = logic; return; }

  // ---- the picker -----------------------------------------------------

  const el = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  let uid = 0;

  function mount(host, opts = {}) {
    if (host.ytPicker) return Object.assign(host.ytPicker.opts, opts) && host.ytPicker;
    const d = host.dataset || {};
    const o = Object.assign({
      title: d.title || '', artist: d.artist || '', current: d.current || '',
      useLabel: d.useLabel || '', label: d.label || '',
    }, opts);
    const n = ++uid;
    host.classList.add('ytpick');

    const form = el('form', 'ytpick-search');
    form.setAttribute('role', 'search');
    form.noValidate = true;
    const label = el('label', 'ytpick-label', o.label || tr('Find the video'));
    const q = el('input');
    q.type = 'search';
    q.id = label.htmlFor = `ytpick-q${n}`;
    q.autocomplete = 'off';
    q.spellcheck = false;
    q.enterKeyHint = 'search';
    q.setAttribute('autocapitalize', 'none');
    q.placeholder = tr('Search YouTube, or paste a link');
    const go = el('button', 'ytpick-go', tr('Search'));
    go.type = 'submit';
    const row = el('div', 'ytpick-search-row');
    row.append(q, go);
    form.append(label, row);
    const status = el('p', 'ytpick-status hint');
    status.setAttribute('role', 'status');
    const list = el('ul', 'ytpick-results');
    list.setAttribute('aria-label', tr('Videos found'));
    host.replaceChildren(form, status, list);

    const st = { list: null, seq: 0, open: '' };
    const picker = {
      el: host, opts: o,
      get candidates() { return st.list; },
      search, render, stop, reset, focus: () => q.focus(),
      setQuery(title, artist) { o.title = title || ''; o.artist = artist || ''; q.value = queryOf(o.title, o.artist); },
      // The link in use now: marked, the preview closed.
      setCurrent(url) { o.current = url || ''; st.open = ''; render(); },
    };
    q.value = queryOf(o.title, o.artist);

    form.addEventListener('submit', (e) => { e.preventDefault(); search(); });
    // A pasted link: read at once.
    q.addEventListener('paste', () => setTimeout(() => { if (looksLink(q.value)) search(); }, 0));

    function say(text, kind) {
      status.textContent = text || '';
      status.classList.toggle('is-error', kind === 'error');
    }

    async function search() {
      const url = findURL(q.value, o.title, o.artist);
      if (!url) { say(tr('Type a title and an artist, or paste a link.'), 'error'); q.focus(); return; }
      const mine = ++st.seq;
      stop();
      const link = looksLink(q.value);
      say(link ? tr('Reading the link…') : tr('Searching YouTube…'));
      host.setAttribute('aria-busy', 'true');
      list.replaceChildren();
      let data = null;
      try {
        const res = await fetch(url);
        data = res.ok ? await res.json() : null;
      } catch (e) { data = null; }
      if (mine !== st.seq) return;
      host.removeAttribute('aria-busy');
      if (!data) { st.list = []; say(link ? tr('Couldn’t read that link.') : tr("Couldn't search YouTube just now."), 'error'); return; }
      st.list = data.candidates || [];
      say(st.list.length ? '' : tr('Nothing found on YouTube.'));
      // A link: that one video, open at once.
      st.open = link && st.list.length ? key(st.list[0]) : '';
      render();
      if (o.onResults) o.onResults(st.list);
    }

    const key = (c) => c.id || c.url;

    // Drawn again (a pick, the order changed): the video playing goes on
    // playing.
    let keep = null;
    function render() {
      if (!st.list) return;
      keep = list.querySelector('.ytpick-item.is-open > .ytpick-preview');
      const shown = o.order ? o.order(st.list.slice()) : st.list;
      list.replaceChildren(...shown.map(item));
      keep = null;
    }

    function item(c) {
      const k = key(c);
      const open = st.open === k;
      const inUse = sameVideo(c.url, o.current);
      const li = el('li', 'ytpick-item' + (open ? ' is-open' : '') + (inUse ? ' is-current' : '') + (c.official ? ' is-official' : ''));
      const pick = el('button', 'ytpick-pick');
      pick.type = 'button';
      pick.setAttribute('aria-expanded', String(open));
      const thumb = el('span', 'ytpick-thumb');
      const src = c.thumb || thumbOf(c.id);
      if (src) {
        const img = el('img');
        img.src = src;
        img.alt = '';
        img.loading = 'lazy';
        img.decoding = 'async';
        img.referrerPolicy = 'no-referrer';
        img.width = 160; img.height = 90;
        thumb.append(img);
      }
      const len = duration(c.duration);
      if (len) thumb.append(el('span', 'ytpick-len', len));
      const text = el('span', 'ytpick-text');
      text.append(el('span', 'ytpick-title', c.title || c.url));
      const meta = metaParts(c, o.current).filter((x) => x !== len);
      if (meta.length) text.append(el('span', 'ytpick-meta', meta.join(' · ')));
      pick.append(thumb, text);
      pick.addEventListener('click', () => {
        st.open = st.open === k ? '' : k;
        render();
        if (st.open) list.querySelector('.ytpick-item.is-open .ytpick-use')?.focus({ preventScroll: true });
      });
      const head = el('div', 'ytpick-row');
      head.append(pick);
      const extra = o.extras ? o.extras(c) : null;
      if (extra) head.append(extra);
      li.append(head);
      if (open) li.append(keep && keep.dataset.key === k ? keep : preview(c, inUse));
      return li;
    }

    // The video picked: its one action, and the video playing in place.
    function preview(c, inUse) {
      const box = el('div', 'ytpick-preview');
      box.dataset.key = key(c);
      const id = c.id || youtubeId(c.url);
      if (id) {
        const frame = el('div', 'ytpick-embed');
        const iframe = el('iframe');
        iframe.src = `https://www.youtube-nocookie.com/embed/${encodeURIComponent(id)}?autoplay=1`;
        iframe.title = tr('Preview: %s', c.title || 'YouTube');
        iframe.allow = 'autoplay; encrypted-media; picture-in-picture';
        iframe.allowFullscreen = true;
        frame.append(iframe);
        box.append(frame);
      }
      const actions = el('div', 'ytpick-actions');
      const use = el('button', 'primary ytpick-use', inUse ? tr('In use') : (o.useLabel || tr('Use this video')));
      use.type = 'button';
      use.disabled = inUse;
      const out = el('p', 'form-status ytpick-use-status');
      out.setAttribute('role', 'status');
      use.addEventListener('click', async () => {
        if (!o.onUse) return;
        if (root.Forms) Forms.busy(use, true); else use.disabled = true;
        out.textContent = '';
        // Its sound stops while it's saved (a question may come first).
        box.querySelector('.ytpick-embed')?.remove();
        let done;
        try {
          done = await o.onUse(c);
        } catch (e) {
          if (root.Forms) Forms.status(out, e.message || tr('Couldn’t use it. Try again.'), 'error');
          else out.textContent = e.message;
        } finally {
          if (root.Forms) Forms.busy(use, false); else use.disabled = false;
        }
        if (done !== false && sameVideo(c.url, o.current)) use.disabled = true;
      });
      const watch = el('a', 'ytpick-watch', id ? tr('Watch on YouTube') : tr('Open the link'));
      watch.href = c.url;
      watch.target = '_blank';
      watch.rel = 'noopener';
      watch.append(' ↗');
      actions.append(use, watch);
      // The action first, beside the row picked; the video under it.
      box.prepend(actions, out);
      return box;
    }

    // The preview off (its sound with it).
    function stop() {
      if (!st.open) return;
      st.open = '';
      render();
    }

    function reset() {
      st.seq++;
      st.list = null;
      st.open = '';
      list.replaceChildren();
      say('');
    }

    host.ytPicker = picker;
    return picker;
  }

  function create(opts = {}) {
    return mount(el('div', 'ytpick'), opts);
  }

  // In a dialog of its own: resolves with the video used, or null.
  let dlg = null;
  function open(opts = {}) {
    if (dlg) { dlg.close(); dlg.remove(); }
    dlg = el('dialog', 'addsong-dialog ytpick-dialog');
    dlg.id = 'ytpick-dialog';
    const head = el('div', 'addsong-head');
    const h = el('h2', '', opts.heading || tr('Choose the video'));
    h.id = 'ytpick-dialog-title';
    dlg.setAttribute('aria-labelledby', h.id);
    const close = el('button', 'icon-btn addsong-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', tr('Close'));
    head.append(h, close);
    const me = dlg;
    return new Promise((resolve) => {
      let used = null;
      const p = create(Object.assign({}, opts, {
        onUse: async (c) => {
          if (opts.onUse && (await opts.onUse(c)) === false) return false;
          used = c;
          me.close();
          return true;
        },
      }));
      me.append(head, p.el);
      // Its keys stay in it (the song page plays on Space).
      me.addEventListener('keydown', (e) => e.stopPropagation());
      close.addEventListener('click', () => me.close());
      me.addEventListener('close', () => { p.stop(); resolve(used); setTimeout(() => me.remove(), 0); if (dlg === me) dlg = null; });
      document.body.append(me);
      me.showModal();
      p.focus();
      if (p.el.querySelector('input').value.trim()) p.search();
    });
  }

  root.YTPicker = Object.assign({ mount, create, open }, logic);
})(typeof window !== 'undefined' ? window : globalThis);

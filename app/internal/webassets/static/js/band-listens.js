// band-listens.js — a band's Discover and Influences pages
// (band_listens.html, httpapi/band_listens.go): add a song by its
// YouTube link (its title and artist read from the video, to fix), remove
// one's own, and — Discover — propose one for a vote (proposal-flow.js,
// looked up from its link). Strings through t() (js-band-votes).
(function () {
  const form = document.getElementById('listen-add');
  if (!form) return;
  const bandId = form.dataset.band, pool = form.dataset.pool;
  const $ = (id) => document.getElementById(id);
  const url = $('listen-url'), title = $('listen-title'), artist = $('listen-artist'), note = $('listen-note'), tags = $('listen-tags');
  const found = form.querySelector('.listen-add-found');
  const submit = form.querySelector('button[type=submit]');
  const openers = document.querySelectorAll('[data-listen-add]');

  function show(on) {
    form.hidden = !on;
    openers.forEach((b) => b.setAttribute('aria-expanded', String(on)));
    if (on) { form.scrollIntoView({ block: 'nearest' }); url.focus(); }
  }
  openers.forEach((b) => b.addEventListener('click', () => show(form.hidden)));
  form.querySelector('[data-listen-cancel]').addEventListener('click', () => { form.reset(); found.hidden = true; show(false); });

  // The link read as soon as it's pasted (or left).
  let looked = '';
  async function lookup() {
    const v = url.value.trim();
    if (!v || v === looked) return;
    looked = v;
    Forms.fieldError(url, '');
    const res = await fetch(`/api/bands/${bandId}/listen-lookup?url=${encodeURIComponent(v)}`).catch(() => null);
    const d = res ? await res.json().catch(() => ({})) : {};
    if (!res || !res.ok) { found.hidden = true; Forms.fieldError(url, d.message || t('Couldn’t read that link. Try again.')); return; }
    url.value = d.url;
    looked = d.url;
    found.querySelector('img').src = `https://i.ytimg.com/vi/${d.videoId}/mqdefault.jpg`;
    if (!title.value) title.value = d.title || '';
    if (!artist.value) artist.value = d.artist || '';
    found.hidden = false;
  }
  url.addEventListener('paste', () => setTimeout(lookup, 0));
  url.addEventListener('change', lookup);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    if (!url.value.trim()) { Forms.fieldError(url, t('Paste a YouTube link.')); url.focus(); return; }
    Forms.run(form, submit, async () => {
      await lookup();
      if (url.getAttribute('aria-invalid')) throw new Error(t('Fix what’s marked, then try again.'));
      const res = await fetch(`/api/bands/${bandId}/listens`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          pool, url: url.value.trim(), title: title.value.trim(), artist: artist.value.trim(), note: note.value.trim(),
          tags: tags ? tags.value.split(',').map((x) => x.trim()).filter(Boolean) : [],
        }),
      }).catch(() => null);
      const d = res ? await res.json().catch(() => ({})) : {};
      if (!res || !res.ok) {
        if (d.field === 'url') { Forms.fieldError(url, d.message); url.focus(); return; }
        throw new Error(d.message || t('Couldn’t add it. Try again.'));
      }
      location.href = `${location.pathname}#l${d.id}`;
      location.reload();
    });
  });

  // Each song's actions.
  document.querySelectorAll('.listen-card').forEach((card) => {
    const st = card.querySelector('.form-status');
    card.querySelector('[data-listen-remove]')?.addEventListener('click', async (e) => {
      if (!confirm(t('Remove “%s” from the list?', card.dataset.title || card.dataset.url))) return;
      Forms.busy(e.currentTarget, true);
      const res = await fetch(`/api/bands/${bandId}/listens/${card.dataset.listen}`, { method: 'DELETE' }).catch(() => null);
      if (res && res.ok) { card.remove(); return; }
      const d = res ? await res.json().catch(() => ({})) : {};
      Forms.busy(e.currentTarget, false);
      Forms.status(st, d.message || t('Couldn’t remove it. Try again.'), 'error');
    });
    card.querySelector('[data-listen-propose]')?.addEventListener('click', () => {
      window.ProposeFlow.open({
        q: card.dataset.url,
        onSent: (proposalId) => fetch(`/api/bands/${bandId}/listens/${card.dataset.listen}/proposal`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ proposalId }),
        }).catch(() => null),
      });
    });
  });
  if (location.hash === '#add') show(true);
})();

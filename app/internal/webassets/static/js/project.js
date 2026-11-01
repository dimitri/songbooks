// ---- New-song wizard --------------------------------------------------
// A 4-step flow — title+reference, key+tempo, lyrics, chords — each step
// writing into fields the song already has a reason to carry, not a new
// data model: everything here lands as ordinary .song header lines or
// grid/lyric text, the same shape a hand-edited song file already has.
const wizard = document.getElementById('song-wizard');
const wizBook = wizard?.dataset.book;
const WIZ_STEPS = 4;
let wizStep = 0;

function wizShowStep(i) {
  wizStep = i;
  document.querySelectorAll('.wizard-panel').forEach(p => { p.hidden = parseInt(p.dataset.step, 10) !== i; });
  document.querySelectorAll('.wizard-step-dot').forEach(d => d.classList.toggle('active', parseInt(d.dataset.step, 10) === i));
  document.getElementById('wiz-back').hidden = i === 0;
  document.getElementById('wiz-next').hidden = i === WIZ_STEPS - 1;
  document.getElementById('wiz-create').hidden = i !== WIZ_STEPS - 1;
}
document.getElementById('wiz-next')?.addEventListener('click', () => {
  if (wizStep === 0 && !document.getElementById('wiz-title').value.trim()) {
    document.getElementById('wiz-title').focus();
    return;
  }
  wizShowStep(Math.min(WIZ_STEPS - 1, wizStep + 1));
});
document.getElementById('wiz-back')?.addEventListener('click', () => wizShowStep(Math.max(0, wizStep - 1)));
document.querySelectorAll('.wizard-step-dot').forEach(dot => {
  dot.addEventListener('click', () => wizShowStep(parseInt(dot.dataset.step, 10)));
});

// Same YouTube-id extraction as the song page's reference player — see
// song.js for why each URL shape is handled (watch/shorts/embed/youtu.be).
function wizYoutubeId(url) {
  try {
    const u = new URL(url);
    const host = u.hostname.replace(/^www\.|^m\./, '');
    if (host === 'youtu.be') return u.pathname.slice(1).split('/')[0] || null;
    if (host === 'youtube.com' || host === 'youtube-nocookie.com') {
      if (u.pathname === '/watch') return u.searchParams.get('v');
      const m = u.pathname.match(/^\/(shorts|embed|live)\/([^/?]+)/);
      if (m) return m[2];
    }
  } catch (e) { /* not a URL */ }
  return null;
}
document.getElementById('wiz-reference-url')?.addEventListener('change', e => {
  const embed = document.getElementById('wiz-reference-embed');
  embed.innerHTML = '';
  const vid = wizYoutubeId(e.target.value.trim());
  if (!vid) return;
  const iframe = document.createElement('iframe');
  iframe.src = `https://www.youtube-nocookie.com/embed/${vid}`;
  iframe.title = 'reference video';
  iframe.allow = 'accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture';
  iframe.allowFullscreen = true;
  embed.appendChild(iframe);
});

// Tap tempo — the same Clock used on the play page (clock.js), just for
// its tapDetermine() median-of-intervals logic; nothing here plays audio
// or runs a clock loop, it only turns taps into a bpm number.
const wizTapClock = new Clock(120, 4);
document.getElementById('wiz-tap-btn')?.addEventListener('click', () => {
  const bpm = wizTapClock.tapDetermine();
  if (bpm) document.getElementById('wiz-tempo').value = bpm;
});

document.getElementById('open-wizard-btn')?.addEventListener('click', () => {
  ['wiz-title', 'wiz-artist', 'wiz-reference-url', 'wiz-key', 'wiz-tempo', 'wiz-lyrics-url', 'wiz-chords-url', 'wiz-lyrics-preview', 'wiz-chords-preview'].forEach(id => {
    document.getElementById(id).value = '';
  });
  document.getElementById('wiz-reference-embed').innerHTML = '';
  document.getElementById('wiz-lyrics-status').textContent = '';
  document.getElementById('wiz-chords-status').textContent = '';
  document.getElementById('wiz-status').textContent = '';
  wizShowStep(0);
  wizard.showModal();
  document.getElementById('wiz-title').focus();
});
document.getElementById('wiz-cancel')?.addEventListener('click', () => wizard.close());

async function wizFetch(kind, endpoint) {
  const url = document.getElementById(`wiz-${kind}-url`).value.trim();
  const status = document.getElementById(`wiz-${kind}-status`);
  const preview = document.getElementById(`wiz-${kind}-preview`);
  if (!url) { status.textContent = 'paste a URL first'; status.className = 'hint err'; return; }
  status.textContent = 'fetching…'; status.className = 'hint';
  const res = await fetch(endpoint, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }),
  });
  const data = await res.json();
  if (!res.ok) { status.textContent = data.error || 'fetch failed — paste the text instead'; status.className = 'hint err'; return; }
  preview.value = data.text;
  status.textContent = data.source ? `fetched from ${data.source} — review below before creating` : 'fetched — review below before creating';
  status.className = 'hint ok';
}
document.getElementById('wiz-lyrics-fetch')?.addEventListener('click', () => wizFetch('lyrics', '/api/import/lyrics'));
document.getElementById('wiz-chords-fetch')?.addEventListener('click', () => wizFetch('chords', '/api/import/chords'));

document.getElementById('wiz-create')?.addEventListener('click', async () => {
  const title = document.getElementById('wiz-title').value.trim();
  const artist = document.getElementById('wiz-artist').value.trim();
  const status = document.getElementById('wiz-status');
  if (!title) { document.getElementById('wiz-title').focus(); return; }
  status.textContent = 'creating…'; status.className = 'hint';
  const res = await fetch(`/api/books/${wizBook}/songs`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title, artist }),
  });
  const data = await res.json();
  if (!res.ok) { status.textContent = 'could not create: ' + data.error; status.className = 'hint err'; return; }
  const { stem } = data;

  const key = document.getElementById('wiz-key').value.trim();
  const tempo = document.getElementById('wiz-tempo').value.trim();
  const reference = document.getElementById('wiz-reference-url').value.trim();
  const chords = document.getElementById('wiz-chords-preview').value.trim();
  const lyrics = document.getElementById('wiz-lyrics-preview').value.trim();
  if (key || tempo || reference || chords || lyrics) {
    let body = `title: ${title}\n`;
    if (artist) body += `artist: ${artist}\n`;
    if (key) body += `key: ${key}\n`;
    if (tempo) body += `tempo: ${tempo}\n`;
    if (reference) body += `reference: ${reference}\n`;
    body += '\n';
    // Chords already carry their own inline lyrics ([Chord]word), so they
    // become the song body; a separately fetched plain-lyrics draft is kept
    // alongside rather than merged into it — reconciling two independent
    // sources for the same song is exactly the kind of guess this app
    // avoids making automatically, so it's left for the editor.
    if (chords) {
      body += `[Verse]\n${chords}\n`;
      if (lyrics) body += `\n[Lyrics draft]\n${lyrics}\n`;
    } else {
      body += `[Verse]\n${lyrics}\n`;
    }
    await fetch(`/api/songs/${wizBook}/${stem}/raw`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ version: null, text: body }),
    });
  }
  window.location.href = `/b/${wizBook}/${stem}`;
});

document.getElementById('remove-project-btn')?.addEventListener('click', async () => {
  const book = document.getElementById('remove-project-btn').dataset.book;
  if (!confirm(`Delete the entire "${book}" project? This removes all its songs and cannot be undone.`)) return;
  const res = await fetch(`/api/books/${book}`, { method: 'DELETE' });
  if (!res.ok) { alert('Could not delete project'); return; }
  window.location.href = '/';
});

document.querySelectorAll('.remove-song-btn').forEach(btn => {
  btn.addEventListener('click', async () => {
    const { book, stem } = btn.dataset;
    if (!confirm(`Delete "${stem}"? This cannot be undone.`)) return;
    const res = await fetch(`/api/books/${book}/songs/${stem}`, { method: 'DELETE' });
    if (!res.ok) { alert('Could not delete song'); return; }
    window.location.reload();
  });
});

// ---- Catalog: read-only by default, Edit order reveals drag-to-reorder
// and the per-row trash icon. Dragging is scoped to within one group's own
// list (SET 1, SET 2, ...) — moving a song between groups isn't supported
// yet, a deliberate scope cut rather than an oversight.
(function initCatalogEdit() {
  const section = document.getElementById('catalog-section');
  const toggle = document.getElementById('catalog-edit-toggle');
  if (!section || !toggle) return;
  const status = document.getElementById('catalog-save-status');
  const book = section.dataset.book;

  toggle.addEventListener('click', () => {
    const editing = section.classList.toggle('editing');
    toggle.classList.toggle('active', editing);
    toggle.textContent = '';
    toggle.append(editing ? 'Done' : 'Edit order');
  });

  function renumber(ul) {
    [...ul.children].forEach((li, i) => { li.querySelector('.song-num').textContent = (i + 1) + '.'; });
  }

  async function saveOrder() {
    const groups = [...document.querySelectorAll('.song-list')].map(ul => ({
      label: ul.dataset.groupLabel,
      stems: [...ul.children].map(li => li.dataset.stem),
    }));
    status.textContent = 'saving…';
    try {
      const res = await fetch(`/api/books/${book}/order`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groups }),
      });
      if (!res.ok) throw new Error(await res.text());
      status.textContent = 'saved';
      setTimeout(() => { status.textContent = ''; }, 1500);
    } catch (e) {
      status.textContent = 'could not save order';
    }
  }

  let dragEl = null;
  document.querySelectorAll('.song-list li').forEach(li => {
    li.addEventListener('dragstart', e => {
      if (!section.classList.contains('editing')) { e.preventDefault(); return; }
      dragEl = li;
      li.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
    });
    li.addEventListener('dragend', () => {
      li.classList.remove('dragging');
      document.querySelectorAll('.drag-over').forEach(el => el.classList.remove('drag-over'));
      dragEl = null;
    });
    li.addEventListener('dragover', e => {
      if (!dragEl || dragEl.parentElement !== li.parentElement || dragEl === li) return;
      e.preventDefault();
      li.classList.add('drag-over');
    });
    li.addEventListener('dragleave', () => li.classList.remove('drag-over'));
    li.addEventListener('drop', e => {
      if (!dragEl || dragEl.parentElement !== li.parentElement || dragEl === li) return;
      e.preventDefault();
      li.classList.remove('drag-over');
      const ul = li.parentElement;
      const before = [...ul.children].indexOf(dragEl) < [...ul.children].indexOf(li);
      ul.insertBefore(dragEl, before ? li.nextSibling : li);
      renumber(ul);
      saveOrder();
    });
    li.setAttribute('draggable', 'true');
  });
})();

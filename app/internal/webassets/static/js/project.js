// ---- New-song wizard --------------------------------------------------
// A 4-step flow — title+reference, key+tempo, lyrics, chords — each step
// writing into fields the song already has a reason to carry, not a new
// data model: everything here lands as ordinary .song header lines or
// grid/lyric text, the same shape a hand-edited song file already has.
const wizard = document.getElementById('song-wizard');
const wizSongbookId = wizard?.dataset.songbookId;
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
  document.getElementById('wiz-ireal-status').textContent = '';
  document.getElementById('wiz-ireal-candidates').innerHTML = '';
  document.getElementById('ug-preview-sections').innerHTML = '';
  ugLastURL = '';
  ugShowSubstep('ug-step-url');
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

// ---- Ultimate Guitar import sub-wizard: URL -> fetch & parse -> preview
// (pick sections) -> import, or skip straight to the raw textarea. The
// parsed sections are kept in memory only — "Use selected sections"
// re-sends the URL + chosen indices to /convert, which re-fetches and
// re-parses server-side rather than trusting this client-side copy (see
// handleImportUGConvert's doc comment).
let ugLastURL = '';
function ugShowSubstep(id) {
  document.querySelectorAll('.ug-import-substep').forEach(el => { el.hidden = el.id !== id; });
}
document.getElementById('ug-skip-to-raw')?.addEventListener('click', () => ugShowSubstep('ug-step-raw'));
document.getElementById('ug-raw-back')?.addEventListener('click', () => ugShowSubstep('ug-step-url'));
document.getElementById('ug-preview-back')?.addEventListener('click', () => ugShowSubstep('ug-step-url'));

document.getElementById('wiz-chords-fetch')?.addEventListener('click', async () => {
  const url = document.getElementById('wiz-chords-url').value.trim();
  const status = document.getElementById('wiz-chords-status');
  if (!url) { status.textContent = 'paste a URL first'; status.className = 'hint err'; return; }
  status.textContent = 'fetching & parsing…'; status.className = 'hint';
  const res = await fetch('/api/import/ultimate-guitar/parse', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }),
  });
  const data = await res.json();
  if (!res.ok) { status.textContent = data.error || 'fetch failed — try "skip" and paste the text instead'; status.className = 'hint err'; return; }
  status.textContent = '';
  ugLastURL = url;
  if (data.title && !document.getElementById('wiz-title').value.trim()) document.getElementById('wiz-title').value = data.title;
  if (data.artist && !document.getElementById('wiz-artist').value.trim()) document.getElementById('wiz-artist').value = data.artist;
  const titleEl = document.getElementById('ug-preview-title');
  titleEl.textContent = `${data.sections.length} section${data.sections.length === 1 ? '' : 's'} found — pick which ones to bring in (bar placement is a naive guess, fix it in the editor after).`;
  const list = document.getElementById('ug-preview-sections');
  list.innerHTML = '';
  data.sections.forEach((sec, i) => {
    const card = document.createElement('label');
    card.className = 'ug-preview-section';
    const lyricsPreview = sec.lyrics.split('\n').slice(0, 3).join('\n');

    const checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    checkbox.className = 'ug-section-checkbox';
    checkbox.dataset.index = i;
    checkbox.checked = true;

    const body = document.createElement('div');
    body.className = 'ug-preview-section-body';
    const label = document.createElement('div');
    label.className = 'ug-preview-section-label';
    label.textContent = sec.label || 'Verse';
    body.appendChild(label);
    if (sec.grid) {
      const grid = document.createElement('code');
      grid.className = 'ug-preview-section-grid';
      grid.textContent = `grid: ${sec.grid}`;
      body.appendChild(grid);
    }
    const lyrics = document.createElement('pre');
    lyrics.className = 'ug-preview-section-lyrics';
    lyrics.textContent = lyricsPreview;
    body.appendChild(lyrics);

    card.appendChild(checkbox);
    card.appendChild(body);
    list.appendChild(card);
  });
  ugShowSubstep('ug-step-preview');
});

document.getElementById('ug-preview-use')?.addEventListener('click', async () => {
  const selected = [...document.querySelectorAll('.ug-section-checkbox:checked')].map(cb => parseInt(cb.dataset.index, 10));
  const status = document.getElementById('wiz-chords-status');
  if (selected.length === 0) { ugShowSubstep('ug-step-raw'); return; }
  const res = await fetch('/api/import/ultimate-guitar/convert', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: ugLastURL, selected }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { status.textContent = data.error || 'import failed'; status.className = 'hint err'; ugShowSubstep('ug-step-url'); return; }
  document.getElementById('wiz-chords-preview').value = data.text;
  ugShowSubstep('ug-step-raw');
});

// iReal Pro bar-accurate chord reference, layered onto whatever's
// already in wiz-chords-preview (an enrichment, never a separate
// fetch-and-replace flow — same backend/semantics as the editor's own
// Import tab; see internal/irealpro/merge.go's doc comment for why a
// transposed match is never auto-applied).
function wizEscapeHtml(s) {
  return s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
document.getElementById('wiz-ireal-check-btn')?.addEventListener('click', async () => {
  const title = document.getElementById('wiz-title').value.trim();
  const artist = document.getElementById('wiz-artist').value.trim();
  const status = document.getElementById('wiz-ireal-status');
  const host = document.getElementById('wiz-ireal-candidates');
  const btn = document.getElementById('wiz-ireal-check-btn');
  const currentText = document.getElementById('wiz-chords-preview').value.trim();
  if (!title) { status.textContent = 'set a title first'; status.className = 'hint err'; return; }
  if (!currentText) { status.textContent = 'nothing to check yet — import or paste a draft first'; status.className = 'hint err'; return; }
  btn.disabled = true;
  status.textContent = 'checking iReal Pro…'; status.className = 'hint';
  host.innerHTML = '';
  try {
    const res = await fetch('/api/import/ireal/merge', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ currentText, title, artist: artist || '' }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'no iReal chart found for this song');
    wizRenderIRealCandidates(data.candidates || []);
    status.textContent = (data.candidates || []).length
      ? 'found a chart — review each section below before accepting.'
      : 'found a chart, but nothing new to propose.';
    status.className = 'hint';
  } catch (e) {
    status.textContent = e.message; status.className = 'hint err';
  } finally {
    btn.disabled = false;
  }
});

function wizRenderIRealCandidates(candidates) {
  const host = document.getElementById('wiz-ireal-candidates');
  host.innerHTML = '';
  if (!candidates.length) return;
  candidates.forEach(c => {
    const row = document.createElement('div');
    row.className = 'import-ireal-candidate' + (c.Safe ? ' is-safe' : '');
    const flag = c.Safe
      ? 'same key — safe to accept'
      : (c.Offset ? `transposed ${c.Offset > 0 ? '+' : ''}${c.Offset} semitones — please check before accepting` : 'low-confidence match — please check');
    row.innerHTML = `
      <div class="import-ireal-candidate-head">
        <span class="import-ireal-candidate-label">[${c.Label}]</span>
        <span class="import-ireal-candidate-flag${c.Safe ? ' is-safe' : ''}">${flag}</span>
      </div>
      <div class="import-ireal-candidate-grids">
        <div><span class="label">current</span>${c.CurrentGrid ? wizEscapeHtml(c.CurrentGrid) : '(none)'}</div>
        <div><span class="label">iReal Pro</span>${wizEscapeHtml(c.ProposedGrid)}</div>
      </div>
      <div class="import-ireal-candidate-actions">
        <label><input type="checkbox" class="wiz-ireal-accept" ${c.Safe ? 'checked' : ''}> accept this section</label>
      </div>`;
    row.querySelector('.wiz-ireal-accept').dataset.label = c.Label;
    host.appendChild(row);
  });
  const applyRow = document.createElement('div');
  applyRow.className = 'import-row';
  const applyBtn = document.createElement('button');
  applyBtn.type = 'button';
  applyBtn.textContent = 'Apply accepted sections to draft';
  const applyStatus = document.createElement('span');
  applyStatus.className = 'hint';
  applyRow.append(applyBtn, applyStatus);
  host.appendChild(applyRow);
  applyBtn.addEventListener('click', async () => {
    const title = document.getElementById('wiz-title').value.trim();
    const artist = document.getElementById('wiz-artist').value.trim();
    const acceptLabels = Array.from(host.querySelectorAll('.wiz-ireal-accept:checked')).map(el => el.dataset.label);
    if (!acceptLabels.length) { applyStatus.textContent = 'tick at least one section first'; applyStatus.className = 'hint err'; return; }
    applyBtn.disabled = true;
    applyStatus.textContent = 'applying…'; applyStatus.className = 'hint';
    try {
      const preview = document.getElementById('wiz-chords-preview');
      const res = await fetch('/api/import/ireal/merge', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ currentText: preview.value.trim(), title, artist: artist || '', apply: true, acceptLabels }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'apply failed');
      preview.value = data.text;
      host.innerHTML = '';
      document.getElementById('wiz-ireal-status').textContent = 'applied — accepted sections now use the iReal Pro grid.';
      document.getElementById('wiz-ireal-status').className = 'hint';
    } catch (e) {
      applyStatus.textContent = e.message; applyStatus.className = 'hint err';
    } finally {
      applyBtn.disabled = false;
    }
  });
}

document.getElementById('wiz-create')?.addEventListener('click', async () => {
  const title = document.getElementById('wiz-title').value.trim();
  const artist = document.getElementById('wiz-artist').value.trim();
  const status = document.getElementById('wiz-status');
  if (!title) { document.getElementById('wiz-title').focus(); return; }
  status.textContent = 'creating…'; status.className = 'hint';
  const res = await fetch(`/api/songbooks/${wizSongbookId}/songs`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title, artist }),
  });
  const data = await res.json();
  if (!res.ok) { status.textContent = 'could not create: ' + data.error; status.className = 'hint err'; return; }
  const variantId = data.variantId;

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
    await fetch(`/api/songs/${variantId}/raw`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ version: null, text: body }),
    });
  }
  window.location.href = `/song/${variantId}?songbook=${wizSongbookId}`;
});

document.querySelectorAll('.remove-song-btn').forEach(btn => {
  btn.addEventListener('click', async () => {
    const { songbookId, variantId } = btn.dataset;
    if (!confirm('Remove this song from the songbook?')) return;
    const res = await fetch(`/api/songbooks/${songbookId}/entries/${variantId}`, { method: 'DELETE' });
    if (!res.ok) { alert('Could not remove song'); return; }
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
  const songbookId = section.dataset.songbookId;

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
      variantIds: [...ul.children].map(li => parseInt(li.dataset.variantId, 10)),
    }));
    status.textContent = 'saving…';
    try {
      const res = await fetch(`/api/songbooks/${songbookId}/order`, {
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

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
// Next and Create are the form's submit buttons, so Enter in a field
// moves on too. Enter in one of the address fields fetches that page
// instead, which is what someone who just pasted a link means.
function wizCheckTitle() {
  const titleInput = document.getElementById('wiz-title');
  if (titleInput.value.trim()) return true;
  wizShowStep(0);
  Forms.fieldError(titleInput, 'Enter the song title.');
  titleInput.focus();
  return false;
}
document.getElementById('wiz-form')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const active = document.activeElement;
  if (active && active.id === 'wiz-lyrics-url') { document.getElementById('wiz-lyrics-fetch').click(); return; }
  if (active && active.id === 'wiz-chords-url') { document.getElementById('wiz-chords-fetch').click(); return; }
  // Only steps 1 and 2 hold fields the new song is made from (the
  // address fields later on are checked by their own Fetch buttons).
  // The step dots let someone skip ahead, so creating checks both.
  const checkSteps = wizStep < WIZ_STEPS - 1 ? [wizStep].filter((i) => i <= 1) : [0, 1];
  for (const i of checkSteps) {
    const panel = document.querySelector(`.wizard-panel[data-step="${i}"]`);
    // validity.valid, not checkValidity(): no events, nothing shown yet.
    if ([...panel.querySelectorAll('input, select, textarea')].some((el) => !el.validity.valid)) {
      wizShowStep(i);
      Forms.validate(panel);
      return;
    }
  }
  if (!wizCheckTitle()) return;
  // Leaving the title step, or creating: someone who typed fast and
  // pressed Enter still sees "Already in the library" once (shown on
  // whichever step holds the list).
  if ((wizStep === 0 || wizStep === WIZ_STEPS - 1) && wizLibrary && !(await wizLibrary.guard())) {
    const panel = wizard.querySelector('.library-match')?.closest('.wizard-panel');
    if (panel && panel.hidden) wizShowStep(parseInt(panel.dataset.step, 10));
    return;
  }
  if (wizStep < WIZ_STEPS - 1) wizShowStep(wizStep + 1);
  else wizCreate();
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
  if (bpm) { const t = document.getElementById('wiz-tempo'); t.value = bpm; Forms.fieldError(t, ''); }
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
  wizLibrary?.reset();
  wizard.querySelectorAll('[aria-invalid]').forEach((el) => Forms.fieldError(el, ''));
  ugLastURL = '';
  ugShowSubstep('ug-step-url');
  wizShowStep(0);
  wizard.showModal();
  document.getElementById('wiz-title').focus();
});
document.getElementById('wiz-cancel')?.addEventListener('click', () => wizard.close());

// ---- Step 1's "Already in the library" list (library-match.js): as the
// title is typed, songs already in the library with that title, each
// with one button that adds it to this songbook and lands on it, which
// short-circuits the rest of the wizard rather than letting the later
// steps collect data for a song that's about to be thrown away.
const wizLibrary = wizard && window.LibraryMatch ? LibraryMatch.attach({
  title: document.getElementById('wiz-title'),
  artist: document.getElementById('wiz-artist'),
  host: document.getElementById('wiz-library-match'),
  addLabel: 'Add to this songbook',
  add: async (hit) => {
    const res = await fetch(`/api/songbooks/${wizSongbookId}/entries`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ variantId: hit.variantId }),
    }).catch(() => null);
    if (!res || !res.ok) throw new Error(`Couldn't add “${hit.title}” to this songbook. Try again.`);
    wizard.close();
    location.href = `/song/${hit.variantId}?songbook=${wizSongbookId}`;
  },
  isInList: (id) => !!document.querySelector(`#catalog-section li[data-variant-id="${id}"]`),
  openHref: (hit) => `/song/${hit.variantId}?songbook=${wizSongbookId}`,
  // "None of these" on the first step moves on, same as Next.
  onNone: () => { if (wizStep === 0) document.getElementById('wiz-form').requestSubmit(document.getElementById('wiz-next')); },
}) : null;

async function wizFetch(kind, endpoint) {
  const url = document.getElementById(`wiz-${kind}-url`).value.trim();
  const status = document.getElementById(`wiz-${kind}-status`);
  const preview = document.getElementById(`wiz-${kind}-preview`);
  const input = document.getElementById(`wiz-${kind}-url`);
  status.textContent = ''; status.className = 'hint';
  if (!url) { Forms.fieldError(input, 'Paste the page address first.'); input.focus(); return; }
  if (!Forms.validate(input.parentElement)) return;
  const btn = document.getElementById(`wiz-${kind}-fetch`);
  Forms.busy(btn, true);
  status.textContent = 'Fetching…';
  try {
    const res = await fetch(endpoint, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) { status.textContent = "Couldn't get the lyrics from that page. Paste them into the box below instead."; status.className = 'hint err'; return; }
    preview.value = data.text;
    status.textContent = data.source ? `Fetched from ${data.source}. Check them below before creating the song.` : 'Fetched. Check them below before creating the song.';
    status.className = 'hint ok';
  } finally {
    Forms.busy(btn, false);
  }
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
  const input = document.getElementById('wiz-chords-url');
  const url = input.value.trim();
  const status = document.getElementById('wiz-chords-status');
  const btn = document.getElementById('wiz-chords-fetch');
  status.textContent = ''; status.className = 'hint';
  if (!url) { Forms.fieldError(input, 'Paste the Ultimate Guitar page address first.'); input.focus(); return; }
  if (!Forms.validate(input.parentElement)) return;
  status.textContent = 'Fetching the chart…';
  Forms.busy(btn, true);
  const res = await fetch('/api/import/ultimate-guitar/parse', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }),
  }).finally(() => Forms.busy(btn, false));
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { status.textContent = "Couldn't read a chart from that page. Check the address, or choose \u201cPaste text instead\u201d."; status.className = 'hint err'; return; }
  status.textContent = '';
  ugLastURL = url;
  const titleFromChart = data.title && !document.getElementById('wiz-title').value.trim();
  if (titleFromChart) document.getElementById('wiz-title').value = data.title;
  if (data.artist && !document.getElementById('wiz-artist').value.trim()) document.getElementById('wiz-artist').value = data.artist;
  // The chart named the song: check the library for it here, on this
  // step, since step 1's list never saw that title.
  if (titleFromChart) wizLibrary?.check({ host: document.getElementById('wiz-import-library-match') });
  const titleEl = document.getElementById('ug-preview-title');
  titleEl.textContent = `Found ${data.sections.length} section${data.sections.length === 1 ? '' : 's'}. Tick the ones to bring in. Where the bars fall is a rough guess; fix it in the editor afterwards.`;
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
  if (!res.ok) { status.textContent = "Couldn't bring those sections in. Try again, or paste the text instead."; status.className = 'hint err'; ugShowSubstep('ug-step-url'); return; }
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
  if (!title) { wizCheckTitle(); return; }
  if (!currentText) { status.textContent = 'Nothing to check yet. Import or paste the chords first.'; status.className = 'hint err'; return; }
  Forms.busy(btn, true);
  status.textContent = 'Checking iReal Pro…'; status.className = 'hint';
  host.innerHTML = '';
  try {
    const res = await fetch('/api/import/ireal/merge', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ currentText, title, artist: artist || '' }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(res.status === 404 ? 'No iReal Pro chart found for this song.' : "Couldn't check iReal Pro just now. Try again.");
    wizRenderIRealCandidates(data.candidates || []);
    status.textContent = (data.candidates || []).length
      ? 'Found a chart. Check each section below before accepting it.'
      : 'Found a chart, but it has nothing new to add.';
    status.className = 'hint';
  } catch (e) {
    status.textContent = e.message; status.className = 'hint err';
  } finally {
    Forms.busy(btn, false);
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
        <label><input type="checkbox" class="wiz-ireal-accept" ${c.Safe ? 'checked' : ''}> Accept this section</label>
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
    if (!acceptLabels.length) { applyStatus.textContent = 'Tick at least one section first.'; applyStatus.className = 'hint err'; return; }
    Forms.busy(applyBtn, true);
    applyStatus.textContent = ''; applyStatus.className = 'hint';
    try {
      const preview = document.getElementById('wiz-chords-preview');
      const res = await fetch('/api/import/ireal/merge', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ currentText: preview.value.trim(), title, artist: artist || '', apply: true, acceptLabels }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error("Couldn't apply those sections. Try again.");
      preview.value = data.text;
      host.innerHTML = '';
      document.getElementById('wiz-ireal-status').textContent = 'Done. The sections you accepted now use the iReal Pro chords.';
      document.getElementById('wiz-ireal-status').className = 'hint';
    } catch (e) {
      applyStatus.textContent = e.message; applyStatus.className = 'hint err';
    } finally {
      Forms.busy(applyBtn, false);
    }
  });
}

async function wizCreate() {
  const title = document.getElementById('wiz-title').value.trim();
  const artist = document.getElementById('wiz-artist').value.trim();
  const status = document.getElementById('wiz-status');
  const createBtn = document.getElementById('wiz-create');
  status.textContent = ''; status.className = 'hint';
  Forms.busy(createBtn, true);
  const res = await fetch(`/api/songbooks/${wizSongbookId}/songs`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title, artist }),
  }).catch(() => null);
  const data = res ? await res.json().catch(() => ({})) : {};
  if (!res || !res.ok) {
    Forms.busy(createBtn, false);
    status.textContent = "Couldn't create the song. Everything you entered is still here; try again.";
    status.className = 'hint err';
    return;
  }
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
}

document.querySelectorAll('.remove-song-btn').forEach(btn => {
  btn.addEventListener('click', async () => {
    const { songbookId, variantId } = btn.dataset;
    const title = btn.closest('li')?.querySelector('.song-title')?.textContent.trim();
    const name = title ? `“${title}”` : 'this song';
    if (!confirm(`Remove ${name} from this songbook? The song itself isn't deleted.`)) return;
    const res = await fetch(`/api/songbooks/${songbookId}/entries/${variantId}`, { method: 'DELETE' });
    if (!res.ok) {
      const status = document.getElementById('catalog-save-status');
      status.textContent = `Couldn't remove ${name}. Try again.`; status.className = 'hint err';
      return;
    }
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
    status.textContent = 'Saving…'; status.className = 'hint';
    try {
      const res = await fetch(`/api/songbooks/${songbookId}/order`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groups }),
      });
      if (!res.ok) throw new Error(await res.text());
      status.textContent = 'Order saved.';
      setTimeout(() => { status.textContent = ''; }, 1500);
    } catch (e) {
      status.textContent = "Couldn't save the new order. Reload the page and try again."; status.className = 'hint err';
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

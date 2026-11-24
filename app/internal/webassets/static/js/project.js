// The songbook page: its songs' order, a row's key, removing a row. Adding
// and creating songs is the one Add / Create dialog's (add-song.js).

document.querySelectorAll('.remove-song-btn').forEach(btn => {
  btn.addEventListener('click', async () => {
    const { songbookId, variantId } = btn.dataset;
    const title = btn.closest('li')?.querySelector('.song-title')?.textContent.trim();
    const name = title ? `“${title}”` : t('this song');
    if (!confirm(t('Remove %s from this songbook? The song itself isn’t deleted.', name))) return;
    const res = await fetch(`/api/songbooks/${songbookId}/entries/${variantId}`, { method: 'DELETE' });
    if (!res.ok) {
      const status = document.getElementById('catalog-save-status');
      status.textContent = t('Couldn’t remove %s. Try again.', name); status.className = 'hint err';
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
    toggle.append(editing ? t('Done') : t('Edit order'));
  });

  function renumber(ul) {
    [...ul.children].forEach((li, i) => { li.querySelector('.song-num').textContent = (i + 1) + '.'; });
  }

  async function saveOrder() {
    const groups = [...document.querySelectorAll('.song-list')].map(ul => ({
      label: ul.dataset.groupLabel,
      variantIds: [...ul.children].map(li => parseInt(li.dataset.variantId, 10)),
    }));
    status.textContent = t('Saving…'); status.className = 'hint';
    try {
      const res = await fetch(`/api/songbooks/${songbookId}/order`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groups }),
      });
      if (!res.ok) throw new Error(await res.text());
      status.textContent = t('Order saved.');
      setTimeout(() => { status.textContent = ''; }, 1500);
    } catch (e) {
      status.textContent = t('Couldn’t save the new order. Reload the page and try again.'); status.className = 'hint err';
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

// ---- A row's key (songbook_keys.go): the song opens in it from this
// songbook, the song itself unchanged. Saved on change; the row's key
// name follows. ----
document.querySelectorAll('.row-key-select').forEach((sel) => {
  let saved = sel.value;
  sel.addEventListener('change', async () => {
    sel.disabled = true;
    const res = await fetch(`/api/songbooks/${sel.dataset.songbookId}/entries/${sel.dataset.variantId}/key`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ semitones: parseInt(sel.value, 10) }),
    }).catch(() => null);
    sel.disabled = false;
    if (!res || !res.ok) {
      sel.value = saved;
      alert(t('Couldn’t save the key.'));
      return;
    }
    saved = sel.value;
    const d = await res.json().catch(() => ({}));
    const meta = sel.closest('li')?.querySelector('.song-meta');
    if (meta && d.keyName) {
      const tempo = meta.textContent.split('·')[0].trim();
      meta.textContent = /bpm/.test(tempo) ? `${tempo} · ${d.keyName}` : d.keyName;
    }
  });
});

// The home page's "Add a song" (?new=1) opens the Add / Create dialog at
// once; the address drops the parameter, so a reload doesn't open it
// again.
if (new URLSearchParams(location.search).has('new')) {
  history.replaceState(null, '', location.pathname);
  document.getElementById('open-add-song')?.click();
}

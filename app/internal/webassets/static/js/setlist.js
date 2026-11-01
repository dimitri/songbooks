// Same in-house icon set as templates/icons.html — duplicated because these
// rows are built client-side, not server-rendered.
const ICONS = {
  handle: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 6h12M4 10h12M4 14h12"/></svg>',
  close: '<svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M5 5l10 10M15 5L5 15"/></svg>',
};

// Shared drag-to-reorder: a row is only draggable while the pointer is down
// on its handle (not from anywhere on the row — a note input or song link
// would otherwise fight with starting a drag). Mouse uses native HTML5
// drag-and-drop, which lets a row cross from one set's list into another's;
// touch gets its own Pointer Events implementation below, since native
// HTML5 DnD never fires from a touch gesture on a phone/tablet — exactly
// the device a setlist is most likely to be reordered on. Drag state is
// kept in a closure, not dataTransfer, so a drop can splice the dragged
// entry out of its OWN array and into the target's.
let dragSource = null;
function wireDragHandle(row, handle, items, index, onReorder) {
  row.draggable = false;
  handle.addEventListener('mousedown', () => { row.draggable = true; });
  row.addEventListener('dragend', () => { row.draggable = false; row.classList.remove('dragging'); dragSource = null; });
  row.addEventListener('dragstart', e => {
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', ''); // Firefox refuses to drag without this
    dragSource = { items, index };
    row.classList.add('dragging');
  });
  row.addEventListener('dragover', e => { e.preventDefault(); });
  row.addEventListener('drop', e => {
    e.preventDefault();
    if (!dragSource) return;
    const [moved] = dragSource.items.splice(dragSource.index, 1);
    const sameList = dragSource.items === items;
    const at = sameList && dragSource.index < index ? index - 1 : index;
    items.splice(at, 0, moved);
    dragSource = null;
    onReorder();
  });
  handle.addEventListener('pointerdown', e => wireTouchDrag(e, row, items, index, onReorder));
}

// Touch drag: reorders within the same list only (a song moving between
// sets by touch is a fallback away — drag it out via the handle, drop it,
// then use the song picker on the target set — not as fluid as mouse DnD,
// but reordering itself, the common case, works the same as everywhere
// else in the app).
let touchDrag = null;
function wireTouchDrag(e, row, items, index, onReorder) {
  if (e.pointerType === 'mouse') return; // mouse already has native HTML5 DnD
  e.preventDefault();
  const list = row.parentElement;
  touchDrag = { items, from: index, row, list, startY: e.clientY, hoverIndex: index };
  row.classList.add('dragging');
  row.style.position = 'relative';
  document.addEventListener('pointermove', onTouchDragMove);
  document.addEventListener('pointerup', onTouchDragEnd, { once: true });
  document.addEventListener('pointercancel', onTouchDragEnd, { once: true });
}
function onTouchDragMove(e) {
  if (!touchDrag) return;
  const dy = e.clientY - touchDrag.startY;
  touchDrag.row.style.transform = `translateY(${dy}px)`;
  touchDrag.row.style.zIndex = '10';
  const siblings = [...touchDrag.list.children].filter(el => el !== touchDrag.row);
  let dropIndex = siblings.length;
  for (let i = 0; i < siblings.length; i++) {
    const rect = siblings[i].getBoundingClientRect();
    if (e.clientY < rect.top + rect.height / 2) { dropIndex = i; break; }
  }
  touchDrag.hoverIndex = dropIndex;
}
function onTouchDragEnd() {
  document.removeEventListener('pointermove', onTouchDragMove);
  if (!touchDrag) return;
  const { items, from, row, hoverIndex, onReorder } = touchDrag;
  row.classList.remove('dragging');
  row.style.transform = ''; row.style.zIndex = ''; row.style.position = '';
  touchDrag = null;
  if (hoverIndex !== from) {
    const [moved] = items.splice(from, 1);
    items.splice(hoverIndex, 0, moved);
    onReorder();
  }
}

(function () {
  const root = document.getElementById('setlist-edit');
  const book = root.dataset.book;
  const stem = root.dataset.stem;
  const songsByBook = window.SONGS_BY_BOOK || {};
  const books = Object.keys(songsByBook).sort();

  let version = null;
  let meta = { title: stem, date: '', venue: '' };
  let sets = [{ label: 'Set 1', entries: [] }];

  function metaArray() {
    return Object.entries(meta).filter(([, v]) => v).map(([key, value]) => ({ key, value }));
  }
  function metaFromArray(arr) {
    const m = { title: '', date: '', venue: '' };
    (arr || []).forEach(f => { m[f.key] = f.value; });
    return m;
  }

  function renderMetaInputs() {
    document.getElementById('meta-title').value = meta.title || '';
    document.getElementById('meta-date').value = meta.date || '';
    document.getElementById('meta-venue').value = meta.venue || '';
  }
  ['title', 'date', 'venue'].forEach(k => {
    document.getElementById('meta-' + k).addEventListener('input', e => { meta[k] = e.target.value; });
  });

  function songPicker(onAdd) {
    const wrap = document.createElement('span');
    wrap.className = 'song-picker';
    const bookSel = document.createElement('select');
    books.forEach(b => bookSel.add(new Option(b, b)));
    const songSel = document.createElement('select');
    function fillSongs() {
      songSel.innerHTML = '';
      (songsByBook[bookSel.value] || []).forEach(s => songSel.add(new Option(s, s)));
    }
    bookSel.addEventListener('change', fillSongs);
    fillSongs();
    const addBtn = document.createElement('button');
    addBtn.type = 'button'; addBtn.textContent = '+ add song';
    addBtn.addEventListener('click', () => { if (songSel.value) onAdd(bookSel.value, songSel.value); });
    wrap.append(bookSel, songSel, addBtn);
    return wrap;
  }

  function render() {
    renderMetaInputs();
    const list = document.getElementById('sets-list');
    list.innerHTML = '';
    sets.forEach((set, si) => {
      const block = document.createElement('div');
      block.className = 'set-block';

      const header = document.createElement('div');
      header.className = 'set-header';
      const handle = document.createElement('span');
      handle.className = 'drag-handle'; handle.innerHTML = ICONS.handle; handle.title = 'drag to reorder';
      wireDragHandle(block, handle, sets, si, render);
      const label = document.createElement('input');
      label.type = 'text'; label.value = set.label;
      label.addEventListener('input', () => { set.label = label.value; });
      const delSet = document.createElement('button');
      delSet.type = 'button'; delSet.className = 'row-remove'; delSet.innerHTML = ICONS.close; delSet.title = 'remove this set';
      delSet.addEventListener('click', () => { sets.splice(si, 1); render(); });
      header.append(handle, label, delSet);
      block.appendChild(header);

      const entryList = document.createElement('div');
      entryList.className = 'chip-list';
      set.entries.forEach((entry, ei) => {
        const chip = document.createElement('div');
        chip.className = 'chip';
        const eHandle = document.createElement('span');
        eHandle.className = 'drag-handle'; eHandle.innerHTML = ICONS.handle; eHandle.title = 'drag to reorder, or move to another set';
        wireDragHandle(chip, eHandle, set.entries, ei, render);
        const title = document.createElement('a');
        title.textContent = entry.stem;
        title.className = 'chip-badge chip-song-link';
        title.href = `/b/${entry.book}/${entry.stem}?setlist=${book}/${stem}`;
        title.title = 'open this song (with setlist prev/next nav)';
        const bookTag = document.createElement('span');
        bookTag.className = 'chip-badge';
        bookTag.textContent = entry.book;
        const note = document.createElement('input');
        note.type = 'text'; note.placeholder = 'note (capo, tempo, reminder…)'; note.value = entry.note || '';
        note.addEventListener('input', () => { entry.note = note.value; });
        const del = document.createElement('button');
        del.type = 'button'; del.className = 'row-remove'; del.innerHTML = ICONS.close; del.title = 'remove from setlist';
        del.addEventListener('click', () => { set.entries.splice(ei, 1); render(); });
        chip.append(eHandle, title, bookTag, note, del);
        entryList.appendChild(chip);
      });
      block.appendChild(entryList);
      block.appendChild(songPicker((b, s) => { set.entries.push({ book: b, stem: s, note: '' }); render(); }));
      list.appendChild(block);
    });
  }

  document.getElementById('add-set').addEventListener('click', () => {
    sets.push({ label: 'Set ' + (sets.length + 1), entries: [] });
    render();
  });

  document.getElementById('save-setlist').addEventListener('click', async () => {
    const status = document.getElementById('setlist-status');
    status.textContent = 'saving…'; status.className = '';
    const res = await fetch(`/api/setlists/${book}/${stem}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version, setlist: { meta: metaArray(), sets } }),
    });
    if (res.status === 409) { status.textContent = 'conflict: reload before saving'; status.className = 'err'; return; }
    if (!res.ok) { status.textContent = 'save failed'; status.className = 'err'; return; }
    const data = await res.json();
    version = data.version;
    status.textContent = 'saved'; status.className = 'ok';
  });

  (async function load() {
    const res = await fetch(`/api/setlists/${book}/${stem}`);
    if (res.status === 404) { render(); return; } // new, unsaved setlist — start from the draft above
    const data = await res.json();
    version = data.version;
    meta = metaFromArray(data.setlist.meta);
    sets = data.setlist.sets && data.setlist.sets.length ? data.setlist.sets : [{ label: 'Set 1', entries: [] }];
    render();
  })();
})();

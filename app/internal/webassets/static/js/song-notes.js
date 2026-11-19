// Song notes: per-section notes on a song, private (only their author
// sees them) or shared (everyone who can see the song), independent of
// edit rights — see httpapi/section_annotations.go.
//
// One sheet (#notes-sheet, play.html) for every way in: the desktop
// sidebar's Notes row, the phone Song sheet's Notes row, the phone's
// floating note button (#notes-fab) and the player's Note option. It
// opens on the section being played, or else the one at the top of the
// screen; its chips switch section in one tap. On a phone it's a bottom
// sheet kept above the on-screen keyboard (visualViewport); on a wide
// screen a panel beside the chart (app.css "Song notes").
//
// The notes are also drawn in the chart, under their section's heading,
// so they're read while playing; tapping one opens it in the sheet (to
// edit, when it's yours).
(function () {
  const sheetEl = document.getElementById('notes-sheet');
  if (!sheetEl) return;
  const t = window.t || function (m) { const a = [].slice.call(arguments, 1); let i = 0; return m.replace(/%[ds]/g, () => String(a[i++])); };
  const $ = (id) => document.getElementById(id);
  const variantId = Number(sheetEl.dataset.variantId);
  const backdrop = $('notes-backdrop');
  const picker = $('notes-sec-picker');
  const list = $('notes-sheet-list');
  const empty = $('notes-empty');
  const form = $('notes-composer');
  const box = $('notes-text');
  const label = $('notes-text-label');
  const saveBtn = $('notes-save');
  const cancelEditBtn = $('notes-cancel-edit');
  const visHint = $('notes-vis-hint');
  const fab = $('notes-fab');
  const chips = [...picker.querySelectorAll('.notes-sec-chip')];
  const phoneMQ = window.matchMedia('(max-width: 900px)');

  let bySection = {};        // section index -> notes, oldest first
  let current = chips.length ? Number(chips[0].dataset.sectionIndex) : 0;
  let editing = null;        // the note being edited, or null for a new one
  let opener = null;         // focus goes back there on close

  function icon(name) {
    const tpl = $('notes-icon-' + name);
    return tpl ? tpl.content.firstElementChild.cloneNode(true) : document.createTextNode('');
  }
  function sectionName(i) {
    const chip = chips.find((c) => Number(c.dataset.sectionIndex) === i);
    return chip ? chip.querySelector('.notes-sec-name').textContent : '';
  }
  function findNote(id) {
    for (const k in bySection) {
      const n = bySection[k].find((x) => x.ID === id);
      if (n) return n;
    }
    return null;
  }

  // ---- Which section --------------------------------------------------------
  // While the recording plays, the section playing (song.js marks it
  // .current-section, and keeps the first one marked when stopped, so
  // only then); else the first one reaching below the top fifth of the
  // screen — the one being read.
  function sectionInView() {
    const secs = [...document.querySelectorAll('#sheet .sheet-section[data-section-index]')]
      .filter((el) => el.offsetParent !== null);
    const vh = window.innerHeight;
    const isPlaying = $('audio-play-btn')?.dataset.state === 'true';
    const playing = isPlaying && secs.find((el) => el.classList.contains('current-section'));
    if (playing) {
      const r = playing.getBoundingClientRect();
      if (r.bottom > 0 && r.top < vh) return Number(playing.dataset.sectionIndex);
    }
    for (const el of secs) {
      if (el.getBoundingClientRect().bottom > vh * 0.2) return Number(el.dataset.sectionIndex);
    }
    return current;
  }

  // ---- Loading and drawing ----------------------------------------------------
  async function load() {
    if (!variantId) return;
    try {
      const res = await fetch(`/api/variants/${variantId}/annotations`, { headers: { Accept: 'application/json' } });
      if (!res.ok) return;
      bySection = (await res.json()) || {};
    } catch (e) { return; }
    drawChart();
    drawSheet();
  }

  function badge(n) {
    const b = document.createElement('span');
    b.className = 'note-badge';
    b.appendChild(icon(n.Visibility === 'shared' ? 'shared' : 'lock'));
    const who = n.Mine ? '' : (n.PersonName || t('someone'));
    b.appendChild(document.createTextNode(n.Visibility === 'shared'
      ? (who ? t('notes|Shared by %s', who) : t('notes|Shared'))
      : t('Only me')));
    return b;
  }

  // Under each section's heading in the chart.
  function drawChart() {
    document.querySelectorAll('#sheet .user-notes').forEach((el) => el.remove());
    document.querySelectorAll('#sheet .sheet-section[data-section-index]').forEach((sec) => {
      const notes = bySection[sec.dataset.sectionIndex] || [];
      if (!notes.length) return;
      const wrap = document.createElement('div');
      wrap.className = 'user-notes';
      notes.forEach((n) => {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'user-note ' + (n.Visibility === 'shared' ? 'is-shared' : 'is-private');
        b.dataset.noteId = n.ID;
        b.dataset.sectionIndex = n.SectionIndex;
        b.title = n.Mine ? t('edit this note') : t('open the notes on this section');
        b.appendChild(icon(n.Visibility === 'shared' ? 'shared' : 'lock'));
        const body = document.createElement('span');
        body.className = 'user-note-body';
        body.textContent = n.Body;
        b.appendChild(body);
        if (!n.Mine) {
          const who = document.createElement('span');
          who.className = 'user-note-who';
          who.textContent = n.PersonName || t('someone');
          b.appendChild(who);
        }
        wrap.appendChild(b);
      });
      const anchor = sec.querySelector(':scope > .section-notes') || sec.querySelector(':scope > h3');
      if (anchor) anchor.insertAdjacentElement('afterend', wrap); else sec.prepend(wrap);
    });
  }

  function drawSheet() {
    chips.forEach((c) => {
      const i = Number(c.dataset.sectionIndex);
      const on = i === current;
      c.setAttribute('aria-checked', String(on));
      c.tabIndex = on ? 0 : -1;
      const n = (bySection[i] || []).length;
      const count = c.querySelector('.notes-sec-count');
      count.hidden = !n;
      count.textContent = n ? String(n) : '';
    });
    list.replaceChildren();
    const notes = bySection[current] || [];
    notes.forEach((n) => {
      const li = document.createElement('li');
      li.className = 'note-item' + (n.Mine ? ' is-mine' : '') + (editing && editing.ID === n.ID ? ' is-editing' : '');
      const p = document.createElement('p');
      p.className = 'note-item-body';
      p.textContent = n.Body;
      li.appendChild(p);
      const meta = document.createElement('div');
      meta.className = 'note-item-meta';
      meta.appendChild(badge(n));
      if (n.Mine) {
        const edit = document.createElement('button');
        edit.type = 'button';
        edit.className = 'note-edit';
        edit.dataset.noteId = n.ID;
        edit.appendChild(icon('edit'));
        edit.appendChild(document.createTextNode(t('Edit')));
        const del = document.createElement('button');
        del.type = 'button';
        del.className = 'note-delete danger';
        del.dataset.noteId = n.ID;
        del.appendChild(icon('trash'));
        del.appendChild(document.createTextNode(t('Delete')));
        meta.append(edit, del);
      }
      li.appendChild(meta);
      list.appendChild(li);
    });
    empty.hidden = notes.length > 0;
    const name = sectionName(current);
    label.textContent = editing ? t('Your note on %s', name) : t('New note on %s', name);
    saveBtn.textContent = editing ? t('Save changes') : t('Save note');
    cancelEditBtn.hidden = !editing;
    syncVisHint();
  }

  function syncVisHint() {
    const v = form.querySelector('input[name="notes-vis"]:checked')?.value || 'private';
    visHint.textContent = visHint.dataset[v] || '';
  }

  // ---- Open and close ---------------------------------------------------------
  function select(i, focusChip) {
    current = i;
    drawSheet();
    const chip = chips.find((c) => Number(c.dataset.sectionIndex) === i);
    if (chip) {
      chip.scrollIntoView({ block: 'nearest', inline: 'center' });
      if (focusChip) chip.focus();
    }
  }

  function open(opts) {
    opts = opts || {};
    // The player's sheet (phone) gives way; its option panel too.
    if ($('audio-dock')?.classList.contains('is-open')) $('audio-dock-grab')?.click();
    $('audio-opts')?.classList.remove('open');
    opener = opts.opener || document.activeElement;
    sheetEl.hidden = false;
    backdrop.hidden = false;
    document.documentElement.classList.add('notes-open');
    fitKeyboard();
    if (opts.note) startEdit(opts.note, false);
    select(opts.section != null ? opts.section : sectionInView(), false);
    if (opts.write) box.focus();
    else (chips.find((c) => c.getAttribute('aria-checked') === 'true') || $('notes-sheet-close')).focus();
  }

  function close() {
    if (sheetEl.hidden) return;
    box.blur();
    sheetEl.hidden = true;
    backdrop.hidden = true;
    document.documentElement.classList.remove('notes-open');
    // An unsaved edit is dropped (the note stays as it was); a new
    // note's unsaved text stays for next time.
    if (editing) stopEdit();
    Forms.status(form, '');
    if (opener && document.contains(opener)) opener.focus({ preventScroll: true });
  }

  // ---- Keyboard: keep the sheet above it --------------------------------------
  // On a phone the on-screen keyboard covers the bottom of the layout
  // viewport without resizing it (iOS; Android in some browsers): the
  // visual viewport is what's left, so the sheet's bottom and height
  // follow it.
  function fitKeyboard() {
    const vv = window.visualViewport;
    if (!vv || sheetEl.hidden) return;
    const covered = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
    sheetEl.style.setProperty('--notes-kb', covered + 'px');
    sheetEl.style.setProperty('--notes-vh', vv.height + 'px');
    sheetEl.classList.toggle('kb-open', covered > 80);
  }
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', fitKeyboard);
    window.visualViewport.addEventListener('scroll', fitKeyboard);
  }
  box.addEventListener('focus', () => {
    // Once the keyboard is up, keep the text box itself in view.
    setTimeout(() => { fitKeyboard(); box.scrollIntoView({ block: 'nearest' }); }, 300);
  });

  // ---- Write, edit, delete ----------------------------------------------------
  function setVisibility(v) {
    const r = form.querySelector(`input[name="notes-vis"][value="${v}"]`);
    if (r) r.checked = true;
    syncVisHint();
  }
  function startEdit(n, focus) {
    editing = n;
    box.value = n.Body;
    setVisibility(n.Visibility);
    Forms.fieldError(box, '');
    Forms.status(form, '');
    if (n.SectionIndex !== current) select(n.SectionIndex, false); else drawSheet();
    if (focus) { box.focus(); box.setSelectionRange(box.value.length, box.value.length); }
  }
  function stopEdit() {
    editing = null;
    box.value = '';
    // A new note starts private, whatever the last one was: sharing is
    // a choice made each time.
    setVisibility('private');
    Forms.fieldError(box, '');
    drawSheet();
  }
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!variantId) return;
    const text = box.value.trim();
    if (!text) { Forms.fieldError(box, t('Write the note first.')); box.focus(); return; }
    const visibility = form.querySelector('input[name="notes-vis"]:checked').value;
    const wasEditing = editing;
    await Forms.run(form, saveBtn, async () => {
      const res = wasEditing
        ? await fetch(`/api/variants/${variantId}/annotations/${wasEditing.ID}`, {
          method: 'PATCH', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: text, visibility, section_index: current }),
        })
        : await fetch(`/api/variants/${variantId}/sections/${current}/annotations`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: text, visibility }),
        });
      // The API's error strings are developer-facing ("missing body").
      if (res.status === 401) throw new Error(t('Sign in again to add notes.'));
      if (!res.ok) throw new Error(t("Couldn't save that note. Your text is still here; try again."));
      editing = null;
      box.value = '';
      setVisibility('private');
      // The keyboard goes away so the saved note shows in the list.
      if (phoneMQ.matches) box.blur();
      await load();
      Forms.status(form, wasEditing ? t('Note saved.') : t('Note added.'), 'ok');
    });
  });

  list.addEventListener('click', async (e) => {
    const editBtn = e.target.closest('.note-edit');
    if (editBtn) { const n = findNote(Number(editBtn.dataset.noteId)); if (n) startEdit(n, true); return; }
    const delBtn = e.target.closest('.note-delete');
    if (!delBtn) return;
    const n = findNote(Number(delBtn.dataset.noteId));
    if (!n || !window.confirm(t('Delete this note? This can’t be undone.'))) return;
    await Forms.run(form, delBtn, async () => {
      const res = await fetch(`/api/variants/${variantId}/annotations/${n.ID}`, { method: 'DELETE' });
      if (res.status === 401) throw new Error(t('Sign in again to add notes.'));
      if (!res.ok && res.status !== 404) throw new Error(t("Couldn't delete that note. Try again."));
      if (editing && editing.ID === n.ID) stopEdit();
      await load();
      Forms.status(form, t('Note deleted.'), 'ok');
    });
  });

  cancelEditBtn.addEventListener('click', () => { stopEdit(); Forms.status(form, ''); });

  // Section chips: a tap picks the section (and moves the note being
  // edited there); arrow keys move along them, as a radio group does.
  picker.addEventListener('click', (e) => {
    const c = e.target.closest('.notes-sec-chip');
    if (!c) return;
    Forms.status(form, '');
    select(Number(c.dataset.sectionIndex), false);
  });
  picker.addEventListener('keydown', (e) => {
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    const at = chips.findIndex((c) => Number(c.dataset.sectionIndex) === current);
    let next = at + (e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 1);
    if (e.key === 'Home') next = 0;
    if (e.key === 'End') next = chips.length - 1;
    next = Math.max(0, Math.min(chips.length - 1, next));
    select(Number(chips[next].dataset.sectionIndex), true);
  });

  form.addEventListener('change', (e) => { if (e.target.name === 'notes-vis') syncVisHint(); });

  // ---- Ways in ----------------------------------------------------------------
  $('notes-toggle')?.addEventListener('click', (e) => open({ opener: e.currentTarget, write: !phoneMQ.matches }));
  $('notes-stack-row')?.addEventListener('click', () => open({ opener: $('song-stack-btn'), write: false }));
  fab?.addEventListener('click', () => open({ opener: fab, write: true }));
  $('audio-note-btn')?.addEventListener('click', (e) => open({ opener: e.currentTarget, write: true }));
  document.getElementById('sheet')?.addEventListener('click', (e) => {
    const b = e.target.closest('.user-note');
    if (!b) return;
    const n = findNote(Number(b.dataset.noteId));
    if (!n) return;
    open({ opener: b, section: n.SectionIndex, note: n.Mine ? n : null, write: false });
  });
  $('notes-sheet-close').addEventListener('click', close);
  backdrop.addEventListener('click', close);
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !sheetEl.hidden) { e.stopPropagation(); close(); }
  });
  // Tab stays in the sheet while it's open.
  sheetEl.addEventListener('keydown', (e) => {
    if (e.key !== 'Tab') return;
    const f = [...sheetEl.querySelectorAll('button, textarea, input, [tabindex="0"]')]
      .filter((el) => !el.disabled && el.offsetParent !== null && el.tabIndex >= 0);
    if (!f.length) return;
    const first = f[0], last = f[f.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  });

  drawSheet();
  load();
})();

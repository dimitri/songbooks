// A band's Settings (band_settings.html): The band section — one form,
// one Save, enabled once something changed; what changed is sent (its
// name, what it plays, its language, its tagline and website), nothing
// reloads: the page's header and the section's row say the new values.
// And Archive or delete: archiving asks in a dialog that says what it
// does; deleting asks for the band's name, typed.
(function () {
  const t = window.t || ((m) => m);

  // ---- The band ----
  const form = document.getElementById('band-form');
  if (form) {
    const bandId = form.dataset.bandId;
    const save = document.getElementById('band-save');
    const state = form.querySelector('[data-changes]');
    const field = (name) => form.elements.namedItem(name);
    // What each field holds now, those the viewer may change only.
    const values = () => {
      const v = {};
      for (const name of ['name', 'musicType', 'tagline', 'website', 'locale']) {
        const el = field(name);
        if (!el) continue;
        v[name] = (el instanceof RadioNodeList || el.type === 'radio')
          ? (form.querySelector(`input[name="${name}"]:checked`)?.value ?? '')
          : el.value.trim();
      }
      return v;
    };
    let saved = values();
    const changed = () => {
      const now = values();
      return Object.keys(now).filter((k) => now[k] !== saved[k]);
    };
    function mark() {
      const n = changed().length;
      save.disabled = n === 0;
      state.textContent = n ? tn(n, '%d change', '%d changes') : t('No changes');
    }
    // The preview beside the form: the header as it will show.
    function preview() {
      const v = values();
      const name = document.querySelector('[data-preview="name"]');
      const line = document.querySelector('[data-preview="tagline"]');
      if (name && v.name !== undefined) name.textContent = v.name || saved.name;
      if (line && v.tagline !== undefined) line.textContent = v.tagline || form.dataset.kind;
    }
    form.addEventListener('input', () => { mark(); preview(); });
    form.addEventListener('change', mark);

    const send = async (method, path, body, fallback) => {
      const res = await fetch(`/api/bands/${bandId}/${path}`, {
        method, headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
      }).catch(() => null);
      if (res && res.ok) return;
      const data = res ? await res.json().catch(() => ({})) : {};
      if (res && res.status === 400 && /https/.test(data.error || '')) {
        Forms.fieldError(field('website'), t('Enter an address that starts with https://, or leave it empty.'));
      }
      throw new Error(fallback);
    };
    // The page around the form says the new values: the band's name in
    // its header and the tab's title, its tagline under it, the row's
    // state in the list of sections.
    function showSaved(v) {
      const old = saved.name;
      if (v.name !== undefined && v.name !== old) {
        // Wherever the page names the band: its header and back link,
        // the switcher, the sidebar's line.
        document.querySelectorAll('.app-header, .space-header, .space-switcher, .sidebar-space.active').forEach((box) => {
          const walk = document.createTreeWalker(box, NodeFilter.SHOW_TEXT);
          for (let n = walk.nextNode(); n; n = walk.nextNode()) {
            if (n.nodeValue.trim() === old) n.nodeValue = n.nodeValue.replace(old, v.name);
          }
        });
        if (document.title.includes(old)) document.title = document.title.replace(old, v.name);
      }
      if (v.tagline !== undefined && v.tagline !== saved.tagline) {
        const kind = document.querySelector('.space-header .space-kind');
        if (kind && kind.firstChild && kind.firstChild.nodeType === Node.TEXT_NODE) kind.firstChild.textContent = v.tagline || form.dataset.kind;
      }
      const row = document.querySelector('[data-section-link="band"] .admin-nav-state');
      if (row && (v.musicType !== undefined || v.locale !== undefined)) {
        const parts = row.textContent.split(' · ');
        const plays = form.querySelector('input[name="musicType"]:checked');
        if (plays) parts[0] = plays.value ? plays.parentElement.textContent.trim() : t('What it plays: not set');
        const lang = field('locale');
        if (lang) parts[1] = lang.value ? lang.selectedOptions[0].textContent.trim() : t('language automatic');
        row.textContent = parts.join(' · ');
      }
    }

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      const todo = changed();
      if (!todo.length) return;
      const v = values();
      Forms.run(form, save, async () => {
        if (todo.includes('name')) await send('POST', 'name', {name: v.name}, t('Couldn’t rename the band. Try again.'));
        if (todo.includes('musicType')) await send('POST', 'music-type', {musicType: v.musicType}, t('Couldn’t save what the band plays. Try again.'));
        if (todo.includes('locale')) await send('PUT', 'locale', {locale: v.locale}, t('Couldn’t save the language. Try again.'));
        if (todo.includes('tagline') || todo.includes('website')) {
          await send('POST', 'brand/words', {tagline: v.tagline ?? '', website: v.website ?? ''}, t('Couldn’t save the tagline and website. Try again.'));
        }
        showSaved(v);
        saved = v;
        mark();
        Forms.status(form, t('Saved.'), 'ok');
      }).finally(mark);
    });
    mark();
  }

  // ---- Archive or delete ----
  const archiveBtn = document.getElementById('archive-band-btn');
  const archiveDialog = document.getElementById('archive-dialog');
  const bandId = document.querySelector('[data-band-id]')?.dataset.bandId
    || location.pathname.split('/')[2];
  async function archive(archived, busyBtn, statusEl) {
    Forms.busy(busyBtn, true);
    const res = await fetch(`/api/bands/${bandId}/archive`, {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({archived}),
    }).catch(() => null);
    if (res && res.ok) { location.hash = 'danger'; location.reload(); return; }
    Forms.busy(busyBtn, false);
    Forms.status(statusEl, t('Couldn’t change that. Try again.'), 'error');
  }
  archiveBtn?.addEventListener('click', () => {
    if (archiveBtn.dataset.archived) { archive(false, archiveBtn, document.getElementById('archive-band-result')); return; }
    Forms.status(document.getElementById('archive-dialog-status'), '');
    archiveDialog.showModal();
  });
  document.getElementById('archive-dialog-go')?.addEventListener('click', (e) => {
    archive(true, e.currentTarget, document.getElementById('archive-dialog-status'));
  });
  archiveDialog?.addEventListener('close', () => archiveBtn.focus());

  const deleteBtn = document.getElementById('delete-band-btn');
  const deleteDialog = document.getElementById('delete-dialog');
  if (deleteBtn && deleteDialog) {
    const input = document.getElementById('delete-confirm');
    const go = document.getElementById('delete-dialog-go');
    const status = document.getElementById('delete-dialog-status');
    // The button stays off until the band's name is typed (spaces and
    // case aside).
    const norm = (s) => s.trim().replace(/\s+/g, ' ').toLocaleLowerCase();
    const typed = () => norm(input.value) === norm(input.dataset.name);
    input.addEventListener('input', () => { go.disabled = !typed(); });
    deleteBtn.addEventListener('click', () => {
      input.value = '';
      go.disabled = true;
      Forms.status(status, '');
      deleteDialog.showModal();
      input.focus();
    });
    deleteDialog.addEventListener('close', () => deleteBtn.focus());
    deleteDialog.querySelectorAll('button[value="cancel"]').forEach((b) => b.addEventListener('click', (e) => { e.preventDefault(); deleteDialog.close(); }));
    // Enter in the field deletes only once the name is right.
    document.getElementById('delete-form').addEventListener('submit', (e) => {
      if (e.submitter && e.submitter.value === 'cancel') return;
      e.preventDefault();
      if (typed()) go.click();
    });
    go.addEventListener('click', async () => {
      if (!typed()) return;
      Forms.busy(go, true);
      const res = await fetch(`/api/bands/${bandId}`, {method: 'DELETE'}).catch(() => null);
      if (res && res.ok) { location.href = '/bands'; return; }
      Forms.busy(go, false);
      Forms.status(status, t('Couldn’t delete %s. Try again.', input.dataset.name), 'error');
    });
    document.getElementById('delete-archive-instead')?.addEventListener('click', () => {
      deleteDialog.close();
      archiveBtn?.click();
    });
  }
})();

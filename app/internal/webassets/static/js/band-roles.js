// A band's roles (band_roles.html, the Roles section of its Settings;
// docs/band-roles.md): a tick (a switch on a phone) saves the role's
// whole privilege set at once, and the other view follows; on a phone
// one role shows at a time, by its chip; "+ New role" and a band's own
// role's Rename ask in a dialog, Delete on confirm. The API is the
// community's (httpapi/roles.go), under /api/bands/{id}.
(function () {
  const root = document.getElementById('band-roles');
  if (!root) return;
  const t = window.t || ((m) => m);
  const base = `/api/bands/${root.dataset.bandId}`;

  // A 4xx's own sentence is worth showing; anything else, the fallback.
  async function api(url, body, fallback, method = 'POST') {
    const opts = {method};
    if (body !== undefined) { opts.headers = {'Content-Type': 'application/json'}; opts.body = JSON.stringify(body); }
    let res;
    try { res = await fetch(url, opts); } catch { throw new Error(fallback); }
    const data = await res.json().catch(() => ({}));
    if (res.status === 409 && /manage roles/.test(data.error || '')) {
      throw new Error(t('Someone must keep the right to manage roles: give it to another member first.'));
    }
    if (!res.ok) throw new Error(fallback);
    return data;
  }
  const status = document.getElementById('band-roles-status');
  async function saveInline(fn) {
    Forms.status(status, t('Saving…'), '');
    try { await fn(); Forms.status(status, t('Saved.'), 'ok'); return true; }
    catch (e) { Forms.status(status, e.message, 'error'); return false; }
  }
  // Back on the Roles section after a reload.
  const reload = () => { location.hash = 'roles'; location.reload(); };

  // A tick: the role's privileges as its view shows them; the other
  // view (the matrix, the phone's switches) follows.
  const twins = (cb) => root.querySelectorAll(`.band-role-priv[data-role-id="${cb.dataset.roleId}"][data-privilege="${cb.dataset.privilege}"]`);
  root.querySelectorAll('.band-role-priv').forEach((cb) => {
    cb.addEventListener('change', async () => {
      const id = cb.dataset.roleId;
      const view = cb.closest('[data-roles-view]') || root;
      const privileges = [...view.querySelectorAll(`.band-role-priv[data-role-id="${id}"]:checked`)].map((c) => c.dataset.privilege);
      twins(cb).forEach((c) => { c.checked = cb.checked; });
      const ok = await saveInline(() => api(`${base}/roles/${id}/privileges`, {privileges}, t('Couldn’t save that change. Try again.')));
      if (!ok) twins(cb).forEach((c) => { c.checked = !cb.checked; });
    });
  });

  // The phone's one role at a time.
  const panels = [...root.querySelectorAll('.band-role-panel')];
  const chips = [...root.querySelectorAll('input[name="band-role-pick"]')];
  function showRole() {
    const id = chips.find((c) => c.checked)?.value;
    panels.forEach((p) => { p.hidden = p.dataset.roleId !== id; });
  }
  chips.forEach((c) => c.addEventListener('change', showRole));
  if (chips.length) { root.classList.add('is-picking'); showRole(); }

  // Dialogs: a new role, a role's new name.
  function dialog(id) {
    const d = document.getElementById(id);
    d.querySelectorAll('.band-dialog-cancel').forEach((b) => b.addEventListener('click', () => d.close()));
    return d;
  }
  const newDialog = dialog('band-role-new-dialog');
  const newForm = document.getElementById('band-role-new-form');
  root.querySelectorAll('.band-role-new-btn').forEach((b) => b.addEventListener('click', () => {
    newForm.reset();
    Forms.status(newForm, '');
    newDialog.showModal();
    document.getElementById('band-role-new-name').focus();
  }));
  newForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const name = document.getElementById('band-role-new-name').value.trim();
    if (!name) { Forms.fieldError(document.getElementById('band-role-new-name'), t('Enter a role name.')); return; }
    const opt = document.getElementById('band-role-new-from').selectedOptions[0];
    const privileges = opt && opt.dataset.privileges ? opt.dataset.privileges.split(' ') : [];
    Forms.run(newForm, document.getElementById('band-role-new-btn'), async () => {
      await api(`${base}/roles`, {name, privileges}, t('Couldn’t create that role. Try a different name.'));
      reload();
    });
  });

  const renameDialog = dialog('band-role-rename-dialog');
  const renameForm = document.getElementById('band-role-rename-form');
  const renameInput = document.getElementById('band-role-rename-name');
  root.querySelectorAll('.band-role-rename').forEach((b) => b.addEventListener('click', () => {
    b.closest('details')?.removeAttribute('open');
    renameForm.dataset.roleId = b.dataset.roleId;
    renameInput.value = b.dataset.roleName;
    Forms.status(renameForm, '');
    renameDialog.showModal();
    renameInput.select();
  }));
  renameForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const name = renameInput.value.trim();
    if (!name) { Forms.fieldError(renameInput, t('Enter a role name.')); return; }
    Forms.run(renameForm, document.getElementById('band-role-rename-btn'), async () => {
      await api(`${base}/roles/${renameForm.dataset.roleId}/rename`, {name}, t('Couldn’t rename that role: is the name already taken?'));
      reload();
    });
  });

  root.querySelectorAll('.band-role-delete').forEach((btn) => {
    btn.addEventListener('click', async () => {
      btn.closest('details')?.removeAttribute('open');
      if (!confirm(t('Delete the “%s” role? Anyone who has it must be given another role first.', btn.dataset.roleName))) return;
      Forms.busy(btn, true);
      const ok = await saveInline(() => api(`${base}/roles/${btn.dataset.roleId}`, undefined, t('Couldn’t delete that role. Give its members another role first, then try again.'), 'DELETE'));
      if (ok) reload(); else Forms.busy(btn, false);
    });
  });

  // ⋯ closes on a click elsewhere or Escape.
  const menus = [...root.querySelectorAll('details.band-role-menu')];
  document.addEventListener('click', (e) => menus.forEach((m) => { if (m.open && !m.contains(e.target)) m.open = false; }));
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') menus.forEach((m) => { if (m.open) { m.open = false; m.querySelector('summary').focus(); } });
  });
})();

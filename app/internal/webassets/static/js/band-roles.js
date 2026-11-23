// A band's roles editor (band_roles.html, docs/band-roles.md): a tick
// saves the role's whole privilege set at once; a custom role renames on
// change and deletes on confirm; a new role starts empty or with another
// role's privileges; a member's role saves as it changes. The API is the
// community's (httpapi/roles.go), under /api/bands/{id}.
(function () {
  const root = document.getElementById('roles');
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
  async function saveInline(statusEl, fn) {
    Forms.status(statusEl, t('Saving…'), '');
    try { await fn(); Forms.status(statusEl, t('Saved.'), 'ok'); return true; }
    catch (e) { Forms.status(statusEl, e.message, 'error'); return false; }
  }

  const status = document.getElementById('band-roles-status');
  root.querySelectorAll('.band-role-priv').forEach((cb) => {
    cb.addEventListener('change', async () => {
      const id = cb.dataset.roleId;
      const privileges = [...root.querySelectorAll(`.band-role-priv[data-role-id="${id}"]:checked`)].map((c) => c.dataset.privilege);
      const ok = await saveInline(status, () => api(`${base}/roles/${id}/privileges`, {privileges}, t('Couldn’t save that change. Try again.')));
      if (!ok) cb.checked = !cb.checked;
    });
  });

  root.querySelectorAll('.band-role-rename').forEach((input) => {
    const original = input.value;
    input.addEventListener('change', async () => {
      const name = input.value.trim();
      if (!name) { Forms.fieldError(input, t('Enter a role name.')); return; }
      Forms.fieldError(input, '');
      const ok = await saveInline(status, () => api(`${base}/roles/${input.dataset.roleId}/rename`, {name}, t('Couldn’t rename that role: is the name already taken?')));
      if (ok) location.reload(); else input.value = original;
    });
  });

  root.querySelectorAll('.band-role-delete').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!confirm(t('Delete the “%s” role? Anyone who has it must be given another role first.', btn.dataset.roleName))) return;
      Forms.busy(btn, true);
      const ok = await saveInline(status, () => api(`${base}/roles/${btn.dataset.roleId}`, undefined, t('Couldn’t delete that role. Give its members another role first, then try again.'), 'DELETE'));
      if (ok) location.reload(); else Forms.busy(btn, false);
    });
  });

  const form = document.getElementById('band-role-new-form');
  if (form) {
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      const name = document.getElementById('band-role-new-name').value.trim();
      const from = document.getElementById('band-role-new-from');
      const opt = from.selectedOptions[0];
      const privileges = opt && opt.dataset.privileges ? opt.dataset.privileges.split(' ') : [];
      Forms.run(form, document.getElementById('band-role-new-btn'), async () => {
        await api(`${base}/roles`, {name, privileges}, t('Couldn’t create that role. Try a different name.'));
        location.hash = 'roles';
        location.reload();
      });
    });
  }

  const memberStatus = document.getElementById('band-member-roles-status');
  root.querySelectorAll('.band-member-role').forEach((sel) => {
    let saved = sel.value;
    sel.addEventListener('change', async () => {
      const ok = await saveInline(memberStatus, () => api(`${base}/members/${sel.dataset.personId}/role`, {roleId: Number(sel.value)}, t('Couldn’t change the role. Try again.')));
      if (ok) { saved = sel.value; location.reload(); } else sel.value = saved;
    });
  });
})();

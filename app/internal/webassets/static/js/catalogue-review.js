// The Big Songbook's review queue and an edit's page
// (catalogue_review.html, catalogue_edit.html; httpapi/catalogue_edits.go):
// Approve / Reject an edit (a note for its author; the staff may keep its
// author from editing), mark a report done, let someone edit again.
(function () {
  async function post(url, body) {
    const res = await fetch(url, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}),
    }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : {};
    if (!res || !res.ok) throw new Error(data.error || t('That didn’t work. Try again.'));
    return data;
  }

  document.querySelectorAll('form.catalogue-edit-decide').forEach((form) => {
    form.addEventListener('submit', (e) => e.preventDefault());
    form.querySelectorAll('[data-decide]').forEach((btn) => btn.addEventListener('click', () => {
      const approve = btn.dataset.decide === 'approve';
      const ban = !approve && form.elements.ban?.checked;
      if (ban && !confirm(t('Reject this edit and keep its author from editing The Big Songbook? Their other edits waiting for review are rejected too.'))) return;
      Forms.run(form, btn, async () => {
        await post(`/api/big-songbook/edits/${form.dataset.editId}/${approve ? 'approve' : 'reject'}`, {
          note: form.elements.note?.value || '', ban,
        });
        location.reload();
      });
    }));
  });

  document.querySelectorAll('[data-report-handled]').forEach((btn) => btn.addEventListener('click', () => {
    Forms.run(btn.parentElement, btn, async () => {
      await post(`/api/big-songbook/reports/${btn.dataset.reportHandled}/handled`);
      btn.closest('.catalogue-report')?.remove();
    });
  }));

  document.querySelectorAll('[data-unban]').forEach((btn) => btn.addEventListener('click', () => {
    Forms.run(btn.parentElement, btn, async () => {
      await post(`/api/big-songbook/people/${btn.dataset.unban}/ban`, { banned: false });
      btn.closest('li')?.remove();
    });
  }));
})();

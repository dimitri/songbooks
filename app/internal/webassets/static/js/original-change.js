// "The original changed" (original_change.html, versions_sync.go): the
// original of this version changed since the version last synced with it.
// Take this change takes the parts ticked (those the version changed too
// can't be); Keep ours keeps the version as it is. Either way the version
// is synced with the original, and the page shows it again.
(function () {
  const panel = document.getElementById('original-change');
  if (!panel) return;
  const status = document.getElementById('original-change-status');
  const takeBtn = panel.querySelector('[data-act=take]');
  const boxes = [...panel.querySelectorAll('.original-change-parts input[type=checkbox]:not(:disabled)')];
  const sync = () => { if (takeBtn) takeBtn.disabled = !boxes.some((b) => b.checked); };
  boxes.forEach((b) => b.addEventListener('change', sync));
  sync();

  panel.querySelectorAll('[data-act]').forEach((btn) => btn.addEventListener('click', async () => {
    const action = btn.dataset.act;
    const parts = action === 'take' ? boxes.filter((b) => b.checked).map((b) => b.value) : [];
    Forms.status(status, '');
    Forms.busy(btn, true);
    const res = await fetch(`/api/variants/${panel.dataset.variantId}/original-change`, {
      method: 'POST', credentials: 'same-origin', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({action, parts, originalVersion: Number(panel.dataset.originalVersion)}),
    }).catch(() => null);
    const data = res ? await res.json().catch(() => ({})) : null;
    if (!res || !res.ok) {
      Forms.busy(btn, false);
      Forms.status(status, (data && data.message) || t("Couldn't answer the original's change. Try again."), 'error');
      return;
    }
    location.reload();
  }));
})();

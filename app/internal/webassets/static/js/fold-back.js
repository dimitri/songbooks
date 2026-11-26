// Fold back (the edit model; httpapi/foldback.go, edit_model.html): an
// arrangement that is only a fix goes back to the shared chart ("Use the
// shared chart", "Send the fix and use the shared chart"), or is kept
// ("It's our arrangement — keep it").
(function () {
  document.querySelectorAll('.fold-offer').forEach((box) => {
    const id = box.dataset.foldId;
    const status = box.querySelector('.form-status');
    const post = async (btn, url, body) => {
      Forms.status(status, '');
      Forms.busy(btn, true);
      const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) }).catch(() => null);
      const data = res ? await res.json().catch(() => ({})) : {};
      Forms.busy(btn, false);
      if (!res || !res.ok && res.status !== 202) {
        Forms.status(status, data.error || t('That didn’t work. Try again.'), 'error');
        return null;
      }
      return { res, data };
    };
    box.querySelector('[data-fold-go]')?.addEventListener('click', async (e) => {
      const out = await post(e.currentTarget, `/api/variants/${id}/fold`, { send: e.currentTarget.hasAttribute('data-fold-send') });
      if (!out) return;
      if (out.res.status === 202) { Forms.status(status, out.data.message || '', 'ok'); return; }
      // On the arrangement's own page: the shared chart, in its place.
      if (location.pathname === `/song/${id}`) { location.href = out.data.url || `/song/${out.data.variantId}`; return; }
      Forms.status(status, out.data.message || '', 'ok');
      box.querySelectorAll('button').forEach((b) => { b.disabled = true; });
    });
    box.querySelector('[data-fold-keep]')?.addEventListener('click', async (e) => {
      const out = await post(e.currentTarget, `/api/variants/${id}/fold-keep`);
      if (out) (box.closest('.fold-offer-row') || box).remove();
    });
  });
})();

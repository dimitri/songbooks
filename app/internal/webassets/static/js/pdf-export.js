// Wires every ".pdf-mode-row" (a PDF export link plus its paired view-mode
// <select> and "notes" checkbox) so picking a mode/toggling notes before
// clicking rewrites that link's query string live — no page reload, no
// server round trip just to preview the choice. One delegated listener on
// document rather than per-element wiring: this file is included on five
// otherwise-unrelated templates (play, project, band_catalog, setlist_gig,
// setlists_index), none of which share any other JS bundle, so delegation
// is simpler than five near-duplicate wiring blocks.
//
// Query-string contract matches internal/httpapi/handlers_pdf.go's
// optionsFromRequest: ?mode=jazz|map|lyrics (omitted for the default
// chords+lyrics mode) and ?notes=0 (the only way to turn notes OFF — an
// absent param means "show notes", today's unconditional behavior).
(() => {
if (window.__pdfExportWired) return;
window.__pdfExportWired = true;
document.addEventListener('change', (e) => {
  const row = e.target.closest('.pdf-mode-row');
  if (!row) return;
  const link = row.querySelector('a.pdf-export-link');
  if (!link) return;
  // The link's href at first load is the plain, no-query-param URL (the
  // same one a bookmark/share would use) — cache it once so repeated
  // mode changes always rewrite from that base, not from a previously
  // rewritten (already-query-stringed) href.
  if (!link.dataset.baseHref) link.dataset.baseHref = link.getAttribute('href');

  // A <select>, or radio buttons (the song page's "Export as PDF…" dialog).
  const mode = (row.querySelector('.pdf-mode-select') || row.querySelector('.pdf-mode-radio:checked'))?.value || '';
  const notesCb = row.querySelector('.pdf-notes-toggle');
  const notesOn = notesCb ? notesCb.checked : true;

  const params = new URLSearchParams();
  if (mode) params.set('mode', mode);
  if (!notesOn) params.set('notes', '0');
  const qs = params.toString();
  link.setAttribute('href', link.dataset.baseHref + (qs ? '?' + qs : ''));
});

// A songbook's or a set list's PDF (pdf_export.html): its button opens
// the dialog of choices ([data-pdf-open] names it); downloading, a click
// outside it or Cancel closes it (focus goes back to the button).
document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-pdf-open]');
  if (btn) {
    const dialog = document.getElementById(btn.dataset.pdfOpen);
    if (!dialog || dialog.open) return;
    if (typeof dialog.showModal === 'function') dialog.showModal(); else dialog.setAttribute('open', '');
    return;
  }
  const dialog = e.target.closest('dialog.song-pdf-dialog');
  if (!dialog || dialog.id === 'song-pdf-dialog') return; // the song page's own (song-controls.js)
  if (e.target === dialog) dialog.close();
  else if (e.target.closest('a.pdf-export-link')) setTimeout(() => dialog.close(), 0);
});
})();

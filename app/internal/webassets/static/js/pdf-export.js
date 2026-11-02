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

  const mode = row.querySelector('.pdf-mode-select')?.value || '';
  const notesCb = row.querySelector('.pdf-notes-toggle');
  const notesOn = notesCb ? notesCb.checked : true;

  const params = new URLSearchParams();
  if (mode) params.set('mode', mode);
  if (!notesOn) params.set('notes', '0');
  const qs = params.toString();
  link.setAttribute('href', link.dataset.baseHref + (qs ? '?' + qs : ''));
});

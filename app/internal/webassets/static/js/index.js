document.getElementById('add-project-btn').addEventListener('click', async () => {
  const name = prompt('New project/book name (e.g. "Side Project"):');
  if (!name) return;
  const res = await fetch('/api/books', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  });
  if (!res.ok) { alert('Could not create project: ' + (await res.json()).error); return; }
  window.location.href = '/b/' + encodeURIComponent(name);
});

// Import: a project exported via the "Export" link on its own page (a
// plain zip of its songs/setlists/scores/assets) becomes a new project
// here — never overwrites an existing one of the same name, see
// handleImportBook's doc comment.
const importFile = document.getElementById('import-project-file');
document.getElementById('import-project-btn').addEventListener('click', () => importFile.click());
importFile.addEventListener('change', async () => {
  const file = importFile.files[0];
  if (!file) return;
  const defaultName = file.name.replace(/\.zip$/i, '');
  const name = prompt('Import as project name:', defaultName);
  importFile.value = '';
  if (!name) return;
  const body = new FormData();
  body.append('book', name);
  body.append('file', file);
  const res = await fetch('/api/books/import', { method: 'POST', body });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { alert('Could not import: ' + (data.error || res.statusText)); return; }
  window.location.href = '/b/' + encodeURIComponent(data.book);
});

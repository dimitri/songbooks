// Dedicated structure mode: reorder/add/duplicate/delete sections as chips.
// No chord/lyric editing surface here on purpose — this is the fast
// "let's try this arrangement" view for a jam session or rehearsal.
(function () {
  const el = document.getElementById('structure');
  if (!el) return; // not on a page with the structure panel
  const book = el.dataset.book;
  const stem = el.dataset.stem;

  let version = null;
  let meta = [];
  let sections = [];

  function emptySection(label, repeat) {
    return { label, repeat, time: '', score: '', measures: [], lyrics: [] };
  }

  function render() {
    const list = document.getElementById('chip-list');
    list.innerHTML = '';
    sections.forEach((sec, i) => {
      const chip = document.createElement('div');
      chip.className = 'chip';

      const up = document.createElement('button');
      up.type = 'button'; up.textContent = '▲'; up.title = 'move up';
      up.disabled = i === 0;
      up.addEventListener('click', () => { [sections[i - 1], sections[i]] = [sections[i], sections[i - 1]]; render(); });

      const down = document.createElement('button');
      down.type = 'button'; down.textContent = '▼'; down.title = 'move down';
      down.disabled = i === sections.length - 1;
      down.addEventListener('click', () => { [sections[i + 1], sections[i]] = [sections[i], sections[i + 1]]; render(); });

      const label = document.createElement('input');
      label.type = 'text'; label.value = sec.label;
      label.addEventListener('input', () => { sec.label = label.value; });

      const badge = document.createElement('span');
      badge.className = 'chip-badge';
      const bars = (sec.measures || []).length;
      badge.textContent = sec.repeat ? '(repeat)' : (bars ? `${bars} bars` : 'empty');

      const dup = document.createElement('button');
      dup.type = 'button'; dup.textContent = '⧉'; dup.title = 'duplicate as repeat marker';
      dup.addEventListener('click', () => { sections.splice(i + 1, 0, emptySection(sec.label, true)); render(); });

      const del = document.createElement('button');
      del.type = 'button'; del.textContent = '✕'; del.title = 'delete';
      del.addEventListener('click', () => { sections.splice(i, 1); render(); });

      chip.append(up, down, label, badge, dup, del);
      list.appendChild(chip);
    });
  }

  document.getElementById('add-section').addEventListener('click', () => {
    sections.push(emptySection('New', false));
    render();
  });

  document.getElementById('save-structure').addEventListener('click', async () => {
    const status = document.getElementById('structure-status');
    status.className = ''; status.textContent = 'saving…';
    const res = await fetch(`/api/songs/${book}/${stem}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version, song: { meta, sections } }),
    });
    if (res.status === 409) {
      status.className = 'err';
      status.textContent = 'conflict: this song changed on disk — reload before saving';
      return;
    }
    if (!res.ok) { status.className = 'err'; status.textContent = 'save failed'; return; }
    const data = await res.json();
    version = data.version;
    status.className = 'ok'; status.textContent = 'saved';
  });

  (async function load() {
    const res = await fetch(`/api/songs/${book}/${stem}`);
    const data = await res.json();
    version = data.version;
    meta = data.song.meta;
    sections = data.song.sections;
    render();
  })();
})();

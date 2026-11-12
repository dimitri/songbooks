// How this person reads guitar and bass parts: "tab", "staff" or "both".
// A personal choice (training and habit), never part of the song: kept on
// the person (GET/PUT /api/me/notation-prefs) and handed to the page as
// <body data-notation-guitar/-bass>. The chart rows (alphatab-tab.js), the
// Scores tab editor and the Mixer all read it here; changing it anywhere
// fires "songbooks:notation" so open views can redraw.
(function () {
  const DEFAULTS = { guitar: 'tab', bass: 'both' };
  const LABELS = { tab: 'Tab', staff: 'Staff', both: 'Staff + tab' };
  const attr = (kind) => 'notation' + kind.charAt(0).toUpperCase() + kind.slice(1);

  function get(kind) {
    const v = document.body.dataset[attr(kind)];
    return LABELS[v] ? v : DEFAULTS[kind] || 'tab';
  }

  async function set(kind, value) {
    if (!LABELS[value] || !(kind in DEFAULTS)) return;
    document.body.dataset[attr(kind)] = value;
    document.dispatchEvent(new CustomEvent('songbooks:notation', { detail: { kind, value } }));
    try {
      await fetch('/api/me/notation-prefs', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ guitar: get('guitar'), bass: get('bass') }),
      });
    } catch (e) { /* stays for this page */ }
  }

  // A <select> for one instrument's notation, kept in step with changes
  // made elsewhere on the page.
  function picker(kind, onChange) {
    const sel = document.createElement('select');
    sel.className = 'notation-picker';
    sel.setAttribute('aria-label', `Show ${kind} as`);
    sel.title = `How you read ${kind} parts, on every song (just for you)`;
    for (const [v, label] of Object.entries(LABELS)) sel.add(new Option(label, v));
    sel.value = get(kind);
    sel.addEventListener('change', async () => { await set(kind, sel.value); if (onChange) onChange(sel.value); });
    document.addEventListener('songbooks:notation', (e) => { if (e.detail.kind === kind) sel.value = e.detail.value; });
    return sel;
  }

  window.songbooksNotation = { get, set, picker, LABELS };

  // The chart's own control, next to the instrument picker: one per kind
  // of part on the page. The rows are drawn once, so a change reloads.
  document.addEventListener('DOMContentLoaded', () => {
    const kinds = [...new Set([...document.querySelectorAll('.alphatab-row[data-instrument]')].map((r) => r.dataset.instrument))]
      .filter((k) => k in DEFAULTS);
    if (!kinds.length) return;
    const bar = document.createElement('div');
    bar.className = 'notation-pickers';
    for (const kind of kinds) {
      const label = document.createElement('label');
      label.append(kinds.length > 1 ? kind.charAt(0).toUpperCase() + kind.slice(1) + ' ' : '', picker(kind, () => location.reload()));
      bar.appendChild(label);
    }
    const anchor = document.getElementById('instrument-picker');
    if (anchor) anchor.insertAdjacentElement('afterend', bar);
    else document.getElementById('sheet')?.insertAdjacentElement('beforebegin', bar);
  });
})();

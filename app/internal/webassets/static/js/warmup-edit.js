// warmup-edit.js — the coach's form for an exercise (warmups.html): a
// pattern, from a classic or from scratch, heard and seen as it's written
// (its written key), its own level and words, saved.
(function () {
  const W = window.songbooksWarmup;
  const page = document.querySelector('.warmup-edit');
  if (!page || !W) return;
  const form = page.querySelector('.warmup-form');
  const api = `/api/communities/${page.dataset.community}/exercises`;
  const loaded = document.getElementById('warmup-exercise');
  const exercise = loaded ? JSON.parse(loaded.textContent) : null;

  // The written key: G2 to G5.
  const tonic = form.tonic;
  for (let m = 43; m <= 79; m++) {
    const o = document.createElement('option');
    o.value = W.nameOf(m, [1, 3, 6, 8, 10].includes(m % 12));
    o.textContent = o.value.replace('b', '♭').replace('#', '♯');
    tonic.append(o);
  }

  const presetSel = form.preset;
  W.PRESETS.forEach((p) => {
    const o = document.createElement('option');
    o.value = p.id; o.textContent = p.name();
    presetSel?.append(o);
  });

  const FIELDS = ['tonic', 'scale', 'notes', 'lengths', 'syllables', 'piano', 'prep', 'tempo', 'breath', 'step', 'repeats'];
  function fill(pattern, kind, name) {
    for (const f of FIELDS) if (form[f] && pattern[f] !== undefined) form[f].value = pattern[f];
    if (kind) form.kind.value = kind;
    if (name !== undefined) form.name.value = name;
    kindChanged();
  }
  function pattern() {
    const p = {};
    for (const f of FIELDS) if (form[f]) p[f] = form[f].type === 'number' ? Number(form[f].value) : form[f].value;
    if (form.kind.value === 'rhythm') { p.step = 0; p.repeats = p.repeats || 4; }
    return p;
  }
  function kindChanged() {
    const rhythm = form.kind.value === 'rhythm';
    page.querySelector('.warmup-repeats').hidden = !rhythm;
    page.querySelector('.warmup-step').hidden = rhythm;
  }
  form.kind.addEventListener('change', () => { kindChanged(); preview(); });

  if (exercise) {
    fill(exercise.pattern, exercise.kind, exercise.name);
    form.notes_text.value = exercise.notes || '';
    form.level.value = exercise.level || '';
    for (const [k, v] of Object.entries(exercise.messages || {})) {
      const ta = form[`msg-${k}`];
      if (ta) ta.value = v;
    }
  } else {
    fill(W.PRESETS[0].pattern, 'pitch', '');
    presetSel.value = W.PRESETS[0].id;
    form.name.value = W.PRESETS[0].name();
  }
  presetSel?.addEventListener('change', () => {
    const p = W.PRESETS.find((x) => x.id === presetSel.value);
    if (!p) return;
    fill(p.pattern, p.kind || 'pitch', p.name());
    preview();
  });

  // Heard and seen: the written key, once (a rhythm: twice).
  const status = page.querySelector('.warmup-preview-status');
  const player = W.player(page.querySelector('.warmup-score-canvas'), { scrollElement: 'html,body' });
  let timer = 0;
  function preview() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      const p = pattern();
      const keys = form.kind.value === 'rhythm' ? [0, 0] : [0];
      try {
        const b = W.build(p, keys, { title: form.name.value, voiceName: t('Voice'), pianoName: t('Piano') });
        player.load(b.xml);
        status.textContent = t('%d notes, %d seconds a repeat', b.notes.length, Math.round(b.duration / keys.length));
      } catch (e) {
        status.textContent = t('This pattern can’t be read: check its notes and lengths.');
      }
    }, 300);
  }
  form.addEventListener('input', (e) => { if (!e.target.name || !e.target.name.startsWith('msg-')) preview(); });
  page.querySelector('.warmup-preview-play').addEventListener('click', () => player.api.playPause());
  preview();

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    const messages = {};
    form.querySelectorAll('.check-message textarea').forEach((ta) => { if (ta.value.trim()) messages[ta.name.slice(4)] = ta.value.trim(); });
    const body = { name: form.name.value.trim(), kind: form.kind.value, pattern: pattern(), notes: form.notes_text.value.trim(), level: form.level.value, messages };
    const saved = page.querySelector('.warmup-saved');
    const url = exercise ? `${api}/${exercise.id}` : api;
    fetch(url, { method: exercise ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
      .then((r) => { if (!r.ok) return r.text().then((m) => { throw new Error(m); }); return exercise ? null : r.json(); })
      .then((res) => {
        const id = exercise ? exercise.id : res.id;
        form.reset();
        location.href = `/communities/${page.dataset.community}/warmups/${id}`;
      })
      .catch((err) => { saved.textContent = t('Couldn’t save.') + ' ' + err.message; });
  });
  page.querySelector('.warmup-delete')?.addEventListener('click', () => {
    if (!confirm(t('Remove this exercise? Its takes and its students’ paths go with it.'))) return;
    fetch(`${api}/${exercise.id}`, { method: 'DELETE' }).then(() => { location.href = `/communities/${page.dataset.community}/warmups`; });
  });
})();

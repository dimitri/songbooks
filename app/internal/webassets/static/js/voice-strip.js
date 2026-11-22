// A student's range strip with the song's melody over it, where a key is
// chosen for them (httpapi/voice_strip.go, docs/coaching.md): the song
// page's Voice & key panel, the coach's "Add to…" and request key pickers.
// The strip is the student page's (C2 to C7: chest, mix, head, the
// comfortable zone, striped); the melody, the song's range from its
// recording, is a frame over it at the key being tried, sliding as − / +
// is pressed. Information, not advice: no verdict, no number.
//
//   VoiceStrip.show(el, { variant, community, person, coach, semitones })
//   VoiceStrip.key(el, semitones)
//   VoiceStrip.hide(el)
//
// person 0: the viewer's own (a student). coach: say "melody range not
// known yet" when it isn't (a student sees nothing then).
(() => {
  const cache = new Map();
  const load = (o) => {
    const k = `${o.variant}/${o.community}/${o.person || 0}`;
    if (!cache.has(k)) {
      // In the page's language (the note names), whatever the browser's.
      const q = new URLSearchParams({ community: o.community, lang: document.documentElement.lang || '' });
      if (o.person) q.set('person', o.person);
      cache.set(k, fetch(`/api/songs/${o.variant}/voice-strip?${q}`).then((r) => (r.ok ? r.json() : null)).catch(() => null));
    }
    return cache.get(k);
  };
  const el = (tag, cls, parent) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (parent) parent.append(e);
    return e;
  };
  const place = (e, sp) => {
    e.style.left = `${sp.left.toFixed(2)}%`;
    e.style.width = `${sp.width.toFixed(2)}%`;
  };
  const state = new WeakMap();

  function draw(box, d, o) {
    box.replaceChildren();
    const noted = d.registers.some((r) => r.width) || d.comfort;
    if (!d.melody && !o.coach) { box.hidden = true; return; }
    if (!d.melody && !noted) { box.hidden = true; return; }
    box.hidden = false;
    const chart = el('div', 'voice-chart voice-strip-chart', box);
    chart.setAttribute('aria-hidden', 'true');
    const keys = el('div', 'voice-chart-keys', chart);
    d.scale.forEach((name, i) => {
      const s = el('span', '', keys);
      s.style.left = `${(i * 100) / (d.scale.length - 1)}%`;
      s.textContent = name;
    });
    d.registers.forEach((r) => {
      const row = el('div', 'voice-chart-row', chart);
      const bar = el('span', `voice-chart-bar is-${r.register}`, row);
      bar.title = r.label;
      if (r.width) place(bar, r); else bar.hidden = true;
    });
    if (d.comfort) place(el('span', 'voice-chart-bar is-tessitura', el('div', 'voice-chart-row', chart)), d.comfort);
    const mel = el('span', 'voice-strip-melody', chart);
    const cap = el('p', 'hint voice-strip-caption', box);
    state.set(box, { d, mel, cap, coach: o.coach });
    key(box, box.dataset.stripKey !== undefined ? Number(box.dataset.stripKey) : (o.semitones || 0));
  }

  function key(box, n) {
    box.dataset.stripKey = n; // the key tried, kept for a strip still loading
    const s = state.get(box);
    if (!s) return;
    const sp = s.d.melody && s.d.melody[String(n)];
    s.mel.hidden = !sp;
    if (sp) {
      place(s.mel, sp);
      s.mel.classList.toggle('is-clipped', !!sp.clipped);
    }
    s.cap.textContent = s.d.melody ? t('the song’s melody (from its recording), in this key') : t('melody range not known yet');
  }

  async function show(box, o) {
    if (!box || !o.community || !o.variant) return;
    box.dataset.stripFor = `${o.variant}/${o.community}/${o.person || 0}`;
    box.dataset.stripKey = o.semitones || 0;
    const want = box.dataset.stripFor;
    const d = await load(o);
    if (box.dataset.stripFor !== want) return; // another student picked meanwhile
    if (!d) { box.hidden = true; return; }
    draw(box, d, o);
  }

  function hide(box) {
    if (!box) return;
    delete box.dataset.stripFor;
    delete box.dataset.stripKey;
    state.delete(box);
    box.hidden = true;
    box.replaceChildren();
  }

  window.VoiceStrip = { show, key, hide };
  // Loaded after the player: it draws now.
  document.dispatchEvent(new Event('songbooks:voice-strip'));
})();

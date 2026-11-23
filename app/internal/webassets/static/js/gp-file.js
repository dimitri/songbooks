// A song's Guitar Pro file, from the browser: load it, save a new version
// of it, or start a new one. Shared by the song page (gp-view.js,
// gp-drift.js) and the file editor (gp-editor.js). Needs alphaTab and
// gp-facts.js.
(function () {
  if (typeof alphaTab === 'undefined' || typeof GPFacts === 'undefined') return;

  async function loadBytes(variantId) {
    const res = await fetch(`/song/${variantId}/gp-file`, { credentials: 'same-origin' });
    if (!res.ok) throw new Error(res.status === 404 ? t('This song has no Guitar Pro file yet.') : t('Could not load the Guitar Pro file.'));
    return new Uint8Array(await res.arrayBuffer());
  }

  function parse(bytes) {
    return alphaTab.importer.ScoreLoader.loadScoreFromBytes(bytes, new alphaTab.Settings());
  }

  // parseLevels parses the file with each track at its level in it
  // (gp-levels.js: Guitar Pro 8 keeps it where alphaTab doesn't read it,
  // and the track would otherwise play, and be saved, silent).
  async function parseLevels(bytes) {
    const score = parse(bytes);
    if (window.GPLevels) GPLevels.apply(score, await GPLevels.read(bytes));
    return score;
  }

  // save writes score as a new version of the song's file, in Guitar Pro 7
  // format (Guitar Pro 7 and 8 open it), with its facts read back from the
  // written bytes, and keeps "gp_source:" naming it. Earlier versions stay
  // on the song's History page. name is the file's current name.
  // alphaTab writes each track's channel strip from the model's levels and
  // drops its automations: those of the file saved over are put back
  // (gp-levels.js), so Guitar Pro plays it as it did.
  async function save(variantId, score, name) {
    let bytes = new alphaTab.exporter.Gp7Exporter().export(score, new alphaTab.Settings());
    if (window.GPLevels) {
      const before = await loadBytes(variantId).catch(() => null);
      if (before) bytes = await GPLevels.keep(bytes, before);
    }
    const facts = GPFacts.extract(parse(bytes));
    const newName = (name || 'song.gp').replace(/\.(gp[345x]?)$/i, '') + '.gp';
    const form = new FormData();
    form.append('file', new Blob([bytes], { type: 'application/octet-stream' }), newName);
    form.append('facts', JSON.stringify(facts));
    const res = await fetch(`/api/songs/${variantId}/gp-file`, { method: 'PUT', credentials: 'same-origin', body: form });
    if (!res.ok) throw new Error(t('Saving the file failed: %s', (await res.text()) || res.status));
    if (newName !== name) {
      await fetch(`/api/songs/${variantId}/meta`, {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: 'gp_source', value: newName }),
      });
    }
    return { name: newName, facts, ...(await res.json()) };
  }

  const TUNINGS = {
    guitar: { tuning: 'E4 B3 G3 D3 A2 E2', program: 25, name: 'Guitar' },
    bass: { tuning: 'G2 D2 A1 E1', program: 33, name: 'Bass' },
  };
  const esc = (s) => String(s || '').replace(/["\\]/g, '\\$&');

  // blank makes an empty score: the given parts ('guitar', 'bass',
  // 'drums'), bars rest bars at tempo and time.
  function blank({ title, artist, tempo, num, den, bars, parts }) {
    let tex = `\\title "${esc(title)}"\n`;
    if (artist) tex += `\\artist "${esc(artist)}"\n`;
    tex += `\\tempo ${tempo || 120}\n`;
    const restBar = Array.from({ length: num || 4 }, () => `r.${den || 4}`).join(' ');
    const rests = Array.from({ length: Math.max(1, bars || 4) }, () => restBar);
    for (const part of parts && parts.length ? parts : ['guitar']) {
      if (part === 'drums') {
        tex += '\\track "Drums" { instrument percussion }\n\\staff {score}\n\\articulation (Kick 36)\n\\articulation (Snare 38)\n\\articulation (HiHatClosed 42)\n.\n';
        tex += `\\ts (${num || 4} ${den || 4}) \\clef neutral ` + rests.join(' | ') + ' |\n';
        continue;
      }
      const t = TUNINGS[part] || TUNINGS.guitar;
      tex += `\\track "${t.name}" { instrument ${t.program} }\n\\staff {tabs}\n\\tuning (${t.tuning})\n.\n`;
      tex += `\\ts (${num || 4} ${den || 4}) ` + rests.join(' | ') + ' |\n';
    }
    const imp = new alphaTab.importer.AlphaTexImporter();
    imp.initFromString(tex, new alphaTab.Settings());
    return imp.readScore();
  }

  window.GPFile = { loadBytes, parse, parseLevels, save, blank };
})();

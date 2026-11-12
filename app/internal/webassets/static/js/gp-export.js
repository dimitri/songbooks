// "Export as Guitar Pro": the song as it is now, every score track over the
// whole song in performed order (the Mixer's own alphaTeX, GET
// /api/songs/{id}/mixer), with its sections as GP markers and its chords as
// chord names, written by alphaTab's Gp7Exporter (Guitar Pro 7 and 8 open
// it). This is what to take into Guitar Pro after fixing something on the
// web, so the fix comes back on the next GP Import instead of being lost.
//
// When the song still has the Guitar Pro file it was imported from and the
// bar counts match, that file's backing track and its sync points come
// along. Anything else the original had (tracks never imported, Guitar
// Pro–only effects) is not in the export: the original stays downloadable
// as it was uploaded.
(function () {
  async function originalScore(variantId, settings) {
    try {
      const res = await fetch(`/song/${variantId}/gp-file`);
      if (!res.ok) return null;
      return alphaTab.importer.ScoreLoader.loadScoreFromBytes(new Uint8Array(await res.arrayBuffer()), settings);
    } catch (e) {
      return null;
    }
  }

  async function exportGP(variantId, title) {
    const res = await fetch(`/api/songs/${variantId}/mixer`);
    const m = await res.json();
    if (!res.ok || !m.alphaTex) throw new Error('this song has no score tracks to export');
    const settings = new alphaTab.Settings();
    const imp = new alphaTab.importer.AlphaTexImporter();
    imp.initFromString(m.alphaTex, settings);
    const score = imp.readScore();
    score.title = title || '';

    // Sections as markers, chords as chord names on the first track.
    const staff = score.tracks[0] && score.tracks[0].staves[0];
    let chordId = 0;
    const chordIds = new Map();
    for (const s of m.sections || []) {
      const mb = score.masterBars[s.startBar];
      if (mb && !mb.section) {
        const sec = new alphaTab.model.Section();
        sec.text = s.label;
        sec.marker = '';
        mb.section = sec;
      }
      if (!staff || staff.isPercussion) continue;
      (s.chords || []).forEach((barChords, i) => {
        const bar = staff.bars[s.startBar + i];
        const voice = bar && bar.voices[0];
        if (!voice || !barChords || !barChords.length) return;
        for (const c of barChords) {
          if (!c.name) continue;
          // The beat that starts at or after the chord's place in the bar.
          const at = c.pos * bar.masterBar.calculateDuration();
          const beat = voice.beats.find((b) => b.playbackStart >= at - 1) || voice.beats[voice.beats.length - 1];
          if (!beat || beat.chordId) continue;
          if (!chordIds.has(c.name)) {
            const chord = new alphaTab.model.Chord();
            chord.name = c.name;
            const id = String(++chordId);
            staff.addChord(id, chord);
            chordIds.set(c.name, id);
          }
          beat.chordId = chordIds.get(c.name);
        }
      });
    }

    // The original file's backing track, when its bars still line up.
    const orig = await originalScore(variantId, settings);
    if (orig && orig.backingTrack && orig.masterBars.length === score.masterBars.length) {
      score.backingTrack = orig.backingTrack;
      orig.masterBars.forEach((mb, i) => { if (mb.syncPoints) score.masterBars[i].syncPoints = mb.syncPoints; });
    }

    const bytes = new alphaTab.exporter.Gp7Exporter().export(score, settings);
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }));
    a.download = (title || 'song').replace(/[\\/:*?"<>|]/g, '-') + '.gp';
    document.body.appendChild(a);
    a.click();
    setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
  }

  document.addEventListener('click', async (e) => {
    const btn = e.target.closest('.gp-export-btn');
    if (!btn) return;
    e.preventDefault();
    if (typeof alphaTab === 'undefined') return;
    const label = btn.textContent;
    btn.setAttribute('aria-busy', 'true');
    try {
      await exportGP(btn.dataset.variantId, btn.dataset.title);
    } catch (err) {
      alert('Couldn’t export: ' + err.message);
    } finally {
      btn.removeAttribute('aria-busy');
      btn.textContent = label;
    }
  });
})();

// The "Out of step with the Guitar Pro file" panel (play.html, gpdrift.go):
// each difference between the song's text and its file can take the
// file's value (the server edits the text), push the text's value into
// the file (done here on alphaTab's model of the file, saved as a new
// file version by GPView.saveScore), or be kept as it is.
(function () {
  const panel = document.getElementById('gp-drift');
  if (!panel) return;
  const variantId = panel.dataset.variantId;

  async function post(path, body) {
    const res = await fetch(`/api/songs/${variantId}/gp-drift/${path}`, {
      method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error((await res.text()) || ('HTTP ' + res.status));
  }

  // Bars from the first one with the old value up to where it next
  // changes: what a header value covers in the file.
  function leadingRun(items, same) {
    const out = [];
    for (const it of items) { if (!same(it)) break; out.push(it); }
    return out;
  }

  function pushInto(score, p) {
    const mbs = score.masterBars;
    switch (p.kind) {
      case 'tempo': {
        score.tempo = p.tempo;
        const mb = mbs[0];
        let a = (mb.tempoAutomations && mb.tempoAutomations[0]) || mb.tempoAutomation;
        if (!a) {
          a = new alphaTab.model.Automation();
          a.type = alphaTab.model.AutomationType.Tempo;
          a.ratioPosition = 0;
          if (mb.tempoAutomations) mb.tempoAutomations.push(a); else mb.tempoAutomation = a;
        }
        a.value = p.tempo;
        break;
      }
      case 'time': {
        const n0 = mbs[0].timeSignatureNumerator, d0 = mbs[0].timeSignatureDenominator;
        for (const mb of leadingRun(mbs, (m) => m.timeSignatureNumerator === n0 && m.timeSignatureDenominator === d0)) {
          mb.timeSignatureNumerator = p.num;
          mb.timeSignatureDenominator = p.den;
        }
        break;
      }
      case 'key': {
        for (const t of score.tracks) {
          for (const st of t.staves) {
            if (st.isPercussion) continue;
            const k0 = st.bars[0].keySignature, t0 = st.bars[0].keySignatureType;
            for (const b of leadingRun(st.bars, (b) => b.keySignature === k0 && b.keySignatureType === t0)) {
              b.keySignature = p.sig;
              b.keySignatureType = p.minor ? 1 : 0;
            }
          }
        }
        for (const mb of mbs) if ('keySignature' in mb) { mb.keySignature = p.sig; mb.keySignatureType = p.minor ? 1 : 0; }
        break;
      }
      case 'swing': {
        const feel = p.swing === '8' ? alphaTab.model.TripletFeel.Triplet8th
          : p.swing === '16' ? alphaTab.model.TripletFeel.Triplet16th : alphaTab.model.TripletFeel.NoTripletFeel;
        const f0 = mbs[0].tripletFeel;
        for (const mb of leadingRun(mbs, (m) => m.tripletFeel === f0)) mb.tripletFeel = feel;
        break;
      }
      case 'diagram': {
        const track = score.tracks[(p.track || 1) - 1] || score.tracks[0];
        const st = track.staves[0];
        let chord = null;
        for (const c of (st.chords ? st.chords.values() : [])) if (c.name === p.name || GPChordSame(c.name, p.name)) chord = c;
        if (!chord) {
          chord = new alphaTab.model.Chord();
          chord.name = p.name;
          let id = 0;
          while (st.chords && st.chords.has(String(id))) id++;
          st.addChord(String(id), chord);
        }
        chord.firstFret = p.base || 1;
        chord.strings = p.frets.slice();
        chord.barreFrets = (p.barres || []).slice();
        chord.showDiagram = true;
        chord.showName = true;
        break;
      }
      case 'marker': {
        const mb = mbs[p.bar - 1];
        if (!mb.section) mb.section = new alphaTab.model.Section();
        mb.section.text = p.name;
        break;
      }
      default:
        throw new Error('This difference can’t be written into the file.');
    }
  }

  // Names as Guitar Pro and this app write them can differ in spelling
  // only (A#7 / Bb7): the server already matched them, so any chord with
  // the same letters ignoring case and spaces is the one.
  function GPChordSame(a, b) {
    const n = (s) => String(s || '').replace(/\s+/g, '').toLowerCase();
    return n(a) === n(b);
  }

  panel.addEventListener('click', async (e) => {
    const btn = e.target.closest('button[data-act]');
    if (!btn) return;
    const li = btn.closest('.gp-drift-item');
    const fact = li.dataset.fact;
    li.querySelectorAll('button').forEach((b) => { b.disabled = true; });
    li.querySelector('.gp-drift-error')?.remove();
    try {
      switch (btn.dataset.act) {
        case 'take': await post('take', { fact }); break;
        case 'keep': await post('keep', { fact, sig: li.dataset.sig }); break;
        case 'unkeep': await post('keep', { fact, sig: '' }); break;
        case 'push': {
          if (!window.GPView) throw new Error('The file viewer didn’t load.');
          const score = await window.GPView.loadScore(true);
          pushInto(score, JSON.parse(li.dataset.push));
          await window.GPView.saveScore(score);
          break;
        }
      }
      location.reload();
    } catch (err) {
      const p = document.createElement('p');
      p.className = 'gp-drift-error';
      p.textContent = err.message;
      li.appendChild(p);
      li.querySelectorAll('button').forEach((b) => { b.disabled = false; });
    }
  });
})();

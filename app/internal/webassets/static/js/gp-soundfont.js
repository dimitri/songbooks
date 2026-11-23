// The soundfont a score plays with (docs/soundfont.md).
//
// The server has GeneralUser GS (a 30 MB General MIDI bank) and serves
// it cut down to a few presets: /soundfonts/subset/{key}.sf2?sf={id},
// key the presets' "bank-program" sorted ("0-24_0-33_128-0"), id the
// bank's (the page says it: data-soundfont on this script's tag). A score
// asks for the presets it plays — its tracks' programs, as chosen
// (gp-instruments.js), the instruments it changes to along the way, its
// drum kit — so a file with a nylon guitar downloads about 400 KB, and
// files playing the same instruments share one cached subset.
//
// No bank on the server (no id), or the subset can't be had: alphaTab's
// own Sonivox (1.3 MB, every GM program), as before. A player starts with
// the subset, not with Sonivox while it downloads: the two are about the
// same size.
(function () {
  const SONIVOX = '/static/js/vendor/alphatab/soundfont/sonivox.sf2';
  const DRUMS = 128; // the drum kits' bank
  const MAX_PRESETS = 64; // the server's limit
  const root = typeof window !== 'undefined' ? window : globalThis;
  const script = typeof document !== 'undefined' ? document.currentScript : null;
  let bankId = (script && script.dataset.soundfont) || '';

  // alphaTab's AutomationType: Instrument (a program change), Bank.
  const AUTO = () => {
    const A = root.alphaTab && root.alphaTab.model && root.alphaTab.model.AutomationType;
    return { instrument: A ? A.Instrument : 2, bank: A && A.Bank !== undefined ? A.Bank : 4 };
  };
  const isDrums = (track) => (track.staves || []).some((st) => st.isPercussion);

  // presetsOf: the [bank, program] pairs score plays, sorted. A drum track
  // plays its kit (bank 128, its program), and kit 0 too, the one alphaTab
  // falls back to; without drums the server adds kit 0's click alone (the
  // metronome).
  function presetsOf(score) {
    const set = new Map();
    const add = (b, p) => { if (b >= 0 && b <= 2047 && p >= 0 && p <= 127) set.set(`${b}-${p}`, [b, p]); };
    const kind = AUTO();
    for (const track of (score && score.tracks) || []) {
      const info = track.playbackInfo || {};
      if (isDrums(track)) {
        add(DRUMS, info.program | 0);
        add(DRUMS, 0);
        continue;
      }
      const banks = new Set([(info.bank | 0) & 2047]);
      const programs = new Set([info.program | 0]);
      for (const st of track.staves || []) {
        for (const bar of st.bars || []) {
          for (const v of bar.voices || []) {
            for (const beat of v.beats || []) {
              for (const a of beat.automations || []) {
                if (a.type === kind.instrument) programs.add(a.value | 0);
                else if (a.type === kind.bank) banks.add((a.value | 0) & 2047);
              }
            }
          }
        }
      }
      for (const b of banks) for (const p of programs) add(b, p);
    }
    return [...set.values()].sort((x, y) => x[0] - y[0] || x[1] - y[1]);
  }

  const keyOf = (presets) => presets.map(([b, p]) => `${b}-${p}`).join('_');

  // urlFor: the soundfont for score — its subset, or Sonivox.
  function urlFor(score) {
    const presets = presetsOf(score);
    if (!bankId || !presets.length || presets.length > MAX_PRESETS) return SONIVOX;
    return `/soundfonts/subset/${keyOf(presets)}.sf2?sf=${encodeURIComponent(bankId)}`;
  }

  // The bytes of a soundfont, fetched once a page.
  const fetched = new Map();
  function fetchBytes(url) {
    if (!fetched.has(url)) {
      const p = fetch(url, { credentials: 'same-origin' })
        .then((r) => { if (!r.ok) throw new Error('soundfont ' + r.status); return r.arrayBuffer(); })
        .then((b) => new Uint8Array(b));
      p.catch(() => fetched.delete(url));
      fetched.set(url, p);
    }
    return fetched.get(url);
  }
  // bytesFor: score's soundfont, Sonivox when its subset can't be had.
  async function bytesFor(score) {
    const url = urlFor(score);
    if (url !== SONIVOX) {
      try { return await fetchBytes(url); } catch (e) { /* Sonivox, below */ }
    }
    return fetchBytes(SONIVOX);
  }

  // ---- alphaTab's own player ----------------------------------------------
  // player(settings, score): the player loads score's soundfont (alphaTab
  // fetches it); returns its URL. attach(api, url), once the player is
  // made: Sonivox if that URL fails. update(api, score),
  // the score's instruments changed: the subset for them, when it's
  // another; resolves once loaded (alphaTab pauses to load it), true if
  // it changed.
  const current = new WeakMap();
  function player(settings, score) {
    const url = urlFor(score);
    settings.player.soundFont = url;
    return url;
  }
  // alphaTab's API reports a soundfont that failed to download (a
  // FileLoadError) or to load (not a SoundFont: a 404's page, say) as an
  // error (api.error), among its others (its AudioWorklet's, …).
  const soundFontError = (e) => !!e && ((root.alphaTab && root.alphaTab.FileLoadError && e instanceof root.alphaTab.FileLoadError) || /sound ?font|riff|sfbk|sf2|chunk/i.test(String(e.message || e)));
  function attach(api, url) {
    current.set(api, url);
    let settled = url === SONIVOX;
    if (api.soundFontLoaded) api.soundFontLoaded.on(() => { settled = true; });
    const failed = api.soundFontLoadFailed || api.error;
    if (!failed || !failed.on) return;
    failed.on((e) => {
      if (settled || !soundFontError(e)) return;
      settled = true;
      current.set(api, SONIVOX);
      api.loadSoundFont(SONIVOX, false);
    });
  }
  async function update(api, score) {
    const url = urlFor(score);
    if (current.get(api) === url) return false;
    const bytes = await bytesFor(score);
    current.set(api, url);
    api.loadSoundFont(bytes, false);
    return true;
  }

  const api = { SONIVOX, presetsOf, keyOf, urlFor, bytesFor, player, attach, update, setBank: (id) => { bankId = id || ''; } };
  root.GPSoundFont = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})();

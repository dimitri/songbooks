# The soundfont scores play with

A Guitar Pro score (the Score view, the Mixer, the import's preview)
plays through alphaTab's synthesizer, which needs a SoundFont 2 bank:
the samples each General MIDI program is played with. alphaTab ships
Sonivox (1.3 MB, every GM program), whose nylon guitar is three short
samples and sounds like a piano (#149). Scores now play with
**GeneralUser GS**, cut down per score to the presets it plays.

## GeneralUser GS

- **What:** a GM/GS bank by S. Christian Collins: 287 presets (13 drum
  kits), 920 samples, 32,319,396 bytes. Its INFO name says "GeneralUser
  GS 2.0.3 BETA"; its license and changelog say v2.0.3.
- **Where from:** the author's GitHub repository, which says it is there
  for automated packaging (it has no release tags), at commit
  `684543d5e5efaef08d02be50dcda8d552478fa60` (2026-02-23, "update
  documentation to r6"; the bank itself is from the commit before,
  "Update SoundFont to v2.0.3"):
  - `https://raw.githubusercontent.com/mrbumpy409/GeneralUser-GS/<commit>/GeneralUser-GS.sf2`,
    SHA-256 `9575028c7a1f589f5770fccc8cff2734566af40cd26ed836944e9a5152688cfe`;
  - `…/<commit>/documentation/LICENSE.txt` (License v2.0),
    SHA-256 `7b32efefdf95ce38a043799f0659853ddc00fbaa14d8c50f0aca16b9b8b405be`.
- **License, in short:** use without restriction, private or commercial;
  free to use in software projects and to modify the bank or its
  packaging. The samples' rights allow full use in music production; the
  author can't vouch for the origin of every sample (none from
  commercial packages, as far as he knows) and asks to hear of any
  restricted one. Featured on a website: link to his site or host your
  own copy, not his download files. The license file is kept next to the
  bank (`GeneralUser-GS-LICENSE.txt`).
- **Not in git** (30 MB): `deploy/Dockerfile` fetches it at build time,
  pinned (the `GUGS_*` ARGs: commit and SHA-256s; the build fails on a
  mismatch), into `/usr/local/share/songbooks/`. In the image rather than
  fetched at first use: the build is where pinned downloads already
  happen here (yt-dlp, LilyPond), the app needs no outbound access for
  it, and a new version is a reviewed change to the Dockerfile.
  `deploy/fetch-soundfont.sh [dir]` fetches the same files (reading the
  pins from the Dockerfile) for a run outside Docker; by default into the
  app's cache directory, where the app finds it. `deploy/Dockerfile.local`
  runs that script.
- **Where the app looks:** `--soundfont` / `SONGBOOKS_SOUNDFONT`; else
  `/usr/local/share/songbooks/GeneralUser-GS.sf2`, or
  `GeneralUser-GS.sf2` in the cache directory when it's there. No bank
  (a local run that didn't fetch it): pages are told none, and play with
  Sonivox as before. The log says which at startup.

## A subset per score

`GET /soundfonts/subset/{key}.sf2?sf={id}`

- **key**: the presets, each `bank-program`, sorted, `_` between:
  `0-24_0-33_128-0`. Files playing the same instruments share one.
  At most 64 presets; melodic banks 0–2047 (alphaTab keeps 11 bits of
  a channel's bank), drum kits in bank 128, programs 0–127.
- **id**: the start of the bank's SHA-256; pages get it from the server
  (`soundfontID`, on `gp-soundfont.js`'s script tag). A URL names its
  content, so it's cached for a year (`public, max-age=31536000,
  immutable`, an ETag). Another spelling of the key, or a page from
  before the bank changed (another id), is redirected (302, `no-cache`)
  to the right URL.
- **What's in it** (`internal/soundfont`): the presets asked for, the
  instruments they play, the samples those play (and the samples
  stereo ones are linked to), every index rewritten, each sample's
  points (its loop included) copied once, followed by the 46 zero points
  the spec asks for; the bank's INFO as it is. A melodic bank the bank
  doesn't have gets bank 0's program under that bank (what a GS synth
  does); a kit it doesn't have is left out (alphaTab plays kit 0).
- **The metronome:** alphaTab's click is kit 0's key 33. A score with
  drums asks for its kit and kit 0; one without gets kit 0 cut down to
  the zones that sound key 33 (the whole kit is 2.7 MB).
- **Made on first use**, then cached on disk: `<cache dir>/soundfont-subsets/<id>/`,
  the file named by the key's hash; one build per key at a time. The
  cache dir is `--cache-dir` / `SONGBOOKS_CACHE_DIR`, by default the
  user's cache directory (`/root/.cache/songbooks` in the image: lost
  with the container, made again on demand, a few milliseconds each).
  **Cap:** `--soundfont-cache-mb`, 256 MB by default; past it the least
  recently used go (another bank's first) until it's down to three
  quarters.
- **Public**, no sign-in: it's the bank's sound, nothing of anyone's.

Typical sizes (with the click):

| Score | Subset |
|---|---|
| nylon guitar (24) | 0.41 MB |
| steel guitar (25) | 0.40 MB |
| clean electric guitar (27) | 0.53 MB |
| finger bass (33) | 0.10 MB |
| grand piano (0) | 2.08 MB |
| band: overdriven and clean guitars, bass, drums | 4.17 MB |
| band: nylon, steel, bass, piano, drums | 5.67 MB |

(`go test ./internal/soundfont -v` with `SONGBOOKS_SOUNDFONT` set prints
them.)

## In the pages (`gp-soundfont.js`)

- **What a score asks for** (`presetsOf`): each track's program, as
  chosen (#149's instruments, `gp-instruments.js`: the person's, the
  song's, the file's), the programs its Instrument automations change to
  along the way, with its banks; a drum track, its kit and kit 0.
- **Score view** (`gp-view.js`): the sound is rendered offline
  (`exportAudio`) with the score's subset; a new instrument renders it
  again with the new one's.
- **Mixer** (`mixer.js`) and **import preview** (`gp-import.js`):
  alphaTab's player loads the subset (`player.soundFont`); a new
  instrument in the Mixer fetches the subset for it (when it isn't the
  same) and loads it (`api.loadSoundFont`) before the MIDI is reloaded.
- **Sonivox stays** as the fallback: no bank on the server, a subset
  that fails to download or to load, more than 64 presets. A player
  doesn't start on Sonivox while the subset downloads: a one-instrument
  subset is smaller than Sonivox, so there is nothing to gain.
- The Guitar Pro editor and the warm-ups still play with Sonivox.

## Tests

- `internal/soundfont`: Sonivox (always there) and GeneralUser GS (when
  `SONGBOOKS_SOUNDFONT` points at it): every subset parses back with
  its references consistent, its presets named as in the bank, playing
  the same samples with the same points; a subset of every preset is the
  bank again; the click alone; the cache and its cap; and
  `TestAlphaTabPlays`: alphaTab's synthesizer, in Node
  (`testdata/render.cjs`), plays a few notes from the subset and from the
  whole bank, and the two renders are the same, sample for sample.
- `internal/httpapi`: the endpoint (headers, ETag, redirects, bad keys).
- `jstest/gp-soundfont.test.mjs`: what a score asks for, the URL, the
  fallbacks.

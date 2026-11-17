# Speakers, key and parts: AirPlay, Chromecast, transposing, stems

A plan, nothing built yet. How the players could send their sound to a
speaker, play a recording in another key, and play it without the voice,
the bass or the guitar.

## Where the sound comes from today

| Player | Sound | Served by |
|---|---|---|
| Practice player (song page, audio-player.js) | `<audio>`, speed with `preservesPitch`; click and count-in in Web Audio | us (YouTube references, uploads, a Guitar Pro file's recording) |
| Score view (gp-view.js, alphaTab) | the file's recording, or alphaTab's synth (Web Audio), click and count-in | us |
| Rehearsals, Concerts, set lists (rec-player) | `<audio>` | pCloud (the browser asks for the link) or us (references) |
| Mixer (mixer.js) | `<audio>` per lane, Web Audio | us |
| Metronome, tuner | Web Audio | — |

pCloud's file links answer with no `Access-Control-Allow-Origin` (checked
on a band's recording: `206`, `audio/mpeg`, Range yes, CORS no). A page can
play them, but Web Audio gets silence from them: no transposing, no stems
in the browser for the band's recordings unless the audio comes through us.

## 1. AirPlay

- **Already works, from the system**: Control Center on an iPhone or iPad,
  the sound menu on a Mac, sends all of a page's sound to the speaker:
  recordings, click, count-in, the score's synth. Nothing to build.
- **A button in our players**: the standard Remote Playback API
  (`audio.remote.watchAvailability()`, `audio.remote.prompt()`), which
  Safari implements for AirPlay; `webkitShowPlaybackTargetPicker()` where
  it's missing. One button, shown only when a target is there, in the
  practice player, the rec-player and the Score view.
- **What a button sends is the `<audio>` alone**: the click and count-in
  (Web Audio) stay on the phone. So with Click or Count-in on, or a
  transposed recording (below), say to use the system's AirPlay instead.
- **Delay**: AirPlay buffers about two seconds. The speaker plays late, the
  cursor, the bar number and the waveform's playhead don't. A per-device
  "speaker delay" setting, applied to what's drawn (and to the click when
  the system route carries it), as games do for Bluetooth.
- About 1–2 days, with the delay setting.

## 2. Chromecast: different

- **No system route**: Android and Chrome have no "send all sound to the
  Chromecast" like Control Center; Chrome's *Cast… → tab* mirrors a tab
  (all its sound, with lag) and needs nothing from us.
- **The same button covers Chrome on Android**: Chrome implements the
  Remote Playback API for a media element, casting it to a Chromecast. So
  the button above is AirPlay in Safari and Cast in Chrome on Android. Not
  on an iPhone's browsers (Chrome there is WebKit): AirPlay only.
- **What casts is the file's address**: the Chromecast fetches the file
  itself. Our files need a short-lived signed link (no cookie on the
  Chromecast). A pCloud link is good for the address that asked for it: at
  home or in a studio the Chromecast goes out through the same router, so
  likely fine; to check.
- **Everything done in the page stays behind**: click, count-in, A–B loop
  (the sender would have to seek it), transposing, stems. Only play, pause,
  seek, volume and speed reach a Chromecast's default receiver.
- **A full Cast integration** (Google's Cast SDK, a queue for a set list,
  the lock screen of an Android phone) is the Web Sender SDK, Chrome only;
  with our click or stems, a custom receiver (a page of ours the Chromecast
  runs, registered with Google). 3–5 days for the SDK with the default
  receiver; the custom receiver only if asked.

## 3. Transposing a recording as it plays

- **How**: `<audio>` → `MediaElementAudioSourceNode` → an AudioWorklet that
  shifts the pitch → speakers. The `<audio>` keeps streaming the file.
  [Signalsmith Stretch](https://www.npmjs.com/package/signalsmith-stretch)
  (MIT, its own official WASM and AudioWorklet build) shifts by semitones,
  keeps the formants (a voice not turned into a cartoon), and stretches
  time too: it could take over the speed from `preservesPitch`, better at
  50%. SoundTouch.js is the lighter, rougher alternative.
- **Which recordings**: ours (references, uploads, a Guitar Pro file's
  recording). The band's pCloud recordings only through us (the server
  asks pCloud for its own link and passes the file on): no more "the audio
  never goes through the app". Ask the band, or leave them out.
- **One control**: the song page's key already transposes the chords on
  screen (view only). The same − / + transposes the recording with them,
  and the click follows. A capo stays a capo.
- **Quality**: ±2–3 semitones sound natural, beyond that it shows.
- **To check**: iOS suspends an AudioContext when the screen locks, and
  the rec-player plays from the lock screen. Transposed, it may stop when
  locked; untransposed, it stays the plain `<audio>` it is now.
- About 3–5 days.

## 4. Without the voice

- **The old trick** (left minus right: what's in the middle goes) takes
  the bass and the kick with the voice, leaves the reverb, ends mono. No.
- **Separating the parts** (stems), done once per recording on the server
  and kept: [Demucs](https://github.com/facebookresearch/demucs) v4 (MIT):
  vocals, drums, bass, the rest. A four-minute song takes a minute or two
  on a server's CPU, in a queue; four Opus stems, about 11 MB.
- **Playing stems**: the four decoded in Web Audio, started together (four
  `<audio>` would drift), each with its volume and mute: "No voice", "No
  bass", "Drums only". The Mixer already lays out lanes.
- **Paid alternatives**: Music.AI (Moises), LALAL.AI, AudioShake, per minute;
  better guitar models. In the browser (Demucs in WASM): too slow on a phone.
- **Rights**: a band's own recordings and a teacher's are theirs. A
  commercial recording (a YouTube reference) separated and kept on our
  server is a copy of a work: for the person or band who added it, never
  shared further.
- About 1–2 weeks: the queue, the storage, the stem player.

## 5. Without the guitar solo, or the bass

- **Bass**: Demucs's bass stem is good. "No bass" is the other three.
- **Guitar**: Demucs's six-part model (`htdemucs_6s`) adds a guitar and a
  piano stem; the guitar is the weakest of its parts. Lead versus rhythm
  guitar (the solo out, the rhythm in) only exists in paid models (Moises'
  *Lead & Rhythm*; whether their API offers it, to check).
- **The solo without a lead/rhythm model**: a solo has its sections. With a
  guitar stem, mute the guitar in the song map's *Solo* sections only: the
  rhythm stays everywhere else. Our structure makes this cheap once stems
  exist.
- **A Guitar Pro file** needs no separation: its tracks are apart. The
  Score view can mute a track of alphaTab's synth ("No guitar"), as Pickup
  Music does. The file's own recording is a mix: that one needs stems.

## Order

1. The cast button (Remote Playback API: AirPlay in Safari, Cast in Chrome
   on Android) and the speaker delay. 1–2 days.
2. Muting a Guitar Pro track in the Score view. 1 day.
3. Transposing our recordings with Signalsmith Stretch, on the key's − / +.
   3–5 days.
4. Demucs stems on the server, the stem player; then the solo muted by
   section. 1–2 weeks.
5. The Cast SDK and a receiver of our own, if a band asks.

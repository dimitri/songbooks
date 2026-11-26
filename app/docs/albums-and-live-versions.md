# Albums and live versions — proposal

Status: **proposed, nothing built** (2026-11-26). The request, for
Other Lies (band 15, original music):

> The main songbook can use the YouTube references and the album audio
> (we need album artwork too, so make it 3 songbooks named per album,
> each with its own cover and metadata — or rather invent a new concept
> of an album). The set lists use a live version of the songs: these are
> a different arrangement.

**The proposal, in one paragraph.** An **album** becomes a thing of its
own, owned by a band: a title, a kind (album, EP, single), a release
date, its cover artwork, a label, credits and links (Bandcamp, Apple
Music, YouTube), and an ordered **track list** of the band's songs, each
track with its printed title, duration and ISRC, and its audio: the
master from the band's recordings folder, or a file uploaded. The band
keeps **one songbook**: on a band with albums it reads album by album,
in track order. An album gets its own page (cover, tracks, *Play the
album* in the app's player, PDF with its cover). A song shows where it
comes from ("From Ultrablanc (2026), track 3 · also a single"). The way
the band plays a song live is a **live version**: a version of the album
song made with the versions mechanism we already have (`MakeVersion`,
`forked_from_id`), named "Live" and marked live, with its own chart, its
own reference (a live video or one of the band's concert recordings),
its own click and sync. A band setting, *Set lists use the live version
when there is one*, makes set lists pick it; the songbook shows the album
song with the live version one tap away.

Mockups (Other Lies' look, its real album titles, years, durations and
artwork links; dark, phone 390 and desktop 1440): see §8.

## 1. What the app has (on `review`, 2026-11-26)

**Albums, already, as text.** `works.album` (migration 0033) is a
free-text album on the *work*; `band_albums` (0034 as `project_albums`,
renamed in 0041) keeps, per (band, album name), a `cover_url`
(hotlinked, never downloaded), a `release_year` and a `credit`. Its only
rows are Other Lies' three albums. The band's Repertoire
(`handleBandCatalogPage`, `groupByAlbum`, `band_catalog.html`) groups an
*original* band's songs by `works.album`, albums and songs sorted
alphabetically ("no per-song track-order or release-year signal is
stored"). Nothing else reads it: no album page, no track order, no
album audio, no link to a recording.

**Band songs and the band songbook.** A band owns songs
(`songs.owner_band_id`, one owner per song, `public` or not — the
ownership model, `docs/ownership-model.md`) and has a default songbook
(`songbooks.owner_band_id`, `is_default`). A songbook entry is the
band's own song or a link, with its position and key.

**Versions (arrangements).** `MakeVersion` (store/versions.go) makes
an owner's version of a song: a new song, private, owned by the owner,
`forked_from_id` the original, an optional `version_name` (80 chars),
its content **and media** copied (`copySongMedia`: the GP file, every
`song_audio` row with its markers). On a band's own song (*New version
from this*) the version is a new row next to the original in the band's
songbooks. The version records the original's chart it started from
(`base_body`, 0117): the original's later changes are offered to it
(*Take its changes*). The songbook shows one row per version ("Bad
Spell — psychedelic").

**Set lists.** `setlist_items` (0115): headings and song lines, each
line a `variant_id` — "a specific arrangement when the band has
several", picked per line (*⋯ › Version*). Gigs (`setlist_gigs`), click
track notes per line (`setlist_clicktrack_notes`). The set list's
running-order page plays each line: *Our recordings | Reference*
(docs/band-recordings.md).

**Audio of a song** (`song_audio`, one row per song and category):
`youtube` (the reference video's sound, fetched by yt-dlp — since 0140 a
background job per song and link, `song_youtube_audio_jobs`, queued by a
trigger whenever `reference_url` changes), `band` (the band's own
recording, uploaded — *Ours*), `gp` (a Guitar Pro file's backing). Each
has its sync markers, stems (0078), analysis (0106) and the song's
recording fit (0116). Uploads of WAV are transcoded to MP3
(`audiotranscode`, ffmpeg). The click, the sync, the parts and the
melody all start from the song's audio.

**Band recordings** (0075, docs/band-recordings.md): the band's pCloud
folder indexed, nothing copied; each file's kind from its top folder
(`Albums` → kind `album`, `Concerts`, `Set List` …), its session date,
its place (`position`), its song (`variant_id`); ½–5 stars; one
recording chosen as *the band's reference* per song
(`band_song_recordings`). Played through the app
(`/recordings/{id}/audio`) for who may hear it; public with a public
song, or one by one (0117, "public band recordings", #150).

**The player** (#130): the persistent player and the playlist page
(`/songbooks/{id}/playlist`): a songbook's songs back to back, each
song's chosen recording, its cover as the lock-screen artwork
(`work_covers`, docs/covers.md: iTunes, Deezer, the YouTube thumbnail).

**Covers and looks.** `work_covers` per work; a songbook banner is a
mosaic of four covers; `brand.Derive` turns covers into a band's look.
Other Lies has its own preset (`other-lies`: ink blue, ice and rose,
Fraunces and Space Grotesk, pills) and its logo from otherlies.fr
(#139).

**Rights** (band roles, #179/#184, docs/band-roles.md):
`edit_songbook` (songs, versions, songbooks), `publish` (make songs,
songbooks, recordings public), `manage_setlists`, `manage_recordings`,
`edit_band` (what the band plays, tagline, website, language), …

**Exports.** `/bands/{id}/export-songbook.pdf`, set list PDFs;
`pdfnative.FrontMatter` has a cover title, band name, date and venue —
no image.

## 2. What Other Lies has (production, 2026-11-26; ids and titles only)

- Band 15, `music_type` original, look `other-lies`. **30 songs**, all
  the band's own, private, no versions, **no reference link on any of
  them** (no `reference_url`, none in a chart), one `gp` audio. One
  songbook (id 3).
- `works.album`: 10 songs each on Beyond Fur, Surfacing, Ultrablanc;
  `band_albums`: the three, with Apple Music artwork (1200 px) and
  years 2020, 2022, 2026.
- Recordings folder: 131 files of kind `album` (the masters: `Albums/1 -
  Beyond Fur`, `Albums/2 - Surfacing`, `Albums/3 - Ultrablanc/Final Mix
  Thomas Baudon Octobre 2025`, and the mixes and masters on the way
  there), 106 concert files (7 concerts, 2023-12 to 2026-04-10), 548
  rehearsal files, 49 in `Set List 2025` — the songs as the band plays
  them in its set (`Set List 2025/`, `…/set liste Supersonic/`) and as
  the album has them (`…/Versions Album/`). No recording is chosen as a
  song's reference yet; none is public.
- **Track order is in the file names**: `Other Lies - Beyond Fur - 01 -
  The Incense of Valletta.wav`, `03 Other lies-UltraBlanc-Bones_90_Mix.v5
  FR-9W1-25-34056.wav` (tempo 90, ISRC FR-9W1-25-34056).
- Set lists: 3 (ids 8 "Set List 2025", 9 "Bastille (Live) — 2 Oct 2026",
  11 "Vasarely — 27 Sep 2024"), 35 song lines, 17 different songs.
- Production's `schema_migrations` read 139 at the time of the query
  (the YouTube audio jobs, 0140, not there yet).

**otherlies.fr and Bandcamp** (public pages): three albums and four
singles; the site shows no artwork, Bandcamp has it all:

| Release | Kind | Date | Tracks | Artwork (Bandcamp, full size) | Notes |
|---|---|---|---|---|---|
| Beyond Fur | album | 2020-04-09 | 10 · 45 min | `https://f4.bcbits.com/img/a1891351164_10.jpg` | all rights reserved |
| Surfacing | album | 2022-04-09 | 10 · 37 min | `https://f4.bcbits.com/img/a0258436060_10.jpg` | concept album (Actaeon); mixed by Adrien Garbar, mastered by Raphael Jonin |
| Teens 4 Today | single | 2025-04-25 | 1 | `https://f4.bcbits.com/img/a0612297811_10.jpg` | lead single of Ultrablanc |
| Voiles | single | 2025-07-04 | 1 | `https://f4.bcbits.com/img/a3648692036_10.jpg` | |
| Bones | single | 2025-11-21 | 1 | `https://f4.bcbits.com/img/a1199860227_10.jpg` | |
| Vanity Rewinds | single | 2026-01-23 | 1 | `https://f4.bcbits.com/img/a2348710439_10.jpg` | |
| Ultrablanc | album | 2026-04-10 | 10 · 37 min | `https://f4.bcbits.com/img/a3454742128_10.jpg` | Eyelash Records; mixed by Thomas Baudon, mastered by Raphael Jonin, sleeve Sik Daren; CC BY-ND 4.0 |

Links: `otherlies.bandcamp.com/album/{beyond-fur,surfacing,ultrablanc}`,
`/track/{teens-4-today,voiles,bones,vanity-rewinds}`; Apple Music
artist page; YouTube channel `@otherliesofficial` (one live clip linked
from the site: a Short from Supersonic, 2026-04-10). No per-track
YouTube links were found: they are for the band to give (§11).

## 2b. Albums as songbooks? (the alternative)

"3 songbooks named per album, each with its own cover and metadata":
a songbook per album (a new `songbooks.kind = 'album'`, or plain
songbooks), its entries the album's songs in track order (links to the
band's own songs), plus columns for the cover and the metadata.

| | 3 songbooks | An album |
|---|---|---|
| Order, PDF, playlist, public flag, page | reused as they are | reused: the album page calls the same player, PDF and access code with the album's tracks |
| Cover, year, label, credits, links | new columns on *every* songbook, empty on all but these | the album's own |
| Per track: printed title, duration, ISRC, the master | nowhere to put them (an entry has a position and a key) | the track's own |
| A song on a single *and* the album | two songbook entries, no "track 9 of" | two tracks, one song |
| What the band plays vs. what it released | mixed: an album songbook holds songs the band no longer plays, and a track that's no song (Skins, an unreleased master) | apart: the songbook is the repertoire, albums are releases |
| Set lists | pick from the owner's songbooks: 4 songbooks to keep in step | unchanged: one songbook |
| The band's pages | 4 songbooks in the songbook picker | one songbook, an Albums tab |

**Why not:** a songbook is what a band plays and arranges (its keys, its
versions, its set lists); an album is what it released, fixed, with its
recording. Folding one into the other gives every songbook album fields
and every album songbook-ness it doesn't need (keys, lessons, links to
others' songs), and leaves the per-track audio and order without a
home. **What we keep from it:** the album page is built from the same
pieces (the songbook's track list rows, the playlist player, the PDF
export), so it costs little more than three songbooks would.

## 3. The album

- **Owned by a band** (a person's or a community's albums later, the
  same table with an owner kind). Edited by whoever may edit the band's
  songbook (`edit_songbook`); made public with `publish` (§6).
- **What it holds:** title; kind (album, EP, single, live album);
  release date (or a year alone); cover artwork (a URL, hotlinked like
  `band_albums.cover_url`; an upload later, §11); its colours
  (`covers.Palette` on the artwork, for the page and the player); label;
  credits (free text); licence; links (Bandcamp, Spotify, Apple Music,
  Deezer, YouTube playlist, other), each a kind and a URL.
- **Tracks**, in order (disc, position): the band's song (its album
  version, never a live version), the title as printed on the release
  (Bandcamp's "Colors of Thunder" vs. the song's "The Color of
  Thunder"), duration, ISRC, where its master is (a file in the band's
  recordings folder), a link (the track's Bandcamp page or video). A
  track may have no song: an interlude, a hidden track, a song the band
  never put in its songbook (Skins).
- **A song on several releases** (a single, then the album): one song,
  two tracks. The song names its album first, its singles after.
- **Album audio**: the track's song's own recording (`song_audio`,
  category `band`, *Ours* — on an original band's own song, *Ours* is
  the album). Filled by an **import from the band's folder**: pick the
  master (or let the app propose the file numbered like the track in
  `Albums/N - Title/`), the app fetches it once from pCloud, transcodes
  it (WAV 40–80 MB → MP3 ≈ 5 MB, `audiotranscode`) and keeps it, and
  remembers which file it came from. Or by an upload, as today. Why copy
  what the folder already has: the click, the sync, the stems and the
  analysis need the audio in the app, and a phone on 4G can't stream a
  70 MB WAV. A deliberate exception to "nothing is copied", for a
  handful of files the band chose (§11).
- **The YouTube reference** of an album song is the official upload of
  that track (an art track or a video), as for any song. Its audio is
  fetched by the YouTube audio job (0140) as for any song. Since it is
  the same recording as the master, a sync made on one holds on the
  other when their lengths agree within a second; the page uses the
  album audio first and the YouTube one when there is no master.
- **The cover of a song on an album is the album's**, before
  `work_covers`' lookup (the songbook banner, the player's artwork).

## 4. Live versions

- **Made with the versions we have.** On an album song, *Make the live
  version* = `MakeVersion` (New version from this) named "Live", with a
  new mark on the song, `live` (so renaming it "Live 2025" or "Live —
  acoustic" keeps it the live version), and three differences from a
  plain version:
  1. the album's audio is **not** copied (it's not how the band plays
     it live): `copySongMedia` skips `song_audio` for a live version;
  2. it asks for **its own reference**: a live video (YouTube — Shorts
     included, `is_youtube_video_url`), or one of the band's recordings
     of the song (concerts, `Set List` folder, rehearsals: the best rated
     first), which becomes the live version's chosen recording
     (`band_song_recordings` is per song: the live version has its own);
  3. its chart starts as the album's, to change: the live intro (Other
     Lies opens with a "Bells of the live" tape at 120 bpm), the longer
     outro, another tempo.
- **It follows the album song's fixes**: a chord fixed on the album
  song is offered to the live version (*Take its changes*, 0117), as to
  any version.
- **Recordings:** the folder's concert and set-list recordings of a
  song with a live version belong to its live version (the song
  matching in `parseRecording` picks the live version for kinds
  `concert` and `setlist`, the album song for `album` and `studio`;
  rehearsals stay on the album song unless the band says otherwise —
  §11).
- **One live version per song** has the mark (a second "Live 2027"
  takes it over when marked).
- **The click, the sync, the YouTube audio job — per version, as
  today.** Each version is a song: its tempo is its chart's, its click
  notes are per set-list line, its sync markers are on its own audio,
  its YouTube job is for its own `reference_url`. The album song's
  click is the album's tempo (from the masters' names: Bones 90,
  Teens 4 Today 127), its sync on the album master; the live version's
  click its live tempo, its sync on the live video or recording, its
  recording fit (0116) read against the live recording.

## 5. Pages

**Band › Albums** (a new tab on an original band, next to Songbook):
the releases, newest first, as cover tiles; singles after the albums;
*New album* for `edit_songbook`.

**The album page** (`/bands/{id}/albums/{albumID}`): the cover large
(over a blur of itself, as the player does), kind and year, title, the
band, the date, label, track count and length; *Play the album* (the
persistent player, #130, the tracks in order, the cover as the
lock-screen artwork), *Shuffle*, *PDF*, *Edit*; the links; *Public*.
The tracks: number, title (the printed one, the song's under it when
they differ), the **Live** chip when the song has a live version,
tempo, duration. A row opens the song. On the side: the credits, where
the audio comes from, more releases.

**Editing an album**: the fields above; the cover by URL (a preview,
its colours); tracks added from the band's songs (or a title with no
song), reordered, each with its master picked from the folder (the file
numbered like it proposed), duration and ISRC read from the file name
when it has them. *Read the folder* fills a new album from
`Albums/N - Title/`: one track per numbered file, the song each file
already matched.

**The band's songbook, by album** (on a band with albums): *By album*
| *A–Z* | *Most played*. By album: each album as a section (its small
cover, kind, year, track count and length, *Play*, *Album page*), its
songs in track order; then *Not on an album*. A live version is not a
row of its own: its album song shows the **Live** chip, which opens it.
Filter chips: each album, *Live* (the songs with a live version),
*Not on an album*. A–Z keeps one row per song (live versions folded
the same way).

**A song page**: under the title, *From Ultrablanc (2026), track 3 ·
also a single, 21 Nov 2025* (the cover, a tap opens the album); an
**Album | Live** switch opening the other version (shown when the song
has a live version; on the live version, the same switch, Live on). The
recordings it plays: the album audio, the YouTube reference, the band's
chosen recording — on the live version, its live reference and the
band's live recordings.

**A set list**: lines are versions, as today. A band whose set lists use
live versions sees each line's **Live** chip, a line without one marked
*Album · Make the live version*. The player's *Our recordings |
Reference* becomes **Live | Album** on such a band: Live plays each
line's version with its recording; Album plays each line's album song
(the version's `forked_from_id`) with the album audio. *Use the live
versions here* on a set list made before: every line whose song has a
live version moves to it, one change in the set list's history.

**The band setting** (Settings › The band): *Set lists use* — **the
live version when there is one** | the song itself. New lines (the add
dialog, quick add, a copied set list) pick the live version; the add
dialog lists album songs once, with the chip. Default: the song itself
(nothing changes for a band that doesn't ask).

## 6. Rights, public, exports

- **Who edits albums:** `edit_songbook` (albums are the band's songbook
  material: songs, versions, now releases). Making the live version:
  `edit_songbook` (it's a version). The *Set lists use* setting:
  `edit_band` (Settings › The band). Using live versions in a set list:
  `manage_setlists`. Picking and importing a master: `edit_songbook`
  (it writes the song's audio; the folder itself stays
  `manage_recordings`').
- **Public:** an album can be public (`publish`): anyone signed in
  sees its page, its cover, its tracks; a track opens its song if the
  song is public. *Make the album public* offers to make its songs
  public too, with their album audio — not their live versions, which
  stay the band's. A public album is the band's showcase on its public
  card (`/for/bands`-style listing later). Logged-out visitors: not
  until the public catalog (docs/public-catalog.md) exists; the
  licences differ (Ultrablanc CC BY-ND, the others all rights
  reserved), the band decides per album.
- **PDF:** `/bands/{id}/albums/{albumID}/export.pdf`: a cover page with
  the artwork (fetched at export time and cached by URL; `FrontMatter`
  gains an image), the album's title, the band, the year, the track
  list with the printed titles, then the songs in track order; *with
  the live versions* prints those instead. The band songbook's PDF
  orders by album when it is shown by album.

## 7. Data model (sketch, migrations numbered when built)

```sql
-- An album (or EP, single, live album) a band released.
CREATE TABLE albums (
  id            bigserial PRIMARY KEY,
  band_id       bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  title         text NOT NULL,
  kind          text NOT NULL DEFAULT 'album' CHECK (kind IN ('album', 'ep', 'single', 'live')),
  released_on   date,                      -- or only the year:
  release_year  int,
  label         text NOT NULL DEFAULT '',
  credits       text NOT NULL DEFAULT '',
  licence       text NOT NULL DEFAULT '',
  cover_url     text NOT NULL DEFAULT '',  -- hotlinked, as band_albums.cover_url
  cover_palette jsonb NOT NULL DEFAULT '[]',
  links         jsonb NOT NULL DEFAULT '[]', -- [{"kind": "bandcamp", "url": "…"}]
  public        boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX albums_band_title_kind ON albums (band_id, lower(title), kind);

-- Its tracks, in order: the band's song (its album version), as printed.
CREATE TABLE album_tracks (
  id           bigserial PRIMARY KEY,
  album_id     bigint NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  disc         int NOT NULL DEFAULT 1,
  position     int NOT NULL,
  song_id      bigint REFERENCES songs(id) ON DELETE SET NULL,
  title        text NOT NULL,
  duration_s   int,
  isrc         text NOT NULL DEFAULT '',
  recording_id bigint REFERENCES band_recordings(id) ON DELETE SET NULL, -- its master in the folder
  link_url     text NOT NULL DEFAULT '',
  UNIQUE (album_id, disc, position)
);
CREATE INDEX album_tracks_song_idx ON album_tracks (song_id) WHERE song_id IS NOT NULL;

-- The live version: a version (forked_from_id) marked live.
ALTER TABLE songs ADD COLUMN live boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX songs_one_live_version ON songs (forked_from_id, owner_kind,
  coalesce(owner_person_id, owner_band_id, owner_community_id)) WHERE live;

-- The band's choice for its set lists.
ALTER TABLE bands ADD COLUMN setlist_versions text NOT NULL DEFAULT 'song'
  CHECK (setlist_versions IN ('song', 'live'));

-- Where a song's audio came from, when imported from the folder.
ALTER TABLE song_audio ADD COLUMN from_recording_id bigint
  REFERENCES band_recordings(id) ON DELETE SET NULL;
```

**Moving what's there:** one `albums` row per `band_albums` row (title,
cover, year, credit into credits) and per distinct `works.album` of a
band's songs with none; one track per song, positions by title (as
shown today) until the band orders them (or *Read the folder*). Then
`groupByAlbum` reads `album_tracks`; `works.album` and `band_albums`
are dropped in a later migration, once nothing reads them (`songexport`
writes `workAlbum`: it reads the song's first album instead).

**Store:** `Albums(band)`, `Album(id)` with its tracks,
`SaveAlbum`, `SetAlbumTracks` (a revision, as a set list's),
`AlbumsOfSong(song)` (through `forked_from_id` for a live version),
`ImportTrackAudio(track)` (pCloud → transcode → `SetSongAudio(…,
AudioBand, …)`), `MakeLiveVersion(person, song, ref)` (MakeVersion +
`live`, no audio copied, the reference or chosen recording set),
`LiveVersionOf(song)`, `UseLiveVersions(setlist)`. Access: an album is
seen by the band's members, or anyone signed in when public; its rights
are `HasBandPrivilege(edit_songbook | publish)`.

## 8. Mockups

Self-contained HTML (Other Lies' look; artwork hotlinked from Bandcamp,
nothing downloaded), rendered with Playwright firefox at 1440 and 390,
dark (the band's look is dark), under
`/private/tmp/claude-501/-Volumes-Data-dev-songbooks/5bcc3e49-129c-4cb8-a4d5-063dc0e98574/scratchpad/cap/albums/`
(the HTML beside it, `scratchpad/albums-mock/`):

| File | Shows |
|---|---|
| `album-desktop.png`, `album-phone.png` | The album page: Ultrablanc's cover, facts, *Play the album*, links, *Public*; the 10 tracks with tempo and length, the Live chips, the printed title where it differs, singles noted; credits; more releases; the player playing track 3 |
| `songbook-desktop.png`, `songbook-phone.png` | The band's songbook by album: *By album / A–Z / Most played*, album filters and *Live 17*, each album a section in track order with its cover, *Play*, *Album page*, the Live chips |
| `song-desktop.png`, `song-phone.png` | Bones: "From Ultrablanc (2026), track 3 · also a single", the Album / Live switch, the map and click on the album master, *Plays*: album master / YouTube reference / the band's rehearsal, the live version's card |
| `setlist-desktop.png`, `setlist-phone.png` | Supersonic (10 Apr 2026): the intro cue, each line its live version with its live reference, one line still the album's (*Make the live version*), Live / Album in the player, the band setting, *Use the live versions here* |

The charts in the mockups are bars with slashes: no chord or lyric of
the band's songs.

## 9. Other Lies: the mapping (nothing applied)

**Albums and tracks** (song ids on production; the printed title where
it differs; tempo and ISRC from the masters' file names; masters:
`Albums/1 - Beyond Fur/`, `Albums/2 - Surfacing/`, `Albums/3 -
Ultrablanc/Final Mix Thomas Baudon Octobre 2025/`):

| # | Beyond Fur (2020) | Surfacing (2022) | Ultrablanc (2026) |
|---|---|---|---|
| 1 | The Incense of Valletta (132) 4:58 | Belle of the Ball (111) 3:35 | The Color of Thunder (131) — "Colors of Thunder" 3:50 · 115 · FR-9W1-25-60193 |
| 2 | Hallucinogenic (115) 4:18 | The Bath (130) 3:55 | The Whirlwind of Lovers (135) 3:25 · 108 · FR-9W1-25-60194 |
| 3 | My Favourite Scarlett (120) — "My Favorite Scarlet" 4:44 | Dead End For Delhia (113) — "Dead End for Delia" 4:56 | Bones (112) 2:23 · 90 · FR-9W1-25-34056 |
| 4 | The Obsession with Control (134) 4:18 | Stars on Black (127) 3:23 | Paramount (122) 4:09 · 105 · FR-9W1-25-60195 |
| 5 | Gin Lane (114) 4:07 | Vibrator (138) 2:31 | Holy Burn (116) 3:55 · 80 · FR-9W1-25-60196 |
| 6 | Vague Idols (136) 2:53 | Kawasaki (118) 2:20 | Voiles (139) 3:50 · 95 · FR-9W1-25-33368 |
| 7 | Sandman Sabotage (125) 4:24 | Ayurveda (110) 3:47 | Mottainai (119) 4:05 · 105 · FR-9W1-25-60197 |
| 8 | Real Red Nail Polish (123) 5:34 | Ruin Porn (124) 3:55 | Vanity Rewinds (137) 4:34 · 80 · FR-9W1-25-60198 |
| 9 | Tatooed Indian Robber (128) — "Tattooed Indian Robber", instrumental 5:45 | In Candesenza (117) 3:33 | Teens 4 Today (129) 2:44 · 127 · FR-9W1-25-20861 |
| 10 | Of Human Bondage (121) 3:38 | Sequoia (126) 5:09 | The Last Embrace (133) 7:44 · 80 · FR-9W1-25-60199 |

The singles: Teens 4 Today (129), Voiles (139), Bones (112), Vanity
Rewinds (137), one track each. The masters' folder has an 11th
Ultrablanc track, **Skins** (98 bpm, FR-9W1-25-60200), not on Bandcamp
and not one of the band's songs.

**Live versions to make: 17**, the songs of the band's set lists, with
the live reference the folders suggest:

| Song (id) | Live reference candidate | Concerts recorded |
|---|---|---|
| Belle of the Ball (111) | `set liste Supersonic/01 … 2025-11-24` | 3 |
| Kawasaki (118) | `set liste Supersonic/02` (an MP3 and a video) | 3 |
| In Candesenza (117) | `set liste Supersonic/03 … 2025-11-24` | 2 |
| Bones (112) | `set liste Supersonic/04 … 2025-11-24` | 3 |
| Gin Lane (114) | `set liste Supersonic/05 … 2025-11-24` | 6 |
| Mottainai (119) | `set liste Supersonic/06 … 2025-11-24` | 6 |
| Stars on Black (127) | `set liste Supersonic/07 … 2025-11-17` (and a video) | 3 |
| Dead End For Delhia (113) | `set liste Supersonic/08 … 2025-11-17`; `Set List 2025/` has the album's WAV | 2 |
| The Last Embrace (133) | `set liste Supersonic/09 … Kiosque Square Trousseau` | 3 |
| Vibrator (138) | `set liste Supersonic/R1` | 0 |
| Teens 4 Today (129) | `set liste Supersonic/R2` | 5 |
| The Whirlwind of Lovers (135) | `Set List 2025/02 … 2025-03-31` | 0 |
| My Favourite Scarlett (120) | `Set List 2025/05 … 2025-05-19` | 0 |
| The Obsession with Control (134) | `Set List 2025/10 … 2024-09-23` | 0 |
| Vanity Rewinds (137) | `Set List 2025/11` is the album master | 2 |
| Voiles (139) | `Set List 2025/13 … 2014-12-09` (a date to check) | 0 |
| Of Human Bondage (121) | `Set List 2025/14` is the album master | 0 |

Three of them (Dead End For Delhia, Vanity Rewinds, Of Human Bondage)
have the album's master in the set-list folder: they may be played as
on the album, and need no live version (§11).

**Set lists:** 8, 9 and 11 — 35 lines — move to the live versions
(*Use the live versions here*), once made. The intro tape ("Bells of the
live", 120 bpm, `Set List 2025/00 …` and `set liste Supersonic/00 …`)
is a cue, not a song: today it can only be a song or a heading (§11).

**How it would run:** `cmd/albums --band 15` (dry run: prints the
albums, tracks, masters and live versions it would make; `--apply`
writes them, on staging first): albums from `band_albums` + Bandcamp's
facts above (given in a small JSON file, not fetched by the command),
tracks from the numbered masters, the album audio imported from the
masters, live versions made with the candidates above as their chosen
recordings, the band's setting set to live, the three set lists moved.

## 10. Implementation plan

1. **Albums model**: migration (`albums`, `album_tracks`, the data
   moved from `band_albums`/`works.album`), store, tests; `groupByAlbum`
   on `album_tracks`, in track order.
2. **Albums tab, album page, editor**: cover URL and colours, metadata,
   links, tracks (add, order, printed title, duration, ISRC, master),
   *Read the folder*.
3. **Album audio**: import a master (pCloud → MP3 → the song's `band`
   audio, `from_recording_id`), *Play the album* in the persistent
   player, the album cover as the song's cover.
4. **Songbook by album, song page's album line.**
5. **Live versions**: `songs.live`, *Make the live version* (no audio
   copied, a live reference or recording), the recordings' matching
   (concert/set-list → live version), the Album | Live switch, the Live
   chip and folding in the songbook.
6. **Set lists**: `bands.setlist_versions`, the add dialog's default,
   *Use the live versions here*, the player's Live | Album.
7. **Public albums, the album PDF** (cover image in `FrontMatter`).
8. **Other Lies**: `cmd/albums` dry run, then the band's answers to §11,
   then `--apply` on staging, then production.
9. Drop `works.album` and `band_albums`.

## 11. Open questions

1. **Copy the masters into the app** (an MP3 each, ≈ 150 MB for Other
   Lies' 30 tracks) so click, sync, stems and phones work — or keep
   "nothing is copied" and stream the WAVs from pCloud (no click/sync on
   them, 40–80 MB a track)?
2. **The album audio as the song's `band` ("Ours") audio**, or a new
   category `album` next to it (a band with both an album master and an
   upload of its own)?
3. **The printed titles**: rename the songs to the release's ("My
   Favorite Scarlet", "Dead End for Delia", "Tattooed Indian Robber",
   "Colors of Thunder"), or keep the band's titles and show the printed
   ones on the album only?
4. **Skins**: a track with no song, a new song of the band's, or left
   out of Ultrablanc (it isn't on Bandcamp)?
5. **Singles**: releases of their own (as proposed), or a line on the
   song only ("also a single, Nov 2025")?
6. **The YouTube references**: none of the 30 songs has one. Which
   videos are the album tracks' (art tracks on YouTube Music, official
   videos), and is there a live video per song for the live versions?
   The band to give the links, or the YouTube picker per song?
7. **Which songs get a live version**: the 17 of the set lists, or
   only those played differently (not Dead End For Delhia, Vanity
   Rewinds, Of Human Bondage)?
8. **Rehearsals**: do the rehearsal recordings of a song with a live
   version go to the live version (the band rehearses the set) or stay
   on the album song?
9. **The intro tape** ("Bells of the live", 120 bpm): a cue line in set
   lists (a new kind of line, with its own sound and click), or a song
   of its own?
10. **The *Set lists use* setting**: a band setting under The band
    (`edit_band`, as proposed), or a set-list choice under
    `manage_setlists`? And the default for Other Lies: live.
11. **Artwork**: hotlink Bandcamp's (as the app does with Apple
    Music's today), or allow an upload (kept in `file_blobs`) for a
    band whose covers aren't online?
12. **Public, logged out**: should a public album be the first page a
    visitor without an account can open (the band's showcase), given
    the public catalog isn't built?

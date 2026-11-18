# Demo data

`cmd/sbdemo` turns a copy of production's database into demo data: for
the public website's screenshots, and for review on staging. Production's
structure stays (songs and their variants, songbooks, communities, bands,
who may open what, looks, courses and lessons); every name a visitor
could recognise is replaced by an invented one; and fixtures make the
coaching and calendar features look alive.

```
sbdemo anonymize --db URL --confirm DB@HOST
sbdemo fixtures  --db URL --confirm DB@HOST
```

`--db` defaults to `$DATABASE_URL`. Run `anonymize` first, then
`fixtures`. Both print how many rows they changed, table by table, and
never a name.

## The guard

sbdemo rewrites a whole database, so it refuses to run:

- unless the connection's host is `localhost`, `127.0.0.1` or `db`
  (staging's compose database); any other host is refused outright;
- unless `--confirm` names that database and host exactly, as
  `songbooks@db` or `songbooks@localhost`;
- where `SONGBOOKS_WEBAUTHN_RP_ID` is production's hostname
  (`app.songbooks.studio`): production's compose database is called `db`
  too, on the same machine. On host `db`, that variable must be set (to
  staging's `songbooks.tapoueh.org`) for it to run at all;
- when the server it reaches isn't the database confirmed
  (`current_database()`).

## On staging

After restoring a production dump into staging (`deploy/restore-db.sh
staging DUMP`), with the image that has the sbdemo binary (built like the
other admin tools, `deploy/Dockerfile`):

```
docker compose exec -T app sbdemo anonymize --confirm songbooks@db
docker compose exec -T app sbdemo fixtures --confirm songbooks@db
docker compose restart app
```

The restart matters where `SONGBOOKS_DEV_AUTO_LOGIN` is set: the app loads
that person once, at start, and would keep greeting them by their old
name.

## Locally

Against the local stack's database (`deploy/local-restore.sh` restores a
production dump into it), from `app/`:

```
DB='postgres://songbooks:songbooks-local@localhost:55432/songbooks?sslmode=disable'
go run ./cmd/sbdemo anonymize --db "$DB" --confirm songbooks@localhost
go run ./cmd/sbdemo fixtures --db "$DB" --confirm songbooks@localhost
docker compose -f deploy/docker-compose.local.yml restart app
SBDEMO_TEST_DB="$DB" go test -run TestAnonymizedDatabase -v ./cmd/sbdemo
```

The last line checks the result, read-only: no lyric line but the
invented ones, no reference link, no real community name, no logo.

## What anonymize changes

| Table | What happens |
|---|---|
| people | invented display names (French and English, from a fixed list, by id), usernames `demo-<id>`, emails `<username>@example.invalid` (none when there was none), no nickname, photo, phone, Gravatar or password. `tapoueh` (the local auto-login) keeps its username and password and becomes **Lucas Mercier**; Studio Lumen's coach becomes **Inès Marchal**. |
| communities | Bands Camp → **Jam du Canal**, Florent Passamonti → **Atelier Blues Rive Gauche**, Chloé Paul → **Studio Lumen**, JamSpace → **Salle Bleue Jam**, any other an invented name; a new look each (`SetBrand`: colours, fonts, a two-letter mark, a French tagline, no logo, no website). Custom role names → "Rôle N". |
| bands | invented names (The Paper Lanterns, Velvet Hour, Nord Express…) and looks; their cloud recording sources (and so their recordings' list), recording fixes, album covers and proposals removed. |
| works, songs | every variant an invented title ("Night Train to Arles", "Cobalt Morning", "Lanterns at Low Tide") and artist (a band of the app by its new name, or one of the invented bands); the body's `title:`/`artist:` lines rewritten, every lyric line, comment and `note:` line removed (`importer.WithoutLyrics`), `reference:` and `cover:` lines removed, section labels carrying words renamed after their kind; chords, grids, sections, tempo, key, time and Guitar Pro references stay. `songs.reference_url` and `song_audio.source_url` cleared (the recordings, their parts and markers stay). Album names and found covers removed. Eight songs (five of Studio Lumen's, three of Jam du Canal's) get invented lyrics. |
| song_gp_source_file | each Guitar Pro file's score header (title, artist; album, authors, copyright, page headers and footers emptied), its lyrics and free texts emptied, the file renamed; older versions removed. Notes, tracks and the embedded recording stay. |
| songbooks, lessons | a community's main songbook takes its new name, a course becomes "Méthode Blues, volume 1/2", a student's "<name>’s songs"; other names that say more than "Songbook" an invented one; playlists cleared. Lessons get technique titles ("Shuffle en la", "Gamme pentatonique, position 2"…), no text, no video. |
| songbook_files | PDFs removed (a course's book carries its author's name on every page); Guitar Pro files rewritten as above; every file named after its song or lesson, folders after their lesson. |
| setlists, setlist_gigs, events | invented titles, venues and names; notes and descriptions cleared. |
| coaching | teaching notes and student notes invented, range notes cleared, group names "Groupe N". |
| private | sessions (but tapoueh's), sign-in tokens, passkeys, calendar feed secrets, the outbox, notifications, revisions (old bodies, lyrics included), search queries, annotations' text. |

Not changed: the iReal Pro library (`ireal_*`, a public catalogue used by
the import wizard), the recordings' audio and the files' audio (a
recording can be recognised by ear; its tags are kept), instrument lists.

## What fixtures makes

All invented, found again before being made (a person by username, a
group by name, an event by owner, kind, name and start, a set list by
band and title, a note by its text): a second run only adds what's
missing, the coming weeks for instance.

- **Studio Lumen** (coaching): thirteen students (`demo-student-NN`, no
  email, no password), each with a voice (chest, mixed and head ranges:
  sopranos, mezzos, altos, tenors, baritones, overlapping), a songbook of
  3 to 5 of the community's songs each in their own key, a profile and a
  teaching note or two. Groups: **Mardi soir** and **Jeudi midi** (the
  school year, September to June), and **Master class de printemps**
  (one-off, around a master class event in March, eight students). Three
  students have individual lessons. Lucas Mercier is in Mardi soir, with
  his own voice, songs and note.
- **The next eight weeks** (Europe/Paris, holidays left out): Tuesday
  19:00 group lessons in the room, Thursday 12:30 online, individual
  lessons (in the room and online), a master class; Jam du Canal's open
  mic every other Friday at La Péniche Rouge and its jam the Fridays
  between at Le Quai 9; weekly rehearsals for two of Lucas's bands and a
  gig each with a set list of the band's songs; an Atelier Blues live
  Q&A online.
- Lucas Mercier is a member of Studio Lumen, Jam du Canal and Atelier
  Blues Rive Gauche, so his home page's *Coming up* mixes lessons, jams,
  rehearsals and gigs.

# Covers and the look of a repertoire

A song gets its cover art, a songbook gets a banner made from its songs'
covers, and a band or community can take the look of its songs as its own.

## How players do it

- **Finding covers.** Players without a library of their own look a song up
  by artist and title in a public catalog. beets' `fetchart`, Navidrome,
  Jellyfin and Plex try several sources in turn: the Cover Art Archive
  (MusicBrainz), iTunes, Deezer, Spotify, Last.fm, fanart.tv. Only some need
  no key or account: iTunes Search, Deezer and the Cover Art Archive.
  MusicBrainz allows one request a second, and often answers "busy".
- **Crediting.** The cover links to the album's page in the store it came
  from, and the image stays where it is, at the source's address.
- **A playlist's cover.** Spotify draws a playlist without its own picture as
  a mosaic of its first four albums. Apple Music and YouTube Music blur the
  playing album under it, to fill the screen.
- **A screen's colors from a cover.** Apple Music's catalog gives each
  artwork a background and four text colors. Spotify's playlist headers
  fade from the cover's main color. Android's Palette and node-vibrant
  pick six swatches (vibrant, muted, and a dark and light version of
  each) out of an image's main colors.

## What the app does

- **Finding** (`internal/covers`). It searches iTunes first: no key, artwork
  up to 3000 px, and the album's genre. It skips compilations ("Various
  Artists") and puts albums that aren't the song's first home after the
  others: live, sessions, best-of, remaster, deluxe. Among the rest, the
  earliest year wins. Then it tries Deezer, asking whose each album is to
  skip compilations. Last comes the thumbnail of the song's YouTube
  reference. iTunes allows about 20 searches a minute: songs are looked up
  one every 3 s, and a refusal is waited out once.
- **Colors** (`covers.Palette`). The image is sampled about 100×100 and
  median-cut into six boxes of color. Each box gives a swatch: its mean
  color and its share of the image.
- **Kept** per work (`work_covers`, migration 0076), shared by every version
  of a song. What's kept is the image's address, the album, its link, year
  and genre, and the swatches. A miss is kept too, so a song is looked for
  once. `sbfiles covers --again` looks again at the songs that got no album
  cover.
- **When.** Opening a songbook looks up, in the background, the covers of
  its songs not looked for yet. `sbfiles covers --songbook ID` does it from
  the command line.

## The look of a repertoire (`brand.Derive`)

- **Accent.** The covers' swatches go into 24 hue bins of 15°, weighted by
  share and chroma (greys don't vote). The strongest bin, averaged with its
  neighbours, is the accent's hue.
- **Second accent.** The next strong bin at least 60° away, else a
  neighbour of the accent.
- **Dark or light.** The covers' mean OKLCH lightness picks one. The
  neutrals (page, cards, header, text) take the accent's hue at low chroma.
- **Readable.** The accent's and second accent's lightness move until every
  pair the app uses reads at WCAG AA (4.5:1): text and secondary text on the
  page, the label on an accent button, the accent on the page, the tagline
  on the header.
- **Fonts and buttons, from the genres.**
  - Blues, soul, jazz, folk, country: Fraunces over Work Sans, rounded.
  - Rock, metal, punk: Anton over Inter, square.
  - Pop, funk, disco, dance, reggae: Space Grotesk over Inter, pills.
  - French pop and chanson: Playfair over Work Sans.

It shows in three places:

- **A songbook's banner**, once four covers are found:
  - a mosaic of the four covers that go best with the look, one per artist,
    album covers before video stills;
  - the band's name (or the songbook's), the genres and the most present
    artists;
  - a "Play the songbook" button and a strip of the covers' colors.

  It wears the look's colors and title font whatever the page's brand. Each
  song in the list shows its cover too.
- **The playlist.** The song's cover, sharp over a blur of itself, is the
  "now playing" picture and the lock screen's artwork.
- **The brand form.** "From our songs" fills the colors, fonts and buttons
  from all the band's (or community's) songbooks. It keeps the logo, tagline
  and website, and saves nothing until saved.

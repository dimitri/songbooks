-- The title of the video or page a song's reference links to (the
-- "reference:" line of its body), for the song page's header and its
-- Reference view: fetched server side (internal/reftitle) when the link is
-- saved, else in the background the first time the song is opened.
-- reference_title_url is the link the title is for: a song whose link
-- changed since has no title until it's fetched again. reference_title_at
-- is the last try, so a link without a title isn't asked again at once.
ALTER TABLE songs
  ADD COLUMN reference_title text NOT NULL DEFAULT '',
  ADD COLUMN reference_title_url text NOT NULL DEFAULT '',
  ADD COLUMN reference_title_at timestamptz;

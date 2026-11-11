-- What a reference recording is, for the song listings and the player:
-- 'full' (the song as performed, every part playing), 'backing' (a
-- play-along track with the part to practise left out — Guitar Pro
-- lesson files ship these as "… PB.mp3"), or '' when nobody has said.
ALTER TABLE song_audio ADD COLUMN content_kind text NOT NULL DEFAULT ''
  CHECK (content_kind IN ('', 'full', 'backing'));

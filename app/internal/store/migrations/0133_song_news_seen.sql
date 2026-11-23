-- The changes in a person's songs, listed on their home page instead of
-- notifications (store/song_news.go): until when they marked the list
-- seen. NULL: never (the list goes back store.SongNewsWindow).

ALTER TABLE people ADD COLUMN song_news_seen_at timestamptz;

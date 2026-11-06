-- An event's own dedicated songbook (the pool of material being
-- prepped for it) alongside its existing setlist (the ordered final
-- running order built FROM any catalog/songbook material) -- two
-- genuinely different things: a songbook is an editable collection,
-- a setlist is a performance sequence. Nullable: an event can exist
-- (and be managed) without ever getting a dedicated songbook, same as
-- it already can without a setlist.
ALTER TABLE events ADD COLUMN songbook_id bigint REFERENCES songbooks(id) ON DELETE SET NULL;

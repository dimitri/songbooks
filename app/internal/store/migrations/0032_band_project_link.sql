-- A band that corresponds 1:1 to a legacy songbook project (e.g. the
-- "Mercy" band and the v2/Mercy project) can link directly to it, so its
-- Repertoire tab shows that project's real song list live instead of a
-- separately maintained band_catalog_entries copy. NULL (the default) is
-- the normal case: an unlinked band's repertoire stays exactly what it
-- was — pointers added/forked by hand via band_catalog_entries.
ALTER TABLE bands ADD COLUMN project_id bigint REFERENCES projects(id);

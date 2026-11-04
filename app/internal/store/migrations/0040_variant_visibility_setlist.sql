-- New variant_visibility value for a setlist-local edit: editing a song
-- from within a setlist defaults to forking a variant scoped to that one
-- setlist, rather than touching the original (see migration 0041's
-- owner_setlist_id column, added separately since Postgres won't let a
-- new enum value be used by DML in the same transaction that adds it —
-- same lesson migrations 0011/0014/0024 already learned).
ALTER TYPE variant_visibility ADD VALUE 'setlist';

-- A community songbook is shared or private, and either may be sold
-- (requires_grant). A shared songbook's songs may be copied — into
-- someone's own songbook, a band's, another community's — by whoever can
-- open them. A private songbook's songs can be opened, played and, with
-- download_files, downloaded, but not copied: an author's lessons stay
-- the author's (CanCopyVariant). Existing songbooks stay shared, as they
-- were; meaningless for a person's or a band's songbook.
ALTER TABLE songbooks ADD COLUMN shared boolean NOT NULL DEFAULT true;

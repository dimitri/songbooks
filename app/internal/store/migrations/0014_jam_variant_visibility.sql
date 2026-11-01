-- Phase 4 (final phase of the data-model rewrite): jam-scoped variants.
-- Adds 'jam' as a new value on the existing variant_visibility enum, same
-- pattern migration 0011 used to add 'band' — split into its own
-- migration file because ALTER TYPE ... ADD VALUE cannot run in the same
-- transaction as a DML statement (or any statement) that references the
-- new value. See migration 0015 for the owner_jam_event_id column and
-- section_annotations table that actually use it.
ALTER TYPE variant_visibility ADD VALUE 'jam';

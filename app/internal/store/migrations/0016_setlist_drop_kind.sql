-- "Setlist kind is not needed, it was [just used to] explain how they'd
-- work at different attachment points (band, profile)" — the user's own
-- words. kind (performance/themed/practice) and reusable were explanatory
-- scaffolding for scope, never a real stored requirement; scope
-- (project/person/band) is the distinction that actually matters and
-- stays untouched. setlist_gigs (date/venue/notes per performance) also
-- stays — it's independently useful regardless of any kind label.
ALTER TABLE setlists DROP COLUMN kind;
ALTER TABLE setlists DROP COLUMN reusable;
DROP TYPE setlist_kind;

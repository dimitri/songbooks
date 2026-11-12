-- How a person reads guitar and bass parts: tab, a standard staff, or
-- both. A matter of training and habit, not of the song: each person
-- sees every song's parts their own way ({"guitar": "tab", "bass":
-- "both"}); missing keys mean the usual default (guitar tab, bass both).
ALTER TABLE people ADD COLUMN notation_prefs jsonb NOT NULL DEFAULT '{}'::jsonb;

-- A community's song player with the voice's volume in it, as a mic and
-- a slider beside play (the recording's parts, docs/stems-and-key.md),
-- and its key out in the open with whose it is: for a voice coach and
-- their students. Off everywhere but in Chloé Paul's coaching community
-- (id 7; Studio Lumen on the demo copy), its staff's to change.
ALTER TABLE communities ADD COLUMN player_vocal_slider boolean NOT NULL DEFAULT false;
UPDATE communities SET player_vocal_slider = true WHERE id = 7 AND kind = 'coaching';

-- A coaching community's key per student, from the song player: in the
-- songs opened from its songbooks, the coach picks a student (those with a
-- lesson in the coming week first) and saves their key there
-- (store/vocal_slider.go, httpapi/coach_keys.go). Its own option, apart
-- from the voice's slider (0093). Off everywhere but in Chloé Paul's
-- coaching community (id 7; Studio Lumen on the demo copy), its staff's
-- to change. (0094-0096 are another branch's.)
ALTER TABLE communities ADD COLUMN coach_student_keys boolean NOT NULL DEFAULT false;
UPDATE communities SET coach_student_keys = true WHERE id = 7 AND kind = 'coaching';

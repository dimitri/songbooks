-- A student's voice (docs/coaching.md, "Voice types"): the range and the
-- comfortable zone lead; the type is the coach's working label, firm or
-- "leaning" (the default from now on; the types noted so far stay firm),
-- with a few optional words beside it: the timbre, and where the voice's
-- two passages sit. All of it in the history (0108). (0110 is
-- song-header's.)
ALTER TABLE student_voice_profiles ADD COLUMN voice_type_leaning boolean NOT NULL DEFAULT false;
ALTER TABLE student_voice_profiles ADD COLUMN timbre text NOT NULL DEFAULT '' CHECK (timbre IN ('', 'bright', 'warm', 'dark'));
ALTER TABLE student_voice_profiles ADD COLUMN passaggio_low_midi int CHECK (passaggio_low_midi BETWEEN 0 AND 127);
ALTER TABLE student_voice_profiles ADD COLUMN passaggio_high_midi int CHECK (passaggio_high_midi BETWEEN 0 AND 127);

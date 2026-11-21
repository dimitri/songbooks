"""sbanalyze's own logic, without the models (they need weights and a few
seconds): the per-beat chord shares, and the beat source chosen.

    python -m unittest deploy/analysis/test_sbanalyze.py
"""

import os
import sys
import unittest

import numpy as np

sys.path.insert(0, os.path.dirname(__file__))
import sbanalyze  # noqa: E402


class BeatPosteriors(unittest.TestCase):
    def test_shares_per_beat(self):
        names = ["N", "C:maj", "A:min"]
        hop = 0.1
        # 2 s: C for the first second, Am for the second.
        logp = np.full((20, 3), -10.0)
        logp[:10, 1] = 0
        logp[10:, 2] = 0
        out = sbanalyze.beat_posteriors(names, logp, hop, [0.0, 1.0, 2.0])
        self.assertEqual(len(out), 2)
        self.assertEqual(out[0][0][0], "C:maj")
        self.assertEqual(out[1][0][0], "A:min")
        self.assertGreater(out[0][0][1], 0.99)

    def test_mixed_beat(self):
        names = ["C:maj", "G:maj"]
        logp = np.array([[0.0, -10.0]] * 3 + [[-10.0, 0.0]] * 1)
        out = sbanalyze.beat_posteriors(names, logp, 0.25, [0.0, 1.0])
        self.assertEqual([g[0] for g in out[0]], ["C:maj", "G:maj"])
        self.assertAlmostEqual(out[0][0][1], 0.75, places=2)


class FakeBeats:
    checkpoint = "fake"

    def __init__(self, by_len):
        self.by_len = by_len

    def __call__(self, x, sr):
        return self.by_len[len(x)]


class FakeChords:
    def __call__(self, path, beats):
        return [{"t": 0.0, "e": 1.0, "c": "C:maj"}], [[["C:maj", 1.0]]] * max(0, len(beats) - 1)


class BeatSource(unittest.TestCase):
    def run_with(self, backing_act):
        import tempfile

        import soundfile as sf

        d = tempfile.mkdtemp()
        mix, back = os.path.join(d, "mix.wav"), os.path.join(d, "back.wav")
        sf.write(mix, np.zeros(1000, dtype="float32"), 1000)
        sf.write(back, np.zeros(999, dtype="float32"), 1000)
        beats = np.arange(10, dtype=float)
        fake = FakeBeats({
            1000: (beats, beats[::4], np.full(10, 0.9)),
            999: (beats, beats[::4], np.full(10, backing_act)),
        })
        return sbanalyze.analyze(mix, back, beats_model=fake, chords_model=FakeChords())

    def test_backing_when_clear(self):
        self.assertEqual(self.run_with(0.95)["beat_source"], "no_vocals")

    def test_mix_when_backing_is_weak(self):
        self.assertEqual(self.run_with(0.1)["beat_source"], "mix")


class StrictJSON(unittest.TestCase):
    def test_no_nan(self):
        import json

        out = sbanalyze.finite({"duration": float("nan"), "beats": [1.0, np.float32("inf"), 2.0], "c": "C:maj", "n": 3})
        self.assertEqual(out, {"duration": None, "beats": [1.0, None, 2.0], "c": "C:maj", "n": 3})
        json.dumps(out, allow_nan=False)

    def test_nan_beats_dropped(self):
        import tempfile

        import soundfile as sf

        d = tempfile.mkdtemp()
        mix = os.path.join(d, "mix.wav")
        sf.write(mix, np.zeros(1000, dtype="float32"), 1000)
        beats = np.array([0.0, 1.0, float("nan"), 3.0] + list(range(4, 12)), dtype=float)
        fake = FakeBeats({1000: (beats, np.array([0.0, float("nan"), 4.0]), np.full(len(beats), 0.9))})
        out = sbanalyze.analyze(mix, "", beats_model=fake, chords_model=FakeChords())
        self.assertNotIn(2.0, out["beats"])
        self.assertEqual(len(out["beats"]), len(out["beat_act"]))
        self.assertEqual(out["downbeats"], [0.0, 4.0])


if __name__ == "__main__":
    unittest.main()

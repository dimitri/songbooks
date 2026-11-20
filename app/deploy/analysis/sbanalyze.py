"""sbanalyze: what a reference recording says about its own beats and chords.

cmd/stemsd runs it after Demucs has split a recording, on the WAVs it
already has, and stores the result (song_audio_analysis, migration 0106).
Everything chart-dependent (the alignment, bar counts, chord checks) is
done later in Go (internal/audioanalysis/align.go): this only listens.

    python sbanalyze.py --mix track.wav --backing no_vocals.wav [--threads 4]

prints one JSON object on stdout:

    {"analyzer": "...", "duration": 250.1, "beat_source": "no_vocals",
     "beats": [0.64, ...], "downbeats": [1.6, ...],
     "beat_act": [0.93, ...],                 # beat activation at each beat
     "chords": [{"t": 9.52, "e": 13.12, "c": "D:maj"}, ...],   # Harte labels
     "beat_chords": [[["D:maj", 0.82], ["B:min", 0.07]], ...]}  # per beat, best first

Models, both MIT licensed (code and weights):
  - Beat This! (CPJKU, ISMIR 2024), checkpoint final0, for beats and
    downbeats, no DBN (the DBN needs madmom, whose models are
    non-commercial). It runs on the backing (no_vocals) part: the voice's
    ad-libs add beats that aren't there (song 334's fade-out). The mix is
    used when the backing gives too little to go on (an a cappella song).
  - lv-chordia (Music X Lab, ISMIR 2019 large-vocabulary chords), on the
    mix: the singing doesn't hurt it. Besides its own segments, every
    beat gets the chords the network finds likeliest there, with their
    share, so the app can say how sure it is and offer alternatives.

No madmom, no Essentia (non-commercial models, AGPL code).
"""

import argparse
import json
import os
import sys
import time

import numpy as np

ANALYZER_VERSION = "1"
BEAT_FPS = 50  # Beat This! frames per second


def log(*a):
    print(*a, file=sys.stderr, flush=True)


def read_mono(path):
    import soundfile as sf

    x, sr = sf.read(path, dtype="float32", always_2d=True)
    return x.mean(axis=1), sr


def sigmoid(x):
    return 1.0 / (1.0 + np.exp(-x))


class Beats:
    """Beat This! on a signal: beat and downbeat times and activations."""

    def __init__(self, checkpoint="final0"):
        from beat_this.inference import Audio2Frames
        from beat_this.model.postprocessor import Postprocessor

        self.checkpoint = checkpoint
        self.frames = Audio2Frames(checkpoint_path=checkpoint, device="cpu")
        self.post = Postprocessor(type="minimal")

    def __call__(self, x, sr):
        beat_logits, down_logits = self.frames(x, sr)
        beats, downs = self.post(beat_logits, down_logits)
        act = sigmoid(beat_logits.cpu().numpy())
        idx = np.clip(np.round(np.asarray(beats) * BEAT_FPS).astype(int), 0, len(act) - 1)
        return np.asarray(beats, float), np.asarray(downs, float), act[idx] if len(idx) else np.zeros(0)


class Chords:
    """lv-chordia's ensemble, loaded once; its HMM segments plus, per beat,
    the chords its network finds likeliest (frame posteriors averaged)."""

    def __init__(self, vocabulary="submission"):
        import importlib.resources

        from lv_chordia.chord_recognition import MODEL_NAMES
        from lv_chordia.chordnet_ismir_naive import ChordNet
        from lv_chordia.extractors.xhmm_ismir import XHMMDecoder
        from lv_chordia.mir.nn.train import NetworkInterface

        with importlib.resources.path("lv_chordia.data", f"{vocabulary}_chord_list.txt") as f:
            self.hmm = XHMMDecoder(template_file=str(f))
        self.nets = [NetworkInterface(ChordNet(None), name, load_checkpoint=False) for name in MODEL_NAMES]

    def __call__(self, path, beats):
        from lv_chordia.extractors.cqt import CQTV2
        from lv_chordia.mir import DataEntry, io
        from lv_chordia.settings import DEFAULT_HOP_LENGTH, DEFAULT_SR

        entry = DataEntry()
        entry.prop.set("sr", DEFAULT_SR)
        entry.prop.set("hop_length", DEFAULT_HOP_LENGTH)
        entry.append_file(path, io.MusicIO, "music")
        entry.append_extractor(CQTV2, "cqt")
        probs = [net.inference(entry.cqt) for net in self.nets]
        probs = [np.mean([p[i] for p in probs], axis=0) for i in range(len(probs[0]))]
        segments = [
            {"t": round(float(a), 2), "e": round(float(b), 2), "c": str(c)}
            for a, b, c in self.hmm.decode_to_chordlab(entry, probs, False)
        ]
        names, logp = self.hmm.get_chord_tag_obs(probs)
        hop = DEFAULT_HOP_LENGTH / DEFAULT_SR
        return segments, beat_posteriors(names, logp, hop, beats)


def beat_posteriors(names, logp, hop, beats, top=3):
    """Per beat interval [beats[i], beats[i+1]): the `top` likeliest chords
    and their share of the frames' posterior, best first."""
    logp = logp - logp.max(axis=1, keepdims=True)
    post = np.exp(logp)
    post /= post.sum(axis=1, keepdims=True)
    out = []
    n = post.shape[0]
    for i in range(len(beats) - 1):
        a = int(np.floor(beats[i] / hop))
        b = int(np.ceil(beats[i + 1] / hop))
        a, b = max(0, min(n - 1, a)), max(1, min(n, b))
        if b <= a:
            b = a + 1
        p = post[a:b].mean(axis=0)
        best = np.argsort(-p)[:top]
        out.append([[names[k], round(float(p[k]), 3)] for k in best if p[k] >= 0.01])
    return out


def analyze(mix, backing, threads=0, beats_model=None, chords_model=None):
    import torch

    if threads:
        torch.set_num_threads(threads)
    t0 = time.time()
    beats_model = beats_model or Beats()
    chords_model = chords_model or Chords()
    t1 = time.time()

    x, sr = read_mono(mix)
    duration = len(x) / sr
    source, bx = "mix", None
    if backing:
        bsig, bsr = read_mono(backing)
        bx = beats_model(bsig, bsr)
        source = "no_vocals"
    # The backing part, unless it gives too little to go on: no beats, or
    # weak ones (an a cappella or spoken stretch), and the mix does better.
    if bx is None or len(bx[0]) < 8 or float(np.mean(bx[2])) < 0.35:
        mx = beats_model(x, sr)
        if bx is None or float(np.mean(mx[2]) if len(mx[2]) else 0) > float(np.mean(bx[2]) if len(bx[2]) else 0):
            bx, source = mx, "mix"
    beats, downs, act = bx
    t2 = time.time()
    segments, per_beat = chords_model(os.path.abspath(mix), beats)
    t3 = time.time()
    log(f"sbanalyze: load {t1 - t0:.1f}s, beats {t2 - t1:.1f}s, chords {t3 - t2:.1f}s")
    return {
        "analyzer": analyzer_name(beats_model),
        "duration": round(duration, 3),
        "beat_source": source,
        "beats": [round(float(b), 3) for b in beats],
        "downbeats": [round(float(b), 3) for b in downs],
        "beat_act": [round(float(a), 2) for a in act],
        "chords": segments,
        "beat_chords": per_beat,
        "timing": {"load": round(t1 - t0, 2), "beats": round(t2 - t1, 2), "chords": round(t3 - t2, 2)},
    }


def analyzer_name(beats_model):
    from importlib.metadata import version

    def v(pkg):
        try:
            return version(pkg)
        except Exception:
            return "?"

    return f"beat_this:{v('beat-this')}/{beats_model.checkpoint}+lv_chordia:{v('lv-chordia')}+sbanalyze:{ANALYZER_VERSION}"


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--mix", help="the recording, a WAV")
    ap.add_argument("--backing", default="", help="its no_vocals part, a WAV (optional)")
    ap.add_argument("--threads", type=int, default=0)
    ap.add_argument("--warm", action="store_true", help="load the models (fetching them when missing) and stop")
    ap.add_argument("--name", action="store_true", help="print the analyzer's name (models and versions) and stop")
    args = ap.parse_args(argv)
    if args.name:
        print(analyzer_name(type("B", (), {"checkpoint": "final0"})))
        return 0
    if args.warm:
        Beats()
        Chords()
        return 0
    if not args.mix:
        ap.error("--mix is required")
    json.dump(analyze(args.mix, args.backing, args.threads), sys.stdout)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())

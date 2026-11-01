#!/usr/bin/env python3
"""Hand corrections that must survive `make import`.

`convert.py` regenerates every .song file from the original LaTeX/LilyPond
sources, which silently overwrites any hand fix applied directly to a .song
file -- that happened once already (the Colors chord sequence). Corrections
belong here instead, as exact-text substitutions applied as a step AFTER
import, so they are never lost to a re-import.

Add an entry as (slug, book, old_exact_text, new_exact_text). `old` must
match exactly once in the generated file, or the correction is skipped with a
warning (rather than silently applying to the wrong spot, or silently doing
nothing if the source changes upstream).
"""
from pathlib import Path

CORRECTIONS = [
    # The source colors.ly encodes a simplified chart (F#m F#m A A E C#m F#m
    # F#m); the real changes, confirmed by ear, use bass motion under C#m/C#
    # rather than a plain E and C#m triad in bars 5-6.
    ("colors", "Mercy", """grid: |: F#m | % | A | % |
grid: | E | C#m | F#m | % :|""", """grid: |: F#m | F#m | A | A |
grid: | C#m/E | C#/F | F#m :|"""),
    # aint-no-love.tex puts \lilypondfile{chorus.ly} BEFORE \beginchorus, with
    # no preceding \textnote -- convert.py's "first lilypondfile with no
    # pending label is the song's own Intro" heuristic (reasonable in
    # general) files chorus.ly's real, correctly-parsed grid under [Intro]
    # instead, leaving [Chorus] to fall back to the cruder one-chord-per-bar
    # grid synthesized from inline markers, which flattens chorus.ly's actual
    # bar-internal splits (Gm for 3 beats + Cm for 1, Eb for 3 + F for 1,
    # Eb for 2 + Cm for 2) into one chord per bar. The real grid belongs on
    # [Chorus]; [Intro] should be empty (intro == chorus here). One
    # correction spanning both sections, not two independent ones: the
    # "already applied?" check in apply() below tests whether `new` already
    # occurs ANYWHERE in the file, and the correct grid text already sits
    # under the wrong heading ([Intro]) before this runs -- two separate
    # corrections would have seen that and silently no-opped on the half
    # that moves it out of [Intro].
    ("aint-no-love", "Mercy", """[Intro]
grid: |: Gm . . Cm | Gm | Eb . . F | Gm |
grid: | Gm . . Cm | Gm | Eb . Cm . | Gm :|

[Chorus]
grid: |: Gm | Cm | Gm | Eb |
grid: | F | Gm | % | Cm |
grid: | Gm | Eb | Cm | Gm :|""", """[Intro]

[Chorus]
grid: |: Gm . . Cm | Gm | Eb . . F | Gm |
grid: | Gm . . Cm | Gm | Eb . Cm . | Gm :|"""),
    # Same bug, found while reviewing the one above: Vanity-Rewards.tex has
    # BOTH \lilypondfile{chorus.ly} and \lilypondfile{verse.ly} before any
    # section opener. The first claims the auto-Intro label and keeps it
    # (chorus.ly's real 10-bar grid -- all single-chord bars, no splits
    # here), the second gets auto-labelled "Verse" from its filename and
    # correctly merges into the real [Verse] section that follows -- but
    # [Chorus] itself, opened later via \beginchorus, never gets a grid at
    # all (chordsoff is set for the whole song, so there are no inline
    # markers for the synthesis fallback to fall back to either). Moving
    # chorus.ly's grid from [Intro] to the first [Chorus] in one correction,
    # same reason as above: the "already applied?" check would otherwise see
    # the grid sitting under the wrong heading and no-op.
    ("Vanity-Rewards", "OtherLies", """[Intro]
grid: | Bm | C#m | D | Em |
grid: | Bm | C#m | D | Em |
grid: | F#m | % |

[Verse]
grid: |: Bm . Em . | % | % | % :|
As the future is unclear and escapes unseen
I can’t see the exit, open wide and clear
Still an obscure dream with the words I mean
I can see your future is full of tears

[Chorus]
And now I see the moon’s shine desire""", """[Intro]

[Verse]
grid: |: Bm . Em . | % | % | % :|
As the future is unclear and escapes unseen
I can’t see the exit, open wide and clear
Still an obscure dream with the words I mean
I can see your future is full of tears

[Chorus]
grid: |: Bm | C#m | D | Em |
grid: | Bm | C#m | D | Em |
grid: | F#m | % :|
And now I see the moon’s shine desire"""),
]

def apply(book_dir):
    n = 0
    for slug, book, old, new in CORRECTIONS:
        if book != book_dir.name:
            continue
        f = book_dir / "songs" / f"{slug}.song"
        if not f.exists():
            print(f"  ! correction for {slug}: no such song in {book_dir}")
            continue
        text = f.read_text()
        if new in text:
            continue  # already applied
        if text.count(old) != 1:
            print(f"  ! correction for {slug}: expected text not found "
                  f"(source may have changed -- reapply by hand)")
            continue
        f.write_text(text.replace(old, new))
        print(f"  corrected {slug}")
        n += 1
    return n

if __name__ == "__main__":
    import sys
    book_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(".")
    apply(book_dir)

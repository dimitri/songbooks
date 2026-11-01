#!/usr/bin/env python3
"""Extract just the notation from a hybrid .ly fragment and render it alone.

These fragments hold two sibling top-level blocks: a `\\new TabStaff { ... }`
(or a staff Voice) carrying a riff, and a `\\new ChordNames` + `\\chordmode`
pair carrying the chord grid. We now draw chord grids natively, so pulling the
chord half into the SVG as well produced a duplicated chord line beside a
cramped tablature. Only the notation goes to SVG; the chords are parsed.

The old `header.ly` is deliberately NOT included: its `line-width = 6cm` is what
wrapped these into unreadable narrow systems.
"""
import re, sys, subprocess, tempfile, shutil
from pathlib import Path

# LilyPond's SVG backend writes the "erase the staff line behind this" patch
# behind a TAB fret number as `<g color="rgba(100%, 100%, 100%, 100.0000%)">`
# + a child `fill="currentColor"` rect. That 4-argument PERCENTAGE rgba() is
# valid CSS, but Typst's SVG renderer (resvg) doesn't parse that particular
# form and silently treats the color as black instead of failing loudly --
# so the white mask paints black over the fret number instead of erasing the
# line behind it, and it reads as a solid black square where a digit should
# be. Every value actually written by this LilyPond version is fully opaque,
# so it is enough to drop the alpha and emit the 3-argument form, which
# renders correctly; the general rgb()/decimal-rgba() forms both work too
# (verified directly against Typst), so a non-opaque value falls back to
# that instead of silently staying broken.
RGBA_PCT = re.compile(
    r'rgba\(\s*([\d.]+)%\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*\)')

def fix_rgba(svg):
    def repl(m):
        r, g, b, a = (float(x) for x in m.groups())
        if a >= 99.9995:  # the only case LilyPond actually emits
            return f'rgb({r:g}%, {g:g}%, {b:g}%)'
        to255 = lambda p: round(p / 100 * 255)
        return f'rgba({to255(r)}, {to255(g)}, {to255(b)}, {a / 100:g})'
    return RGBA_PCT.sub(repl, svg)

def match_block(src, start):
    """Given an index at or before a '{', return (end_index_after_matching '}')."""
    i = src.index('{', start)
    depth = 0
    while i < len(src):
        if src[i] == '{': depth += 1
        elif src[i] == '}':
            depth -= 1
            if depth == 0: return i + 1
        i += 1
    return None

NOTATION = re.compile(r'\\new\s+(TabStaff|Staff|Voice|StaffGroup)\b')
WITH = re.compile(r'\s*\\with\b')

def extract(src):
    """Return the notation blocks only, in source order."""
    out, pos = [], 0
    for m in NOTATION.finditer(src):
        # `\new TabStaff \with { stringTunings = ... } { ... the music ... }`:
        # an optional \with block configures the staff and sits before its
        # real body. Searching for the body's '{' from m.end() found THIS
        # block's '{' instead and matched only to its close, truncating the
        # extracted fragment to the \with config with no music in it --
        # LilyPond then fails on the dangling block, silently (render()
        # swallows subprocess output), so nothing SVG-shaped ever appeared
        # and it looked like the file had no notation at all.
        after = m.end()
        wm = WITH.match(src, after)
        if wm is not None:
            after = match_block(src, wm.end())
            if after is None: continue
        end = match_block(src, after)
        if end is None: continue
        out.append(src[m.start():end])
    # a << >> wrapper that contained both halves: keep only what we extracted
    return '\n\n'.join(out)

PAPER = r'''\version "2.26.0"
\paper {
  indent = 0\mm
  ragged-right = ##t
  line-width = %(width)s\mm
  top-margin = 0\mm  bottom-margin = 0\mm
  left-margin = 0\mm right-margin = 0\mm
  make-header = ##f  make-footer = ##f
  system-system-spacing.basic-distance = #6
}
#(set-global-staff-size %(size)s)
'''

def render(ly_path, out_svg, width_mm=170, staff_size=20):
    src = Path(ly_path).read_text(errors='replace')
    body = extract(src)
    if not body.strip():
        return False
    tmp = Path(tempfile.mkdtemp())
    try:
        f = tmp / 'score.ly'
        f.write_text(PAPER % {'width': width_mm, 'size': staff_size} + '\n' + body)
        subprocess.run(
            ['lilypond', '-dcrop', '-dbackend=svg', '--formats=svg',
             '-dno-point-and-click', '-o', str(tmp / 'score'), str(f)],
            capture_output=True)
        cropped = tmp / 'score.cropped.svg'
        if not cropped.exists():
            return False
        Path(out_svg).parent.mkdir(parents=True, exist_ok=True)
        fixed = fix_rgba(cropped.read_text())
        Path(out_svg).write_text(fixed)
        return True
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

if __name__ == '__main__':
    ok = render(sys.argv[1], sys.argv[2],
                width_mm=float(sys.argv[3]) if len(sys.argv) > 3 else 170)
    sys.exit(0 if ok else 1)

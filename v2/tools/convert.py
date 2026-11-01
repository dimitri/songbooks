#!/usr/bin/env python3
"""Convert the LaTeX `songs` + LilyPond sources into the .song format.

The two chord sources (the .ly grid and the inline \\[Chord] markers) are
merged into one file with one key and one transposition. Where the two
disagree about transposition, that is reported rather than silently resolved --
nothing has ever checked this before.
"""
import os, re, sys, json
from pathlib import Path

STEPS = {'c':0,'d':2,'e':4,'f':5,'g':7,'a':9,'b':11}
SHARP = ["C","C#","D","D#","E","F","F#","G","G#","A","A#","B"]
FLAT  = ["C","Db","D","Eb","E","F","Gb","G","Ab","A","Bb","B"]
# what a chart actually writes when nothing forces the choice
USUAL = ["C","Db","D","Eb","E","F","F#","G","Ab","A","Bb","B"]
PITCH_OF = {}
for _i, _n in enumerate(SHARP): PITCH_OF.setdefault(_n, _i)
for _i, _n in enumerate(FLAT):  PITCH_OF.setdefault(_n, _i)

def ly_pitch(tok):
    """'bes' -> 10, 'fis' -> 6, 'c' -> 0"""
    m = re.match(r'^([a-g])((?:is|es|s)*)', tok)
    if not m: return None
    p = STEPS[m.group(1)]
    acc = m.group(2)
    i = 0
    while i < len(acc):
        if acc.startswith('is', i): p += 1; i += 2
        elif acc.startswith('es', i): p -= 1; i += 2
        elif acc.startswith('s', i): p -= 1; i += 1
        else: break
    return p % 12

# LilyPond chord-quality suffix -> conventional chord symbol suffix
QUAL = [
    ('m7.5-', 'm7b5'), ('7m.5-', '7b5'), ('m7b5', 'm7b5'), ('7.5-', '7b5'),
    ('7.9+', '7#9'), ('7.9-', '7b9'), ('9sus4', '9sus4'), ('sus4', 'sus4'),
    ('sus2', 'sus2'), ('maj7', 'maj7'), ('maj9', 'maj9'), ('m11', 'm11'),
    ('m9', 'm9'), ('m7', 'm7'), ('m6', 'm6'), ('m', 'm'), ('dim', 'dim'),
    ('aug', '+'), ('sus', 'sus4'), ('13', '13'), ('11', '11'), ('9', '9'),
    ('7', '7'), ('6', '6'), ('79', '9'), ('47', 'sus4'), ('2', 'sus2'),
]

def ly_chord(tok, flat):
    """'bes1:m7' -> 'Bbm7';  'f/g' -> 'F/G'"""
    tok = tok.strip()
    if not tok: return None
    bass = None
    if '/' in tok:
        tok, b = tok.split('/', 1)
        b = b.split(':')[0]
        bp = ly_pitch(b)
        if bp is not None: bass = (FLAT if flat else SHARP)[bp]
    qual = ''
    if ':' in tok:
        tok, q = tok.split(':', 1)
        q = q.strip()
        for a, b in QUAL:
            if q.startswith(a): qual = b + q[len(a):].replace('.', ''); break
        else:
            qual = q.replace('.', '')
    root = re.match(r'^[a-g](?:is|es|s)*', tok)
    if not root: return None
    p = ly_pitch(tok)
    if p is None: return None
    name = (FLAT if flat else SHARP)[p]
    return name + qual + (('/' + bass) if bass else '')

DUR = {'1':4.0, '2':2.0, '4':1.0, '8':0.5, '16':0.25}

def shift_chord(sym, semitones, flat):
    """Re-spell a chord symbol up by `semitones`."""
    if not semitones or not sym:
        return sym
    names = FLAT if flat else USUAL
    m = re.match(r'^([A-G][b#]?)(.*?)(?:/([A-G][b#]?))?$', sym)
    if not m:
        return sym
    def mv(n):
        if not n: return n
        i = PITCH_OF.get(n)
        return names[(i + semitones) % 12] if i is not None else n
    root, qual, bass = m.group(1), m.group(2) or '', m.group(3)
    return mv(root) + qual + (('/' + mv(bass)) if bass else '')


def parse_ly(path, flat):
    """Return (measures, tempo, transpose_pair) from a chordmode .ly fragment."""
    src = Path(path).read_text(errors='replace')
    tempo = None
    m = re.search(r'\\tempo\s+\d+\s*=\s*(\d+)', src)
    if m: tempo = m.group(1)
    tp = None
    m = re.search(r'\\transpose\s+([a-g][a-z]*)\s+([a-g][a-z]*)', src)
    if m: tp = (m.group(1), m.group(2))
    # These charts label a meter with a \typewriter markup on noChordSymbol
    # rather than a real \time, so that is where the signature lives.
    meter = None
    mm = re.search(r'\\typewriter\s+"(\d+/\d+)"', src)
    if mm: meter = mm.group(1)
    if '\\chordmode' not in src and '\\chords' not in src:
        return None, tempo, tp, meter   # pure notation: nothing to parse

    # isolate the chordmode body
    i = src.find('\\chordmode')
    if i < 0: i = src.find('\\chords')
    body = src[i:]
    body = body[body.find('{') + 1:]
    # strip scheme/markup noise we don't model
    body = re.sub(r'\\set\s+\S+\s*=\s*\\markup\s*\{[^}]*\}', ' ', body)
    body = re.sub(r'\\markup\s*\{[^}]*\}', ' ', body)
    body = re.sub(r'\\tempo[^\n]*', ' ', body)
    body = re.sub(r'%[^\n]*', ' ', body)

    measures, cur, filled = [], [], 0.0
    dur = 4.0
    block_shift = 0
    open_next = False
    pending_volta = None
    depth = 0
    repeat_depth = None   # brace depth at which the current \repeat body sits
    toks = re.findall(r'\\[a-zA-Z]+|[{}]|[^\s{}]+', body)
    k = 0
    while k < len(toks):
        t = toks[k]; k += 1
        if t == '\\repeat':
            if k < len(toks) and toks[k] == 'volta': k += 2
            open_next = True
            repeat_depth = depth + 1   # the body's braces open next
            continue
        if t == '\\transpose':
            # the two pitch arguments are not chords; consume them and record
            # the interval so it can be baked into this block's chords
            if k + 1 < len(toks):
                a, b = ly_pitch(toks[k]), ly_pitch(toks[k + 1])
                if a is not None and b is not None:
                    block_shift = (b - a) % 12
                k += 2
            continue
        if t == '\\alternative': continue
        if t == '\\volta':
            if k < len(toks):
                pending_volta = toks[k].rstrip('{').strip(); k += 1
            continue
        if t == '\\bar':
            if k < len(toks): k += 1
            continue
        if t == '\\break':
            continue
        if t.startswith('\\'): continue
        if t == '{':
            depth += 1; continue
        if t == '}':
            depth -= 1
            if repeat_depth is not None and depth < repeat_depth:
                if measures: measures[-1]['close'] = True
                repeat_depth = None
            continue
        # A rest/spacer can carry its own duration digit fused to the letter,
        # exactly like a chord token (r1, s2, r4...) -- matching only the bare
        # letter silently dropped any bar that ended on a rest, which is how
        # Who's Been Talking lost its 12th (silent) bar of a 12-bar blues.
        mr = re.match(r'^([rs])(\d+)?(\.?)$', t)
        if mr:
            if mr.group(2): dur = DUR.get(mr.group(2), dur)
            filled += dur * (1.5 if mr.group(3) else 1.0)
            if filled >= 4.0 - 1e-6:
                measures.append({'chords': cur, 'open': open_next,
                                 'close': False, 'volta': pending_volta})
                open_next = False; pending_volta = None; cur = []; filled = 0.0
            continue
        # a chord token, possibly carrying a duration
        md = re.match(r'^([a-g](?:is|es|s)*)(\d+)?(\.?)(.*)$', t)
        if not md: continue
        if md.group(2): dur = DUR.get(md.group(2), dur)
        d = dur * (1.5 if md.group(3) else 1.0)
        tok = md.group(1) + md.group(4)
        c = ly_chord(tok, flat)
        if c:
            c = shift_chord(c, block_shift, flat)
            cur.append((c, d, filled))     # chord, beats, offset within the bar
        filled += d
        if filled >= 4.0 - 1e-6:
            measures.append({'chords': cur, 'open': open_next,
                             'close': False, 'volta': pending_volta})
            open_next = False; pending_volta = None; cur = []; filled = 0.0
    if cur:
        measures.append({'chords': cur, 'open': open_next,
                         'close': False, 'volta': pending_volta})
    return measures, tempo, tp, meter

def beat_slots(chords, beats=4):
    """[(chord, dur, offset)] -> ['Gm', '.', '.', 'Cm'] on a beat grid.

    A bar whose single chord fills it needs no slots at all; anything else is
    written out beat by beat so the reader (and the renderer) can see exactly
    where each change lands.
    """
    if not chords:
        return []
    if len(chords) == 1 and chords[0][1] >= beats - 1e-6:
        return [chords[0][0]]
    slots = ['.'] * beats
    placed = False
    for name, dur, off in chords:
        i = int(round(off))
        if 0 <= i < beats:
            slots[i] = name
            placed = True
    if not placed:
        return [c[0] for c in chords]
    return slots


def collapse_repeats(measures):
    """Mark bars identical to what came before with a repeat sign.

    `%` = play the previous bar again; `%%` on the second of a pair = play the
    previous two. This is standard notation and it is the cheapest page-space
    saving there is: it costs no legibility, unlike shrinking the type.

    A bar's OPEN/CLOSE repeat barline and its chord content are independent --
    a closing bar can still show "%" for its content while the barline itself
    draws normally, exactly as a printed chart does. Only a volta keeps its
    real chords: seeing what actually plays under "2." is more useful than a
    repeat sign there.
    """
    out = []
    for i, m in enumerate(measures):
        keep = m.get('volta')
        prev = measures[i - 1] if i else None
        same1 = (prev is not None and not keep
                 and not prev.get('volta') and m['chords'] == prev['chords']
                 and m['chords'])
        # two-bar repeat: this bar and the one before match the pair before them
        same2 = False
        if i >= 3 and not keep and not same1:
            a, b, c, d = measures[i-3], measures[i-2], measures[i-1], m
            same2 = (a['chords'] == c['chords'] and b['chords'] == d['chords']
                     and a['chords'] and b['chords']
                     and not any(x.get('volta') for x in (b, c, d)))
        n = dict(m)
        if same1:
            n['repeat_sign'] = '%'
        elif same2 and out and out[-1].get('repeat_sign') != '%%':
            n['repeat_sign'] = '%%'
        out.append(n)
    return out


def fmt_grid(measures):
    if not measures: return None
    measures = collapse_repeats(measures)
    out, line = [], []
    for i, m in enumerate(measures):
        if m.get('repeat_sign'):
            cell = m['repeat_sign']
        else:
            cell = ' '.join(beat_slots(m['chords'])) if m['chords'] else ''
        if m.get('volta'): cell = f"({m['volta']}.) {cell}".strip()
        pre = '|:' if m.get('open') else '|'
        line.append((pre, cell, m.get('close', False)))
        if len(line) == 4 or i == len(measures) - 1:
            s = ''
            closed = False
            for pre, cell, close in line:
                s += f"{pre} {cell} " if cell else f"{pre} "
                closed = close
            s += ':|' if closed else '|'
            out.append(s.replace('  ', ' ').strip())
            line = []
    return out

TEX_CHORD = re.compile(r'\\\[([^\]]*)\]')
def tex_chord(c):
    return c.replace('&', 'b').strip()

def convert(texpath, outdir, report):
    src = Path(texpath).read_text(errors='replace')
    d = Path(texpath).parent
    slug = Path(texpath).stem

    m = re.search(r'\\beginsong\{(.+?)\}(?:\[by=\{(.*?)\}\])?', src, re.S)
    if not m: return None
    title, artist = m.group(1).strip(), (m.group(2) or '').strip()

    tr = re.search(r'^\s*\\transpose\{(-?\d+)\}', src, re.M)
    semitones = int(tr.group(1)) if tr else 0
    capo = re.search(r'\\capo\{(\d+)\}', src)

    # a key hint: first chord of the first grid or the first inline chord
    lys = sorted(d.glob('*.ly'))
    # The .ly source already records the writer's spelling choice: an `es`
    # suffix (bes, ees, aes) means this song is written with flats. The inline
    # \[B&7] markers say the same thing with `&`.
    blob = ' '.join(ly.read_text(errors='replace') for ly in lys)
    flat = bool(re.search(r'(?<![a-z])[a-g]es(?![a-z])', blob)) or '&' in src
    grids, tempo, ly_tp, scores, meters = {}, None, None, {}, {}
    for ly in lys:
        ms, tp, tpair, meter = parse_ly(ly, flat)
        if tp and not tempo: tempo = tp
        if tpair: ly_tp = tpair
        if ms:
            grids[ly.stem] = ms
            if meter: meters[ly.stem] = meter
        # Notation/tab fragments (TabStaff) are no longer embedded as SVG: the
        # extraction mixed a cramped tablature with a duplicate chord line and
        # did not read well at performance size. A section with no grid and no
        # inline chords is left with no harmonic content, same as a true
        # instrumental passage; one with inline chords gets a synthesised grid
        # from them (see synth_grid_from_inline, below).

    if semitones and ly_tp:
        report.append(f"{slug}: tex \\transpose{{{semitones}}} AND ly \\transpose {ly_tp[0]} {ly_tp[1]} -- verify")
    elif semitones and grids:
        report.append(f"{slug}: tex \\transpose{{{semitones}}} but grid has no \\transpose -- grid may be in the written key")

    # walk the body into sections
    body = src[m.end():]
    sections, cur, chords_on = [], None, True
    def new_sec(label):
        nonlocal cur
        if cur: sections.append(cur)
        cur = {'label': label, 'grid': [], 'lines': [], 'score': None}

    pending_label = None
    for raw in body.split('\n'):
        line = raw.strip()
        if not line or line.startswith('%'): continue
        if line.startswith('\\endsong'): break
        if line.startswith('\\chordsoff'): chords_on = False; continue
        if line.startswith('\\chordson'):  chords_on = True;  continue
        tn = re.match(r'\\textnote\{(.+?)\}', line)
        if tn:
            # Two \textnote calls back to back with nothing real between them
            # (e.g. \textnote{Break} immediately followed by \textnote{Solo})
            # otherwise silently drop the first: it sets pending_label, and
            # the second one overwrites it before anything ever consumed it.
            if pending_label is not None:
                sections.append({'label': pending_label, 'grid': [],
                                 'lines': [], 'score': None, 'time': None})
            pending_label = tn.group(1).rstrip(':')
            continue
        if line.startswith('\\beginverse') or line.startswith('\\beginchorus'):
            kind = 'Verse' if line.startswith('\\beginverse') else 'Chorus'
            label = pending_label or kind
            # keep a grid-only section that is really this one's chord changes
            if (cur is not None and not cur['lines']
                    and (cur['grid'] or cur.get('score'))
                    and cur['label'].lower().startswith(kind.lower())):
                cur['label'] = label
            else:
                new_sec(label)
            pending_label = None
            continue
        if line.startswith('\\endverse') or line.startswith('\\endchorus'):
            continue
        lf = re.match(r'\\lilypondfile(?:\[[^\]]*\])?\{(.+?)\}', line)
        if lf:
            stem = Path(lf.group(1)).stem
            # A grid introduces the section it belongs to. Attaching it to
            # whatever section happened to be open stapled the verse grid onto
            # the preceding chorus, so every tag after the first was wrong.
            auto_label = stem.replace('-', ' ').title()
            # A lead-in grid with no label is the song's own intro, not a
            # section named after the song. Matching the auto-label against
            # the full song title was too fragile -- it only caught an EXACT
            # match, and missed every title with punctuation, an apostrophe,
            # or more words than the .ly filename has (e.g. "mercy.ly" in
            # "Mercy, Mercy, Mercy", or "blind.ly" in "I'd Rather Go Blind"),
            # so the raw filename-derived label leaked through as a section
            # heading instead. The real signal is simpler and doesn't need
            # string matching at all: nothing has opened a section yet and
            # there's no explicit \textnote, so this grid is definitionally
            # the song's own intro.
            if cur is None and pending_label is None: auto_label = 'Intro'
            label = pending_label or auto_label
            if cur is None or cur['lines'] or cur['grid'] or cur.get('score'):
                new_sec(label)
            elif pending_label:
                cur['label'] = pending_label
            pending_label = None
            if stem in grids: cur['grid'] = grids[stem]
            if stem in meters: cur['time'] = meters[stem]
            if stem in scores: cur['score'] = scores[stem]
            continue
        # lyric line: convert \[C] -> [C], drop ^ replay marks and \echo{}.
        # Cleaned BEFORE deciding whether to open a section: a bare setup
        # command on its own line (\transpose{-2}, \capo{2}, ...) cleans down
        # to nothing and must not spawn an empty section, but a lyric line
        # that happens to OPEN on a chord marker (\[Am]Words...) must not be
        # mistaken for one either -- both start with a literal backslash, so
        # the only reliable test is what survives after the real cleanup.
        l = line.replace('\\&', '&').replace("\\'", "'")
        l = re.sub(r'\\echo\{(.*?)\}', r'(\1)', l)
        l = re.sub(r'\\rep\{(\d+)\}', r'(x\1)', l)
        l = re.sub(r'\\brk', '', l)
        if chords_on:
            l = TEX_CHORD.sub(
                lambda mm: '[' + shift_chord(tex_chord(mm.group(1)),
                                             semitones % 12, flat) + ']', l)
        else:
            l = TEX_CHORD.sub('', l)
        l = l.replace('^', '')
        l = re.sub(r'\\[a-zA-Z]+\s*', '', l)
        l = re.sub(r'\{[^}]*\}', '', l).strip()  # leftover command arguments
        # '|' is reserved grid syntax in the .song format. Some of the original
        # charts use a literal '|' inline as an informal beat marker (e.g.
        # "I don't |want no one to squeeze me |"); left in, a line that then
        # STARTS with '|' gets read back as a chord grid by the .song parser,
        # and its words get split up and placed on the beat grid as if they
        # were chord names. The marker carries no information our renderer
        # needs (the real beat grid is the LilyPond-derived one above), so it
        # is dropped rather than preserved.
        l = l.replace('|', '').strip()
        l = re.sub(r'\s{2,}', ' ', l)
        if not l: continue   # cleaned to nothing: a setup command, not lyric
        if cur is None: new_sec(pending_label or 'Verse'); pending_label = None
        cur['lines'].append(l)
    if cur: sections.append(cur)
    # A bare \textnote{Label} with nothing after it before \endsong -- an
    # "Outro" marker at the very end of a song is the common case, and two
    # \textnote calls in a row (e.g. "Break" immediately followed by "Solo")
    # is another -- set pending_label but never got consumed by a following
    # \beginverse/\beginchorus/\lilypondfile, so the label was silently
    # lost entirely rather than just losing its (nonexistent) content.
    if pending_label is not None:
        sections.append({'label': pending_label, 'grid': [], 'lines': [],
                         'score': None, 'time': None})

    # A section with NO grid of its own (no .ly fragment was ever attached to
    # it) but that DOES carry inline [Chord] markers has real harmonic
    # information that would otherwise simply vanish now that chords are no
    # longer drawn over the lyric text -- that was a true data-loss
    # regression (an entire song could lose every chord it had). Build a grid
    # from the marker sequence instead: one bar per marker, in the order it
    # appears, which is what the marker already encodes (each one marks a
    # change, not a syllable-level repeat). It carries no claim about beat
    # placement within the bar, only that this chord is next -- collapse_repeats
    # still turns genuine consecutive repeats into "%" afterwards.
    INLINE_CHORD = re.compile(r'\[([^\]]*)\]')
    for sec in sections:
        if sec['grid']:
            continue
        # Only real chord-looking tokens: a stray marker like the literal "?"
        # placeholder some charts use for an unresolved spot is not a chord.
        seq = [m for l in sec['lines'] for m in INLINE_CHORD.findall(l)
               if re.match(r'^[A-G]', m)]
        if not seq:
            continue
        # Measures elsewhere carry (name, duration, offset) tuples, matching
        # what parse_ly produces from real LilyPond durations -- beat_slots()
        # indexes into that shape, so a synthesised whole-bar chord needs the
        # same shape, not a bare name.
        sec['grid'] = [
            {'chords': [(c, 4.0, 0.0)], 'open': i == 0,
             'close': i == len(seq) - 1, 'volta': None}
            for i, c in enumerate(seq)
        ]

    # A section with a grid -- real (from a .ly file) or just synthesised
    # above -- says its chords once, in the grid. The TEX source still marks
    # chords inline on top of that (every lyric line under an active
    # \beginverse/\beginchorus got a [Chord] bracket regardless of whether a
    # grid already covered that section), which the renderer has never shown
    # since chords-over-lyrics was dropped, but it sat in the .song source as
    # dead, duplicated data. Strip it so the file says each chord exactly once.
    for sec in sections:
        if not sec['grid']:
            continue
        sec['lines'] = [INLINE_CHORD.sub('', l).strip() for l in sec['lines']]
        sec['lines'] = [re.sub(r' {2,}', ' ', l) for l in sec['lines'] if l]

    # Print each distinct lyric once. A repeated chorus becomes a reference to
    # the section that already carries the text -- the single biggest page-space
    # lever there is, and it costs no legibility at all. The old sources
    # re-typed every repeat in full, which is why the books sprawled.
    def norm(ls):
        # match on the words, ignoring chord markers, punctuation and case:
        # the author re-typed repeats by hand, so they differ in small ways
        # that do not make them different choruses
        out = []
        for l in ls:
            t = re.sub(r'\[[^\]]*\]', '', l).lower()
            t = re.sub(r"[^a-z0-9' ]", ' ', t)
            out.append(' '.join(t.split()))
        return tuple(x for x in out if x)

    seen = {}
    for sec in sections:
        key = (sec['label'], norm(sec['lines']))
        if sec['lines'] and key in seen:
            sec['repeat'] = seen[key]
            sec['lines'] = []
        elif sec['lines']:
            seen[key] = sec['label']
    used = {s['label'] for s in sections}
    out = [f"title:  {title}"]
    if artist: out.append(f"artist: {artist}")
    if tempo:  out.append(f"tempo:  {tempo}")
    out.append("time:   4/4")
    if capo:   out.append(f"capo:   {capo.group(1)}")
    if flat: out.append("flats:  yes")   # the source spelled them flat
    out.append(f"map:    {' · '.join(s['label'] for s in sections)}")
    out.append("")
    for s in sections:
        out.append(f"[{s['label']}]" if not s.get('repeat')
                   else f"[{s['label']}] (repeat)")
        # Four bars per system is the strong default. A section whose bar
        # count is not a clean multiple of 4 is the real exception -- a
        # six-bar phrase, or a 4-bar phrase plus a 1-bar alternate ending
        # (an `\alternative` pair, which adds exactly one bar on the "2."
        # pass) -- and must not be cut arbitrarily by the generic default
        # (a 5-bar verse chopped 4+1 split its own alternate ending onto an
        # orphan line by itself).
        nbars = len(s['grid'])
        if nbars and nbars % 4 != 0:
            out.append(f"bars: {nbars}")
        if s.get('time'):
            # The source carries this as a \typewriter label on the whole
            # fragment, so it cannot say WHICH bar changes meter. Emit it as a
            # hint; mark the bar by hand with {N/M} in the grid.
            out.append(f"# source labels this section {s['time']} -- mark the"
                       f" bar with {{{s['time']}}} if only one bar changes")
        if s.get('score'): out.append(f"score: {s['score']}")
        for g in (fmt_grid(s['grid']) or []):
            out.append(f"grid: {g}")
        for l in s['lines']:
            out.append(l)
        out.append("")
    Path(outdir, slug + '.song').write_text('\n'.join(out))
    return slug

if __name__ == '__main__':
    book, outdir = sys.argv[1], sys.argv[2]
    os.makedirs(outdir, exist_ok=True)
    report, n = [], 0
    # Mercy/MySongs/Trio are book/song/song.tex; Other Lies nests an album
    # level, book/album/song/song.tex.
    found = sorted(Path(book).glob('*/*.tex')) + sorted(Path(book).glob('*/*/*.tex'))
    for tex in found:
        if convert(tex, outdir, report): n += 1
    print(f"converted {n} songs -> {outdir}")
    if report:
        print(f"\n{len(report)} transposition items to verify:")
        for r in report: print("  " + r)

package chordsym

import (
	"regexp"
	"strings"
)

// Key is a song key as written in a "key:" header ("Bb", "F#m", "A minor").
// Suffix keeps the mode exactly as the author typed it, so rewriting the
// key ("A minor" -> "G minor") changes only the root.
type Key struct {
	Root   string
	Minor  bool
	Suffix string
}

// ParseKey reads a key name. ok is false for anything that isn't a plain
// major or minor key (an empty value, "dorian", free text).
func ParseKey(s string) (Key, bool) {
	s = strings.TrimSpace(s)
	root, rest, ok := splitRoot(s)
	if !ok {
		return Key{}, false
	}
	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "", "maj", "major":
		return Key{Root: root, Suffix: rest}, true
	case "m", "mi", "min", "minor", "-":
		return Key{Root: root, Minor: true, Suffix: rest}, true
	}
	return Key{}, false
}

// String renders the key the way it was written, root re-spelled.
func (k Key) String() string { return k.Root + k.Suffix }

// PitchClass is the key's tonic, 0-11 (C=0).
func (k Key) PitchClass() int { return PitchClass(k.Root) }

// SameKey reports whether two key names mean the same key ("A#m" and
// "Bbm" do, "A" and "Am" don't). Unparseable names compare as text.
func SameKey(a, b string) bool {
	ka, oka := ParseKey(a)
	kb, okb := ParseKey(b)
	if !oka || !okb {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ka.Minor == kb.Minor && ka.PitchClass() == kb.PitchClass()
}

// Conventional names for each tonic: the spelling with the fewest
// accidentals, F#/Gb going to the song's "flats:" preference.
var (
	majorKeyNames      = [12]string{"C", "Db", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}
	majorKeyNamesFlats = [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}
	minorKeyNames      = [12]string{"C", "C#", "D", "Eb", "E", "F", "F#", "G", "G#", "A", "Bb", "B"}
)

// KeyName names the major (or minor) key on tonic pc the usual way.
func KeyName(pc int, minor, flats bool) string {
	pc = ((pc % 12) + 12) % 12
	switch {
	case minor:
		return minorKeyNames[pc] + "m"
	case flats:
		return majorKeyNamesFlats[pc]
	}
	return majorKeyNames[pc]
}

// Transpose moves the key by semitones, keeping its mode and suffix and
// picking the conventional name for the new tonic.
func (k Key) Transpose(semitones int, flats bool) Key {
	name := KeyName(k.PitchClass()+semitones, k.Minor, flats)
	root, _, _ := splitRoot(name)
	return Key{Root: root, Minor: k.Minor, Suffix: k.Suffix}
}

// UsesFlats reports whether chords in this key read better with flats:
// a flat in the tonic's name, or one of the flat-side natural keys (F,
// Dm, Gm, Cm, Fm). C and Am have no accidentals of their own, so they
// follow the song's "flats:" header (flatsPref).
func (k Key) UsesFlats(flatsPref bool) bool {
	if len(k.Root) > 1 {
		return k.Root[1] == 'b'
	}
	switch {
	case !k.Minor && k.Root == "F":
		return true
	case k.Minor && strings.Contains("DGCF", k.Root):
		return true
	case !k.Minor && k.Root == "C", k.Minor && k.Root == "A":
		return flatsPref
	}
	return false
}

const letters = "CDEFGAB"

// degreeSteps is how many letter names above the tonic each semitone
// interval is written: b2, 2, b3, 3, 4, #4, 5, b6, 6, b7, 7 — the way a
// chart in that key spells borrowed and secondary chords (Bb in C, not
// A#; C# in G as the #4, F as the b7).
var degreeSteps = [12]int{0, 1, 1, 2, 2, 3, 3, 4, 5, 5, 6, 6}

// Spell names pitch class pc as it reads in this key. When the
// letter-by-degree spelling would need a double accidental or one of
// B#/E#/Cb/Fb, it falls back to the key's plain sharp or flat name.
func (k Key) Spell(pc int, flatsPref bool) string {
	pc = ((pc % 12) + 12) % 12
	flats := k.UsesFlats(flatsPref)
	if k.Root == "" {
		return SpellPitchClass(pc, flats)
	}
	iv := ((pc-k.PitchClass())%12 + 12) % 12
	steps := degreeSteps[iv]
	if iv == 6 && flats {
		steps = 4 // b5 rather than #4 on the flat side
	}
	letter := letters[(strings.IndexByte(letters, k.Root[0])+steps)%7]
	acc := ((pc-pitchClassOf[letter])%12 + 12) % 12
	if acc > 6 {
		acc -= 12
	}
	name := string(letter)
	switch acc {
	case 0:
	case 1:
		name += "#"
	case -1:
		name += "b"
	default:
		return SpellPitchClass(pc, flats)
	}
	switch name {
	case "B#", "E#", "Cb", "Fb":
		return SpellPitchClass(pc, flats)
	}
	return name
}

// qualityRE is what may follow a root for a token to count as a chord
// rather than a word: the usual quality and extension spellings, digits,
// accidentals, parentheses. "Bridge", "Break" or "Coda" don't match, so
// a section-like note in brackets is never transposed.
var qualityRE = regexp.MustCompile(`^(?:maj|Maj|MAJ|min|mi|m|M|dim|aug|sus|add|alt|no|omit|[0-9]|[b#♭♯+\-o°øΔ△^(),./])*$`)

// IsChord reports whether sym is a chord symbol with a root ("Am7",
// "D/F#", "Bbmaj7(#11)"), as opposed to N.C., %, a rest or a word.
func IsChord(sym string) bool {
	_, _, _, ok := splitChord(sym)
	return ok
}

// splitChord is a stricter Split: the quality has to look like one and a
// slash bass has to be exactly a note.
func splitChord(sym string) (root, quality, bass string, ok bool) {
	root, rest, ok := splitRoot(sym)
	if !ok {
		return "", "", "", false
	}
	quality = rest
	if i := strings.LastIndexByte(rest, '/'); i >= 0 {
		if b, after, isNote := splitRoot(rest[i+1:]); isNote && after == "" {
			quality, bass = rest[:i], b
		}
	}
	if !qualityRE.MatchString(quality) {
		return "", "", "", false
	}
	return root, quality, bass, true
}

// TransposeInKey moves a chord by semitones and spells its root and bass
// as they read in the destination key to. ok is false (sym returned as
// is) for anything that isn't a chord.
func TransposeInKey(sym string, semitones int, to Key, flatsPref bool) (string, bool) {
	root, quality, bass, ok := splitChord(sym)
	if !ok {
		return sym, false
	}
	out := to.Spell(PitchClass(root)+semitones, flatsPref) + quality
	if bass != "" {
		out += "/" + to.Spell(PitchClass(bass)+semitones, flatsPref)
	}
	return out, true
}

// SameChord reports whether two chord symbols name the same chord, however
// the root and bass are spelled ("A#m7" and "Bbm7" do). Used to match a
// chord in the song to its diagram.
func SameChord(a, b string) bool {
	ra, qa, ba, oka := splitChord(a)
	rb, qb, bb, okb := splitChord(b)
	if !oka || !okb {
		return a == b
	}
	return PitchClass(ra) == PitchClass(rb) && qa == qb && PitchClass(ba) == PitchClass(bb)
}

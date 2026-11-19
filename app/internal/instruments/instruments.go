// Package instruments is the app's one list of instruments: a fixed set
// of canonical IDs ("voice", "guitar", "bass"…), their English and French
// names, the Demucs stem each one is heard in, and Normalize, which turns
// the free text people have typed so far ("guitar & singer", "Basse",
// "drummer", "Chant") into those IDs.
//
// Granularity. One entry per instrument a jam host or a band would call
// out separately when building a line-up, and no finer:
//
//   - One "guitar", not electric/acoustic/classical: the same player
//     switches between them, a jam calls "a guitarist", and Demucs hears
//     both in its "other" stem. "Electric", "acoustic", "lead", "rhythm"
//     are read as modifiers and dropped.
//   - "bass" (electric) and "double_bass" (upright) apart: different
//     instruments to play, though both land in the bass stem.
//   - "keys" covers piano and keyboards (the same seat in a band); organ
//     and synth have their own entries because players who own one say so.
//   - Horns: trumpet, trombone and sax each have their own entry (the
//     usual horn section), "brass" catches the rest (tuba, French horn);
//     likewise flute and clarinet, with "woodwinds" for oboe, bassoon…
//   - Strings: violin (with viola), cello, double bass.
//   - Folk instruments common at jams: harmonica, ukulele, banjo,
//     mandolin, accordion.
//   - "other" for anything else; the person's own word for it is kept by
//     the caller (see Parse).
package instruments

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Family groups instruments for display (pickers list them by family).
type Family string

const (
	FamilyVoice      Family = "voice"
	FamilyGuitars    Family = "guitars" // guitars, bass and other fretted strings
	FamilyDrums      Family = "drums"   // drums and percussion
	FamilyKeys       Family = "keys"    // keyboards, accordion
	FamilyWinds      Family = "winds"   // brass, woodwinds, harmonica
	FamilyStrings    Family = "strings" // bowed strings
	FamilyOtherGroup Family = "other"
)

// Stem names, as Demucs makes them (internal/stems.Parts).
const (
	StemVocals = "vocals"
	StemDrums  = "drums"
	StemBass   = "bass"
	StemOther  = "other"
)

// Instrument is one canonical instrument.
type Instrument struct {
	ID     string // stable, lowercase ASCII; stored in the database
	EN, FR string // display names
	Family Family
	Stem   string // the Demucs stem it's heard in
}

// Other is the catch-all ID for an instrument not in the list.
const Other = "other"

// All is every instrument, in display order (by family, the common ones
// first). Other is last.
var All = []Instrument{
	{"voice", "Voice", "Chant", FamilyVoice, StemVocals},
	{"guitar", "Guitar", "Guitare", FamilyGuitars, StemOther},
	{"bass", "Bass", "Basse", FamilyGuitars, StemBass},
	{"ukulele", "Ukulele", "Ukulélé", FamilyGuitars, StemOther},
	{"banjo", "Banjo", "Banjo", FamilyGuitars, StemOther},
	{"mandolin", "Mandolin", "Mandoline", FamilyGuitars, StemOther},
	{"drums", "Drums", "Batterie", FamilyDrums, StemDrums},
	{"percussion", "Percussion", "Percussions", FamilyDrums, StemDrums},
	{"keys", "Piano / keys", "Piano / claviers", FamilyKeys, StemOther},
	{"organ", "Organ", "Orgue", FamilyKeys, StemOther},
	{"synth", "Synth", "Synthé", FamilyKeys, StemOther},
	{"accordion", "Accordion", "Accordéon", FamilyKeys, StemOther},
	{"sax", "Saxophone", "Saxophone", FamilyWinds, StemOther},
	{"trumpet", "Trumpet", "Trompette", FamilyWinds, StemOther},
	{"trombone", "Trombone", "Trombone", FamilyWinds, StemOther},
	{"brass", "Other brass", "Autres cuivres", FamilyWinds, StemOther},
	{"flute", "Flute", "Flûte", FamilyWinds, StemOther},
	{"clarinet", "Clarinet", "Clarinette", FamilyWinds, StemOther},
	{"woodwinds", "Other woodwinds", "Autres bois", FamilyWinds, StemOther},
	{"harmonica", "Harmonica", "Harmonica", FamilyWinds, StemOther},
	{"violin", "Violin / viola", "Violon / alto", FamilyStrings, StemOther},
	{"cello", "Cello", "Violoncelle", FamilyStrings, StemOther},
	{"double_bass", "Double bass", "Contrebasse", FamilyStrings, StemBass},
	{Other, "Other", "Autre", FamilyOtherGroup, StemOther},
}

var byID = func() map[string]Instrument {
	m := make(map[string]Instrument, len(All))
	for _, in := range All {
		m[in.ID] = in
	}
	return m
}()

var order = func() map[string]int {
	m := make(map[string]int, len(All))
	for i, in := range All {
		m[in.ID] = i
	}
	return m
}()

// Get returns the instrument with this ID.
func Get(id string) (Instrument, bool) {
	in, ok := byID[id]
	return in, ok
}

// Valid reports whether id is a canonical instrument ID.
func Valid(id string) bool {
	_, ok := byID[id]
	return ok
}

// Label is id's display name in lang ("fr" or anything else for
// English); an unknown id is returned as is (it's someone's own word).
func Label(id, lang string) string {
	in, ok := byID[id]
	if !ok {
		return id
	}
	if lang == "fr" {
		return in.FR
	}
	return in.EN
}

// Stem is the Demucs stem (vocals, drums, bass or other) id is heard in;
// StemOther for an unknown id.
func Stem(id string) string {
	if in, ok := byID[id]; ok {
		return in.Stem
	}
	return StemOther
}

// Sort orders ids in display order (unknown ones last, alphabetically).
func Sort(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		oi, iok := order[ids[i]]
		oj, jok := order[ids[j]]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok
		default:
			return ids[i] < ids[j]
		}
	})
}

// aliases maps a folded word or phrase (lowercase, no accents, single
// spaces) to an instrument ID. Plurals are handled by Normalize (a
// trailing "s" is tried off), so only singulars are listed, unless the
// plural is the usual form ("drums", "keys").
var aliases = map[string]string{
	// voice
	"voice": "voice", "voix": "voice", "vocal": "voice", "vocals": "voice", "vox": "voice",
	"singer": "voice", "singing": "voice", "sing": "voice", "vocalist": "voice",
	"chant": "voice", "chanteur": "voice", "chanteuse": "voice",
	"choriste": "voice", "choeur": "voice", "chorus": "voice", "choir": "voice",
	"backing vocal": "voice", "lead vocal": "voice",
	// guitar
	"guitar": "guitar", "guitare": "guitar", "guitarist": "guitar", "guitariste": "guitar",
	"gratte": "guitar", "gtr": "guitar", "guit": "guitar",
	// bass
	"bass": "bass", "basse": "bass", "bassist": "bass", "bassiste": "bass",
	"bass guitar": "bass", "guitare basse": "bass", "bass player": "bass",
	// double bass
	"double bass": "double_bass", "contrebasse": "double_bass", "contrebassiste": "double_bass",
	"upright bass": "double_bass", "upright": "double_bass", "stand up bass": "double_bass",
	"standup bass": "double_bass", "doublebass": "double_bass",
	// drums
	"drums": "drums", "drum": "drums", "drummer": "drums", "batterie": "drums",
	"batteur": "drums", "batteuse": "drums", "drum kit": "drums", "drumkit": "drums",
	// percussion
	"percussion": "percussion", "percussionist": "percussion", "percussionniste": "percussion",
	"cajon": "percussion", "conga": "percussion", "bongo": "percussion", "djembe": "percussion",
	"tambourine": "percussion", "tambourin": "percussion",
	// keys
	"keys": "keys", "key": "keys", "keyboard": "keys", "keyboardist": "keys",
	"piano": "keys", "pianist": "keys", "pianiste": "keys", "clavier": "keys",
	"claviers": "keys", "clavieriste": "keys", "rhodes": "keys", "wurlitzer": "keys",
	// organ
	"organ": "organ", "orgue": "organ", "organist": "organ", "organiste": "organ", "hammond": "organ",
	// synth
	"synth": "synth", "synthe": "synth", "synthesizer": "synth", "synthesiser": "synth",
	"synthetiseur": "synth", "synthes": "synth",
	// accordion
	"accordion": "accordion", "accordeon": "accordion", "accordionist": "accordion",
	"accordeoniste": "accordion", "bandoneon": "accordion",
	// sax
	"sax": "sax", "saxo": "sax", "saxophone": "sax", "saxophonist": "sax", "saxophoniste": "sax",
	// trumpet
	"trumpet": "trumpet", "trompette": "trumpet", "trumpeter": "trumpet", "trumpet player": "trumpet",
	"trompettiste": "trumpet", "cornet": "trumpet", "bugle": "trumpet", "flugelhorn": "trumpet",
	// trombone
	"trombone": "trombone", "tromboniste": "trombone", "trombonist": "trombone",
	// other brass
	"brass": "brass", "cuivre": "brass", "horn": "brass", "french horn": "brass", "cor": "brass",
	"tuba": "brass", "euphonium": "brass", "sousaphone": "brass", "soubassophone": "brass",
	// flute
	"flute": "flute", "flutist": "flute", "flautist": "flute", "flutiste": "flute", "piccolo": "flute",
	// clarinet
	"clarinet": "clarinet", "clarinette": "clarinet", "clarinettist": "clarinet",
	"clarinetist": "clarinet", "clarinettiste": "clarinet",
	// other woodwinds
	"woodwind": "woodwinds", "bois": "woodwinds", "oboe": "woodwinds", "hautbois": "woodwinds",
	"bassoon": "woodwinds", "basson": "woodwinds", "recorder": "woodwinds", "flute a bec": "woodwinds",
	// harmonica
	"harmonica": "harmonica", "harmo": "harmonica", "harmoniciste": "harmonica",
	"blues harp": "harmonica",
	// violin / viola
	"violin": "violin", "violon": "violin", "violinist": "violin", "violoniste": "violin",
	"fiddle": "violin", "fiddler": "violin", "viola": "violin", "violist": "violin",
	// cello
	"cello": "cello", "cellist": "cello", "violoncelle": "cello", "violoncelliste": "cello",
	"violincello": "cello", "violoncello": "cello",
	// folk
	"ukulele": "ukulele", "uke": "ukulele", "ukulelist": "ukulele",
	"banjo": "banjo", "banjoist": "banjo", "banjoiste": "banjo",
	"mandolin": "mandolin", "mandoline": "mandolin", "mandolinist": "mandolin", "mandoliniste": "mandolin",
}

// separators split one free-text entry into several instruments.
var separators = strings.NewReplacer(
	"&", ",", "/", ",", "+", ",", ";", ",", "|", ",", "\n", ",", "\\", ",",
	" and ", ",", " et ", ",", " & ", ",",
)

var folder = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// fold lowercases s, strips accents, spells œ/æ out, turns punctuation
// into spaces and collapses runs of spaces.
func fold(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("œ", "oe", "æ", "ae", "ß", "ss").Replace(s)
	if out, _, err := transform.String(folder, s); err == nil {
		s = out
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// lookup finds a phrase's instrument, trying it without a plural "s"
// (or the French "x") too.
func lookup(phrase string) (string, bool) {
	if id, ok := aliases[phrase]; ok {
		return id, true
	}
	for _, suf := range []string{"s", "x"} {
		if strings.HasSuffix(phrase, suf) && len(phrase) > 3 {
			if id, ok := aliases[strings.TrimSuffix(phrase, suf)]; ok {
				return id, true
			}
		}
	}
	return "", false
}

// Parse splits free text into canonical IDs (in the order met, without
// duplicates) and the parts it didn't recognise, trimmed as typed (the
// caller keeps them as "other" instruments under the person's own word).
// A part with at least one instrument in it counts as recognised, and
// its other words are dropped as qualifiers: "electric guitar", "lead
// vocals", "alto sax", "bass (fretless)".
func Parse(text string) (ids, unknown []string) {
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, part := range strings.Split(separators.Replace(" "+text+" "), ",") {
		raw := strings.TrimSpace(part)
		words := strings.Fields(fold(raw))
		if len(words) == 0 {
			continue
		}
		found := false
		for i := 0; i < len(words); {
			matched := false
			// Longest phrase first: "double bass" before "bass".
			for n := min(3, len(words)-i); n >= 1; n-- {
				if id, ok := lookup(strings.Join(words[i:i+n], " ")); ok {
					add(id)
					found, matched = true, true
					i += n
					break
				}
			}
			if !matched {
				i++
			}
		}
		if !found {
			unknown = append(unknown, raw)
		}
	}
	return ids, unknown
}

// Normalize turns free text ("guitar & singer", "Basse", "drummer") into
// canonical IDs, in the order met and without duplicates. Anything it
// doesn't recognise becomes a single Other. Empty text gives nil.
func Normalize(text string) []string {
	ids, unknown := Parse(text)
	if len(unknown) > 0 {
		has := false
		for _, id := range ids {
			has = has || id == Other
		}
		if !has {
			ids = append(ids, Other)
		}
	}
	return ids
}

// NormalizeAll is Normalize over a list of entries (a people.instruments
// array), merged without duplicates.
func NormalizeAll(texts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range texts {
		for _, id := range Normalize(t) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// Stems is the set of Demucs stems the instruments are heard in, in
// Demucs's order (vocals, drums, bass, other), without duplicates.
func Stems(ids []string) []string {
	has := map[string]bool{}
	for _, id := range ids {
		has[Stem(id)] = true
	}
	var out []string
	for _, s := range []string{StemVocals, StemDrums, StemBass, StemOther} {
		if has[s] {
			out = append(out, s)
		}
	}
	return out
}

// Level is a self-assessed level on one instrument. Four, each with a
// "can do" line (the research is in the PR that added them): few enough that
// neighbours are clearly apart, described by what one can do with others
// rather than by an adjective alone (self-ratings on vague words run
// high), and no "pro" level: being paid is a status, not a skill.
type Level struct {
	ID   string // stored in person_instruments.level
	Name string // English; French in internal/i18n/fr/instruments.json, as "skill|Name"
	Says string // the "can do" line, English
}

// Levels, lowest first.
var Levels = []Level{
	{"beginner", "Beginner", "I play a few chords or simple parts, slowly, on a song I've prepared."},
	{"intermediate", "Intermediate", "I keep my part and the tempo with a group, on a song I've prepared."},
	{"advanced", "Advanced", "I follow a song I don't know from a chart or by ear, and can take a short solo."},
	{"expert", "Expert", "I can lead the band, improvise freely, adapt to any style and help others."},
}

// GetLevel returns the level with this ID.
func GetLevel(id string) (Level, bool) {
	for _, l := range Levels {
		if l.ID == id {
			return l, true
		}
	}
	return Level{}, false
}

// ValidLevel reports whether id is a level's ID.
func ValidLevel(id string) bool {
	_, ok := GetLevel(id)
	return ok
}

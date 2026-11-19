package instruments

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// Values really found in people.instruments and roster slots.
		{"singer", []string{"voice"}},
		{"guitar & singer", []string{"guitar", "voice"}},
		{"Basse", []string{"bass"}},
		{"drummer", []string{"drums"}},
		{"Chant", []string{"voice"}},
		{"guitar/bass/singer", []string{"guitar", "bass", "voice"}},
		{"keys", []string{"keys"}},
		{"bassist", []string{"bass"}},
		// Plurals, agent nouns, accents, separators.
		{"Guitares", []string{"guitar"}},
		{"chanteuse, guitariste", []string{"voice", "guitar"}},
		{"Batterie + percussions", []string{"drums", "percussion"}},
		{"guitare et chant", []string{"guitar", "voice"}},
		{"bass and backing vocals", []string{"bass", "voice"}},
		{"Flûte", []string{"flute"}},
		{"Accordéon", []string{"accordion"}},
		{"chœurs", []string{"voice"}},
		{"Synthé", []string{"synth"}},
		{"violoncelle", []string{"cello"}},
		{"Pianiste; claviériste", []string{"keys"}},
		// Phrases and qualifiers.
		{"double bass", []string{"double_bass"}},
		{"Contrebasse", []string{"double_bass"}},
		{"upright bass", []string{"double_bass"}},
		{"bass guitar", []string{"bass"}},
		{"guitare basse", []string{"bass"}},
		{"electric guitar", []string{"guitar"}},
		{"Guitare électrique", []string{"guitar"}},
		{"acoustic guitar", []string{"guitar"}},
		{"lead vocals", []string{"voice"}},
		{"alto sax", []string{"sax"}},
		{"Saxophone ténor", []string{"sax"}},
		{"bass (fretless)", []string{"bass"}},
		{"trumpet player", []string{"trumpet"}},
		{"French horn", []string{"brass"}},
		{"Hautbois", []string{"woodwinds"}},
		{"ukulélé", []string{"ukulele"}},
		{"harmo", []string{"harmonica"}},
		// Unknown and empty.
		{"theremin", []string{"other"}},
		{"guitar, theremin, kazoo", []string{"guitar", "other"}},
		{"", nil},
		{"  ,  & ", nil},
	}
	for _, c := range cases {
		if got := Normalize(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeAll(t *testing.T) {
	got := NormalizeAll([]string{"bassist", "guitarist", "singer", "guitar"})
	want := []string{"bass", "guitar", "voice"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeAll = %q, want %q", got, want)
	}
	if got := NormalizeAll(nil); got != nil {
		t.Errorf("NormalizeAll(nil) = %q, want nil", got)
	}
}

func TestParseKeepsUnknownWords(t *testing.T) {
	ids, unknown := Parse("Guitar, Theremin / kazoo")
	if !reflect.DeepEqual(ids, []string{"guitar"}) {
		t.Errorf("ids = %q", ids)
	}
	if !reflect.DeepEqual(unknown, []string{"Theremin", "kazoo"}) {
		t.Errorf("unknown = %q", unknown)
	}
}

func TestAliasesPointAtRealIDs(t *testing.T) {
	for alias, id := range aliases {
		if !Valid(id) {
			t.Errorf("alias %q -> %q, not an instrument", alias, id)
		}
		if alias != fold(alias) {
			t.Errorf("alias %q isn't folded (%q): it would never match", alias, fold(alias))
		}
	}
	// Every instrument is reachable from its own English and French name.
	for _, in := range All {
		if in.ID == Other {
			continue
		}
		for _, name := range []string{in.EN, in.FR} {
			got := Normalize(name)
			if len(got) == 0 || got[0] != in.ID {
				t.Errorf("Normalize(%q) = %q, want %s first", name, got, in.ID)
			}
		}
	}
}

func TestLabelsAndStems(t *testing.T) {
	if Label("voice", "fr") != "Chant" || Label("voice", "en") != "Voice" {
		t.Error("voice labels")
	}
	if Label("theremin", "fr") != "theremin" {
		t.Error("unknown ids are their own label")
	}
	stems := map[string]string{
		"voice": "vocals", "drums": "drums", "percussion": "drums", "bass": "bass",
		"double_bass": "bass", "guitar": "other", "keys": "other", "sax": "other", "nope": "other",
	}
	for id, want := range stems {
		if got := Stem(id); got != want {
			t.Errorf("Stem(%q) = %q, want %q", id, got, want)
		}
	}
	for _, in := range All {
		switch in.Stem {
		case StemVocals, StemDrums, StemBass, StemOther:
		default:
			t.Errorf("%s: stem %q isn't a Demucs part", in.ID, in.Stem)
		}
		if in.ID != strings.ToLower(in.ID) || in.EN == "" || in.FR == "" {
			t.Errorf("%+v: bad entry", in)
		}
	}
	if got := Stems([]string{"guitar", "voice", "keys", "bass"}); !reflect.DeepEqual(got, []string{"vocals", "bass", "other"}) {
		t.Errorf("Stems = %q", got)
	}
}

func TestSort(t *testing.T) {
	ids := []string{"zither", "drums", "other", "voice", "accordion", "guitar"}
	Sort(ids)
	want := []string{"voice", "guitar", "drums", "accordion", "other", "zither"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("Sort = %q, want %q", ids, want)
	}
}

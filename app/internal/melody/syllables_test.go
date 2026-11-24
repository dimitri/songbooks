package melody

import (
	"strings"
	"testing"
)

func joinSyls(ss []Syl) string {
	var b strings.Builder
	for i, s := range ss {
		if i > 0 {
			if s.WordStart {
				b.WriteByte(' ')
			} else {
				b.WriteByte('-')
			}
		}
		b.WriteString(s.Text)
	}
	return b.String()
}

func TestSyllablesFrench(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"chanter", "chan-ter"},
		{"la montagne est belle", "la mon-tagne est belle"}, // elided before a vowel
		{"la montagne te voit", "la mon-ta-gne te voit"},
		{"une montagne", "u-ne mon-tagne"}, // sung before a consonant, not at the end,
		{"l'amour toujours", "l'a-mour tou-jours"},
		{"le poète chante", "le po-è-te chante"},
		{"naïf", "na-ïf"},
		{"quelque chose", "quel-que chose"},
		{"obstacle de", "obs-ta-cle de"},
		{"il pleuvait sur la ville", "il pleu-vait sur la ville"},
		{"c'est-à-dire", "c'est-à-dire"},
		{"Écoute, mon ami !", "É-cou-te, mon a-mi!"},
		{"ils chantent tous", "ils chan-tent tous"},
		{"(oh oh) je t'aime", "je t'aime"},
	} {
		if got := joinSyls(SplitLine(c.in, French)); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSyllablesEnglish(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"beautiful", "beau-ti-ful"},
		{"make it", "make it"},
		{"the table", "the ta-ble"},
		{"singing", "sing-ing"},
		{"walked", "walked"},
		{"wanted", "wan-ted"},
		{"wishes", "wi-shes"},
		{"tomorrow", "to-mor-row"},
		{"yesterday", "ye-ster-day"},
		{"Hello, my friend!", "Hel-lo, my friend!"},
		{"little", "litt-le"},
		{"laughing", "laugh-ing"},
		{"someone's", "so-meone's"}, // two, as sung (a compound cut by rule)
		{"don't", "don't"},
	} {
		if got := joinSyls(SplitLine(c.in, English)); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGuessLang(t *testing.T) {
	if GuessLang("Je t'aime, et la mer est calme dans le port") != French {
		t.Error("French read as English")
	}
	if GuessLang("I love you and the night is young") != English {
		t.Error("English read as French")
	}
}

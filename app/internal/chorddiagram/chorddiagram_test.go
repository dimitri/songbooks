package chorddiagram

import "testing"

func TestParseSpec_Gone(t *testing.T) {
	cases := []struct {
		name     string
		spec     string
		baseFret int
		nStrings int
		barres   int
	}{
		{"Cm", "8:XX3(111)", 8, 6, 1},
		{"G", "7:XX(3121)", 7, 6, 1},
		{"Ab", "8:X4312X", 8, 6, 0},
		{"Eb", "8:X3(111)X", 8, 6, 1},
	}
	for _, c := range cases {
		d := ParseSpec(c.spec)
		if d.BaseFret != c.baseFret {
			t.Errorf("%s: BaseFret = %d, want %d", c.name, d.BaseFret, c.baseFret)
		}
		if len(d.Strings) != c.nStrings {
			t.Errorf("%s: len(Strings) = %d, want %d", c.name, len(d.Strings), c.nStrings)
		}
		if len(d.Barres) != c.barres {
			t.Errorf("%s: len(Barres) = %d, want %d", c.name, len(d.Barres), c.barres)
		}
	}
}

func TestParseSpec_CmShape(t *testing.T) {
	// \gtab{Cm}{8:XX3(111)} — low E..high e: X X 3 (1 1 1)
	d := ParseSpec("8:XX3(111)")
	want := []StringPos{
		{State: Muted},
		{State: Muted},
		{State: Fretted, Row: 3},
		{State: Fretted, Row: 0}, // absorbed into the barre
		{State: Fretted, Row: 0},
		{State: Fretted, Row: 0},
	}
	for i, w := range want {
		if d.Strings[i] != w {
			t.Errorf("string %d = %+v, want %+v", i, d.Strings[i], w)
		}
	}
	if len(d.Barres) != 1 || d.Barres[0] != (Barre{Row: 1, FromString: 3, ToString: 5}) {
		t.Errorf("barre = %+v", d.Barres)
	}
}

func TestParseSpec_NoFretPrefix(t *testing.T) {
	d := ParseSpec("X02220")
	if d.BaseFret != 1 {
		t.Errorf("BaseFret = %d, want 1 (open position)", d.BaseFret)
	}
	if len(d.Strings) != 6 {
		t.Errorf("len(Strings) = %d, want 6", len(d.Strings))
	}
}

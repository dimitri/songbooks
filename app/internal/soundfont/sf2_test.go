package soundfont

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sonivox is the bank shipped with alphaTab (always here); GeneralUser GS
// is tested too when SONGBOOKS_SOUNDFONT points at it (docs/soundfont.md).
const sonivox = "../webassets/static/js/vendor/alphatab/soundfont/sonivox.sf2"

func openBank(t *testing.T, path string) *Bank {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(f, st.Size())
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return b
}

func generalUser(t *testing.T) *Bank {
	t.Helper()
	p := os.Getenv("SONGBOOKS_SOUNDFONT")
	if p == "" {
		t.Skip("SONGBOOKS_SOUNDFONT not set (GeneralUser GS): tested with Sonivox only")
	}
	return openBank(t, p)
}

// roundTrip writes b and parses it back.
func roundTrip(t *testing.T, b *Bank) (*Bank, int) {
	t.Helper()
	var buf bytes.Buffer
	n, err := b.WriteTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if int(n) != buf.Len() {
		t.Fatalf("WriteTo said %d bytes, wrote %d", n, buf.Len())
	}
	back, err := Parse(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("parsing the subset back: %v", err)
	}
	return back, buf.Len()
}

// samplesOf is the samples preset i plays (by name), through its
// instruments.
func samplesOf(b *Bank, i int) []string {
	var out []string
	for bag := b.Presets[i].BagIndex; bag < b.Presets[i+1].BagIndex; bag++ {
		for _, g := range b.PGens[b.PBags[bag].GenIndex:b.PBags[bag+1].GenIndex] {
			if g.Oper != genInstrument {
				continue
			}
			in := g.Amount
			for ib := b.Insts[in].BagIndex; ib < b.Insts[in+1].BagIndex; ib++ {
				for _, ig := range b.IGens[b.IBags[ib].GenIndex:b.IBags[ib+1].GenIndex] {
					if ig.Oper == genSampleID {
						out = append(out, b.Samples[ig.Amount].SampleName())
					}
				}
			}
		}
	}
	return out
}

// points reads a sample's points.
func points(t *testing.T, b *Bank, s SampleHeader) []byte {
	t.Helper()
	p := make([]byte, 2*(s.End-s.Start))
	if _, err := b.r.ReadAt(p, b.smplOff+2*int64(s.Start)); err != nil {
		t.Fatal(err)
	}
	return p
}

// checkSubset: the subset parses back, has the presets asked for (named as
// in the bank), plays the same samples with the same points.
func checkSubset(t *testing.T, b *Bank, want []PresetID) (*Bank, int) {
	t.Helper()
	sub, err := b.Subset(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	back, size := roundTrip(t, sub)
	if back.PresetCount() != len(want) {
		t.Fatalf("subset has %d presets, want %d", back.PresetCount(), len(want))
	}
	if back.InfoField("ifil") != b.InfoField("ifil") || back.InfoField("INAM") != b.InfoField("INAM") {
		t.Errorf("INFO not copied")
	}
	for _, w := range want {
		i, src := back.Find(w), b.Find(w)
		if i < 0 || src < 0 {
			t.Fatalf("preset %v: in subset %d, in bank %d", w, i, src)
		}
		if back.Presets[i].PresetName() != b.Presets[src].PresetName() {
			t.Errorf("preset %v is %q, was %q", w, back.Presets[i].PresetName(), b.Presets[src].PresetName())
		}
		got, was := samplesOf(back, i), samplesOf(b, src)
		if strings.Join(got, ",") != strings.Join(was, ",") || len(got) == 0 {
			t.Errorf("preset %v plays %v, was %v", w, got, was)
		}
	}
	// Each sample's points are the bank's.
	byName := map[string]SampleHeader{}
	for _, s := range b.Samples[:len(b.Samples)-1] {
		byName[s.SampleName()] = s
	}
	for _, s := range back.Samples[:len(back.Samples)-1] {
		o := byName[s.SampleName()]
		if s.End-s.Start != o.End-o.Start || s.EndLoop-s.StartLoop != o.EndLoop-o.StartLoop || s.StartLoop-s.Start != o.StartLoop-o.Start {
			t.Errorf("sample %s: span/loop changed", s.SampleName())
		}
		if !bytes.Equal(points(t, back, s), points(t, b, o)) {
			t.Errorf("sample %s: points differ", s.SampleName())
		}
	}
	return back, size
}

func TestSonivoxRoundTrip(t *testing.T) {
	b := openBank(t, sonivox)
	if b.PresetCount() < 128 {
		t.Fatalf("Sonivox has %d presets", b.PresetCount())
	}
	// Every preset, written back, is the same bank.
	all := make([]PresetID, 0, b.PresetCount())
	for _, p := range b.Presets[:b.PresetCount()] {
		all = append(all, PresetID{int(p.Bank), int(p.Program)})
	}
	back, size := checkSubset(t, b, all)
	if back.SamplePoints() > b.SamplePoints()+int64(len(b.Samples))*zeroPad {
		t.Errorf("every preset: %d points, the bank has %d", back.SamplePoints(), b.SamplePoints())
	}
	t.Logf("Sonivox, all %d presets: %d bytes", len(all), size)
}

func TestSonivoxSubset(t *testing.T) {
	b := openBank(t, sonivox)
	back, size := checkSubset(t, b, []PresetID{{0, 24}, {DrumBank, 0}})
	if back.PresetCount() != 2 || len(back.Samples) >= len(b.Samples) {
		t.Errorf("subset: %d presets, %d samples (bank: %d)", back.PresetCount(), len(back.Samples)-1, len(b.Samples)-1)
	}
	t.Logf("Sonivox nylon guitar + kit: %d bytes", size)
}

func TestSubsetMissingPresets(t *testing.T) {
	b := openBank(t, sonivox)
	sub, err := b.Subset([]PresetID{{8, 24}, {DrumBank, 99}, {0, 24}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := roundTrip(t, sub)
	// The missing variation is bank 0's program, under its own bank; the
	// missing kit is left out.
	if back.PresetCount() != 2 || back.Find(PresetID{8, 24}) < 0 || back.Find(PresetID{0, 24}) < 0 || back.Find(PresetID{DrumBank, 99}) >= 0 {
		t.Fatalf("got presets %v", presetIDs(back))
	}
	// The two share their instruments.
	if len(back.Insts)-1 != len(instrumentsOf(back, back.Find(PresetID{0, 24}))) {
		t.Errorf("instruments not shared: %d", len(back.Insts)-1)
	}
	if empty, err := b.Subset(nil, nil); err != nil || empty.PresetCount() != 0 {
		t.Errorf("empty subset: %v %v", empty, err)
	} else {
		roundTrip(t, empty)
	}
}

func presetIDs(b *Bank) []PresetID {
	var out []PresetID
	for _, p := range b.Presets[:b.PresetCount()] {
		out = append(out, PresetID{int(p.Bank), int(p.Program)})
	}
	return out
}

func instrumentsOf(b *Bank, i int) map[uint16]bool {
	out := map[uint16]bool{}
	for bag := b.Presets[i].BagIndex; bag < b.Presets[i+1].BagIndex; bag++ {
		for _, g := range b.PGens[b.PBags[bag].GenIndex:b.PBags[bag+1].GenIndex] {
			if g.Oper == genInstrument {
				out[g.Amount] = true
			}
		}
	}
	return out
}

// The click alone: kit 0 with only the zones that sound key 33.
func TestSubsetKeys(t *testing.T) {
	b := openBank(t, sonivox)
	full, err := b.Subset([]PresetID{Metronome}, nil)
	if err != nil {
		t.Fatal(err)
	}
	click, err := b.Subset([]PresetID{Metronome}, map[PresetID][]int{Metronome: {MetronomeKey}})
	if err != nil {
		t.Fatal(err)
	}
	back, _ := roundTrip(t, click)
	if back.PresetCount() != 1 || len(back.Samples) >= len(full.Samples) || len(back.Samples) < 2 {
		t.Fatalf("click: %d presets, %d samples (whole kit: %d)", back.PresetCount(), len(back.Samples)-1, len(full.Samples)-1)
	}
	for i, g := range back.IGens {
		if g.Oper == genKeyRange && (int(g.Amount&0xff) > MetronomeKey || int(g.Amount>>8) < MetronomeKey) {
			t.Errorf("instrument generator %d: key range %d-%d kept", i, g.Amount&0xff, g.Amount>>8)
		}
	}
}

func TestParseRejects(t *testing.T) {
	data, err := os.ReadFile(sonivox)
	if err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string][]byte{
		"empty":     nil,
		"not riff":  []byte("RIFX\x04\x00\x00\x00sfbk"),
		"not sfbk":  append([]byte("RIFF\x04\x00\x00\x00WAVE"), make([]byte, 8)...),
		"truncated": data[:len(data)/2],
	} {
		if _, err := Parse(bytes.NewReader(d), int64(len(d))); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestKey(t *testing.T) {
	k := Key([]PresetID{{0, 33}, {DrumBank, 0}, {0, 24}, {0, 24}, {DrumBank, 25}})
	if k != "0-24_0-33_128-0_128-25" {
		t.Errorf("Key = %q", k)
	}
	ps, err := ParseKey("0-33_0-24")
	if err != nil || Key(ps) != "0-24_0-33" {
		t.Errorf("ParseKey: %v %v", ps, err)
	}
	for _, bad := range []string{"", "0-128", "2048-0", "-1-0", "0-", "a-b", "0-01", "0-24__0-25", "0+24", strings.Repeat("0-1_", 65) + "0-1"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) accepted", bad)
		}
	}
}

func TestSubsetsCache(t *testing.T) {
	dir := t.TempDir()
	s := NewSubsets(sonivox, dir, 1<<40)
	if s.ID() == "" || len(s.ID()) != 12 {
		t.Fatalf("ID %q: %v", s.ID(), s.Err())
	}
	ctx := context.Background()
	p1, e1, err := s.Get(ctx, []PresetID{{0, 24}})
	if err != nil {
		t.Fatal(err)
	}
	p2, e2, err := s.Get(ctx, []PresetID{{0, 24}, {0, 24}})
	if err != nil || p1 != p2 || e1 != e2 {
		t.Fatalf("same presets, other subset: %s %s / %s %s (%v)", p1, e1, p2, e2, err)
	}
	if b := openBank(t, p1); b.PresetCount() != 2 {
		t.Errorf("cached subset has %d presets", b.PresetCount())
	}
	p3, e3, err := s.Get(ctx, []PresetID{{0, 24}, Metronome})
	if err != nil || p3 == p1 || e3 == e1 {
		t.Fatalf("other presets, same subset")
	}
	// Without the kit asked for, the click alone; with it, the whole kit.
	st1, _ := os.Stat(p1)
	st3, _ := os.Stat(p3)
	if st1.Size() >= st3.Size() {
		t.Errorf("click alone %d bytes, whole kit %d", st1.Size(), st3.Size())
	}

	// A small cap: the least recently used go.
	small := NewSubsets(sonivox, dir, st1.Size()+1)
	if _, _, err := small.Get(ctx, []PresetID{{0, 40}}); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, s.ID(), "*.sf2"))
	var total int64
	for _, p := range left {
		st, _ := os.Stat(p)
		total += st.Size()
	}
	if total > st1.Size()+1 {
		t.Errorf("cache is %d bytes over its cap (%d files)", total, len(left))
	}

	if missing := NewSubsets(filepath.Join(dir, "nope.sf2"), dir, 0); missing.ID() != "" || missing.Err() == nil {
		t.Errorf("a missing file has an ID")
	}
}

// GeneralUser GS: the subsets a score asks for, and their sizes.
func TestGeneralUserSubsets(t *testing.T) {
	b := generalUser(t)
	t.Logf("%s: %d presets, %d instruments, %d samples, %d MB of points", b.InfoField("INAM"), b.PresetCount(), len(b.Insts)-1, len(b.Samples)-1, b.SamplePoints()*2>>20)
	for _, c := range []struct {
		name string
		want []PresetID
	}{
		{"nylon guitar", []PresetID{{0, 24}}},
		{"steel guitar", []PresetID{{0, 25}}},
		{"clean electric guitar", []PresetID{{0, 27}}},
		{"electric bass (finger)", []PresetID{{0, 33}}},
		{"piano", []PresetID{{0, 0}}},
		{"band: overdriven + clean guitar, bass, drums", []PresetID{{0, 29}, {0, 27}, {0, 33}, Metronome}},
		{"band: nylon, steel, bass, piano, drums", []PresetID{{0, 24}, {0, 25}, {0, 33}, {0, 0}, Metronome}},
		{"the whole kit alone", []PresetID{Metronome}},
	} {
		back, _ := checkSubset(t, b, c.want)
		var names []string
		for i := 0; i < back.PresetCount(); i++ {
			names = append(names, back.Presets[i].PresetName()+" ("+strings.Join(samplesOf(back, i)[:min(2, len(samplesOf(back, i)))], ", ")+"…)")
		}
		t.Logf("%s: %s", c.name, strings.Join(names, "; "))
		// As served (Subsets.make): the click alone when no kit is asked for.
		dir := t.TempDir()
		path, _, err := NewSubsets(os.Getenv("SONGBOOKS_SOUNDFONT"), dir, 0).Get(context.Background(), c.want)
		if err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		t.Logf("%-45s %7.2f MB", c.name, float64(st.Size())/(1<<20))
	}
}

// alphaTab plays a subset as it plays the whole bank: the same notes from
// both, rendered offline by its synthesizer in Node, are the same sound.
func TestAlphaTabPlays(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	alphaTab, err := filepath.Abs("../webassets/static/js/vendor/alphatab/alphaTab.min.js")
	if err != nil {
		t.Fatal(err)
	}
	type sound struct {
		Samples int
		RMS     float64
		Peak    float64
		SHA256  string
		Error   string
	}
	play := func(bank, programs string) sound {
		out, err := exec.Command(node, "testdata/render.cjs", alphaTab, bank, programs).CombinedOutput()
		var s sound
		if jerr := json.Unmarshal(out, &s); err != nil || jerr != nil {
			t.Fatalf("render %s %s: %v %v\n%s", filepath.Base(bank), programs, err, jerr, out)
		}
		return s
	}
	banks := []string{sonivox}
	if p := os.Getenv("SONGBOOKS_SOUNDFONT"); p != "" {
		banks = append(banks, p)
	}
	for _, path := range banks {
		b := openBank(t, path)
		for _, programs := range []string{"24", "25,33", "48"} {
			var want []PresetID
			for _, p := range strings.Split(programs, ",") {
				n, _ := strconv.Atoi(p)
				want = append(want, PresetID{0, n})
			}
			path2, _, err := NewSubsets(path, t.TempDir(), 0).Get(context.Background(), want)
			if err != nil {
				t.Fatal(err)
			}
			whole, sub := play(path, programs), play(path2, programs)
			if sub.Error != "" || whole.Samples == 0 || whole.RMS < 0.005 {
				t.Fatalf("%s %s: whole bank %+v, subset %+v", filepath.Base(path), programs, whole, sub)
			}
			if sub.SHA256 != whole.SHA256 || sub.Samples != whole.Samples {
				t.Errorf("%s %s: the subset sounds otherwise: %+v / %+v", filepath.Base(path), programs, sub, whole)
			}
			st, _ := os.Stat(path2)
			t.Logf("%s, programs %s: %d KB, rms %.3f, same sound", b.InfoField("INAM"), programs, st.Size()>>10, sub.RMS)
		}
	}
}

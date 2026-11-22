// Package soundfont reads a SoundFont 2 bank and writes a smaller one
// holding only some of its presets (Subset), for the scores the app plays
// with alphaTab's synthesizer: a Guitar Pro file uses a handful of General
// MIDI programs, and a page downloads only those instead of the whole
// 30 MB bank (docs/soundfont.md).
//
// A SoundFont 2 file is a RIFF "sfbk" form with three lists:
//
//	INFO  the bank's name, version, copyright (copied as it is)
//	sdta  smpl: every sample's 16-bit points, one after the other
//	      (sm24: their low bytes, rarely present)
//	pdta  the "hydra": nine tables of fixed-size records —
//	      phdr presets → pbag zones → pgen/pmod generators/modulators,
//	      a zone's generator 41 naming an instrument;
//	      inst instruments → ibag zones → igen/imod,
//	      a zone's generator 53 naming a sample;
//	      shdr samples: where each one's points are in smpl.
//
// Each table ends with a terminal record whose index fields close the
// previous record's range (a preset's zones run from its bag index to the
// next preset's).
package soundfont

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
)

// DrumBank is the bank General MIDI drum kits are in (the percussion
// channel's presets).
const DrumBank = 128

// Generator operators that point at another table.
const (
	genKeyRange   = 43
	genInstrument = 41
	genSampleID   = 53
)

// Sample types (shdr.sfSampleType).
const (
	sampleMono  = 1
	sampleRight = 2
	sampleLeft  = 4
	sampleLink  = 8
	sampleROM   = 0x8000
)

// zeroPad is the run of zero points the spec wants after each sample.
const zeroPad = 46

// PresetID names a preset: a bank (DrumBank for drum kits) and a program.
type PresetID struct {
	Bank, Program int
}

func (p PresetID) String() string { return fmt.Sprintf("%d-%d", p.Bank, p.Program) }

// PresetHeader is a phdr record.
type PresetHeader struct {
	Name       [20]byte
	Program    uint16
	Bank       uint16
	BagIndex   uint16
	Library    uint32
	Genre      uint32
	Morphology uint32
}

// PresetName is the preset's name, without its padding.
func (p PresetHeader) PresetName() string { return cstr(p.Name[:]) }

// Bag is a pbag or ibag record: where a zone's generators and modulators
// start.
type Bag struct {
	GenIndex, ModIndex uint16
}

// Gen is a pgen or igen record.
type Gen struct {
	Oper   uint16
	Amount uint16
}

// Mod is a pmod or imod record, kept as its 10 bytes (nothing in it points
// at another table).
type Mod [10]byte

// InstHeader is an inst record.
type InstHeader struct {
	Name     [20]byte
	BagIndex uint16
}

// SampleHeader is a shdr record; Start, End and the loop points are
// offsets into smpl, in sample points.
type SampleHeader struct {
	Name            [20]byte
	Start, End      uint32
	StartLoop       uint32
	EndLoop         uint32
	SampleRate      uint32
	OriginalPitch   uint8
	PitchCorrection int8
	Link            uint16
	Type            uint16
}

// SampleName is the sample's name, without its padding.
func (s SampleHeader) SampleName() string { return cstr(s.Name[:]) }

// Bank is a parsed SoundFont: its hydra in memory, its sample points read
// from r when needed. Every table keeps its terminal record.
type Bank struct {
	r                io.ReaderAt
	info             []byte // the INFO list's body, after "INFO"
	smplOff, smplLen int64  // the smpl chunk's data, in bytes
	sm24Off, sm24Len int64  // 0, 0 without sm24

	Presets []PresetHeader
	PBags   []Bag
	PMods   []Mod
	PGens   []Gen
	Insts   []InstHeader
	IBags   []Bag
	IMods   []Mod
	IGens   []Gen
	Samples []SampleHeader
}

// SamplePoints is how many 16-bit points smpl holds.
func (b *Bank) SamplePoints() int64 { return b.smplLen / 2 }

// Info is the INFO list's body (its sub-chunks).
func (b *Bank) Info() []byte { return b.info }

// InfoField is one INFO sub-chunk's text ("INAM", "ICOP"…), "" if absent.
func (b *Bank) InfoField(id string) string {
	d := b.info
	for len(d) >= 8 {
		n := int(binary.LittleEndian.Uint32(d[4:8]))
		if 8+n > len(d) {
			break
		}
		if string(d[:4]) == id {
			return cstr(d[8 : 8+n])
		}
		d = d[8+n+n%2:]
	}
	return ""
}

// PresetCount is how many presets the bank has (its terminal excluded).
func (b *Bank) PresetCount() int { return len(b.Presets) - 1 }

// Find is the index of the preset at id, -1 if none.
func (b *Bank) Find(id PresetID) int {
	for i := 0; i < b.PresetCount(); i++ {
		if int(b.Presets[i].Bank) == id.Bank && int(b.Presets[i].Program) == id.Program {
			return i
		}
	}
	return -1
}

// ---- Reading ---------------------------------------------------------------

type chunk struct {
	id  string
	off int64 // the data's offset
	n   int64 // the data's size
}

func readChunk(r io.ReaderAt, off, end int64) (chunk, error) {
	if off+8 > end {
		return chunk{}, errors.New("soundfont: truncated chunk header")
	}
	var h [8]byte
	if _, err := r.ReadAt(h[:], off); err != nil {
		return chunk{}, fmt.Errorf("soundfont: reading a chunk header: %w", err)
	}
	c := chunk{id: string(h[:4]), off: off + 8, n: int64(binary.LittleEndian.Uint32(h[4:]))}
	if c.off+c.n > end {
		return chunk{}, fmt.Errorf("soundfont: chunk %q runs past its parent", c.id)
	}
	return c, nil
}

// next is the offset after c (chunks are padded to an even size).
func (c chunk) next() int64 { return c.off + c.n + c.n%2 }

func readAll(r io.ReaderAt, c chunk) ([]byte, error) {
	b := make([]byte, c.n)
	if _, err := r.ReadAt(b, c.off); err != nil {
		return nil, fmt.Errorf("soundfont: reading %q: %w", c.id, err)
	}
	return b, nil
}

// Parse reads the bank in r (size bytes): the INFO list and the hydra are
// read now, the sample points stay in r. The bank is checked (Check).
func Parse(r io.ReaderAt, size int64) (*Bank, error) {
	riff, err := readChunk(r, 0, size)
	if err != nil {
		return nil, err
	}
	var form [4]byte
	if riff.id != "RIFF" || riff.n < 4 {
		return nil, errors.New("soundfont: not a RIFF file")
	}
	if _, err := r.ReadAt(form[:], riff.off); err != nil || string(form[:]) != "sfbk" {
		return nil, errors.New("soundfont: not a SoundFont (sfbk) file")
	}
	b := &Bank{r: r}
	hasSmpl := false
	end := riff.off + riff.n
	var pdta map[string][]byte
	for off := riff.off + 4; off+8 <= end; {
		c, err := readChunk(r, off, end)
		if err != nil {
			return nil, err
		}
		off = c.next()
		if c.id != "LIST" || c.n < 4 {
			continue
		}
		var kind [4]byte
		if _, err := r.ReadAt(kind[:], c.off); err != nil {
			return nil, err
		}
		body := chunk{id: string(kind[:]), off: c.off + 4, n: c.n - 4}
		switch body.id {
		case "INFO":
			if b.info, err = readAll(r, body); err != nil {
				return nil, err
			}
		case "sdta":
			for o := body.off; o+8 <= body.off+body.n; {
				s, err := readChunk(r, o, body.off+body.n)
				if err != nil {
					return nil, err
				}
				o = s.next()
				switch s.id {
				case "smpl":
					b.smplOff, b.smplLen, hasSmpl = s.off, s.n, true
				case "sm24":
					b.sm24Off, b.sm24Len = s.off, s.n
				}
			}
		case "pdta":
			pdta = map[string][]byte{}
			for o := body.off; o+8 <= body.off+body.n; {
				s, err := readChunk(r, o, body.off+body.n)
				if err != nil {
					return nil, err
				}
				o = s.next()
				if pdta[s.id], err = readAll(r, s); err != nil {
					return nil, err
				}
			}
		}
	}
	if pdta == nil {
		return nil, errors.New("soundfont: no pdta list")
	}
	if !hasSmpl {
		return nil, errors.New("soundfont: no sample data")
	}
	if b.sm24Len != 0 && b.sm24Len < b.smplLen/2 {
		b.sm24Off, b.sm24Len = 0, 0 // a short sm24 is ignored, as the spec says
	}
	if err := b.readHydra(pdta); err != nil {
		return nil, err
	}
	return b, b.Check()
}

func records(pdta map[string][]byte, id string, size int) ([]byte, int, error) {
	d, ok := pdta[id]
	if !ok {
		return nil, 0, fmt.Errorf("soundfont: no %q table", id)
	}
	if len(d)%size != 0 || len(d) < size {
		return nil, 0, fmt.Errorf("soundfont: %q is %d bytes, not whole %d-byte records", id, len(d), size)
	}
	return d, len(d) / size, nil
}

func (b *Bank) readHydra(pdta map[string][]byte) error {
	le := binary.LittleEndian
	d, n, err := records(pdta, "phdr", 38)
	if err != nil {
		return err
	}
	b.Presets = make([]PresetHeader, n)
	for i := range b.Presets {
		r := d[i*38:]
		p := &b.Presets[i]
		copy(p.Name[:], r[:20])
		p.Program, p.Bank, p.BagIndex = le.Uint16(r[20:]), le.Uint16(r[22:]), le.Uint16(r[24:])
		p.Library, p.Genre, p.Morphology = le.Uint32(r[26:]), le.Uint32(r[30:]), le.Uint32(r[34:])
	}
	bags := func(id string) ([]Bag, error) {
		d, n, err := records(pdta, id, 4)
		if err != nil {
			return nil, err
		}
		out := make([]Bag, n)
		for i := range out {
			out[i] = Bag{le.Uint16(d[i*4:]), le.Uint16(d[i*4+2:])}
		}
		return out, nil
	}
	gens := func(id string) ([]Gen, error) {
		d, n, err := records(pdta, id, 4)
		if err != nil {
			return nil, err
		}
		out := make([]Gen, n)
		for i := range out {
			out[i] = Gen{le.Uint16(d[i*4:]), le.Uint16(d[i*4+2:])}
		}
		return out, nil
	}
	mods := func(id string) ([]Mod, error) {
		d, n, err := records(pdta, id, 10)
		if err != nil {
			return nil, err
		}
		out := make([]Mod, n)
		for i := range out {
			copy(out[i][:], d[i*10:])
		}
		return out, nil
	}
	if b.PBags, err = bags("pbag"); err != nil {
		return err
	}
	if b.PMods, err = mods("pmod"); err != nil {
		return err
	}
	if b.PGens, err = gens("pgen"); err != nil {
		return err
	}
	if d, n, err = records(pdta, "inst", 22); err != nil {
		return err
	}
	b.Insts = make([]InstHeader, n)
	for i := range b.Insts {
		copy(b.Insts[i].Name[:], d[i*22:i*22+20])
		b.Insts[i].BagIndex = le.Uint16(d[i*22+20:])
	}
	if b.IBags, err = bags("ibag"); err != nil {
		return err
	}
	if b.IMods, err = mods("imod"); err != nil {
		return err
	}
	if b.IGens, err = gens("igen"); err != nil {
		return err
	}
	if d, n, err = records(pdta, "shdr", 46); err != nil {
		return err
	}
	b.Samples = make([]SampleHeader, n)
	for i := range b.Samples {
		r := d[i*46:]
		s := &b.Samples[i]
		copy(s.Name[:], r[:20])
		s.Start, s.End, s.StartLoop, s.EndLoop, s.SampleRate = le.Uint32(r[20:]), le.Uint32(r[24:]), le.Uint32(r[28:]), le.Uint32(r[32:]), le.Uint32(r[36:])
		s.OriginalPitch, s.PitchCorrection = r[40], int8(r[41])
		s.Link, s.Type = le.Uint16(r[42:]), le.Uint16(r[44:])
	}
	return nil
}

// Check makes sure every index in the hydra points where it can: zones in
// order and inside their tables, instruments and samples that exist, sample
// points inside smpl.
func (b *Bank) Check() error {
	ascending := func(what string, idx []int, limit int) error {
		for i, v := range idx {
			if v > limit || (i > 0 && v < idx[i-1]) {
				return fmt.Errorf("soundfont: %s index %d (%d) out of order or past %d", what, i, v, limit)
			}
		}
		return nil
	}
	pb := make([]int, len(b.Presets))
	for i, p := range b.Presets {
		pb[i] = int(p.BagIndex)
	}
	if err := ascending("preset bag", pb, len(b.PBags)-1); err != nil {
		return err
	}
	ib := make([]int, len(b.Insts))
	for i, p := range b.Insts {
		ib[i] = int(p.BagIndex)
	}
	if err := ascending("instrument bag", ib, len(b.IBags)-1); err != nil {
		return err
	}
	for _, t := range []struct {
		what       string
		bags       []Bag
		gens, mods int
	}{{"preset", b.PBags, len(b.PGens), len(b.PMods)}, {"instrument", b.IBags, len(b.IGens), len(b.IMods)}} {
		g, m := make([]int, len(t.bags)), make([]int, len(t.bags))
		for i, bg := range t.bags {
			g[i], m[i] = int(bg.GenIndex), int(bg.ModIndex)
		}
		if err := ascending(t.what+" generator", g, t.gens-1); err != nil {
			return err
		}
		if err := ascending(t.what+" modulator", m, t.mods-1); err != nil {
			return err
		}
	}
	for i, g := range b.PGens[:len(b.PGens)-1] {
		if g.Oper == genInstrument && int(g.Amount) >= len(b.Insts)-1 {
			return fmt.Errorf("soundfont: preset generator %d names instrument %d of %d", i, g.Amount, len(b.Insts)-1)
		}
	}
	for i, g := range b.IGens[:len(b.IGens)-1] {
		if g.Oper == genSampleID && int(g.Amount) >= len(b.Samples)-1 {
			return fmt.Errorf("soundfont: instrument generator %d names sample %d of %d", i, g.Amount, len(b.Samples)-1)
		}
	}
	points := uint32(b.SamplePoints())
	for i, s := range b.Samples[:len(b.Samples)-1] {
		if s.Type&sampleROM != 0 {
			continue
		}
		if s.Start > s.End || s.End > points {
			return fmt.Errorf("soundfont: sample %d (%s) runs %d–%d, smpl has %d points", i, s.SampleName(), s.Start, s.End, points)
		}
		if s.Type&(sampleRight|sampleLeft|sampleLink) != 0 && int(s.Link) >= len(b.Samples)-1 {
			return fmt.Errorf("soundfont: sample %d (%s) links to sample %d of %d", i, s.SampleName(), s.Link, len(b.Samples)-1)
		}
	}
	return nil
}

// ---- Subsetting ------------------------------------------------------------

// Subset is a bank with only the presets wanted (in bank, program order),
// the instruments they play and the samples those play. A preset of a
// melodic bank the bank doesn't have is bank 0's program, under the bank
// asked for (what GS synthesizers do for a missing variation); a drum kit
// it doesn't have is left out (alphaTab plays kit 0 for it). Presets found
// in neither are left out.
//
// keys, when it names a preset, keeps only that preset's zones (and its
// instruments' zones) that sound one of those keys: a drum kit's click
// alone, say, not its every drum.
func (b *Bank) Subset(wanted []PresetID, keys map[PresetID][]int) (*Bank, error) {
	type pick struct {
		src int
		as  PresetID
	}
	seen := map[PresetID]bool{}
	var picks []pick
	for _, w := range wanted {
		if seen[w] {
			continue
		}
		seen[w] = true
		i := b.Find(w)
		if i < 0 && w.Bank != DrumBank && w.Bank != 0 {
			i = b.Find(PresetID{0, w.Program})
		}
		if i >= 0 {
			picks = append(picks, pick{i, w})
		}
	}
	sort.Slice(picks, func(i, j int) bool {
		a, c := picks[i].as, picks[j].as
		return a.Bank < c.Bank || (a.Bank == c.Bank && a.Program < c.Program)
	})

	// sounds: whether a zone (its generators) plays one of only's keys.
	sounds := func(gens []Gen, only []int) bool {
		if only == nil {
			return true
		}
		for _, g := range gens {
			if g.Oper != genKeyRange {
				continue
			}
			lo, hi := int(g.Amount&0xff), int(g.Amount>>8)
			for _, k := range only {
				if k >= lo && k <= hi {
					return true
				}
			}
			return false
		}
		return true
	}

	out := &Bank{info: b.info}
	// An instrument is copied once per set of keys it's kept for.
	type instUse struct {
		old  int
		keys string
	}
	instMap := map[instUse]int{}
	sampleMap := map[int]int{} // old sample → new
	var instOrder []instUse
	var sampleOrder []int
	instKeys := map[string][]int{}

	// Presets, their zones and generators; instruments in the order met.
	for _, p := range picks {
		only := keys[p.as]
		sig := ""
		if only != nil {
			sig = fmt.Sprint(only)
			instKeys[sig] = only
		}
		h := b.Presets[p.src]
		h.Bank, h.Program = uint16(p.as.Bank), uint16(p.as.Program)
		h.BagIndex = uint16(len(out.PBags))
		out.Presets = append(out.Presets, h)
		for bag := int(b.Presets[p.src].BagIndex); bag < int(b.Presets[p.src+1].BagIndex); bag++ {
			gens := b.PGens[b.PBags[bag].GenIndex:b.PBags[bag+1].GenIndex]
			if !sounds(gens, only) {
				continue
			}
			out.PBags = append(out.PBags, Bag{uint16(len(out.PGens)), uint16(len(out.PMods))})
			out.PMods = append(out.PMods, b.PMods[b.PBags[bag].ModIndex:b.PBags[bag+1].ModIndex]...)
			for _, g := range gens {
				if g.Oper == genInstrument {
					use := instUse{int(g.Amount), sig}
					n, ok := instMap[use]
					if !ok {
						n = len(instOrder)
						instMap[use] = n
						instOrder = append(instOrder, use)
					}
					g.Amount = uint16(n)
				}
				out.PGens = append(out.PGens, g)
			}
		}
	}

	// Instruments; samples in the order met, then the samples those link to.
	addSample := func(old int) int {
		n, ok := sampleMap[old]
		if !ok {
			n = len(sampleOrder)
			sampleMap[old] = n
			sampleOrder = append(sampleOrder, old)
		}
		return n
	}
	for _, use := range instOrder {
		old, only := use.old, instKeys[use.keys]
		h := b.Insts[old]
		h.BagIndex = uint16(len(out.IBags))
		out.Insts = append(out.Insts, h)
		for bag := int(b.Insts[old].BagIndex); bag < int(b.Insts[old+1].BagIndex); bag++ {
			gens := b.IGens[b.IBags[bag].GenIndex:b.IBags[bag+1].GenIndex]
			if !sounds(gens, only) {
				continue
			}
			out.IBags = append(out.IBags, Bag{uint16(len(out.IGens)), uint16(len(out.IMods))})
			out.IMods = append(out.IMods, b.IMods[b.IBags[bag].ModIndex:b.IBags[bag+1].ModIndex]...)
			for _, g := range gens {
				if g.Oper == genSampleID {
					g.Amount = uint16(addSample(int(g.Amount)))
				}
				out.IGens = append(out.IGens, g)
			}
		}
	}
	for i := 0; i < len(sampleOrder); i++ { // grows as links are met
		s := b.Samples[sampleOrder[i]]
		if s.Type&sampleROM == 0 && s.Type&(sampleRight|sampleLeft|sampleLink) != 0 {
			addSample(int(s.Link))
		}
	}
	if len(out.Presets) > 0xfffe || len(out.PBags) > 0xfffe || len(out.PGens) > 0xfffe || len(out.PMods) > 0xfffe ||
		len(out.IBags) > 0xfffe || len(out.IGens) > 0xfffe || len(out.IMods) > 0xfffe {
		return nil, errors.New("soundfont: subset too large")
	}

	// Sample points: each sample's span (its loop included) once, then the
	// spec's run of zeros. Samples sharing a span share it here too.
	type span struct{ lo, hi uint32 }
	placed := map[span]uint32{}
	var spans []span
	var pos uint32
	for _, old := range sampleOrder {
		s := b.Samples[old]
		if s.Type&sampleROM != 0 {
			s.Start, s.End, s.StartLoop, s.EndLoop = 0, 0, 0, 0
			out.Samples = append(out.Samples, s)
			continue
		}
		sp := span{s.Start, s.End}
		if s.StartLoop < s.EndLoop && s.StartLoop >= s.Start && s.EndLoop <= uint32(b.SamplePoints()) {
			sp.hi = max(sp.hi, s.EndLoop)
		}
		at, ok := placed[sp]
		if !ok {
			at = pos
			placed[sp] = at
			spans = append(spans, sp)
			pos += sp.hi - sp.lo + zeroPad
		}
		shift := func(v uint32) uint32 { return v - sp.lo + at }
		s.Start, s.End = shift(s.Start), shift(s.End)
		if s.StartLoop >= sp.lo && s.StartLoop <= sp.hi && s.EndLoop >= sp.lo && s.EndLoop <= sp.hi {
			s.StartLoop, s.EndLoop = shift(s.StartLoop), shift(s.EndLoop)
		} else {
			s.StartLoop, s.EndLoop = s.Start, s.Start
		}
		if s.Type&(sampleRight|sampleLeft|sampleLink) != 0 {
			s.Link = uint16(sampleMap[int(s.Link)])
		}
		out.Samples = append(out.Samples, s)
	}

	// The sample points, read now, into memory.
	smpl := make([]byte, 2*int64(pos))
	var sm24 []byte
	if b.sm24Len != 0 {
		sm24 = make([]byte, pos)
	}
	for _, sp := range spans {
		at := placed[sp]
		n := sp.hi - sp.lo
		if _, err := b.r.ReadAt(smpl[2*int64(at):2*int64(at+n)], b.smplOff+2*int64(sp.lo)); err != nil {
			return nil, fmt.Errorf("soundfont: reading samples: %w", err)
		}
		if sm24 != nil {
			if _, err := b.r.ReadAt(sm24[at:at+n], b.sm24Off+int64(sp.lo)); err != nil {
				return nil, fmt.Errorf("soundfont: reading sm24: %w", err)
			}
		}
	}
	out.r = memReader{smpl, sm24}
	out.smplOff, out.smplLen = 0, int64(len(smpl))
	if sm24 != nil {
		out.sm24Off, out.sm24Len = int64(len(smpl)), int64(len(sm24))
	}

	// Terminal records.
	eop := PresetHeader{BagIndex: uint16(len(out.PBags))}
	copy(eop.Name[:], "EOP")
	out.Presets = append(out.Presets, eop)
	out.PBags = append(out.PBags, Bag{uint16(len(out.PGens)), uint16(len(out.PMods))})
	out.PMods = append(out.PMods, Mod{})
	out.PGens = append(out.PGens, Gen{})
	eoi := InstHeader{BagIndex: uint16(len(out.IBags))}
	copy(eoi.Name[:], "EOI")
	out.Insts = append(out.Insts, eoi)
	out.IBags = append(out.IBags, Bag{uint16(len(out.IGens)), uint16(len(out.IMods))})
	out.IMods = append(out.IMods, Mod{})
	out.IGens = append(out.IGens, Gen{})
	var eos SampleHeader
	copy(eos.Name[:], "EOS")
	out.Samples = append(out.Samples, eos)
	return out, out.Check()
}

// memReader is a subset's sample points: smpl, then sm24.
type memReader struct{ smpl, sm24 []byte }

func (m memReader) ReadAt(p []byte, off int64) (int, error) {
	all := int64(len(m.smpl))
	var src []byte
	switch {
	case off < all:
		src = m.smpl[off:]
	case off-all < int64(len(m.sm24)):
		src = m.sm24[off-all:]
	}
	n := copy(p, src)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// ---- Writing ---------------------------------------------------------------

// WriteTo writes the bank as a SoundFont 2 file.
func (b *Bank) WriteTo(w io.Writer) (int64, error) {
	le := binary.LittleEndian
	hdr := func(id string, n int) []byte {
		h := make([]byte, 8)
		copy(h, id)
		le.PutUint32(h[4:], uint32(n))
		return h
	}
	padded := func(n int) int { return n + n%2 }

	// pdta, built in memory (a few hundred KB at most).
	var pdta []byte
	table := func(id string, rec int, n int, put func(r []byte, i int)) {
		d := make([]byte, rec*n)
		for i := 0; i < n; i++ {
			put(d[i*rec:], i)
		}
		pdta = append(pdta, hdr(id, len(d))...)
		pdta = append(pdta, d...)
	}
	table("phdr", 38, len(b.Presets), func(r []byte, i int) {
		p := b.Presets[i]
		copy(r, p.Name[:])
		le.PutUint16(r[20:], p.Program)
		le.PutUint16(r[22:], p.Bank)
		le.PutUint16(r[24:], p.BagIndex)
		le.PutUint32(r[26:], p.Library)
		le.PutUint32(r[30:], p.Genre)
		le.PutUint32(r[34:], p.Morphology)
	})
	putBags := func(bags []Bag) func(r []byte, i int) {
		return func(r []byte, i int) { le.PutUint16(r, bags[i].GenIndex); le.PutUint16(r[2:], bags[i].ModIndex) }
	}
	putGens := func(gens []Gen) func(r []byte, i int) {
		return func(r []byte, i int) { le.PutUint16(r, gens[i].Oper); le.PutUint16(r[2:], gens[i].Amount) }
	}
	putMods := func(mods []Mod) func(r []byte, i int) {
		return func(r []byte, i int) { copy(r, mods[i][:]) }
	}
	table("pbag", 4, len(b.PBags), putBags(b.PBags))
	table("pmod", 10, len(b.PMods), putMods(b.PMods))
	table("pgen", 4, len(b.PGens), putGens(b.PGens))
	table("inst", 22, len(b.Insts), func(r []byte, i int) {
		copy(r, b.Insts[i].Name[:])
		le.PutUint16(r[20:], b.Insts[i].BagIndex)
	})
	table("ibag", 4, len(b.IBags), putBags(b.IBags))
	table("imod", 10, len(b.IMods), putMods(b.IMods))
	table("igen", 4, len(b.IGens), putGens(b.IGens))
	table("shdr", 46, len(b.Samples), func(r []byte, i int) {
		s := b.Samples[i]
		copy(r, s.Name[:])
		le.PutUint32(r[20:], s.Start)
		le.PutUint32(r[24:], s.End)
		le.PutUint32(r[28:], s.StartLoop)
		le.PutUint32(r[32:], s.EndLoop)
		le.PutUint32(r[36:], s.SampleRate)
		r[40], r[41] = s.OriginalPitch, byte(s.PitchCorrection)
		le.PutUint16(r[42:], s.Link)
		le.PutUint16(r[44:], s.Type)
	})

	smplN, sm24N := int(b.smplLen), int(b.sm24Len)
	sdtaN := 4 + 8 + padded(smplN)
	if sm24N > 0 {
		sdtaN += 8 + padded(sm24N)
	}
	infoN := 4 + len(b.info)
	pdtaN := 4 + len(pdta)
	riffN := 4 + 8 + padded(infoN) + 8 + padded(sdtaN) + 8 + pdtaN

	cw := &countWriter{w: w}
	put := func(p []byte) {
		if cw.err == nil {
			_, cw.err = cw.Write(p)
		}
	}
	pad := func(n int) {
		if n%2 == 1 {
			put([]byte{0})
		}
	}
	put(hdr("RIFF", riffN))
	put([]byte("sfbk"))
	put(hdr("LIST", infoN))
	put([]byte("INFO"))
	put(b.info)
	pad(infoN)
	put(hdr("LIST", sdtaN))
	put([]byte("sdta"))
	copyPoints := func(id string, off int64, n int) {
		put(hdr(id, n))
		if cw.err == nil {
			_, cw.err = io.Copy(cw, io.NewSectionReader(b.r, off, int64(n)))
		}
		pad(n)
	}
	copyPoints("smpl", b.smplOff, smplN)
	if sm24N > 0 {
		copyPoints("sm24", b.sm24Off, sm24N)
	}
	put(hdr("LIST", pdtaN))
	put([]byte("pdta"))
	put(pdta)
	return cw.n, cw.err
}

type countWriter struct {
	w   io.Writer
	n   int64
	err error
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

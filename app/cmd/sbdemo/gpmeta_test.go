package main

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

// A made-up score.gpif with every field DemoGPIF touches.
const sampleGPIF = `<?xml version="1.0" encoding="utf-8"?>
<GPIF><Score>
<Title><![CDATA[Real Title]]></Title>
<SubTitle><![CDATA[Live]]></SubTitle>
<Artist><![CDATA[Real Artist]]></Artist>
<Album><![CDATA[Real Album]]></Album>
<Words><![CDATA[Real Lyricist]]></Words>
<Music><![CDATA[Real Composer]]></Music>
<WordsAndMusic><![CDATA[]]></WordsAndMusic>
<Copyright><![CDATA[Real Publisher]]></Copyright>
<Tabber><![CDATA[Real Tabber]]></Tabber>
<Instructions><![CDATA[]]></Instructions>
<Notices><![CDATA[]]></Notices>
<FirstPageHeader><![CDATA[<p>Real Title by Real Artist</p>]]></FirstPageHeader>
<FirstPageFooter><![CDATA[Real Footer]]></FirstPageFooter>
<PageHeader><![CDATA[]]></PageHeader>
<PageFooter><![CDATA[Real Page Footer]]></PageFooter>
</Score>
<Tracks><Track id="0"><Name><![CDATA[Clean Guitar]]></Name>
<Lyrics dispatched="true"><Line><Text><![CDATA[invented words to sing]]></Text><Offset>0</Offset></Line></Lyrics>
</Track></Tracks>
<Beats><Beat id="0"><FreeText><![CDATA[Real Note]]></FreeText><Lyrics><Line><![CDATA[syl]]></Line></Lyrics></Beat></Beats>
<BackingTrack><OriginalFilePath><![CDATA[/Users/someone/Real Title.mp3]]></OriginalFilePath><EmbeddedFilePath><![CDATA[Content/Assets/a.mp3]]></EmbeddedFilePath></BackingTrack>
</GPIF>`

func TestDemoGPIF(t *testing.T) {
	out := string(DemoGPIF([]byte(sampleGPIF), "Cobalt Morning", "Velvet Hour"))
	if strings.Contains(out, "Real") || strings.Contains(out, "someone") {
		t.Fatalf("a real name is left:\n%s", out)
	}
	for _, want := range []string{
		"<Title><![CDATA[Cobalt Morning]]></Title>", "<Artist><![CDATA[Velvet Hour]]></Artist>",
		"<Name><![CDATA[Clean Guitar]]></Name>", "<Offset>0</Offset>",
		"<EmbeddedFilePath><![CDATA[Content/Assets/a.mp3]]></EmbeddedFilePath>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(out, "invented words") || strings.Contains(out, "syl") {
		t.Error("lyrics left")
	}
}

func TestDemoGP(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"VERSION": "7.0", gpifPath: sampleGPIF, "Content/Assets/a.mp3": "not really audio"} {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	zw.Close()
	out, err := DemoGP(buf.Bytes(), "Cobalt Morning", "Velvet Hour")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	if got["VERSION"] != "7.0" || got["Content/Assets/a.mp3"] != "not really audio" {
		t.Fatal("other entries should be copied as they were")
	}
	if !strings.Contains(got[gpifPath], "Cobalt Morning") || strings.Contains(got[gpifPath], "Real") {
		t.Fatal("score not rewritten")
	}
	again, err := DemoGP(out, "Cobalt Morning", "Velvet Hour")
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("a second pass should change nothing (%v)", err)
	}
	if _, err := DemoGP([]byte("FICHIER GUITAR PRO v5.00"), "x", "y"); err != errNotGP {
		t.Fatalf("an old .gp5: got %v", err)
	}
}

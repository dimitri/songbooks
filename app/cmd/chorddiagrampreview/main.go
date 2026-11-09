package main

import (
	"fmt"
	"os"

	"app/internal/chorddiagram"
)

func main() {
	specs := []struct{ name, spec string }{
		{"Cm", "8:XX3(111)"},
		{"G", "7:XX(3121)"},
		{"Ab", "8:X4312X"},
		{"Eb", "8:X3(111)X"},
		{"Am", "X02210"},
		{"D9/F#", "2:X10213"},
	}
	f, _ := os.Create("/tmp/chorddiagram_preview.html")
	defer f.Close()
	fmt.Fprint(f, `<!doctype html><html><head><meta charset="utf-8"><style>
	body{background:#f6f1e7;font-family:Georgia,serif;padding:2rem;}
	.row{display:flex;gap:28px;align-items:flex-end;}
	.chord-diagram-name{font-weight:bold;font-size:14px;fill:#241e18;}
	.chord-diagram-nut{stroke:#241e18;stroke-width:3;}
	.chord-diagram-fretline{stroke:#241e18;stroke-width:1;}
	.chord-diagram-string{stroke:#241e18;stroke-width:1;}
	.chord-diagram-barre{stroke:#241e18;}
	.chord-diagram-dot{fill:#241e18;}
	.chord-diagram-open{fill:none;stroke:#241e18;stroke-width:1.5;}
	.chord-diagram-muted line{stroke:#241e18;stroke-width:1.5;}
	.chord-diagram-fretnum{font-size:11px;fill:#241e18;font-family:Georgia,serif;}
	</style></head><body><div class="row">`)
	for _, s := range specs {
		d := chorddiagram.ParseSpec(s.spec)
		fmt.Fprint(f, chorddiagram.RenderSVG(d, s.name))
	}
	fmt.Fprint(f, `</div></body></html>`)
}

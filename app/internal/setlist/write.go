package setlist

import (
	"fmt"
	"strings"
)

func Write(l *Setlist) string {
	var b strings.Builder
	for _, f := range l.Meta {
		b.WriteString(f.Key + ": " + f.Value + "\n")
	}
	for _, set := range l.Sets {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[" + set.Label + "]\n")
		for _, e := range set.Entries {
			if e.IsVariant() {
				b.WriteString(fmt.Sprintf("- variant:%d\n", e.VariantID))
			} else {
				b.WriteString("- " + e.Book + "/" + e.Stem + "\n")
			}
			if e.Note != "" {
				b.WriteString("  note: " + e.Note + "\n")
			}
		}
	}
	return b.String()
}

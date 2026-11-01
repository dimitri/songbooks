package webassets

import "testing"

func TestTemplatesParse(t *testing.T) {
	if len(Templates.DefinedTemplates()) == 0 {
		t.Fatal("no templates defined")
	}
}

package server

import (
	"strings"
	"testing"
)

func TestThemeCSSKeepsTokenColorsOnly(t *testing.T) {
	css := themeCSS("github")
	if !strings.Contains(css, ".chroma .k {") {
		t.Fatalf("no keyword rule in:\n%s", css)
	}
	if strings.Contains(css, "background") {
		t.Errorf("background rule leaked:\n%s", css)
	}
}

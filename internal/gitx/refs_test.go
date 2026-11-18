package gitx

import "testing"

func TestParseTrack(t *testing.T) {
	for in, want := range map[string][3]int{
		"":                   {0, 0, 0},
		"ahead 3":            {3, 0, 0},
		"behind 2":           {0, 2, 0},
		"ahead 1, behind 12": {1, 12, 0},
		"gone":               {0, 0, 1},
	} {
		a, b, g := parseTrack(in)
		gi := 0
		if g {
			gi = 1
		}
		if got := [3]int{a, b, gi}; got != want {
			t.Errorf("parseTrack(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRemoteOf(t *testing.T) {
	remotes := []string{"origin", "fork", "fork/sub"}
	for in, want := range map[string]string{
		"origin/main":         "origin",
		"origin/feature/x":    "origin",
		"fork/sub/topic":      "fork/sub",
		"fork/topic":          "fork",
		"unknown/branch/name": "unknown",
	} {
		if got := remoteOf(in, remotes); got != want {
			t.Errorf("remoteOf(%q) = %q, want %q", in, got, want)
		}
	}
}

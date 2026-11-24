package main

import (
	"slices"
	"testing"
)

func TestWithQuery(t *testing.T) {
	vicinae := []string{"/usr/bin/vicinae", "dmenu"}
	if got := withQuery(vicinae, "dune"); !slices.Equal(got, []string{"/usr/bin/vicinae", "dmenu", "--query", "dune"}) {
		t.Fatalf("vicinae: %q", got)
	}
	if got := withQuery(vicinae, ""); !slices.Equal(got, vicinae) {
		t.Fatalf("empty query changed argv: %q", got)
	}
	if got := withQuery([]string{"dmenu", "-i"}, "dune"); !slices.Equal(got, []string{"dmenu", "-i"}) {
		t.Fatalf("dmenu has no query flag: %q", got)
	}
	if got := withPrompt([]string{"vicinae", "dmenu", "--navigation-title", "Kino"}, "Severance"); got[3] != "Severance" {
		t.Fatalf("withPrompt: %q", got)
	}
}

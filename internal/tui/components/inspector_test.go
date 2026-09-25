package components

import (
	"errors"
	"strings"
	"testing"
)

func TestInspectorDistinguishesUnknownZeroAndProgress(t *testing.T) {
	i := NewInspector()
	i.SetLibraryStates(map[string]CollectionFeedback{"lib": {Activity: LoadActivity{Visible: true, Loaded: 3, Total: 20}}})
	if strings.Contains(i.renderLibraryFeedback("lib"), "Items:") {
		t.Fatal("unknown count rendered as a partial or zero count")
	}
	i.SetLibraryStates(map[string]CollectionFeedback{"lib": {
		Summary:  CollectionSummary{Known: true, Count: 0},
		Activity: LoadActivity{Visible: true, Loaded: 3, Total: 20},
	}})
	view := i.renderLibraryFeedback("lib")
	if !strings.Contains(view, "Items: 0") || !strings.Contains(view, "Loading: 3/20") {
		t.Fatal("zero and progress not distinguished")
	}
	i.SetLibraryStates(map[string]CollectionFeedback{"lib": {
		Summary: CollectionSummary{Known: true, Count: 12, Stale: true}, Error: errors.New("offline"),
	}})
	view = i.renderLibraryFeedback("lib")
	if !strings.Contains(view, "Items: 12 (cached)") || !strings.Contains(view, "r to retry") {
		t.Fatal("offline fallback or recovery hint missing")
	}
}

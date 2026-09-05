package search

import (
	"context"
	"github.com/mmcdole/kino/internal/domain"
	"testing"
)

func TestIndexRejectsStaleUpdatesAndReturnsDetachedResults(t *testing.T) {
	index := NewIndex()
	index.ReplaceLibrary("lib", 2, []domain.ListItem{&domain.Show{ID: "show", Title: "Current"}})
	index.ReplaceLibrary("lib", 1, []domain.ListItem{&domain.Show{ID: "show", Title: "Obsolete"}})
	libs := []domain.Library{{ID: "lib"}}
	found := index.Search(context.Background(), "Current", libs)
	if len(found) != 1 || found[0].Type != domain.MediaTypeShow {
		t.Fatal("new snapshot lost from index")
	}
	found[0].Item.(*domain.Show).Title = "mutated"
	if again := index.Search(context.Background(), "Current", libs); again[0].Item.GetTitle() != "Current" {
		t.Fatal("consumer mutated the index")
	}
	if removed := index.Search(context.Background(), "Current", nil); len(removed) != 0 {
		t.Fatal("removed library remained searchable")
	}
}

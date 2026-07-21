package listenbrainz

import (
	"path/filepath"
	"testing"
)

func TestLoadStoreMissingFileIsEmpty(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Has(1) {
		t.Errorf("Has(1) on empty store = true, want false")
	}
}

func TestSubmittedStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "submitted.json")

	s, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkSubmitted([]int64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3} {
		if !reloaded.Has(id) {
			t.Errorf("reloaded.Has(%d) = false, want true", id)
		}
	}
	if reloaded.Has(4) {
		t.Errorf("reloaded.Has(4) = true, want false")
	}

	if err := reloaded.MarkSubmitted([]int64{4}); err != nil {
		t.Fatal(err)
	}
	final, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3, 4} {
		if !final.Has(id) {
			t.Errorf("final.Has(%d) = false, want true", id)
		}
	}
}

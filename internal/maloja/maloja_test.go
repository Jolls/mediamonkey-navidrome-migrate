package maloja

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSubmitAllSkipsDuplicateTimestamps locks in that a 409
// duplicate_timestamp response (Maloja already has a scrobble at that exact
// time) is treated as settled, not a failure: SubmitAll counts it and keeps
// going, rather than aborting the whole run — Maloja nests desc under
// "error", not at the top level, which is what previously caused this to be
// misclassified as a generic error.
func TestSubmitAllSkipsDuplicateTimestamps(t *testing.T) {
	var titles []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title string `json:"title"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		titles = append(titles, body.Title)

		if body.Title == "Dupe" {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"status": "error", "error": {"type": "duplicate_timestamp", "desc": "A scrobble is already registered with this timestamp."}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	scrobbles := []Scrobble{
		{Title: "First", Artists: []string{"A"}, Time: 1},
		{Title: "Dupe", Artists: []string{"A"}, Time: 2},
		{Title: "Last", Artists: []string{"A"}, Time: 3},
	}

	var progressCalls []int
	var duplicates []Scrobble
	res, err := c.SubmitAll(scrobbles,
		func(done, total int) { progressCalls = append(progressCalls, done) },
		func(s Scrobble) { duplicates = append(duplicates, s) },
	)
	if err != nil {
		t.Fatalf("SubmitAll returned error, want nil (duplicate should not abort the run): %v", err)
	}
	if res.Submitted != 2 {
		t.Errorf("res.Submitted = %d, want 2", res.Submitted)
	}
	if res.Duplicates != 1 {
		t.Errorf("res.Duplicates = %d, want 1", res.Duplicates)
	}
	if len(titles) != 3 {
		t.Fatalf("server saw %d requests, want 3 (all scrobbles submitted despite the duplicate)", len(titles))
	}
	if len(duplicates) != 1 || duplicates[0].Title != "Dupe" {
		t.Errorf("onDuplicate calls = %+v, want one call for %q", duplicates, "Dupe")
	}
	// progress must be called once per scrobble, including the duplicate,
	// counting up 1,2,3 — a caller relies on this to persist submitted state
	// (ids[:done]) for duplicates too, so they aren't retried next run.
	want := []int{1, 2, 3}
	if len(progressCalls) != len(want) {
		t.Fatalf("progress called %d times, want %d", len(progressCalls), len(want))
	}
	for i, v := range want {
		if progressCalls[i] != v {
			t.Errorf("progressCalls[%d] = %d, want %d", i, progressCalls[i], v)
		}
	}
}

// TestSubmitAllStopsOnNonDuplicateError locks in that a real failure (not a
// duplicate-timestamp response) still aborts the run, preserving credit for
// what succeeded before it.
func TestSubmitAllStopsOnNonDuplicateError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"status": "error", "error": {"type": "something_else", "desc": "boom"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	scrobbles := []Scrobble{
		{Title: "First", Artists: []string{"A"}, Time: 1},
		{Title: "Boom", Artists: []string{"A"}, Time: 2},
		{Title: "Never reached", Artists: []string{"A"}, Time: 3},
	}

	res, err := c.SubmitAll(scrobbles, nil, nil)
	if err == nil {
		t.Fatal("SubmitAll returned nil error, want the non-duplicate failure to propagate")
	}
	if IsDuplicateTimestamp(err) {
		t.Error("IsDuplicateTimestamp(err) = true, want false for a generic error")
	}
	if res.Submitted != 1 {
		t.Errorf("res.Submitted = %d, want 1 (only the scrobble before the failure)", res.Submitted)
	}
	if calls != 2 {
		t.Errorf("server saw %d requests, want 2 (run must stop at the failure)", calls)
	}
}

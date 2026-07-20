package listenbrainz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-token")
	c.baseURL = srv.URL
	c.http = srv.Client()
	return c
}

func TestSubmitAllBatches(t *testing.T) {
	var gotBatches int
	var gotListens int
	handler := func(w http.ResponseWriter, r *http.Request) {
		var body listenPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ListenType != "import" {
			t.Errorf("listen_type = %q, want import", body.ListenType)
		}
		if got := r.Header.Get("Authorization"); got != "Token test-token" {
			t.Errorf("Authorization = %q", got)
		}
		gotBatches++
		gotListens += len(body.Payload)
		w.WriteHeader(http.StatusOK)
	}
	c := newTestClient(t, handler)

	listens := make([]Listen, 2500)
	for i := range listens {
		listens[i] = Listen{ListenedAt: int64(i), ArtistName: "A", TrackName: "T"}
	}

	res, err := c.SubmitAll(listens, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Batches != 3 {
		t.Errorf("Batches = %d, want 3", res.Batches)
	}
	if res.Submitted != 2500 {
		t.Errorf("Submitted = %d, want 2500", res.Submitted)
	}
	if gotBatches != 3 || gotListens != 2500 {
		t.Errorf("server saw %d batches / %d listens, want 3 / 2500", gotBatches, gotListens)
	}
}

func TestSubmitImportErrorSurfaced(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(errorResponse{Code: 401, Error: "Invalid authorization token"})
	}
	c := newTestClient(t, handler)

	err := c.SubmitImport([]Listen{{ListenedAt: 1, ArtistName: "A", TrackName: "T"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "listenbrainz: submit-listens: HTTP 401: Invalid authorization token" {
		t.Errorf("err = %q", got)
	}
}

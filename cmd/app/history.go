package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jolls/mm5-navidrome-migrate/internal/listenbrainz"
	"github.com/jolls/mm5-navidrome-migrate/internal/mm"
	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

// Play History / ListenBrainz — independent of the Navidrome migration
// pipeline above: only needs MM5.DB (and, for submission, a ListenBrainz
// user token), no Navidrome server/db, no music root, no scope.

const defaultPlaysLimit = 200

// historyOpenRequest is the JSON body for POST /api/history/open.
type historyOpenRequest struct {
	MMDBPath          string `json:"mmDbPath"`
	ListenBrainzToken string `json:"listenBrainzToken"`
}

func (s *apiServer) handleHistoryOpen(w http.ResponseWriter, r *http.Request) {
	var req historyOpenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	log.Printf("history: opening MM5.DB at %q", req.MMDBPath)
	source, err := mm.Open(req.MMDBPath)
	if err != nil {
		log.Printf("history: open MM5.DB failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Errorf("open MM5.DB: %w", err))
		return
	}
	plays, err := source.ReadPlays()
	if err != nil {
		log.Printf("history: read plays failed: %v", err)
		source.Close()
		writeError(w, http.StatusInternalServerError, fmt.Errorf("read play history: %w", err))
		return
	}
	log.Printf("history: loaded %d play(s)", len(plays))

	var lb *listenbrainz.Client
	if req.ListenBrainzToken != "" {
		lb = listenbrainz.New(req.ListenBrainzToken)
	}

	s.mu.Lock()
	if s.historySource != nil {
		s.historySource.Close()
	}
	s.historySource = source
	s.historyPlays = plays
	s.historyOpen = true
	s.lbClient = lb
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "total": len(plays)})
}

// playsResponse is the JSON body for GET /api/history/plays.
type playsResponse struct {
	Total int          `json:"total"`
	Rows  []model.Play `json:"rows"`
}

func (s *apiServer) handleHistoryPlays(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, plays := s.historyOpen, s.historyPlays
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filtered := plays
	if q != "" {
		filtered = make([]model.Play, 0, len(plays))
		for _, p := range plays {
			if strings.Contains(strings.ToLower(p.Artist), q) ||
				strings.Contains(strings.ToLower(p.Title), q) ||
				strings.Contains(strings.ToLower(p.Album), q) ||
				strings.Contains(strings.ToLower(p.Path), q) {
				filtered = append(filtered, p)
			}
		}
	}

	limit := intQuery(r, "limit", defaultPlaysLimit)
	if limit <= 0 || limit > 1000 {
		limit = defaultPlaysLimit
	}
	offset := intQuery(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	total := len(filtered)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, playsResponse{Total: total, Rows: filtered[offset:end]})
}

// playsToListens converts plays to ListenBrainz listens, skipping any without
// a real timestamp or artist/title — the only filter step between "loaded
// plays" and "what gets submitted", shared by the preview and submit
// endpoints so the count a user confirms matches what's actually sent.
func playsToListens(plays []model.Play, limit int) []listenbrainz.Listen {
	listens := make([]listenbrainz.Listen, 0, len(plays))
	for _, p := range plays {
		if p.PlayedAt.IsZero() || p.Artist == "" || p.Title == "" {
			continue
		}
		listens = append(listens, listenbrainz.Listen{
			ListenedAt:  p.PlayedAt.Unix(),
			ArtistName:  p.Artist,
			TrackName:   p.Title,
			ReleaseName: p.Album,
		})
		if limit > 0 && len(listens) >= limit {
			break
		}
	}
	return listens
}

func intQuery(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// listenBrainzPreviewResponse is the JSON body for GET /api/history/listenbrainz/preview.
type listenBrainzPreviewResponse struct {
	Count    int    `json:"count"`
	Earliest string `json:"earliest,omitempty"`
	Latest   string `json:"latest,omitempty"`
}

func (s *apiServer) handleListenBrainzPreview(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, lb, plays := s.historyOpen, s.lbClient, s.historyPlays
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}
	if lb == nil {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no ListenBrainz token configured"))
		return
	}

	listens := playsToListens(plays, 0)
	resp := listenBrainzPreviewResponse{Count: len(listens)}
	if len(listens) > 0 {
		// listens is derived from plays, which ReadPlays sorts newest first
		// by real UTC instant: listens[0] is latest, last is earliest.
		resp.Latest = time.Unix(listens[0].ListenedAt, 0).UTC().Format("2006-01-02T15:04:05Z")
		resp.Earliest = time.Unix(listens[len(listens)-1].ListenedAt, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
	writeJSON(w, http.StatusOK, resp)
}

// listenBrainzSubmitResponse is the JSON body for POST /api/history/listenbrainz/submit.
type listenBrainzSubmitResponse struct {
	Submitted int      `json:"submitted"`
	Batches   int      `json:"batches"`
	Errors    []string `json:"errors,omitempty"`
}

func (s *apiServer) handleListenBrainzSubmit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, lb, plays := s.historyOpen, s.lbClient, s.historyPlays
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}
	if lb == nil {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no ListenBrainz token configured"))
		return
	}

	limit := intQuery(r, "limit", 0)
	listens := playsToListens(plays, limit)

	log.Printf("listenbrainz: submitting %d listen(s)", len(listens))
	res, err := lb.SubmitAll(listens, nil)
	if err != nil {
		log.Printf("listenbrainz: submit failed after %d applied: %v", res.Submitted, err)
		writeJSON(w, http.StatusBadGateway, listenBrainzSubmitResponse{
			Submitted: res.Submitted,
			Batches:   res.Batches,
			Errors:    []string{err.Error()},
		})
		return
	}
	log.Printf("listenbrainz: submitted %d listen(s) in %d batch(es)", res.Submitted, res.Batches)
	writeJSON(w, http.StatusOK, listenBrainzSubmitResponse{Submitted: res.Submitted, Batches: res.Batches})
}

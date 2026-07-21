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

	lbState, err := listenbrainz.LoadStore(listenbrainz.StorePath(req.MMDBPath))
	if err != nil {
		log.Printf("history: load submitted-listens state failed: %v", err)
		source.Close()
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load ListenBrainz submission state: %w", err))
		return
	}

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
	s.lbState = lbState
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "total": len(plays)})
}

// playRow is one row of GET /api/history/plays, adding the submitted status
// that model.Play itself doesn't carry (a ListenBrainz/display concern, not
// a MediaMonkey domain field).
type playRow struct {
	model.Play
	Submitted bool `json:"Submitted"`
}

// playsResponse is the JSON body for GET /api/history/plays.
type playsResponse struct {
	Total int       `json:"total"`
	Rows  []playRow `json:"rows"`
}

func (s *apiServer) handleHistoryPlays(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, plays, lbState := s.historyOpen, s.historyPlays, s.lbState
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	unsubmittedOnly := r.URL.Query().Get("unsubmitted") == "true"
	filtered := plays
	if q != "" || unsubmittedOnly {
		filtered = make([]model.Play, 0, len(plays))
		for _, p := range plays {
			if unsubmittedOnly && lbState != nil && lbState.Has(p.ID) {
				continue
			}
			if q != "" && !(strings.Contains(strings.ToLower(p.Artist), q) ||
				strings.Contains(strings.ToLower(p.Title), q) ||
				strings.Contains(strings.ToLower(p.Album), q) ||
				strings.Contains(strings.ToLower(p.Path), q)) {
				continue
			}
			filtered = append(filtered, p)
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

	page := filtered[offset:end]
	rows := make([]playRow, len(page))
	for i, p := range page {
		rows[i] = playRow{Play: p, Submitted: lbState != nil && lbState.Has(p.ID)}
	}

	writeJSON(w, http.StatusOK, playsResponse{Total: total, Rows: rows})
}

// pendingListens is the result of filtering plays down to what's actually
// left to submit: listens and ids are parallel slices (ids[i] is the
// model.Play.ID that produced listens[i]), so a caller can report back to
// the SubmittedStore exactly which plays a submission covered.
type pendingListens struct {
	Listens []listenbrainz.Listen
	IDs     []int64
}

// playsToListens converts plays to ListenBrainz listens, skipping any
// without a real timestamp or artist/title, and any already recorded in
// alreadySubmitted (nil means "nothing submitted yet") — the only filter
// step between "loaded plays" and "what gets submitted", shared by the
// preview and submit endpoints so the count a user confirms matches what's
// actually sent.
func playsToListens(plays []model.Play, alreadySubmitted *listenbrainz.SubmittedStore, limit int) pendingListens {
	pending := pendingListens{
		Listens: make([]listenbrainz.Listen, 0, len(plays)),
		IDs:     make([]int64, 0, len(plays)),
	}
	for _, p := range plays {
		if p.PlayedAt.IsZero() || p.Artist == "" || p.Title == "" {
			continue
		}
		if alreadySubmitted != nil && alreadySubmitted.Has(p.ID) {
			continue
		}
		pending.Listens = append(pending.Listens, listenbrainz.Listen{
			ListenedAt:  p.PlayedAt.Unix(),
			ArtistName:  p.Artist,
			TrackName:   p.Title,
			ReleaseName: p.Album,
		})
		pending.IDs = append(pending.IDs, p.ID)
		if limit > 0 && len(pending.Listens) >= limit {
			break
		}
	}
	return pending
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
	Count            int    `json:"count"`
	AlreadySubmitted int    `json:"alreadySubmitted"`
	Earliest         string `json:"earliest,omitempty"`
	Latest           string `json:"latest,omitempty"`
}

func (s *apiServer) handleListenBrainzPreview(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, lb, lbState, plays := s.historyOpen, s.lbClient, s.lbState, s.historyPlays
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}
	if lb == nil {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no ListenBrainz token configured"))
		return
	}

	pending := playsToListens(plays, lbState, 0)
	all := playsToListens(plays, nil, 0)
	resp := listenBrainzPreviewResponse{
		Count:            len(pending.Listens),
		AlreadySubmitted: len(all.Listens) - len(pending.Listens),
	}
	if len(pending.Listens) > 0 {
		// pending.Listens is derived from plays, which ReadPlays sorts newest
		// first by real UTC instant: [0] is latest, last is earliest.
		resp.Latest = time.Unix(pending.Listens[0].ListenedAt, 0).UTC().Format("2006-01-02T15:04:05Z")
		resp.Earliest = time.Unix(pending.Listens[len(pending.Listens)-1].ListenedAt, 0).UTC().Format("2006-01-02T15:04:05Z")
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
	open, lb, lbState, plays := s.historyOpen, s.lbClient, s.lbState, s.historyPlays
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
	pending := playsToListens(plays, lbState, limit)

	// progress reports the cumulative count of listens confirmed submitted
	// so far, in submission order — since pending.IDs is built in the same
	// order as pending.Listens, ids[:done] are exactly the plays that batch
	// covered. Persisted incrementally so a failure partway through a large
	// submit doesn't lose credit for batches that did succeed.
	progress := func(done, total int) {
		if err := lbState.MarkSubmitted(pending.IDs[:done]); err != nil {
			log.Printf("listenbrainz: failed to persist submitted-listens state: %v", err)
		}
	}

	log.Printf("listenbrainz: submitting %d listen(s)", len(pending.Listens))
	res, err := lb.SubmitAll(pending.Listens, progress)
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

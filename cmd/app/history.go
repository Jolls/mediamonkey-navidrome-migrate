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
	"github.com/jolls/mm5-navidrome-migrate/internal/maloja"
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
	MalojaURL         string `json:"malojaUrl"`
	MalojaAPIKey      string `json:"malojaApiKey"`
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
	mjState, err := maloja.LoadStore(maloja.StorePath(req.MMDBPath))
	if err != nil {
		log.Printf("history: load submitted-scrobbles state failed: %v", err)
		source.Close()
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load Maloja submission state: %w", err))
		return
	}

	var lb *listenbrainz.Client
	if req.ListenBrainzToken != "" {
		lb = listenbrainz.New(req.ListenBrainzToken)
	}
	var mj *maloja.Client
	if req.MalojaURL != "" && req.MalojaAPIKey != "" {
		mj = maloja.New(req.MalojaURL, req.MalojaAPIKey)
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
	s.mjClient = mj
	s.mjState = mjState
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "total": len(plays)})
}

// playRow is one row of GET /api/history/plays, adding the per-exporter
// submitted status that model.Play itself doesn't carry (a display concern,
// not a MediaMonkey domain field).
type playRow struct {
	model.Play
	SubmittedLB     bool `json:"SubmittedLB"`
	SubmittedMaloja bool `json:"SubmittedMaloja"`
}

// playsResponse is the JSON body for GET /api/history/plays.
type playsResponse struct {
	Total int       `json:"total"`
	Rows  []playRow `json:"rows"`
}

func (s *apiServer) handleHistoryPlays(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, plays, lbState, mjState := s.historyOpen, s.historyPlays, s.lbState, s.mjState
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	hideLB := r.URL.Query().Get("unsubmitted") == "true"
	hideMaloja := r.URL.Query().Get("unsubmittedMaloja") == "true"
	filtered := plays
	if q != "" || hideLB || hideMaloja {
		filtered = make([]model.Play, 0, len(plays))
		for _, p := range plays {
			if hideLB && lbState != nil && lbState.Has(p.ID) {
				continue
			}
			if hideMaloja && mjState != nil && mjState.Has(p.ID) {
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
		rows[i] = playRow{
			Play:            p,
			SubmittedLB:     lbState != nil && lbState.Has(p.ID),
			SubmittedMaloja: mjState != nil && mjState.Has(p.ID),
		}
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

// pendingScrobbles is the Maloja counterpart to pendingListens.
type pendingScrobbles struct {
	Scrobbles []maloja.Scrobble
	IDs       []int64
}

// playsToScrobbles is the Maloja counterpart to playsToListens, sharing the
// same filter rules (real timestamp, non-empty artist/title, not already
// submitted) so the count a user previews matches what's actually sent.
//
// MediaMonkey's PlayDate carries sub-second precision, but Maloja's API only
// accepts whole-second timestamps, and Maloja dedups scrobbles by timestamp
// alone — so two different songs played less than a second apart can round
// to the same second and Maloja will reject the second one as a duplicate,
// silently discarding a real, distinct play. secondsInUse resolves that by
// nudging one second earlier whenever a different song would otherwise
// collide with one already assigned in this batch. A genuine duplicate
// (same song, same rounded second — MediaMonkey occasionally double-logs a
// play) is deliberately left colliding: Maloja's own duplicate-timestamp
// response for that case is what SubmitAll already treats as "already
// recorded, skip it" (see maloja.IsDuplicateTimestamp).
func playsToScrobbles(plays []model.Play, alreadySubmitted *maloja.SubmittedStore, limit int) pendingScrobbles {
	pending := pendingScrobbles{
		Scrobbles: make([]maloja.Scrobble, 0, len(plays)),
		IDs:       make([]int64, 0, len(plays)),
	}
	secondsInUse := make(map[int64]int64) // unix second -> SongID that claimed it
	for _, p := range plays {
		if p.PlayedAt.IsZero() || p.Artist == "" || p.Title == "" {
			continue
		}
		if alreadySubmitted != nil && alreadySubmitted.Has(p.ID) {
			continue
		}
		sec := p.PlayedAt.Unix()
		for {
			owner, taken := secondsInUse[sec]
			if !taken || owner == p.SongID {
				break
			}
			sec--
		}
		secondsInUse[sec] = p.SongID
		s := maloja.Scrobble{
			Time:    sec,
			Artists: []string{p.Artist},
			Title:   p.Title,
			Album:   p.Album,
			Length:  p.Duration,
		}
		if p.AlbumArtist != "" {
			s.AlbumArtists = []string{p.AlbumArtist}
		}
		pending.Scrobbles = append(pending.Scrobbles, s)
		pending.IDs = append(pending.IDs, p.ID)
		if limit > 0 && len(pending.Scrobbles) >= limit {
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

// malojaPreviewResponse is the JSON body for GET /api/history/maloja/preview.
type malojaPreviewResponse struct {
	Count            int    `json:"count"`
	AlreadySubmitted int    `json:"alreadySubmitted"`
	Earliest         string `json:"earliest,omitempty"`
	Latest           string `json:"latest,omitempty"`
}

func (s *apiServer) handleMalojaPreview(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, mj, mjState, plays := s.historyOpen, s.mjClient, s.mjState, s.historyPlays
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}
	if mj == nil {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no Maloja server/API key configured"))
		return
	}

	pending := playsToScrobbles(plays, mjState, 0)
	all := playsToScrobbles(plays, nil, 0)
	resp := malojaPreviewResponse{
		Count:            len(pending.Scrobbles),
		AlreadySubmitted: len(all.Scrobbles) - len(pending.Scrobbles),
	}
	if len(pending.Scrobbles) > 0 {
		// pending.Scrobbles is derived from plays, which ReadPlays sorts newest
		// first by real UTC instant: [0] is latest, last is earliest.
		resp.Latest = time.Unix(pending.Scrobbles[0].Time, 0).UTC().Format("2006-01-02T15:04:05Z")
		resp.Earliest = time.Unix(pending.Scrobbles[len(pending.Scrobbles)-1].Time, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
	writeJSON(w, http.StatusOK, resp)
}

// malojaSubmitResponse is the JSON body for POST /api/history/maloja/submit.
type malojaSubmitResponse struct {
	Submitted  int      `json:"submitted"`
	Duplicates int      `json:"duplicates,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

func (s *apiServer) handleMalojaSubmit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	open, mj, mjState, plays := s.historyOpen, s.mjClient, s.mjState, s.historyPlays
	s.mu.Unlock()
	if !open {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no play history loaded: POST /api/history/open first"))
		return
	}
	if mj == nil {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("no Maloja server/API key configured"))
		return
	}

	limit := intQuery(r, "limit", 0)
	pending := playsToScrobbles(plays, mjState, limit)

	// progress reports the cumulative count of scrobbles handled so far
	// (submitted or skipped as duplicates), in submission order — since
	// pending.IDs is built in the same order as pending.Scrobbles, ids[:done]
	// are exactly the plays settled so far. Persisted incrementally so a
	// failure partway through a large submit doesn't lose credit for
	// scrobbles that did succeed.
	progress := func(done, total int) {
		if err := mjState.MarkSubmitted(pending.IDs[:done]); err != nil {
			log.Printf("maloja: failed to persist submitted-scrobbles state: %v", err)
		}
	}
	// onDuplicate logs scrobbles Maloja already had recorded at that
	// timestamp (HTTP 409 duplicate_timestamp) — not a failure, so these are
	// still marked submitted via progress above rather than retried.
	onDuplicate := func(s maloja.Scrobble) {
		log.Printf("maloja: skipping duplicate already registered at Maloja: %s - %q (time=%d)", strings.Join(s.Artists, ", "), s.Title, s.Time)
	}

	log.Printf("maloja: submitting %d scrobble(s)", len(pending.Scrobbles))
	res, err := mj.SubmitAll(pending.Scrobbles, progress, onDuplicate)
	if err != nil {
		log.Printf("maloja: submit failed after %d applied (%d duplicates skipped): %v", res.Submitted, res.Duplicates, err)
		writeJSON(w, http.StatusBadGateway, malojaSubmitResponse{
			Submitted:  res.Submitted,
			Duplicates: res.Duplicates,
			Errors:     []string{err.Error()},
		})
		return
	}
	log.Printf("maloja: submitted %d scrobble(s), %d duplicate(s) skipped", res.Submitted, res.Duplicates)
	writeJSON(w, http.StatusOK, malojaSubmitResponse{Submitted: res.Submitted, Duplicates: res.Duplicates})
}

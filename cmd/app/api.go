package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/jolls/mm5-navidrome-migrate/internal/listenbrainz"
	"github.com/jolls/mm5-navidrome-migrate/internal/maloja"
	"github.com/jolls/mm5-navidrome-migrate/internal/migrate"
	"github.com/jolls/mm5-navidrome-migrate/internal/mm"
	"github.com/jolls/mm5-navidrome-migrate/internal/model"
	"github.com/jolls/mm5-navidrome-migrate/internal/nav"
	"github.com/jolls/mm5-navidrome-migrate/internal/subsonic"
)

// apiServer holds the sources opened from the last POST /api/config call and
// serves the scan/dry-run/commit endpoints against them.
type apiServer struct {
	mu sync.Mutex

	configured bool
	cfg        model.Config
	source     mm.Source
	navReader  nav.Reader
	client     *subsonic.Client

	// Play History / ListenBrainz state — independent of the fields above:
	// this needs only MM5.DB (and, for submission, a LB token), not a
	// Navidrome server/db or the main config flow.
	historyOpen   bool
	historySource mm.Source
	historyPlays  []model.Play // cached on open; ~28k rows is trivial in memory
	lbClient      *listenbrainz.Client
	lbState       *listenbrainz.SubmittedStore
	mjClient      *maloja.Client
	mjState       *maloja.SubmittedStore
}

func newAPIServer() *apiServer {
	return &apiServer{}
}

func (s *apiServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/users", s.handleUsers)
	mux.HandleFunc("GET /api/scan", s.handleScan)
	mux.HandleFunc("GET /api/dry-run", s.handleDryRun)
	mux.HandleFunc("GET /api/verify", s.handleVerify)
	mux.HandleFunc("POST /api/commit", s.handleCommit)
	mux.HandleFunc("GET /api/browse-file", s.handleBrowseFile)
	mux.HandleFunc("POST /api/quit", s.handleQuit)

	mux.HandleFunc("POST /api/history/open", s.handleHistoryOpen)
	mux.HandleFunc("GET /api/history/plays", s.handleHistoryPlays)
	mux.HandleFunc("GET /api/history/listenbrainz/preview", s.handleListenBrainzPreview)
	mux.HandleFunc("POST /api/history/listenbrainz/submit", s.handleListenBrainzSubmit)
	mux.HandleFunc("GET /api/history/maloja/preview", s.handleMalojaPreview)
	mux.HandleFunc("POST /api/history/maloja/submit", s.handleMalojaSubmit)
}

// handleQuit responds then terminates the process, letting the user close
// the app from the UI instead of switching back to the terminal.
func (s *apiServer) handleQuit(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	go func() {
		os.Exit(0)
	}()
}

// configRequest is the JSON body for POST /api/config.
type configRequest struct {
	MMDBPath      string   `json:"mmDbPath"`
	NavDBPath     string   `json:"navDbPath"`
	ServerURL     string   `json:"serverUrl"`
	Username      string   `json:"username"`
	Password      string   `json:"password"`
	MMRoot        string   `json:"musicRoot"`
	UserID        string   `json:"userId"`
	Fields        []string `json:"fields"`        // any of "rating", "playCount", "starred", "dateAdded"
	StarThreshold int      `json:"starThreshold"` // 0-5; 0 means "use the default"
	RatingMap     [11]int  `json:"ratingMap"`     // MM rating step (0=unrated, 1-10=half-star) -> Navidrome rating 0-5
}

// ratingMapFromInts converts and clamps the UI-supplied rating map into
// model.Rating values.
func ratingMapFromInts(in [11]int) [11]model.Rating {
	var out [11]model.Rating
	for i, v := range in {
		if v < 0 {
			v = 0
		}
		if v > 5 {
			v = 5
		}
		out[i] = model.Rating(v)
	}
	return out
}

func fieldsFromNames(names []string) (model.Fields, error) {
	var f model.Fields
	for _, n := range names {
		switch n {
		case "rating":
			f |= model.Fields(model.FieldRating)
		case "playCount":
			f |= model.Fields(model.FieldPlayCount)
		case "starred":
			f |= model.Fields(model.FieldStarred)
		case "dateAdded":
			f |= model.Fields(model.FieldDateAdded)
		default:
			return 0, fmt.Errorf("unknown field %q", n)
		}
	}
	return f, nil
}

func (s *apiServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	var req configRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	fields, err := fieldsFromNames(req.Fields)
	if err != nil {
		log.Printf("config: bad fields: %v", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := model.Config{
		MMDBPath:      req.MMDBPath,
		NavDBPath:     req.NavDBPath,
		ServerURL:     req.ServerURL,
		Username:      req.Username,
		Password:      req.Password,
		MMRoot:        req.MMRoot,
		UserID:        req.UserID,
		Fields:        fields,
		StarThreshold: model.Rating(req.StarThreshold),
		RatingMap:     ratingMapFromInts(req.RatingMap),
	}

	log.Printf("config: opening MM5.DB at %q", cfg.MMDBPath)
	source, err := mm.Open(cfg.MMDBPath)
	if err != nil {
		log.Printf("config: open MM5.DB failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Errorf("open MM5.DB: %w", err))
		return
	}
	log.Printf("config: opening navidrome.db at %q", cfg.NavDBPath)
	navReader, err := nav.OpenReader(cfg.NavDBPath)
	if err != nil {
		log.Printf("config: open navidrome.db failed: %v", err)
		source.Close()
		writeError(w, http.StatusBadGateway, fmt.Errorf("open navidrome.db: %w", err))
		return
	}
	log.Printf("config: pinging navidrome server at %q as %q", cfg.ServerURL, cfg.Username)
	client := subsonic.New(cfg.ServerURL, cfg.Username, cfg.Password)
	if err := client.Ping(); err != nil {
		log.Printf("config: ping navidrome server failed: %v", err)
		source.Close()
		navReader.Close()
		writeError(w, http.StatusBadGateway, fmt.Errorf("ping navidrome server: %w", err))
		return
	}

	s.mu.Lock()
	s.closeSourcesLocked()
	s.cfg = cfg
	s.source = source
	s.navReader = navReader
	s.client = client
	s.configured = true
	s.mu.Unlock()

	log.Printf("config: ok, sources open and server reachable")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// closeSourcesLocked closes any previously opened source/reader; caller must
// hold s.mu.
func (s *apiServer) closeSourcesLocked() {
	if s.source != nil {
		s.source.Close()
	}
	if s.navReader != nil {
		s.navReader.Close()
	}
}

// handleUsers lists Navidrome users so the UI can pick the one that owns the
// annotations being migrated.
func (s *apiServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	configured, navReader := s.configured, s.navReader
	s.mu.Unlock()
	if !configured {
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("not configured: POST /api/config first"))
		return
	}
	users, err := navReader.Users()
	if err != nil {
		log.Printf("users: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("users: found %d navidrome user(s)", len(users))
	writeJSON(w, http.StatusOK, users)
}

// readOnlyPipeline builds a Pipeline suitable for Scan/DryRun (no writer).
// Returns an error if /api/config hasn't been called yet.
func (s *apiServer) readOnlyPipeline() (*migrate.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configured {
		return nil, fmt.Errorf("not configured: POST /api/config first")
	}
	return &migrate.Pipeline{
		Cfg:    s.cfg,
		Source: s.source,
		NavDB:  s.navReader,
		API:    s.client,
	}, nil
}

func scopeFromQuery(r *http.Request) model.Scope {
	return model.Scope{Dir: r.URL.Query().Get("dir")}
}

func (s *apiServer) handleScan(w http.ResponseWriter, r *http.Request) {
	p, err := s.readOnlyPipeline()
	if err != nil {
		writeError(w, http.StatusPreconditionRequired, err)
		return
	}
	scope := scopeFromQuery(r)
	log.Printf("scan: scope dir=%q", scope.Dir)
	matches, err := p.Scan(scope)
	if err != nil {
		log.Printf("scan: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("scan: %d match(es)", len(matches))
	writeJSON(w, http.StatusOK, matches)
}

func (s *apiServer) handleDryRun(w http.ResponseWriter, r *http.Request) {
	p, err := s.readOnlyPipeline()
	if err != nil {
		writeError(w, http.StatusPreconditionRequired, err)
		return
	}
	scope := scopeFromQuery(r)
	log.Printf("dry-run: scope dir=%q", scope.Dir)
	rep, err := p.DryRun(scope)
	if err != nil {
		log.Printf("dry-run: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("dry-run: matched=%d ambiguous=%d unmatched=%d changes=%d", rep.Matched, rep.Ambiguous, rep.Unmatched, len(rep.Changes))
	writeJSON(w, http.StatusOK, rep)
}

func (s *apiServer) handleVerify(w http.ResponseWriter, r *http.Request) {
	p, err := s.readOnlyPipeline()
	if err != nil {
		writeError(w, http.StatusPreconditionRequired, err)
		return
	}
	scope := scopeFromQuery(r)
	log.Printf("verify: scope dir=%q", scope.Dir)
	rep, err := p.Verify(scope)
	if err != nil {
		log.Printf("verify: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("verify: checked=%d mismatched=%d", rep.Checked, rep.Mismatched)
	writeJSON(w, http.StatusOK, rep)
}

// commitRequest is the JSON body for POST /api/commit.
type commitRequest struct {
	Dir string `json:"dir"`
}

// commitResponse reports the result of a commit plus the backup taken before
// the direct navidrome.db writes.
type commitResponse struct {
	BackupPath string             `json:"backupPath"`
	Result     model.CommitResult `json:"result"`
}

func (s *apiServer) handleCommit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if !s.configured {
		s.mu.Unlock()
		writeError(w, http.StatusPreconditionRequired, fmt.Errorf("not configured: POST /api/config first"))
		return
	}
	cfg, source, navReader, client := s.cfg, s.source, s.navReader, s.client
	s.mu.Unlock()

	var req commitRequest
	if r.Body != nil {
		// The body is optional; an empty/absent body means the whole library.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	log.Printf("commit: scope dir=%q", req.Dir)
	if err := nav.EnsureUnlocked(cfg.NavDBPath); err != nil {
		log.Printf("commit: navidrome.db locked: %v", err)
		writeError(w, http.StatusConflict, fmt.Errorf("navidrome.db is in use: %w", err))
		return
	}
	backupPath, err := nav.Backup(cfg.NavDBPath)
	if err != nil {
		log.Printf("commit: backup failed: %v", err)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("backup navidrome.db: %w", err))
		return
	}
	log.Printf("commit: backed up navidrome.db to %q", backupPath)
	writer, err := nav.OpenWriter(cfg.NavDBPath)
	if err != nil {
		log.Printf("commit: open navidrome.db for writing failed: %v", err)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("open navidrome.db for writing: %w", err))
		return
	}
	defer writer.Close()

	p := &migrate.Pipeline{
		Cfg:    cfg,
		Source: source,
		NavDB:  navReader,
		Writer: writer,
		API:    client,
	}
	res, err := p.Commit(model.Scope{Dir: req.Dir})
	if err != nil {
		log.Printf("commit: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("commit: applied=%d errors=%d", res.Applied, len(res.Errors))
	writeJSON(w, http.StatusOK, commitResponse{BackupPath: backupPath, Result: res})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// Package maloja is a minimal client for submitting backdated scrobbles to a
// self-hosted Maloja instance (https://github.com/krateng/maloja), for the
// one-time backfill of MediaMonkey's Played history. This is separate from,
// and doesn't overlap with, Navidrome's forward-looking scrobbling (via
// multi-scrobbler) to Maloja.
package maloja

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client submits scrobbles to one Maloja server on behalf of one API key.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds a client for the given Maloja server URL (e.g.
// "http://localhost:42010") and API key (from Maloja's admin API key page).
func New(baseURL, apiKey string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

// Scrobble is one play, in the shape Maloja's newscrobble expects.
type Scrobble struct {
	Time         int64 // unix seconds
	Artists      []string
	Title        string
	Album        string   // "" omitted from the payload
	AlbumArtists []string // nil/empty omitted from the payload
	Length       int      // full track length in seconds; 0 omitted from the payload
}

// scrobblePayload mirrors Maloja's newscrobble JSON body. Length in
// particular matters beyond display: Maloja's own clients (e.g.
// multi-scrobbler) always send it, and album pages that aggregate track
// length across scrobbles can error out if every scrobble for an album is
// missing it — worth sending whenever we have it.
type scrobblePayload struct {
	Key          string   `json:"key"`
	Artists      []string `json:"artists"`
	Title        string   `json:"title"`
	Album        string   `json:"album,omitempty"`
	AlbumArtists []string `json:"albumartists,omitempty"`
	Time         int64    `json:"time"`
	Length       int      `json:"length,omitempty"`
}

// scrobbleResponse mirrors Maloja's general response envelope. Desc can
// appear at the top level or nested in error (Maloja puts it in error for at
// least duplicate_timestamp responses) — check both.
type scrobbleResponse struct {
	Status string `json:"status"`
	Desc   string `json:"desc"`
	Error  *struct {
		Type string `json:"type"`
		Desc string `json:"desc"`
	} `json:"error"`
}

// DuplicateTimestampError means Maloja already has a scrobble recorded at
// this exact timestamp (HTTP 409, error.type "duplicate_timestamp") — the
// play is already registered server-side, so callers should treat it as
// settled rather than retrying it.
type DuplicateTimestampError struct {
	Desc string
}

func (e *DuplicateTimestampError) Error() string {
	return fmt.Sprintf("maloja: newscrobble: duplicate timestamp: %s", e.Desc)
}

// IsDuplicateTimestamp reports whether err is a DuplicateTimestampError.
func IsDuplicateTimestamp(err error) bool {
	var d *DuplicateTimestampError
	return errors.As(err, &d)
}

// SubmitScrobble posts one scrobble to /apis/mlj_1/newscrobble. Maloja has no
// documented batch endpoint, so scrobbles are submitted one at a time.
func (c *Client) SubmitScrobble(s Scrobble) error {
	body, err := json.Marshal(scrobblePayload{
		Key:          c.apiKey,
		Artists:      s.Artists,
		Title:        s.Title,
		Album:        s.Album,
		AlbumArtists: s.AlbumArtists,
		Time:         s.Time,
		Length:       s.Length,
	})
	if err != nil {
		return fmt.Errorf("maloja: encode payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/apis/mlj_1/newscrobble", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("maloja: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("maloja: newscrobble: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("maloja: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var r scrobbleResponse
		if json.Unmarshal(respBody, &r) == nil {
			if resp.StatusCode == http.StatusConflict && r.Error != nil && r.Error.Type == "duplicate_timestamp" {
				return &DuplicateTimestampError{Desc: r.Error.Desc}
			}
			desc := r.Desc
			if desc == "" && r.Error != nil {
				desc = r.Error.Desc
			}
			if desc != "" {
				return fmt.Errorf("maloja: newscrobble: HTTP %d: %s", resp.StatusCode, desc)
			}
		}
		return fmt.Errorf("maloja: newscrobble: HTTP %d: %s", resp.StatusCode, respBody)
	}

	var r scrobbleResponse
	if json.Unmarshal(respBody, &r) == nil && (r.Status == "error" || r.Status == "failure") {
		return fmt.Errorf("maloja: newscrobble: %s", r.Desc)
	}
	return nil
}

// Result summarizes a full SubmitAll run.
type Result struct {
	Submitted  int
	Duplicates int // already registered at Maloja (HTTP 409 duplicate_timestamp) — skipped, not retried
}

// SubmitAll posts scrobbles one at a time, calling progress after each item
// (success or duplicate) with the cumulative processed count, so a caller can
// persist credit for everything handled so far. A duplicate-timestamp
// response means Maloja already has that scrobble recorded, so it's counted
// and skipped via onDuplicate rather than treated as a failure; any other
// error stops the run immediately.
func (c *Client) SubmitAll(scrobbles []Scrobble, progress func(done, total int), onDuplicate func(s Scrobble)) (Result, error) {
	var res Result
	total := len(scrobbles)
	for i, s := range scrobbles {
		if err := c.SubmitScrobble(s); err != nil {
			if !IsDuplicateTimestamp(err) {
				return res, err
			}
			res.Duplicates++
			if onDuplicate != nil {
				onDuplicate(s)
			}
		} else {
			res.Submitted++
		}
		if progress != nil {
			progress(i+1, total)
		}
	}
	return res, nil
}

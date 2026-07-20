// Package listenbrainz is a minimal client for submitting backdated listens
// to ListenBrainz (https://listenbrainz.org), for the one-time backfill of
// MediaMonkey's Played history. This is separate from, and doesn't overlap
// with, Navidrome's own forward-looking ListenBrainz scrobbling.
package listenbrainz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://api.listenbrainz.org/1"
	maxBatch       = 1000 // ListenBrainz's max listens per submit-listens request
	defaultBackoff = 5 * time.Second
)

// Client submits listens to ListenBrainz on behalf of one user token.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// New builds a client for the given ListenBrainz user token (from
// listenbrainz.org/settings).
func New(token string) *Client {
	return &Client{token: token, baseURL: defaultBaseURL, http: &http.Client{Timeout: 30 * time.Second}}
}

// Listen is one play, in the shape ListenBrainz's submit-listens expects.
type Listen struct {
	ListenedAt  int64  // unix seconds
	ArtistName  string
	TrackName   string
	ReleaseName string // "" omitted from the payload
}

// listenPayload mirrors ListenBrainz's submit-listens JSON body.
type listenPayload struct {
	ListenType string       `json:"listen_type"`
	Payload    []listenItem `json:"payload"`
}

type listenItem struct {
	ListenedAt    int64         `json:"listened_at"`
	TrackMetadata trackMetadata `json:"track_metadata"`
}

type trackMetadata struct {
	ArtistName  string `json:"artist_name"`
	TrackName   string `json:"track_name"`
	ReleaseName string `json:"release_name,omitempty"`
}

// errorResponse mirrors ListenBrainz's error envelope.
type errorResponse struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
}

// SubmitImport posts up to maxBatch listens as one backdated "import" batch.
func (c *Client) SubmitImport(listens []Listen) error {
	if len(listens) == 0 {
		return nil
	}
	if len(listens) > maxBatch {
		return fmt.Errorf("listenbrainz: %d listens exceeds max batch size %d", len(listens), maxBatch)
	}

	items := make([]listenItem, len(listens))
	for i, l := range listens {
		items[i] = listenItem{
			ListenedAt: l.ListenedAt,
			TrackMetadata: trackMetadata{
				ArtistName:  l.ArtistName,
				TrackName:   l.TrackName,
				ReleaseName: l.ReleaseName,
			},
		}
	}
	body, err := json.Marshal(listenPayload{ListenType: "import", Payload: items})
	if err != nil {
		return fmt.Errorf("listenbrainz: encode payload: %w", err)
	}

	return c.post(body)
}

// post issues one submit-listens request, retrying once on 429 after
// honoring Retry-After (falling back to a fixed backoff if absent).
func (c *Client) post(body []byte) error {
	resp, err := c.do(body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		wait := retryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		time.Sleep(wait)
		resp, err = c.do(body)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("listenbrainz: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e errorResponse
		if json.Unmarshal(respBody, &e) == nil && e.Error != "" {
			return fmt.Errorf("listenbrainz: submit-listens: HTTP %d: %s", resp.StatusCode, e.Error)
		}
		return fmt.Errorf("listenbrainz: submit-listens: HTTP %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

func (c *Client) do(body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/submit-listens", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("listenbrainz: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listenbrainz: submit-listens: %w", err)
	}
	return resp, nil
}

func retryAfter(header string) time.Duration {
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return defaultBackoff
}

// Result summarizes a full SubmitAll run.
type Result struct {
	Submitted int
	Batches   int
}

// SubmitAll batches listens into ≤maxBatch-item requests and posts them
// sequentially, calling progress after each batch.
func (c *Client) SubmitAll(listens []Listen, progress func(done, total int)) (Result, error) {
	var res Result
	total := len(listens)
	for start := 0; start < total; start += maxBatch {
		end := start + maxBatch
		if end > total {
			end = total
		}
		batch := listens[start:end]
		if err := c.SubmitImport(batch); err != nil {
			return res, err
		}
		res.Submitted += len(batch)
		res.Batches++
		if progress != nil {
			progress(res.Submitted, total)
		}
	}
	return res, nil
}

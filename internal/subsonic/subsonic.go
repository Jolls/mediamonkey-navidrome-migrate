// Package subsonic is a minimal Subsonic API client for the writes Navidrome
// supports cleanly: ratings and stars.
package subsonic

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

const (
	clientID   = "mm5-navidrome-migrate"
	apiVersion = "1.16.1"
	saltBytes  = 12
)

// Client talks to a Navidrome server's Subsonic endpoints.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New builds a client for the given server and credentials.
func New(serverURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimSuffix(serverURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// subsonicEnvelope is the top-level JSON response every endpoint returns.
type subsonicEnvelope struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"subsonic-response"`
}

// authParams returns a fresh token+salt auth query, per the Subsonic auth
// scheme: token = md5(password + salt).
func (c *Client) authParams() url.Values {
	saltRaw := make([]byte, saltBytes)
	_, _ = rand.Read(saltRaw)
	salt := hex.EncodeToString(saltRaw)
	sum := md5.Sum([]byte(c.password + salt))
	token := hex.EncodeToString(sum[:])

	v := url.Values{}
	v.Set("u", c.username)
	v.Set("t", token)
	v.Set("s", salt)
	v.Set("v", apiVersion)
	v.Set("c", clientID)
	v.Set("f", "json")
	return v
}

// call issues a GET to endpoint (e.g. "ping") with extra params merged into
// the auth params, and returns an error if the response isn't "ok".
func (c *Client) call(endpoint string, extra url.Values) error {
	v := c.authParams()
	for k, vals := range extra {
		for _, val := range vals {
			v.Add(k, val)
		}
	}

	reqURL := fmt.Sprintf("%s/rest/%s.view?%s", c.baseURL, endpoint, v.Encode())
	resp, err := c.http.Get(reqURL)
	if err != nil {
		return fmt.Errorf("subsonic %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("subsonic %s: read response: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subsonic %s: HTTP %d: %s", endpoint, resp.StatusCode, body)
	}

	var env subsonicEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("subsonic %s: decode response: %w", endpoint, err)
	}
	if env.Response.Status != "ok" {
		if env.Response.Error != nil {
			return fmt.Errorf("subsonic %s: error %d: %s", endpoint, env.Response.Error.Code, env.Response.Error.Message)
		}
		return fmt.Errorf("subsonic %s: status %q", endpoint, env.Response.Status)
	}
	return nil
}

// Ping verifies connectivity and credentials (rest/ping).
func (c *Client) Ping() error { return c.call("ping", nil) }

// SetRating sets a 0-5 rating on a media file; rating 0 clears it (rest/setRating).
func (c *Client) SetRating(navID string, r model.Rating) error {
	v := url.Values{}
	v.Set("id", navID)
	v.Set("rating", fmt.Sprintf("%d", r))
	return c.call("setRating", v)
}

// Star stars or unstars a media file (rest/star, rest/unstar).
func (c *Client) Star(navID string, starred bool) error {
	v := url.Values{}
	v.Set("id", navID)
	endpoint := "unstar"
	if starred {
		endpoint = "star"
	}
	return c.call(endpoint, v)
}

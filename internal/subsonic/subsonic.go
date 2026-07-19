// Package subsonic is a minimal Subsonic API client for the writes Navidrome
// supports cleanly: ratings and stars.
package subsonic

import (
	"errors"

	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

// ErrNotImplemented marks skeleton stubs still to be filled in.
var ErrNotImplemented = errors.New("not implemented")

// Client talks to a Navidrome server's Subsonic endpoints.
type Client struct {
	baseURL  string
	username string
	password string
	// TODO(sonnet): add *http.Client, auth (token+salt), api version ("c"/"v").
}

// New builds a client for the given server and credentials.
func New(serverURL, username, password string) *Client {
	return &Client{baseURL: serverURL, username: username, password: password}
}

// Ping verifies connectivity and credentials (rest/ping).
//
// TODO(sonnet): implement the auth handshake and use it to validate config.
func (c *Client) Ping() error { return ErrNotImplemented }

// SetRating sets a 0-5 rating on a media file; rating 0 clears it (rest/setRating).
//
// TODO(sonnet): GET rest/setRating?id=<navID>&rating=<r> with subsonic auth
// params and parse the subsonic-response status.
func (c *Client) SetRating(navID string, r model.Rating) error { return ErrNotImplemented }

// Star stars or unstars a media file (rest/star, rest/unstar).
func (c *Client) Star(navID string, starred bool) error { return ErrNotImplemented }

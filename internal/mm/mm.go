// Package mm reads a MediaMonkey 5 database (MM5.DB, SQLite) read-only and
// normalizes the songs we care about.
package mm

import (
	"errors"

	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

// ErrNotImplemented marks skeleton stubs still to be filled in.
var ErrNotImplemented = errors.New("not implemented")

// Source reads MediaMonkey songs.
type Source interface {
	// ReadTracks returns every song, with RelPath computed against root.
	// Rows whose path is not under root should be skipped.
	ReadTracks(root string) ([]model.Track, error)
	Close() error
}

// Open opens MM5.DB read-only.
//
// TODO(sonnet): open with modernc.org/sqlite using a read-only DSN, e.g.
// "file:<path>?mode=ro&immutable=1", and return a concrete Source. ReadTracks
// should query Songs for SongPath, Rating, PlayCounter, LastTimePlayed and (if
// present) a MusicBrainz id, convert Rating via FromMMRating and SongPath via
// match.NormalizeRel, and drop rows outside root.
func Open(path string) (Source, error) {
	return nil, ErrNotImplemented
}

// FromMMRating converts MediaMonkey's 0-100 rating (with -1 = unrated and
// half-star steps of 10) to a 0-5 star value, rounding to the nearest star.
// Navidrome ratings are integer stars, so half-stars are unavoidably lost.
func FromMMRating(mm int) model.Rating {
	if mm < 0 {
		return 0
	}
	r := (mm + 10) / 20
	if r > 5 {
		r = 5
	}
	return model.Rating(r)
}

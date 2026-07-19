// Package mm reads a MediaMonkey 5 database (MM5.DB, SQLite) read-only and
// normalizes the songs we care about.
package mm

import (
	"errors"
	"time"

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
// "file:<path>?mode=ro&immutable=1", and return a concrete Source. ReadTracks:
//
//	SELECT SongPath, Rating, PlayCounter, LastTimePlayed FROM Songs
//
//   - RelPath: match.NormalizeRel(SongPath, root). NB: MM blanks the drive
//     letter, so SongPath looks like ":\My Music\Artist\...". The chosen root
//     is in that same stored form (e.g. ":\My Music"). Drop rows not under root.
//   - Rating: FromMMRating (Rating is 0-100; -1/NULL = unrated).
//   - LastPlayed: FromMMDate(LastTimePlayed) (0 => never / zero Time).
//   - MBID: leave "" — MM5 has no MBID column (it hides in ExtendedTags);
//     relative-path matching carries the load. (Future: parse ExtendedTags.)
func Open(path string) (Source, error) {
	return nil, ErrNotImplemented
}

// mmEpoch is MediaMonkey's TDateTime epoch: float day 0 == 1899-12-30.
var mmEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// FromMMDate converts a MediaMonkey TDateTime (float days since 1899-12-30) to
// a time.Time. A value <= 0 means "never" and yields the zero Time.
//
// The float is the stored wall-clock, interpreted here as UTC. MM keeps local
// time without a reliable per-row offset (see the Played.UTCOffset column), and
// Navidrome stores play_date in UTC — so exact-timezone callers may adjust.
func FromMMDate(d float64) time.Time {
	if d <= 0 {
		return time.Time{}
	}
	return mmEpoch.Add(time.Duration(d * float64(24*time.Hour)))
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

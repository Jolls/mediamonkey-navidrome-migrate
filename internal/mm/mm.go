// Package mm reads a MediaMonkey 5 database (MM5.DB, SQLite) read-only and
// normalizes the songs we care about.
package mm

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jolls/mm5-navidrome-migrate/internal/match"
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

type sqliteSource struct {
	db *sql.DB
}

// Open opens MM5.DB read-only.
func Open(path string) (Source, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteSource{db: db}, nil
}

func (s *sqliteSource) Close() error { return s.db.Close() }

func (s *sqliteSource) ReadTracks(root string) ([]model.Track, error) {
	rows, err := s.db.Query(`SELECT SongPath, Rating, PlayCounter, LastTimePlayed FROM Songs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []model.Track
	for rows.Next() {
		var (
			songPath   sql.NullString
			rating     sql.NullInt64
			playCount  int
			lastPlayed float64
		)
		if err := rows.Scan(&songPath, &rating, &playCount, &lastPlayed); err != nil {
			return nil, err
		}
		if !songPath.Valid {
			continue
		}

		rel, ok := match.NormalizeRel(songPath.String, root)
		if !ok {
			continue
		}

		mmRating := -1
		if rating.Valid {
			mmRating = int(rating.Int64)
		}

		tracks = append(tracks, model.Track{
			RelPath:    rel,
			OrigPath:   songPath.String,
			Rating:     FromMMRating(mmRating),
			PlayCount:  playCount,
			LastPlayed: FromMMDate(lastPlayed),
		})
	}
	return tracks, rows.Err()
}

// mmEpoch is MediaMonkey's TDateTime epoch: float day 0 == 1899-12-30.
var mmEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// FromMMDate converts a MediaMonkey TDateTime (float days since 1899-12-30) to
// a time.Time. A value <= 0 means "never" and yields the zero Time.
//
// The float is the stored wall-clock, interpreted here as UTC purely as an
// arbitrary Location tag — MM keeps local time without a reliable per-row
// offset (see the Played.UTCOffset column), so this is not a real UTC
// instant. Callers that write it back out (see nav.SetAnnotation) must
// re-tag it with a real zone rather than trust this Location.
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

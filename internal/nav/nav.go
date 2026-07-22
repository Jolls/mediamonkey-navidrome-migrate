// Package nav reads Navidrome's database for matching and performs the direct
// writes the Subsonic API cannot express: exact play counts and backdated
// last-played (annotation), and backdated date-added (media_file.created_at).
// All direct writes use SET semantics (overwrite, never add) so re-running a
// scope is idempotent.
package nav

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jolls/mm5-navidrome-migrate/internal/match"
	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

// ErrNotImplemented marks skeleton stubs still to be filled in.
var ErrNotImplemented = errors.New("not implemented")

// User is a Navidrome account; annotations are per-user.
type User struct {
	ID       string
	Username string
}

// Annotation is the per-track state we set directly in navidrome.db.
type Annotation struct {
	NavID      string
	PlayCount  int
	LastPlayed time.Time // zero => leave play_date null
}

// Reader reads navidrome.db read-only (for matching and user selection).
type Reader interface {
	// ReadTracks returns media files with RelPath = the normalized, already
	// library-relative media_file.path (no external root needed).
	ReadTracks() ([]model.NavTrack, error)
	Users() ([]User, error)
	// ReadState returns each media_file's currently-stored rating/play-count/
	// play-date (for userID) and created_at, keyed by media_file.id — for
	// verifying what Commit would write against what's actually there.
	ReadState(userID string) (map[string]NavState, error)
	Close() error
}

// NavState is a track's currently-stored Navidrome values, as read back by
// ReadState.
type NavState struct {
	Rating    int
	PlayCount int
	PlayDate  time.Time // zero if NULL
	CreatedAt time.Time // zero if NULL or unparsable
}

// AnnotationWriter performs the direct, idempotent play-count/date writes.
type AnnotationWriter interface {
	// SetAnnotation upserts the annotation row for (userID, a.NavID), setting
	// play_count and play_date to the given values (overwrite, never add).
	SetAnnotation(userID string, a Annotation) error
	// SetCreatedAt overwrites media_file.created_at for navID.
	SetCreatedAt(navID string, t time.Time) error
	Close() error
}

type sqliteReader struct {
	db *sql.DB
}

// OpenReader opens navidrome.db read-only.
func OpenReader(path string) (Reader, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteReader{db: db}, nil
}

func (r *sqliteReader) Close() error { return r.db.Close() }

func (r *sqliteReader) ReadTracks() ([]model.NavTrack, error) {
	rows, err := r.db.Query(`SELECT id, path, mbz_recording_id FROM media_file WHERE COALESCE(missing,0)=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []model.NavTrack
	for rows.Next() {
		var (
			id   string
			path string
			mbid sql.NullString
		)
		if err := rows.Scan(&id, &path, &mbid); err != nil {
			return nil, err
		}
		tracks = append(tracks, model.NavTrack{
			ID:      id,
			RelPath: match.Normalize(path),
			MBID:    mbid.String,
		})
	}
	return tracks, rows.Err()
}

// ReadState reads each media_file's currently-stored state, left-joining
// annotation for userID so tracks with no annotation row yet still appear
// (with zero rating/play_count/play_date).
func (r *sqliteReader) ReadState(userID string) (map[string]NavState, error) {
	rows, err := r.db.Query(`
		SELECT m.id, m.created_at, COALESCE(a.rating, 0), COALESCE(a.play_count, 0), a.play_date
		FROM media_file m
		LEFT JOIN annotation a ON a.item_id = m.id AND a.item_type = 'media_file' AND a.user_id = ?
		WHERE COALESCE(m.missing, 0) = 0
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]NavState)
	for rows.Next() {
		var (
			id        string
			createdAt string
			rating    int
			playCount int
			playDate  sql.NullString
		)
		if err := rows.Scan(&id, &createdAt, &rating, &playCount, &playDate); err != nil {
			return nil, err
		}
		st := NavState{Rating: rating, PlayCount: playCount}
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			st.CreatedAt = t
		}
		if playDate.Valid {
			if t, err := time.Parse(time.RFC3339Nano, playDate.String); err == nil {
				st.PlayDate = t
			}
		}
		out[id] = st
	}
	return out, rows.Err()
}

func (r *sqliteReader) Users() ([]User, error) {
	rows, err := r.db.Query(`SELECT id, user_name FROM user`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

type sqliteWriter struct {
	db *sql.DB
}

// OpenWriter opens navidrome.db read-write for annotation upserts. Callers MUST
// have already run EnsureUnlocked and Backup.
func OpenWriter(path string) (AnnotationWriter, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteWriter{db: db}, nil
}

func (w *sqliteWriter) Close() error { return w.db.Close() }

// navTimeFormat matches the layout Navidrome itself writes into its own
// datetime columns (e.g. created_at/updated_at): RFC3339 with an explicit
// numeric offset, never a bare "Z".
const navTimeFormat = "2006-01-02T15:04:05.999999999-07:00"

// SetAnnotation upserts (userID, a.NavID)'s play_count/play_date, touching only
// those two columns so an API-set rating/starred on the same row survives.
func (w *sqliteWriter) SetAnnotation(userID string, a Annotation) error {
	var playDate any
	if !a.LastPlayed.IsZero() {
		// a.LastPlayed carries a naive wall-clock reading (from MM's
		// TDateTime, which has no reliable offset) tagged with UTC as an
		// arbitrary Location, not a real UTC instant. Formatting it at that
		// zero offset makes modernc.org/sqlite canonicalize the stored text
		// to a trailing "Z" (confirmed: even an explicit "+00:00" string
		// gets rewritten to "Z" on insert into this datetime-affinity
		// column) — a shape none of Navidrome's own timestamp columns ever
		// use. Reinterpreting the same clock reading in the local zone
		// keeps the offset non-zero, matching Navidrome's own convention
		// and avoiding that canonicalization path entirely.
		lp := a.LastPlayed
		local := time.Date(lp.Year(), lp.Month(), lp.Day(), lp.Hour(), lp.Minute(), lp.Second(), lp.Nanosecond(), time.Local)
		playDate = local.Format(navTimeFormat)
	}
	_, err := w.db.Exec(`
		INSERT INTO annotation (user_id, item_id, item_type, play_count, play_date)
		VALUES (?, ?, 'media_file', ?, ?)
		ON CONFLICT(user_id, item_id, item_type)
		DO UPDATE SET play_count=excluded.play_count, play_date=excluded.play_date
	`, userID, a.NavID, a.PlayCount, playDate)
	return err
}

// SetCreatedAt overwrites media_file.created_at for navID. A zero t is a no-op
// — MM's DateAdded is unknown/never, and media_file.created_at is NOT NULL so
// there's nothing sensible to write.
func (w *sqliteWriter) SetCreatedAt(navID string, t time.Time) error {
	if t.IsZero() {
		return nil
	}
	// See the comment in SetAnnotation: same TDateTime-sourced ambiguity, same
	// local-time reinterpretation to avoid sqlite's "Z" canonicalization.
	local := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
	_, err := w.db.Exec(`UPDATE media_file SET created_at = ? WHERE id = ?`, local.Format(navTimeFormat), navID)
	return err
}

// EnsureUnlocked returns an error if Navidrome appears to be running against
// dbPath. Direct writes must never race a live server.
func EnsureUnlocked(dbPath string) error {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(0)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("navidrome.db appears to be in use (is Navidrome running?): %w", err)
	}
	_, _ = conn.ExecContext(ctx, "COMMIT")
	return nil
}

// Backup copies dbPath to a timestamped sibling and returns the backup path.
// Always call this before the first direct write.
func Backup(dbPath string) (string, error) {
	dst := fmt.Sprintf("%s.backup-%s", dbPath, time.Now().Format("20060102-150405"))
	in, err := os.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return "", err
	}
	return dst, out.Close()
}

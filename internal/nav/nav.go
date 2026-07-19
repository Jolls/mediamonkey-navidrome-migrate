// Package nav reads Navidrome's database for matching and performs the direct
// annotation writes the Subsonic API cannot express (exact play counts and
// backdated last-played). All direct writes use SET semantics (overwrite, never
// add) so re-running a scope is idempotent.
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
	Close() error
}

// AnnotationWriter performs the direct, idempotent play-count/date writes.
type AnnotationWriter interface {
	// SetAnnotation upserts the annotation row for (userID, a.NavID), setting
	// play_count and play_date to the given values (overwrite, never add).
	SetAnnotation(userID string, a Annotation) error
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

// SetAnnotation upserts (userID, a.NavID)'s play_count/play_date, touching only
// those two columns so an API-set rating/starred on the same row survives.
func (w *sqliteWriter) SetAnnotation(userID string, a Annotation) error {
	var playDate any
	if !a.LastPlayed.IsZero() {
		playDate = a.LastPlayed.UTC()
	}
	_, err := w.db.Exec(`
		INSERT INTO annotation (user_id, item_id, item_type, play_count, play_date)
		VALUES (?, ?, 'media_file', ?, ?)
		ON CONFLICT(user_id, item_id, item_type)
		DO UPDATE SET play_count=excluded.play_count, play_date=excluded.play_date
	`, userID, a.NavID, a.PlayCount, playDate)
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

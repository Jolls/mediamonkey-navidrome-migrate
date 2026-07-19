// Package nav reads Navidrome's database for matching and performs the direct
// annotation writes the Subsonic API cannot express (exact play counts and
// backdated last-played). All direct writes use SET semantics (overwrite, never
// add) so re-running a scope is idempotent.
package nav

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

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

// OpenReader opens navidrome.db read-only.
//
// TODO(sonnet): implement with modernc.org/sqlite (read-only DSN). ReadTracks
// selects id, path, mbz_recording_id from media_file (skip missing=1) and
// normalizes path with match.Normalize (path is already library-relative — do
// NOT strip a root). Users selects id, user_name from the user table.
func OpenReader(path string) (Reader, error) { return nil, ErrNotImplemented }

// OpenWriter opens navidrome.db read-write for annotation upserts. Callers MUST
// have already run EnsureUnlocked and Backup.
//
// TODO(sonnet): implement. Upsert keyed by (user_id, item_id,
// item_type='media_file'):
//
//	INSERT INTO annotation (user_id,item_id,item_type,play_count,play_date)
//	VALUES (?,?, 'media_file', ?, ?)
//	ON CONFLICT(user_id,item_id,item_type)
//	DO UPDATE SET play_count=excluded.play_count, play_date=excluded.play_date;
//
// Only touch play_count/play_date so a rating/starred set via the API (same
// row) is preserved. play_date is nullable — pass NULL when LastPlayed is zero.
func OpenWriter(path string) (AnnotationWriter, error) { return nil, ErrNotImplemented }

// EnsureUnlocked returns an error if Navidrome appears to be running against
// dbPath. Direct writes must never race a live server.
//
// TODO(sonnet): implement a real liveness/lock check (e.g. attempt an exclusive
// lock, or inspect the -wal/-shm sidecar files).
func EnsureUnlocked(dbPath string) error { return ErrNotImplemented }

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

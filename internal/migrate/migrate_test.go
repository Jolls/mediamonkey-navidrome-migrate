package migrate

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jolls/mm5-navidrome-migrate/internal/mm"
	"github.com/jolls/mm5-navidrome-migrate/internal/model"
	"github.com/jolls/mm5-navidrome-migrate/internal/nav"
)

// buildMM5 creates a real on-disk MM5.DB-shaped SQLite file (mm.Open needs a
// real file, not :memory:) with one rated/played/dated track and one
// never-played/never-dated track.
func buildMM5(t *testing.T) mm.Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), "MM5.DB")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE Songs (ID INTEGER PRIMARY KEY, SongPath TEXT, Rating INTEGER, PlayCounter INTEGER, LastTimePlayed REAL, DateAdded REAL);
		INSERT INTO Songs (SongPath, Rating, PlayCounter, LastTimePlayed, DateAdded) VALUES (':\My Music\a.mp3', 100, 5, 41650, 41600);
		INSERT INTO Songs (SongPath, Rating, PlayCounter, LastTimePlayed, DateAdded) VALUES (':\My Music\b.mp3', -1, 0, 0, 0);
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	src, err := mm.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	return src
}

// buildNav creates a real on-disk navidrome.db-shaped SQLite file: track "a"
// has a stale annotation/created_at (simulating an un-migrated or
// scanner-reset row), track "b" has no annotation row at all.
func buildNav(t *testing.T) nav.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "navidrome.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE media_file (id TEXT PRIMARY KEY, path TEXT, mbz_recording_id TEXT, missing INTEGER, created_at TEXT);
		CREATE TABLE annotation (user_id TEXT, item_id TEXT, item_type TEXT, play_count INTEGER, play_date TEXT, rating INTEGER, starred INTEGER,
			PRIMARY KEY (user_id, item_id, item_type));
		CREATE TABLE user (id TEXT PRIMARY KEY, user_name TEXT);
		INSERT INTO media_file (id, path, created_at) VALUES ('m-a', 'a.mp3', '2026-01-01T00:00:00-07:00');
		INSERT INTO media_file (id, path, created_at) VALUES ('m-b', 'b.mp3', '2026-01-01T00:00:00-07:00');
		INSERT INTO annotation (user_id, item_id, item_type, play_count, play_date, rating) VALUES ('u1', 'm-a', 'media_file', 1, NULL, 3);
		INSERT INTO user (id, user_name) VALUES ('u1', 'alice');
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := nav.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func testCfg() model.Config {
	return model.Config{
		MMRoot: `:\My Music`,
		UserID: "u1",
		RatingMap: [11]model.Rating{
			0: 0, 1: 1, 2: 1, 3: 2, 4: 2, 5: 3, 6: 3, 7: 4, 8: 4, 9: 5, 10: 5,
		},
	}
}

// TestVerifyFindsMismatches locks in that Verify flags a's stale
// created_at/wrong play_count/rating as mismatches, while b (never
// rated/played/dated in MM) matches trivially since MM has nothing to check
// it against for DateAdded and its zero-value annotation state agrees with an
// absent annotation row.
func TestVerifyFindsMismatches(t *testing.T) {
	p := &Pipeline{Cfg: testCfg(), Source: buildMM5(t), NavDB: buildNav(t)}
	rep, err := p.Verify(model.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 2 {
		t.Fatalf("Checked = %d, want 2", rep.Checked)
	}
	if rep.Mismatched != 1 {
		t.Fatalf("Mismatched = %d, want 1; rows=%+v", rep.Mismatched, rep.Rows)
	}
	row := rep.Rows[0]
	if row.RelPath != "a.mp3" {
		t.Fatalf("mismatched row RelPath = %q, want a.mp3", row.RelPath)
	}
	if row.RatingMatch {
		t.Error("expected RatingMatch = false (MM rating 100 -> mapped 5, actual annotation.rating = 3)")
	}
	if row.PlayCountMatch {
		t.Error("expected PlayCountMatch = false (MM PlayCounter 5, actual annotation.play_count = 1)")
	}
	if row.LastPlayedMatch {
		t.Error("expected LastPlayedMatch = false (MM has a LastTimePlayed, actual play_date is NULL)")
	}
	if !row.DateAddedChecked {
		t.Error("expected DateAddedChecked = true (MM DateAdded is non-zero)")
	}
	if row.DateAddedMatch {
		t.Error("expected DateAddedMatch = false (MM DateAdded is 2014-ish, actual created_at is the scanner default)")
	}
}

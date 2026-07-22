package nav

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openWriterFixture(t *testing.T) *sqliteWriter {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE media_file (id TEXT PRIMARY KEY, path TEXT, created_at TEXT);
		CREATE TABLE annotation (user_id TEXT, item_id TEXT, item_type TEXT, play_count INTEGER, play_date TEXT,
			rating INTEGER, starred INTEGER, PRIMARY KEY (user_id, item_id, item_type));
		INSERT INTO media_file (id, path, created_at) VALUES ('m1', 'a.mp3', '2020-01-01T00:00:00-07:00');
	`); err != nil {
		t.Fatal(err)
	}
	return &sqliteWriter{db: db}
}

func TestSetCreatedAt(t *testing.T) {
	w := openWriterFixture(t)
	want := time.Date(2015, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := w.SetCreatedAt("m1", want); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := w.db.QueryRow(`SELECT created_at FROM media_file WHERE id = 'm1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse(navTimeFormat, got)
	if err != nil {
		t.Fatalf("stored created_at %q doesn't match navTimeFormat: %v", got, err)
	}
	local := time.Date(want.Year(), want.Month(), want.Day(), want.Hour(), want.Minute(), want.Second(), want.Nanosecond(), time.Local)
	if !parsed.Equal(local) {
		t.Errorf("stored created_at = %v, want %v", parsed, local)
	}
}

func TestSetCreatedAtZeroIsNoOp(t *testing.T) {
	w := openWriterFixture(t)
	if err := w.SetCreatedAt("m1", time.Time{}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := w.db.QueryRow(`SELECT created_at FROM media_file WHERE id = 'm1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "2020-01-01T00:00:00-07:00" {
		t.Errorf("created_at changed on zero-time SetCreatedAt: got %q", got)
	}
}

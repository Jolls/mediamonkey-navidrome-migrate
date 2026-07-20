package mm

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestFromMMPlayDate locks down the UTCOffset sign convention: local time
// minus offset gives UTC (Pacific Time, offset -8h, comes back 8h ahead of
// the naive reading FromMMDate would give the same PlayDate float).
func TestFromMMPlayDate(t *testing.T) {
	const playDate = 41653.0
	naive := FromMMDate(playDate) // interprets the float as if it were already UTC
	got := FromMMPlayDate(playDate, -8.0/24.0)
	want := naive.Add(8 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("FromMMPlayDate(%v, -8h) = %v, want %v (naive + 8h)", playDate, got, want)
	}
}

func TestFromMMPlayDateNever(t *testing.T) {
	if got := FromMMPlayDate(0, 0); !got.IsZero() {
		t.Errorf("FromMMPlayDate(0, 0) = %v, want zero time", got)
	}
}

// openFixture builds an in-memory MM5.DB-shaped SQLite database with just the
// columns ReadPlays/ReadTracks touch.
func openFixture(t *testing.T) *sqliteSource {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE Songs (ID INTEGER PRIMARY KEY, SongPath TEXT, Artist TEXT, SongTitle TEXT, Album TEXT,
			Rating INTEGER, PlayCounter INTEGER, LastTimePlayed REAL);
		CREATE TABLE Played (IDPlayed INTEGER PRIMARY KEY, IDSong INTEGER, PlayDate REAL, UTCOffset REAL);
		INSERT INTO Songs (ID, SongPath, Artist, SongTitle, Album, Rating, PlayCounter, LastTimePlayed)
			VALUES (1, ':\My Music\a.mp3', 'Artist A', 'Title A', 'Album A', 80, 3, 41650);
		INSERT INTO Songs (ID, SongPath, Artist, SongTitle, Album, Rating, PlayCounter, LastTimePlayed)
			VALUES (2, ':\My Music\b.mp3', 'Artist B', 'Title B', 'Album B', -1, 0, 0);
		INSERT INTO Played (IDSong, PlayDate, UTCOffset) VALUES (1, 41650.5, -0.333333333);
		INSERT INTO Played (IDSong, PlayDate, UTCOffset) VALUES (1, 41651.5, -0.333333333);
		INSERT INTO Played (IDSong, PlayDate, UTCOffset) VALUES (2, 41652.5, 0);
	`); err != nil {
		t.Fatal(err)
	}
	return &sqliteSource{db: db}
}

func TestReadPlays(t *testing.T) {
	src := openFixture(t)
	plays, err := src.ReadPlays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 3 {
		t.Fatalf("got %d plays, want 3", len(plays))
	}
	// Newest first by real UTC instant.
	if plays[0].SongID != 2 || plays[0].Artist != "Artist B" || plays[0].Title != "Title B" || plays[0].Album != "Album B" {
		t.Errorf("plays[0] = %+v, want SongID=2 Artist B/Title B/Album B", plays[0])
	}
	if plays[2].SongID != 1 {
		t.Errorf("plays[2].SongID = %d, want 1", plays[2].SongID)
	}
}

// TestReadPlaysOrdersByUTCInstantNotLocalPlayDate locks in the fix for a real
// bug: sorting by raw PlayDate (local wall-clock days) instead of the derived
// UTC instant gives the wrong order whenever two rows have different
// UTCOffset (DST changes, travel) — a later local PlayDate can still be an
// earlier UTC instant than an earlier local PlayDate at a very different
// offset.
func TestReadPlaysOrdersByUTCInstantNotLocalPlayDate(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE Songs (ID INTEGER PRIMARY KEY, SongPath TEXT, Artist TEXT, SongTitle TEXT, Album TEXT);
		CREATE TABLE Played (IDPlayed INTEGER PRIMARY KEY, IDSong INTEGER, PlayDate REAL, UTCOffset REAL);
		INSERT INTO Songs (ID, SongPath, Artist, SongTitle, Album) VALUES (1, ':\My Music\earlier-utc.mp3', 'A', 'Earlier UTC', 'Alb');
		INSERT INTO Songs (ID, SongPath, Artist, SongTitle, Album) VALUES (2, ':\My Music\later-utc.mp3', 'B', 'Later UTC', 'Alb');
		-- Song 1: later local PlayDate (100.9) but at UTC+12h -> UTC instant 100.4.
		INSERT INTO Played (IDSong, PlayDate, UTCOffset) VALUES (1, 100.9, 0.5);
		-- Song 2: earlier local PlayDate (100.5) but at UTC-12h -> UTC instant 101.0 (truly later).
		INSERT INTO Played (IDSong, PlayDate, UTCOffset) VALUES (2, 100.5, -0.5);
	`); err != nil {
		t.Fatal(err)
	}

	plays, err := (&sqliteSource{db: db}).ReadPlays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 2 {
		t.Fatalf("got %d plays, want 2", len(plays))
	}
	if plays[0].SongID != 2 {
		t.Errorf("plays[0].SongID = %d, want 2 (the truly-later UTC instant, despite the earlier local PlayDate)", plays[0].SongID)
	}
}

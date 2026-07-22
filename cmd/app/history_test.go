package main

import (
	"testing"
	"time"

	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

// TestPlaysToScrobblesSeparatesDifferentSongsOnSameSecond locks in that two
// different songs played less than a second apart — which MediaMonkey can
// log with sub-second precision but Maloja's API only accepts as whole
// seconds — end up with distinct Time values. Without this, Maloja's
// timestamp-only dedup would reject the second one as a duplicate of the
// first and it would be silently lost, even though they're different tracks.
func TestPlaysToScrobblesSeparatesDifferentSongsOnSameSecond(t *testing.T) {
	base := time.Unix(1000, 0)
	plays := []model.Play{
		{ID: 1, SongID: 100, Artist: "A", Title: "Song A", PlayedAt: base.Add(10 * time.Millisecond)},
		{ID: 2, SongID: 200, Artist: "B", Title: "Song B", PlayedAt: base.Add(900 * time.Millisecond)},
	}

	pending := playsToScrobbles(plays, nil, 0)
	if len(pending.Scrobbles) != 2 {
		t.Fatalf("got %d scrobbles, want 2", len(pending.Scrobbles))
	}
	if pending.Scrobbles[0].Time == pending.Scrobbles[1].Time {
		t.Errorf("both scrobbles got Time=%d, want distinct seconds for two different songs", pending.Scrobbles[0].Time)
	}
}

// TestPlaysToScrobblesKeepsGenuineDuplicatesColliding locks in the other
// side: when MediaMonkey has genuinely double-logged the *same* song at the
// same rounded second, the two scrobbles must keep the identical Time —
// that collision is intentional, so Maloja's own duplicate-timestamp
// response (handled by SubmitAll/IsDuplicateTimestamp) can catch and skip
// the repeat instead of it being submitted twice as separate plays.
func TestPlaysToScrobblesKeepsGenuineDuplicatesColliding(t *testing.T) {
	base := time.Unix(2000, 0)
	plays := []model.Play{
		{ID: 1, SongID: 100, Artist: "A", Title: "Song A", PlayedAt: base.Add(1 * time.Millisecond)},
		{ID: 2, SongID: 100, Artist: "A", Title: "Song A", PlayedAt: base.Add(2 * time.Millisecond)},
	}

	pending := playsToScrobbles(plays, nil, 0)
	if len(pending.Scrobbles) != 2 {
		t.Fatalf("got %d scrobbles, want 2", len(pending.Scrobbles))
	}
	if pending.Scrobbles[0].Time != pending.Scrobbles[1].Time {
		t.Errorf("Time values = %d, %d, want equal for a genuine same-song duplicate", pending.Scrobbles[0].Time, pending.Scrobbles[1].Time)
	}
}

// TestPlaysToScrobblesThreeWayCollision checks the cascade case: three
// different songs all rounding to the same second must each land on a
// distinct second, not just the first pair.
func TestPlaysToScrobblesThreeWayCollision(t *testing.T) {
	base := time.Unix(3000, 0)
	plays := []model.Play{
		{ID: 1, SongID: 100, Artist: "A", Title: "Song A", PlayedAt: base.Add(10 * time.Millisecond)},
		{ID: 2, SongID: 200, Artist: "B", Title: "Song B", PlayedAt: base.Add(20 * time.Millisecond)},
		{ID: 3, SongID: 300, Artist: "C", Title: "Song C", PlayedAt: base.Add(30 * time.Millisecond)},
	}

	pending := playsToScrobbles(plays, nil, 0)
	if len(pending.Scrobbles) != 3 {
		t.Fatalf("got %d scrobbles, want 3", len(pending.Scrobbles))
	}
	seen := make(map[int64]bool)
	for _, s := range pending.Scrobbles {
		if seen[s.Time] {
			t.Errorf("Time=%d reused across a 3-way collision, want every song on its own second", s.Time)
		}
		seen[s.Time] = true
	}
}

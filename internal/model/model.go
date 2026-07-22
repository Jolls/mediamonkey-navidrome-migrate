// Package model holds the core domain types shared across the migration
// pipeline. These types are the contract every other package agrees on; keep
// them free of I/O and framework dependencies.
package model

import "time"

// Rating is a 0-5 star value, matching how Navidrome and the Subsonic API
// represent ratings. 0 means unrated.
type Rating int

// Track is a normalized MediaMonkey song, reduced to the fields we migrate.
type Track struct {
	RelPath    string    // path relative to the shared root, normalized (see match.NormalizeRel)
	OrigPath   string    // original MM SongPath, for display only
	RatingStep int       // MM rating as a 0-10 half-star step (0 = unrated); see Config.MapRating
	PlayCount  int       // MM PlayCounter
	LastPlayed time.Time // zero value means never played
	DateAdded  time.Time // MM DateAdded; zero value means unknown
	MBID       string    // MusicBrainz recording id; "" when absent
}

// Play is one MediaMonkey play-history entry (from the Played table),
// denormalized for display and ListenBrainz/Maloja export. Independent of the
// Navidrome matching pipeline — Path is raw and unnormalized, for display only.
type Play struct {
	ID          int64 // MM Played.IDPlayed — stable identity for ListenBrainz/Maloja submission tracking
	SongID      int64
	Path        string // raw MM SongPath, display only
	Artist      string
	AlbumArtist string // MM Songs.AlbumArtist; "" when absent
	Title       string
	Album       string
	Duration    int       // MM Songs.SongLength in whole seconds; 0 when absent
	PlayedAt    time.Time // real UTC instant: see mm.FromMMPlayDate
}

// NavTrack is a Navidrome media_file, reduced to what matching needs.
type NavTrack struct {
	ID      string // media_file.id — the stable id the Subsonic API uses
	RelPath string // normalized like Track.RelPath
	MBID    string
}

// MatchStatus classifies the outcome of matching one MM track to Navidrome.
type MatchStatus int

const (
	Unmatched MatchStatus = iota // no Navidrome track found
	Matched                      // exactly one confident match
	Ambiguous                    // multiple candidates; needs user resolution
)

// Match pairs a source track with its Navidrome counterpart (if any).
type Match struct {
	Source     Track
	Nav        *NavTrack // nil unless Status == Matched
	Status     MatchStatus
	Via        string     // how it matched: "relpath" or "mbid"
	Candidates []NavTrack // populated when Status == Ambiguous
}

// Field is a single migratable datum; Fields is a bitset of them.
type Field int

const (
	FieldRating    Field = 1 << iota
	FieldPlayCount       // includes LastPlayed
	FieldStarred
	FieldDateAdded
)

// Fields is a bitset of Field values.
type Fields int

// Has reports whether the bitset includes x.
func (f Fields) Has(x Field) bool { return int(f)&int(x) != 0 }

// Scope restricts an operation to tracks whose RelPath sits under Dir. An empty
// Dir means the whole library. Dir must be normalized like Track.RelPath.
type Scope struct {
	Dir string
}

// Config is everything the UI collects before a run.
type Config struct {
	MMDBPath  string
	NavDBPath string
	ServerURL string
	Username  string
	Password  string
	MMRoot    string // absolute root MM's SongPaths live under, on this machine.
	// Navidrome needs no root here: media_file.path is already library-relative.
	UserID string // Navidrome user that owns the annotations
	Fields Fields

	// StarThreshold is the minimum mapped Navidrome rating (0-5) treated as
	// "starred" when FieldStarred is set. MM has no true favorite flag, so
	// this maps the rating scale onto Navidrome's boolean star. Zero means
	// "unset"; callers should default it to 5 (DefaultStarThreshold).
	StarThreshold Rating

	// RatingMap converts a Track.RatingStep (0-10; 0 = unrated, 1-10 = MM's
	// half-star steps 0.5-5.0) to the Navidrome rating actually written.
	// Index 0 is the unrated mapping; indices 1-10 are the half-star steps.
	// The UI is responsible for filling this in (its "round up"/"round down"
	// presets and its custom editor all just produce this same table).
	RatingMap [11]Rating
}

// DefaultStarThreshold is the StarThreshold used when a Config leaves it unset.
const DefaultStarThreshold Rating = 5

// MapRating converts a MM rating step (0-10, see Track.RatingStep) to the
// Navidrome rating to write, via cfg.RatingMap. Out-of-range steps clamp.
func (c Config) MapRating(step int) Rating {
	if step < 0 {
		step = 0
	}
	if step > 10 {
		step = 10
	}
	return c.RatingMap[step]
}

// Change is one track's intended write, as shown in a dry-run. A nil pointer
// means "leave this field untouched".
type Change struct {
	RelPath    string
	NavID      string
	Rating     *Rating
	PlayCount  *int
	LastPlayed *time.Time
	Starred    *bool
	DateAdded  *time.Time
}

// UnresolvedTrack is one track that didn't cleanly match, shown in the dry-run
// review so the user can see what needs attention before commit.
type UnresolvedTrack struct {
	RelPath string
	Status  MatchStatus // Unmatched or Ambiguous
}

// DryRunReport summarizes what a Commit over the same scope would do.
type DryRunReport struct {
	Scope                         Scope
	Matched, Ambiguous, Unmatched int
	Changes                       []Change
	Unresolved                    []UnresolvedTrack
}

// CommitResult reports what actually happened.
type CommitResult struct {
	Applied int
	Errors  []CommitError
}

// VerifyRow compares what MediaMonkey says a matched track's fields should be
// against what's actually stored in navidrome.db right now. Only fields
// migrate.go's Commit can write are checked (not Starred, which is a boolean
// derived from Rating).
type VerifyRow struct {
	RelPath string
	NavID   string

	ExpectedRating, ActualRating int
	RatingMatch                  bool

	ExpectedPlayCount, ActualPlayCount int
	PlayCountMatch                     bool

	// nil means "never played" / no play_date.
	ExpectedLastPlayed, ActualLastPlayed *time.Time
	LastPlayedMatch                      bool

	// DateAddedChecked is false when MM's DateAdded is unknown (zero) — Commit
	// leaves media_file.created_at untouched in that case, so there's nothing
	// to verify. Expected/ActualDateAdded are nil in that case too.
	DateAddedChecked                   bool
	ExpectedDateAdded, ActualDateAdded *time.Time
	DateAddedMatch                     bool
}

// VerifyReport summarizes a comparison of MediaMonkey's data against
// navidrome.db's current state. Rows lists only mismatched tracks.
type VerifyReport struct {
	Scope      Scope
	Checked    int
	Mismatched int
	Rows       []VerifyRow
}

// CommitError records a single failed write.
type CommitError struct {
	RelPath string
	Err     string
}

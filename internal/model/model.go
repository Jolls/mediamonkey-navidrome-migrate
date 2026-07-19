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
	Rating     Rating    // 0-5, converted from MM's 0-100 scale
	PlayCount  int       // MM PlayCounter
	LastPlayed time.Time // zero value means never played
	MBID       string    // MusicBrainz recording id; "" when absent
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
	Fields    Fields

	// StarThreshold is the minimum MM rating (0-5) treated as "starred" when
	// FieldStarred is set. MM has no true favorite flag, so this maps its
	// rating scale onto Navidrome's boolean star. Zero means "unset"; callers
	// should default it to 5 (DefaultStarThreshold).
	StarThreshold Rating
}

// DefaultStarThreshold is the StarThreshold used when a Config leaves it unset.
const DefaultStarThreshold Rating = 5

// Change is one track's intended write, as shown in a dry-run. A nil pointer
// means "leave this field untouched".
type Change struct {
	RelPath    string
	NavID      string
	Rating     *Rating
	PlayCount  *int
	LastPlayed *time.Time
	Starred    *bool
}

// DryRunReport summarizes what a Commit over the same scope would do.
type DryRunReport struct {
	Scope                         Scope
	Matched, Ambiguous, Unmatched int
	Changes                       []Change
}

// CommitResult reports what actually happened.
type CommitResult struct {
	Applied int
	Errors  []CommitError
}

// CommitError records a single failed write.
type CommitError struct {
	RelPath string
	Err     string
}

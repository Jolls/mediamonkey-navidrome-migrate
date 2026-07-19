// Package match turns absolute file paths into comparable relative keys and
// pairs MediaMonkey tracks with Navidrome tracks. Relative path is the primary
// signal (both libraries point at the same files); MusicBrainz id is the
// fallback.
package match

import (
	"path"
	"strings"

	"github.com/jolls/mm5-navidrome-migrate/internal/model"
)

// NormalizeRel reduces an absolute path to a key relative to root, suitable for
// cross-platform comparison: forward slashes, lowercased, root prefix stripped.
// ok is false when abs is not under root.
func NormalizeRel(abs, root string) (rel string, ok bool) {
	a := Normalize(abs)
	r := strings.TrimSuffix(Normalize(root), "/")
	if r != "" && a != r && !strings.HasPrefix(a, r+"/") {
		return "", false
	}
	rel = strings.TrimPrefix(strings.TrimPrefix(a, r), "/")
	return rel, true
}

// Normalize converts any OS path to lowercase, forward-slash, cleaned form.
// Use this for Navidrome's media_file.path, which is already library-relative
// and must not have a root stripped.
func Normalize(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.ToLower(path.Clean(p))
}

// Index looks up Navidrome tracks by relative path or MBID.
type Index struct {
	byRel  map[string][]model.NavTrack
	byMBID map[string]model.NavTrack
}

// BuildIndex builds lookup tables over the Navidrome tracks.
func BuildIndex(tracks []model.NavTrack) *Index {
	ix := &Index{
		byRel:  make(map[string][]model.NavTrack, len(tracks)),
		byMBID: make(map[string]model.NavTrack),
	}
	for _, t := range tracks {
		ix.byRel[t.RelPath] = append(ix.byRel[t.RelPath], t)
		if t.MBID != "" {
			ix.byMBID[t.MBID] = t
		}
	}
	return ix
}

// Match resolves one source track. Relative path is tried first; on a miss we
// fall back to MusicBrainz id.
func (ix *Index) Match(t model.Track) model.Match {
	m := model.Match{Source: t}
	switch cands := ix.byRel[t.RelPath]; {
	case len(cands) == 1:
		nav := cands[0]
		m.Status, m.Nav, m.Via = model.Matched, &nav, "relpath"
		return m
	case len(cands) > 1:
		m.Status, m.Candidates, m.Via = model.Ambiguous, cands, "relpath"
		return m
	}
	if t.MBID != "" {
		if nav, ok := ix.byMBID[t.MBID]; ok {
			n := nav
			m.Status, m.Nav, m.Via = model.Matched, &n, "mbid"
			return m
		}
	}
	m.Status = model.Unmatched
	return m
}

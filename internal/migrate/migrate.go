// Package migrate orchestrates the scan -> match -> dry-run -> commit pipeline.
// It owns the policy decisions from DESIGN.md: relative-path matching, scope
// filtering, and the API-first / direct-DB split of writes. All I/O is
// delegated to the injected interfaces, so this logic stays testable.
package migrate

import (
	"log"
	"strings"
	"time"

	"github.com/jolls/mm5-navidrome-migrate/internal/match"
	"github.com/jolls/mm5-navidrome-migrate/internal/mm"
	"github.com/jolls/mm5-navidrome-migrate/internal/model"
	"github.com/jolls/mm5-navidrome-migrate/internal/nav"
	"github.com/jolls/mm5-navidrome-migrate/internal/subsonic"
)

// commitLogInterval controls how often Commit reports progress to the
// terminal — frequent enough to show it's alive on a large library, not so
// frequent it floods the log.
const commitLogInterval = 250

// Pipeline wires the sources and sinks together. Ratings/stars go to the
// Subsonic API; play counts and backdated dates go straight to navidrome.db.
type Pipeline struct {
	Cfg    model.Config
	Source mm.Source
	NavDB  nav.Reader
	Writer nav.AnnotationWriter
	API    *subsonic.Client
}

// Scan reads both libraries and matches every source track within scope.
func (p *Pipeline) Scan(scope model.Scope) ([]model.Match, error) {
	src, err := p.Source.ReadTracks(p.Cfg.MMRoot)
	if err != nil {
		return nil, err
	}
	navTracks, err := p.NavDB.ReadTracks()
	if err != nil {
		return nil, err
	}
	ix := match.BuildIndex(navTracks)
	var out []model.Match
	for _, t := range src {
		if !inScope(scope, t.RelPath) {
			continue
		}
		out = append(out, ix.Match(t))
	}
	return out, nil
}

// DryRun computes the changes Commit would apply for the given scope and the
// configured fields, writing nothing.
func (p *Pipeline) DryRun(scope model.Scope) (model.DryRunReport, error) {
	matches, err := p.Scan(scope)
	if err != nil {
		return model.DryRunReport{}, err
	}
	rep := model.DryRunReport{Scope: scope}
	for _, m := range matches {
		switch m.Status {
		case model.Ambiguous:
			rep.Ambiguous++
			rep.Unresolved = append(rep.Unresolved, model.UnresolvedTrack{RelPath: m.Source.RelPath, Status: m.Status})
		case model.Unmatched:
			rep.Unmatched++
			rep.Unresolved = append(rep.Unresolved, model.UnresolvedTrack{RelPath: m.Source.RelPath, Status: m.Status})
		case model.Matched:
			rep.Matched++
			rep.Changes = append(rep.Changes, p.change(m))
		}
	}
	return rep, nil
}

// Verify compares what MediaMonkey says each matched track's fields should be
// against what's actually stored in navidrome.db right now, independent of
// which Fields are currently configured to migrate and independent of
// whether Commit has ever run. Read-only — usable any time before or after a
// commit.
func (p *Pipeline) Verify(scope model.Scope) (model.VerifyReport, error) {
	matches, err := p.Scan(scope)
	if err != nil {
		return model.VerifyReport{}, err
	}
	states, err := p.NavDB.ReadState(p.Cfg.UserID)
	if err != nil {
		return model.VerifyReport{}, err
	}

	rep := model.VerifyReport{Scope: scope}
	for _, m := range matches {
		if m.Status != model.Matched {
			continue
		}
		st, ok := states[m.Nav.ID]
		if !ok {
			continue
		}
		rep.Checked++
		row := p.verifyRow(m, st)
		if !row.RatingMatch || !row.PlayCountMatch || !row.LastPlayedMatch || (row.DateAddedChecked && !row.DateAddedMatch) {
			rep.Mismatched++
			rep.Rows = append(rep.Rows, row)
		}
	}
	return rep, nil
}

// verifyRow computes one track's expected-vs-actual comparison.
func (p *Pipeline) verifyRow(m model.Match, st nav.NavState) model.VerifyRow {
	row := model.VerifyRow{RelPath: m.Source.RelPath, NavID: m.Nav.ID}

	row.ExpectedRating = int(p.Cfg.MapRating(m.Source.RatingStep))
	row.ActualRating = st.Rating
	row.RatingMatch = row.ExpectedRating == row.ActualRating

	row.ExpectedPlayCount = m.Source.PlayCount
	row.ActualPlayCount = st.PlayCount
	row.PlayCountMatch = row.ExpectedPlayCount == row.ActualPlayCount

	row.ExpectedLastPlayed = nonZeroPtr(m.Source.LastPlayed)
	row.ActualLastPlayed = nonZeroPtr(st.PlayDate)
	row.LastPlayedMatch = sameWallClock(m.Source.LastPlayed, st.PlayDate)

	row.DateAddedChecked = !m.Source.DateAdded.IsZero()
	row.ExpectedDateAdded = nonZeroPtr(m.Source.DateAdded)
	row.ActualDateAdded = nonZeroPtr(st.CreatedAt)
	row.DateAddedMatch = sameWallClock(m.Source.DateAdded, st.CreatedAt)

	return row
}

// nonZeroPtr returns nil for the zero time, else a pointer to t — so JSON
// encodes "no value" as null rather than the zero time's "0001-01-01..." text.
func nonZeroPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// sameWallClock reports whether a and b carry the same year/month/day
// hour/minute/second, ignoring time zone and any sub-second remainder — the
// comparison that matches how nav.SetAnnotation/SetCreatedAt write MM's
// TDateTime-derived wall clock (see the comment there): a real UTC instant is
// never guaranteed, so only the literal digits are meaningful. Two zero times
// are considered equal ("never" == "never" / NULL == NULL).
func sameWallClock(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() == b.IsZero()
	}
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd && a.Hour() == b.Hour() && a.Minute() == b.Minute() && a.Second() == b.Second()
}

// Commit applies the changes for scope: ratings/stars via the Subsonic API,
// play counts/dates directly to navidrome.db. It re-derives changes via DryRun
// so a dry-run and its commit can never diverge.
func (p *Pipeline) Commit(scope model.Scope) (model.CommitResult, error) {
	rep, err := p.DryRun(scope)
	if err != nil {
		return model.CommitResult{}, err
	}
	total := len(rep.Changes)
	log.Printf("commit: applying %d change(s)", total)
	var res model.CommitResult
	for i, c := range rep.Changes {
		if err := p.apply(c); err != nil {
			res.Errors = append(res.Errors, model.CommitError{RelPath: c.RelPath, Err: err.Error()})
			log.Printf("commit: error on %q: %v", c.RelPath, err)
			continue
		}
		res.Applied++
		if n := i + 1; n%commitLogInterval == 0 || n == total {
			log.Printf("commit: %d/%d done (%d applied, %d errors)", n, total, res.Applied, len(res.Errors))
		}
	}
	log.Printf("commit: finished — %d applied, %d errors", res.Applied, len(res.Errors))
	return res, nil
}

// change builds the intended write for a matched track, limited to the
// configured fields.
func (p *Pipeline) change(m model.Match) model.Change {
	c := model.Change{RelPath: m.Source.RelPath, NavID: m.Nav.ID}
	f := p.Cfg.Fields
	rating := p.Cfg.MapRating(m.Source.RatingStep)
	if f.Has(model.FieldRating) {
		r := rating
		c.Rating = &r
	}
	if f.Has(model.FieldPlayCount) {
		pc := m.Source.PlayCount
		lp := m.Source.LastPlayed
		c.PlayCount, c.LastPlayed = &pc, &lp
	}
	if f.Has(model.FieldStarred) {
		threshold := p.Cfg.StarThreshold
		if threshold == 0 {
			threshold = model.DefaultStarThreshold
		}
		s := rating >= threshold
		c.Starred = &s
	}
	if f.Has(model.FieldDateAdded) && !m.Source.DateAdded.IsZero() {
		da := m.Source.DateAdded
		c.DateAdded = &da
	}
	return c
}

// apply routes one change to its sinks: rating/star to the API, play data to DB.
func (p *Pipeline) apply(c model.Change) error {
	if c.Rating != nil {
		if err := p.API.SetRating(c.NavID, *c.Rating); err != nil {
			return err
		}
	}
	if c.Starred != nil {
		if err := p.API.Star(c.NavID, *c.Starred); err != nil {
			return err
		}
	}
	if c.PlayCount != nil {
		a := nav.Annotation{NavID: c.NavID, PlayCount: *c.PlayCount}
		if c.LastPlayed != nil {
			a.LastPlayed = *c.LastPlayed
		}
		if err := p.Writer.SetAnnotation(p.Cfg.UserID, a); err != nil {
			return err
		}
	}
	if c.DateAdded != nil {
		if err := p.Writer.SetCreatedAt(c.NavID, *c.DateAdded); err != nil {
			return err
		}
	}
	return nil
}

// inScope reports whether relPath sits within scope. Both are normalized keys.
func inScope(scope model.Scope, relPath string) bool {
	if scope.Dir == "" {
		return true
	}
	d := strings.TrimSuffix(scope.Dir, "/")
	return relPath == d || strings.HasPrefix(relPath, d+"/")
}

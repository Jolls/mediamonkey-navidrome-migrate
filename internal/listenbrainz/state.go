package listenbrainz

import (
	"encoding/json"
	"os"
)

// SubmittedStore tracks which MM Played.IDPlayed rows have already been
// confirmed submitted to ListenBrainz, persisted as a sidecar JSON file next
// to MM5.DB (mirrors internal/nav.Backup's sibling-file convention, rather
// than writing anything back into MM5.DB itself).
type SubmittedStore struct {
	path string
	ids  map[int64]bool
}

// StorePath derives the sidecar path for a given MM5.DB path.
func StorePath(mmDBPath string) string {
	return mmDBPath + ".listenbrainz-submitted.json"
}

type submittedFile struct {
	Submitted []int64 `json:"submitted"`
}

// LoadStore reads the sidecar file if present. A missing file is not an
// error — it just means nothing has been submitted yet — and yields an
// empty store.
func LoadStore(path string) (*SubmittedStore, error) {
	s := &SubmittedStore{path: path, ids: make(map[int64]bool)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f submittedFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	for _, id := range f.Submitted {
		s.ids[id] = true
	}
	return s, nil
}

// Has reports whether id was already marked submitted.
func (s *SubmittedStore) Has(id int64) bool {
	return s.ids[id]
}

// MarkSubmitted adds ids and persists the store. Writes via a temp file +
// rename so a crash mid-write can't corrupt previously-saved state.
func (s *SubmittedStore) MarkSubmitted(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		s.ids[id] = true
	}

	all := make([]int64, 0, len(s.ids))
	for id := range s.ids {
		all = append(all, id)
	}
	data, err := json.Marshal(submittedFile{Submitted: all})
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

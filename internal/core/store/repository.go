package store

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
)

// Repository abstracts persisted state storage so Store logic does not depend on a specific backend format.
type Repository interface {
	Load() (PersistedState, bool, error)
	Save(state PersistedState) error
	Path() string
}

// JSONRepository persists state as a single JSON document on disk.
type JSONRepository struct {
	path string
}

// NewJSONRepository returns the default file-based repository implementation.
func NewJSONRepository(path string) *JSONRepository {
	return &JSONRepository{path: path}
}

// Path returns the storage path used by the repository.
func (r *JSONRepository) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Load loads state from disk into target and reports whether an existing file was found.
func (r *JSONRepository) Load() (PersistedState, bool, error) {
	if r == nil {
		return PersistedState{}, false, errors.New("state repository is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return PersistedState{}, false, err
	}

	payload, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return PersistedState{}, false, nil
	}
	if err != nil {
		return PersistedState{}, false, err
	}

	state := PersistedState{}
	if err := json.Unmarshal(payload, &state); err != nil {
		return PersistedState{}, false, err
	}
	return state, true, nil
}

// Save persists source to disk using a temporary file plus atomic replace.
func (r *JSONRepository) Save(state PersistedState) error {
	if r == nil {
		return errors.New("state repository is not configured")
	}

	// Nil slices stay JSON null so legacy state files round-trip the same way
	// encoding/json did. HTTP responses use the json/v2 default and emit [].
	payload, err := json.Marshal(
		state,
		jsontext.WithIndent("  "),
		json.FormatNilSliceAsNull(true),
		json.FormatNilMapAsNull(true),
	)
	if err != nil {
		return err
	}

	tempPath := r.path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, r.path)
}

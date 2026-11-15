package release

import (
	_ "embed"
	"encoding/json"
)

// Frontend release notes and the health endpoint use the same release metadata.
//
//go:embed notes.json
var notes []byte

var version = func() string {
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(notes, &metadata); err != nil || metadata.Version == "" {
		panic("invalid embedded release metadata")
	}
	return metadata.Version
}()

func Version() string { return version }

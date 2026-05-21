package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadLayers reads the repo and (optional) personal manifests from dir.
// The team layer (via extends:) is resolved by ResolveExtends in a later task;
// LoadLayers returns the directly-present layers only.
func LoadLayers(dir string) (map[Layer]*Manifest, error) {
	out := map[Layer]*Manifest{}
	repo, err := loadFile(filepath.Join(dir, "ai-stack.yaml"))
	if err != nil {
		return nil, err
	}
	out[LayerRepo] = repo

	personal, err := loadFile(filepath.Join(dir, "ai-stack.personal.yaml"))
	if err == nil {
		out[LayerPersonal] = personal
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// loadFile reads and minimally validates a manifest file. It returns the raw
// os error on read failure so callers can test it with os.IsNotExist.
func loadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d (want 1)", path, m.Version)
	}
	return &m, nil
}

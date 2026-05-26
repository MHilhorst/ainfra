package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/diag"
	"gopkg.in/yaml.v3"
)

// LoadLayers reads the repo and personal manifests from dir.
//
// The personal layer is the merge of two optional sources: the repo's
// ainfra.personal.yaml (more specific, gitignored), and the user's global
// personal manifest at $XDG_CONFIG_HOME/ainfra/personal.yaml (or
// ~/.config/ainfra/personal.yaml). The repo file wins per key; the global
// file provides cross-repo personal tooling that follows the developer.
//
// The team layer (via extends:) is resolved by ResolveExtends in a later task;
// LoadLayers returns the directly-present layers only.
func LoadLayers(dir string) (map[Layer]*Manifest, error) {
	out := map[Layer]*Manifest{}
	repo, err := loadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		return nil, err
	}
	out[LayerRepo] = repo

	repoPersonal, err := loadFile(filepath.Join(dir, "ainfra.personal.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err != nil {
		repoPersonal = nil
	}
	globalPersonal, err := loadGlobalPersonal()
	if err != nil {
		return nil, err
	}
	if merged := mergePersonal(repoPersonal, globalPersonal); merged != nil {
		out[LayerPersonal] = merged
	}
	return out, nil
}

// loadGlobalPersonal reads the user's cross-repo personal manifest from
// $XDG_CONFIG_HOME/ainfra/personal.yaml (or ~/.config/ainfra/personal.yaml if
// XDG_CONFIG_HOME is unset). A missing file is not an error.
func loadGlobalPersonal() (*Manifest, error) {
	path := globalPersonalPath()
	if path == "" {
		return nil, nil
	}
	m, err := loadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// globalPersonalPath returns the resolved path of the global personal file,
// or "" when no home/XDG dir can be determined.
func globalPersonalPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "ainfra", "personal.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "ainfra", "personal.yaml")
}

// loadFile reads and minimally validates a manifest file. It returns the raw
// os error on read failure so callers can test it with os.IsNotExist; a parse
// or version problem comes back as a *diag.Diagnostic.
//
// Decoding is strict: an unknown or misspelled key is a hard error, never a
// silent drop. A config-as-code tool that quietly ignores a typo cannot honour
// its core promise — that the manifest is the source of truth (design §13).
func loadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, &diag.Diagnostic{
			Summary: "manifest could not be parsed",
			File:    filepath.Base(path),
			Detail:  yamlErrorDetail(err),
			Hint:    "Check for a misspelled or misplaced key — ainfra rejects unknown fields, so a typo is reported instead of silently ignored.",
		}
	}
	if m.Version != 1 {
		return nil, &diag.Diagnostic{
			Summary: fmt.Sprintf("unsupported manifest version %d", m.Version),
			File:    filepath.Base(path),
			Detail:  "ainfra understands version 1 manifests only.",
			Hint:    "Set  version: 1  at the top of the file.",
		}
	}
	return &m, nil
}

// yamlErrorDetail renders a yaml decode error as readable detail text. A
// strict-decoding failure arrives as a *yaml.TypeError carrying one line per
// offending field; anything else (a syntax error) is reported verbatim.
func yamlErrorDetail(err error) string {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		return strings.Join(te.Errors, "\n")
	}
	return err.Error()
}

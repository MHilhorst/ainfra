package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MHilhorst/ainfra/internal/diag"
	"github.com/MHilhorst/ainfra/internal/version"
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
	repoPath := filepath.Join(dir, "ainfra.yaml")
	repo, err := loadFile(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			// User-scope mode: no repo manifest, only the global personal
			// layer applies. Lets `ainfra install` work in any directory.
			repo = nil
		} else {
			return nil, err
		}
	}
	if repo != nil {
		out[LayerRepo] = repo
	}

	repoPersonal, err := loadFile(filepath.Join(dir, "ainfra.personal.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err != nil {
		repoPersonal = nil
	}
	globalPersonal, err := LoadGlobalPersonal()
	if err != nil {
		return nil, err
	}
	if merged := mergePersonal(repoPersonal, globalPersonal); merged != nil {
		out[LayerPersonal] = merged
	}
	return out, nil
}

// StalenessHookID is the synthetic id under which `ainfra install`
// auto-emits the built-in SessionStart staleness hook. The `__ainfra_`
// prefix keeps it out of any user-defined hook namespace.
const StalenessHookID = "__ainfra_staleness"

// StalenessHookCommand is the shell command Claude Code runs at SessionStart.
const StalenessHookCommand = "ainfra _staleness-check"

// LoadGlobalPersonal reads the user's cross-repo personal manifest from
// $XDG_CONFIG_HOME/ainfra/personal.yaml (or ~/.config/ainfra/personal.yaml if
// XDG_CONFIG_HOME is unset). A missing file is not an error.
func LoadGlobalPersonal() (*Manifest, error) {
	path := GlobalPersonalPath()
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

// GlobalPersonalPath returns the resolved path of the global personal file,
// or "" when no home/XDG dir can be determined.
func GlobalPersonalPath() string {
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
		// Strict decoding cannot tell a typo from a field a NEWER ainfra
		// added, and it runs before anything reads `ainfraVersion:` — so a
		// repo that adopts a new field reports "field X not found" to every
		// teammate on an older binary and never reaches the version warning
		// written for exactly this case. Check the pin here, on the error
		// path only, and say what to actually do.
		if d := outdatedBinaryDiagnostic(data, path, err); d != nil {
			return nil, d
		}
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

// outdatedBinaryDiagnostic reports the real cause when a manifest failed
// strict decoding AND pins an ainfra newer than this build: the field is not a
// typo, the binary is old. Returns nil when the pin is absent, not newer, or
// either version is not a plain MAJOR.MINOR.PATCH — in every one of those the
// caller's parse error is the honest answer and guessing would bury a real typo
// under an upgrade prompt.
//
// The re-parse is deliberately NOT strict. The document just failed strict
// decoding; reading the pin out of it is only possible by ignoring the very
// fields that failed.
func outdatedBinaryDiagnostic(data []byte, path string, cause error) *diag.Diagnostic {
	var probe struct {
		AinfraVersion string `yaml:"ainfraVersion"`
	}
	if yaml.Unmarshal(data, &probe) != nil || probe.AinfraVersion == "" {
		return nil
	}
	want, ok := parseSemver(probe.AinfraVersion)
	if !ok {
		return nil
	}
	have, ok := parseSemver(version.Version)
	if !ok {
		// A dev or otherwise unversioned build. Claiming it is out of date
		// would send a developer to Homebrew to "fix" their own working tree.
		return nil
	}
	if !less(have, want) {
		return nil
	}
	return &diag.Diagnostic{
		Summary: fmt.Sprintf("this repo needs ainfra %s; you are running %s", probe.AinfraVersion, version.Version),
		File:    filepath.Base(path),
		Detail: "The manifest uses a field this build does not know:\n" + yamlErrorDetail(cause) +
			"\n\nainfra rejects unknown fields, so a manifest written for a newer\nainfra fails to parse rather than silently ignoring what it cannot do.",
		Hint: "Upgrade, then re-run:  brew upgrade --cask ainfra",
	}
}

// parseSemver splits a plain MAJOR.MINOR.PATCH string. Anything else — a
// pre-release suffix, a "v" prefix, an empty string — reports false rather
// than a best guess, because every caller uses the result to decide whether to
// tell someone their binary is wrong.
func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// less reports whether a orders before b.
func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
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

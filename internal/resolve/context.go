package resolve

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// DefaultIdentity is the identity assumed when neither --identity nor
// AINFRA_IDENTITY is set. Human callers do not have to declare anything; only
// non-human callers (CI runners, agent identities) need to opt in.
const DefaultIdentity = "human"

// ResolutionContext carries the per-invocation inputs the resolve pipeline
// uses to gate entries. It is intentionally small: identity for who-am-I
// gates, invocation path for where-am-I gates. Agent gating lives in
// manifest.ValidateAll, which has its own resolution.
type ResolutionContext struct {
	// Identity is the caller identity an entry's scope.identities matches
	// against. The empty string is treated as DefaultIdentity.
	Identity string
	// InvocationPath is the cwd of the ainfra invocation, expressed as a
	// repo-relative slash-path. "." when run at the repo root. Used to match
	// against entry scope.paths globs (path.Match syntax).
	InvocationPath string
}

// NewContextFromEnv builds a ResolutionContext from a flag value (which wins
// if non-empty), then the AINFRA_IDENTITY env var, then the default. The
// invocation path is dir relative to repoRoot, slash-formatted, "." at the
// root.
func NewContextFromEnv(identityFlag, dir, repoRoot string) ResolutionContext {
	id := strings.TrimSpace(identityFlag)
	if id == "" {
		id = strings.TrimSpace(os.Getenv("AINFRA_IDENTITY"))
	}
	if id == "" {
		id = DefaultIdentity
	}
	return ResolutionContext{
		Identity:       id,
		InvocationPath: relPath(dir, repoRoot),
	}
}

// DefaultContext returns the context applied when no caller supplies one:
// human identity, "." path. Tests and the back-compat RenderResources entry
// point use it so behaviour is unchanged when selectors are absent.
func DefaultContext() ResolutionContext {
	return ResolutionContext{Identity: DefaultIdentity, InvocationPath: "."}
}

// relPath returns dir relative to repoRoot, slash-formatted. Falls back to
// "." when either is empty or when filepath.Rel fails.
func relPath(dir, repoRoot string) string {
	if dir == "" || repoRoot == "" {
		return "."
	}
	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		return "."
	}
	rel = filepath.ToSlash(rel)
	if rel == "" {
		return "."
	}
	return rel
}

// SelectorMatches reports whether s admits ctx. A nil or fully-empty selector
// matches everything (the historical default). Each non-empty axis must
// match: identities is a literal membership test; paths is OR of
// path.Match globs against ctx.InvocationPath.
func SelectorMatches(s *manifest.Selector, ctx ResolutionContext) bool {
	if s == nil {
		return true
	}
	if len(s.Identities) > 0 {
		if !contains(s.Identities, ctx.Identity) {
			return false
		}
	}
	if len(s.Paths) > 0 {
		if !pathMatchesAny(s.Paths, ctx.InvocationPath) {
			return false
		}
	}
	return true
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// pathMatchesAny returns true when invocationPath matches any glob, or when
// any glob is a literal path-prefix of invocationPath (so "services/api"
// matches "services/api" and "services/api/v2" alike, matching the intuition
// "this entry applies inside services/api"). path.Match's lack of ** is the
// reason we add the prefix shortcut.
func pathMatchesAny(globs []string, invocationPath string) bool {
	for _, g := range globs {
		g = filepath.ToSlash(strings.TrimSpace(g))
		if g == "" {
			continue
		}
		if g == invocationPath {
			return true
		}
		// Prefix shortcut: "services/api" matches "services/api/anything".
		if strings.HasPrefix(invocationPath, g+"/") {
			return true
		}
		// Glob fall-through. Matches one segment by default; "**" is not
		// honoured (documented limitation).
		if ok, _ := filepath.Match(g, invocationPath); ok {
			return true
		}
	}
	return false
}

package adopt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// versionInArgRe spots a "pkg@1.2.3" suffix in an npx/uvx package launch arg.
var versionInArgRe = regexp.MustCompile(`@(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.\-]+)?)$`)

// distTagInArgRe spots a "pkg@<non-numeric>" suffix (e.g. @latest, @next,
// @beta) in an npx/uvx package launch arg — these are dist tags that don't
// satisfy ainfra's "package launchers must pin an exact version" rule.
// Capture the package name (without scope leading slash) and the tag so we
// can resolve the tag to a concrete version via the registry.
var distTagInArgRe = regexp.MustCompile(`^(@?[A-Za-z0-9._\-/]+)@([A-Za-z][A-Za-z0-9.\-]*)$`)

// readMCP scans the given .mcp.json file and emits MCP server entries plus any
// stripped secret declarations. A missing file is not an error. Older Claude
// Code conventions stored MCP config under a top-level "servers" key instead
// of "mcpServers"; both are accepted so 'init --adopt' captures repos using
// either layout.
func readMCP(path string) (mcpServers map[string]manifest.MCPServer, secrets map[string]manifest.Secret, warnings []Warning, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("adopt: read %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("adopt: parse %s: %w", path, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if len(servers) == 0 {
		servers, _ = doc["servers"].(map[string]any)
	}
	if len(servers) == 0 {
		return nil, nil, nil, nil
	}

	mcpServers = map[string]manifest.MCPServer{}
	secrets = map[string]manifest.Secret{}

	ids := make([]string, 0, len(servers))
	for id := range servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entry, _ := servers[id].(map[string]any)
		if entry == nil {
			continue
		}
		srv := manifest.MCPServer{}

		if v, ok := entry["type"].(string); ok && v != "" {
			srv.Transport = v
		} else if v, ok := entry["transport"].(string); ok && v != "" {
			srv.Transport = v
		}
		if srv.Transport == "" {
			srv.Transport = "stdio"
		}

		if v, ok := entry["command"].(string); ok {
			srv.Command = v
		}
		if v, ok := entry["url"].(string); ok {
			srv.URL = v
		}
		if args, ok := entry["args"].([]any); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					srv.Args = append(srv.Args, s)
				}
			}
		}
		if srv.Command != "" {
			if pinned := inferVersion(srv.Command, srv.Args); pinned != "" {
				srv.Version = pinned
			}
			// If no concrete version was inferred but an arg uses a dist
			// tag like @latest, ask the registry for the current version.
			// On success, pin it and strip the tag from the arg so we
			// don't end up with "pkg@latest" in args + a separate version
			// field.
			if srv.Version == "" {
				if pkg, idx, ok := findDistTaggedArg(srv.Command, srv.Args); ok {
					if v, w := resolveNpmTagVersion(pkg, srv.Args[idx]); v != "" {
						srv.Version = v
						srv.Args[idx] = pkg
					} else if w != "" {
						warnings = append(warnings, Warning{Message: w})
					}
				}
			}
		}

		if env, ok := entry["env"].(map[string]any); ok {
			out, ws := stripStringMap(env, "mcpServers."+id+".env", secrets)
			if len(out) > 0 {
				srv.Env = out
			}
			warnings = append(warnings, ws...)
		}
		if headers, ok := entry["headers"].(map[string]any); ok {
			out, ws := stripStringMap(headers, "mcpServers."+id+".headers", secrets)
			if len(out) > 0 {
				srv.Headers = out
			}
			warnings = append(warnings, ws...)
		}

		// http transport: drop stdio-only fields the manifest validator rejects.
		if srv.Transport == "http" {
			srv.Command = ""
			srv.Args = nil
			srv.Version = ""
		}

		mcpServers[id] = srv
	}
	return mcpServers, secrets, warnings, nil
}

// findDistTaggedArg locates an arg of the form "pkg@latest" (or any non-
// numeric tag) for an npx-family launcher, returning the bare package name
// and the index of the matching arg. ainfra needs an exact version, not a
// dist tag, so this hands the caller the data needed to resolve the tag
// against the registry.
func findDistTaggedArg(command string, args []string) (pkg string, idx int, ok bool) {
	switch command {
	case "npx", "bunx":
	default:
		return "", 0, false
	}
	for i, a := range args {
		m := distTagInArgRe.FindStringSubmatch(a)
		if m == nil {
			continue
		}
		// Skip already-pinned literals (the numeric regex would have
		// matched first in the caller, but be defensive in case the tag
		// looks numeric for some edge case).
		if versionInArgRe.MatchString(a) {
			continue
		}
		return m[1], i, true
	}
	return "", 0, false
}

// resolveNpmTagVersion runs `npm view <pkg> version` to turn a dist tag
// into a concrete version. Returns the resolved version on success, or a
// warning string when the lookup failed so adopt can surface why a server
// is still unpinned. The lookup is deliberately bounded by a short
// timeout — adopt is supposed to be fast, and a missing pin is recoverable
// (the user can edit ainfra.yaml).
func resolveNpmTagVersion(pkg, originalArg string) (version, warning string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "view", pkg, "version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Sprintf("adopt: could not resolve %q via `npm view` (%v) — add a version: field to mcpServers entry manually", originalArg, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Sprintf("adopt: `npm view %s version` returned empty — add a version: field manually", pkg)
	}
	return v, ""
}

// inferVersion extracts a pinned version from launcher args. For npx/uvx-style
// launchers, a "pkg@1.2.3" suffix in any arg counts as a pin.
func inferVersion(command string, args []string) string {
	switch command {
	case "npx", "uvx", "pipx", "bunx":
	default:
		return ""
	}
	for _, a := range args {
		if m := versionInArgRe.FindStringSubmatch(a); m != nil {
			return m[1]
		}
	}
	return ""
}

// stripStringMap copies a JSON env/header map into a string-keyed map suitable
// for the manifest, replacing any literal credential with an ainfra secret
// reference (and adding the secret declaration to secrets).
func stripStringMap(in map[string]any, pathPrefix string, secrets map[string]manifest.Secret) (map[string]string, []Warning) {
	out := map[string]string{}
	var warnings []Warning
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := in[k].(string)
		if !ok {
			continue
		}
		res := inspectValue(k, v)
		if !res.Stripped {
			out[k] = v
			continue
		}
		secrets[res.SecretName] = manifest.Secret{
			Mode:  "direct",
			Scope: "personal",
			Ref:   secretRefTODO(res.SecretName),
		}
		// Replace literal with an ainfra ${secrets.<name>} reference so the
		// emitted manifest never contains the original credential.
		out[k] = "${secrets." + res.SecretName + "}"
		warnings = append(warnings, Warning{
			Kind:    WarnStripped,
			Target:  "secrets." + res.SecretName,
			Origin:  pathPrefix + "." + k,
			Message: "stripped literal credential",
		})
	}
	return out, warnings
}

// secretRefTODO is the placeholder ref written for every newly-synthesized
// secret. The user is expected to replace it with a real vault path.
func secretRefTODO(name string) string {
	return "# TODO: set vault reference (op://Private/" + sanitize(name) + "/value)"
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "secret"
	}
	return s
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/secret"
)

// secretSchemesUsed returns the distinct ref schemes (e.g. "op", "env") every
// secret in the resolved locks and the manifest's envFile/path secrets resolves
// through. It is the input to the backend preflight: which credential backends
// must be ready for this install to materialize secrets.
func secretSchemesUsed(dir string, committed, personal *lockfile.Lock) []string {
	return secretSchemesUsedFor(dir, committed, personal, resolve.DefaultContext())
}

// secretSchemesUsedFor is secretSchemesUsed gated on identity. Without the
// gate, a box whose only 1Password-backed secret is out of scope still
// preflights the 1Password backend and fails readiness for a credential it was
// never going to resolve.
func secretSchemesUsedFor(dir string, committed, personal *lockfile.Lock, ctx resolve.ResolutionContext) []string {
	set := map[string]bool{}
	for _, l := range []*lockfile.Lock{committed, personal} {
		if l == nil {
			continue
		}
		for _, sr := range l.Secrets {
			if sr.Scheme != "" && lockRefAppliesTo(sr, ctx) {
				set[sr.Scheme] = true
			}
		}
	}
	// envFile/path secrets live in the manifest, not the lockfile.
	if layers, err := manifest.LoadLayers(dir); err == nil {
		for _, m := range layers {
			if m == nil {
				continue
			}
			for _, sec := range m.Secrets {
				if !sec.EnvFile && sec.Path == "" {
					continue
				}
				if !secretAppliesTo(sec, ctx) {
					continue
				}
				if scheme, err := secret.SchemeOf(expandUser(sec.Ref)); err == nil {
					set[scheme] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// preflightSecretBackends verifies every credential backend a secret resolves
// through is ready (e.g. the 1Password CLI is installed and signed in) before
// apply writes any config, so an unusable backend fails fast instead of leaving
// a half-configured repo. Backends with no readiness probe are skipped.
func preflightSecretBackends(dir string, reg *secret.Registry, committed, personal *lockfile.Lock, ctx resolve.ResolutionContext) []string {
	var failures []string
	for _, scheme := range secretSchemesUsedFor(dir, committed, personal, ctx) {
		if err := reg.CheckBackend(scheme); err != nil {
			failures = append(failures, err.Error())
		}
	}
	return failures
}

// syncResult reports what syncSecrets materialized.
type syncResult struct {
	EnvCount     int      // environment variables written to the settings file
	SettingsPath string   // the settings file written
	ShimPath     string   // launcher shim that injects secrets at startup, "" when no secrets
	ShimNote     string   // why the shim was baked against the dir it was, "" when unremarkable
	Files        []string // credential files written, by path
}

// resolveSecretEnv resolves every environment-variable secret referenced by
// the locks (single-value secrets) and the manifest at dir (envFile blobs).
// It is shared by `ainfra install`, which persists the result, and
// `ainfra exec`, which injects it into a child process at launch.
// Credential-file (path:) secrets are install-time artifacts and are not
// resolved here. Resolution failures are returned as messages, not an error —
// the callers decide whether a failure is fatal (install) or a warning (exec).
// It resolves for the default identity; callers that know who they are should
// use resolveSecretEnvFor.
func resolveSecretEnv(dir string, reg *secret.Registry, committed, personal *lockfile.Lock) (map[string]string, []string) {
	return resolveSecretEnvFor(dir, reg, committed, personal, resolve.DefaultContext())
}

// secretAppliesTo reports whether a secret should be resolved for this caller.
//
// Two gates, both about relevance and neither about errors:
//
//  1. identities: [...] — explicit, matching the scope.identities axis every
//     other channel has. Empty means everyone.
//  2. scope: personal — implicit. A personal secret lives in a per-human vault
//     (op://Private/...), which by construction no service account can read.
//     A non-human identity attempting it is not a misconfiguration to report,
//     it is a category error: that credential was never addressed to it.
//
// The second gate is what makes this fix deployable. Gating only on an explicit
// key would require every manifest to add it, and an older ainfra rejects
// unknown keys outright (strict decoding), so the manifest edit could not land
// before every machine had upgraded. Deriving the common case from `scope`,
// which manifests already declare, means a box stops warning the moment it gets
// this binary, with no manifest change and no version-skew window.
func secretAppliesTo(sec manifest.Secret, ctx resolve.ResolutionContext) bool {
	// Match the selector against the NORMALIZED identity, not the raw field.
	//
	// resolve.SelectorMatches does a literal contains() on ctx.Identity and knows
	// nothing about ctx.Agent. Passing ctx unmodified meant the two gates in this
	// function read identity two different ways: `install --agent codex` leaves
	// Identity as the default "human", so a secret declared `identities: [codex]`
	// -- the exact mechanism this exists to provide -- failed the selector and was
	// silently skipped for the one invocation it was written for.
	ictx := ctx
	ictx.Identity = effectiveIdentity(ctx)
	if !resolve.SelectorMatches(&manifest.Selector{Identities: sec.Identities}, ictx) {
		return false
	}
	// An explicit identities list WINS over the implicit rule. Otherwise
	// `identities: [agent]` on a personal-scope secret would match the selector
	// and then be dropped anyway, making a legitimate intent impossible to
	// express and silently denying a caller a credential it was named for --
	// which is a worse failure than the noise this gate removes.
	if len(sec.Identities) > 0 {
		return true
	}
	return !personalAndNotHuman(sec.Scope, ctx)
}

// personalAndNotHuman is the implicit rule shared by manifest secrets and
// lockfile-backed refs: a per-human vault is not addressed to a service account.
//
// It deliberately reads the RAW caller identity and ignores ctx.Agent, which is
// the opposite of what the selector does. The two questions are different:
//
//	selector  -- "which identity is this secret written for?"  --agent counts,
//	             because scoping a secret to `identities: [codex]` is exactly
//	             how you say "this one is for the Codex install".
//	this rule -- "can this caller reach a per-human vault?"     --agent does NOT
//	             count, because `install --agent codex` on a laptop is the same
//	             human, the same machine and the same 1Password session, just
//	             writing config for a different tool. Treating it as a different
//	             principal would silently drop that human's own personal secrets
//	             from their Codex install -- the failure this change exists to
//	             prevent, reintroduced one flag away.
//
// An agent identity is a property of the CALLER (AINFRA_IDENTITY on a headless
// box), not of the output format being written.
func personalAndNotHuman(scope string, ctx resolve.ResolutionContext) bool {
	if scope != "personal" {
		return false
	}
	identity := ctx.Identity
	if identity == "" {
		identity = resolve.DefaultIdentity
	}
	return identity != resolve.DefaultIdentity
}

// effectiveIdentity is the identity a scope.identities selector is matched
// against: an explicit identity wins, an --agent override stands in for one,
// otherwise the default. It mirrors resolve.RenderResourcesAndLocksFor, so a
// secret and the resources it feeds are gated by the same rule.
func effectiveIdentity(ctx resolve.ResolutionContext) string {
	if ctx.Identity != "" && ctx.Identity != resolve.DefaultIdentity {
		return ctx.Identity
	}
	if ctx.Agent != "" {
		return ctx.Agent
	}
	if ctx.Identity == "" {
		return resolve.DefaultIdentity
	}
	return ctx.Identity
}

// lockRefAppliesTo is secretAppliesTo for lockfile-backed refs. The lockfile
// carries scope but no identities list, so only the implicit rule applies.
//
// Gating this path matters: MCP env and header bindings resolve through the
// lockfile, so without it a service account still attempts a personal vault and
// the warning this change exists to remove survives by another route.
func lockRefAppliesTo(sr lockfile.SecretRef, ctx resolve.ResolutionContext) bool {
	return !personalAndNotHuman(sr.Scope, ctx)
}

// resolveSecretEnvFor is resolveSecretEnv gated on caller identity, per
// secretAppliesTo.
//
// Skipping is silent BY DESIGN, and that is the whole point: a secret the
// caller was never meant to hold is not a failure, and reporting it as one is
// what produced a warning on every healthy run of a headless box -- which is
// how a warning that matters stops being read. An in-scope secret that cannot
// resolve is still reported. Relevance is filtered here, never errors.
func resolveSecretEnvFor(dir string, reg *secret.Registry, committed, personal *lockfile.Lock, ctx resolve.ResolutionContext) (map[string]string, []string) {
	// The single-value secret set is the union of both locks.
	refs := map[string]lockfile.SecretRef{}
	for _, l := range []*lockfile.Lock{committed, personal} {
		if l != nil {
			maps.Copy(refs, l.Secrets)
		}
	}

	resolved := map[string]string{}
	var failures []string
	for _, v := range slices.Sorted(maps.Keys(refs)) {
		sr := refs[v]
		if !lockRefAppliesTo(sr, ctx) {
			continue
		}
		val, err := reg.Resolve(expandUser(sr.Ref))
		if err != nil {
			failures = append(failures, "  "+err.Error())
			continue
		}
		resolved[sr.Var] = val
	}

	// envFile secrets expand one ref into many env vars; they are declared in
	// the manifest, not the lockfile.
	if layers, lerr := manifest.LoadLayers(dir); lerr == nil {
		for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
			m := layers[ln]
			if m == nil {
				continue
			}
			for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
				sec := m.Secrets[id]
				if !sec.EnvFile {
					continue
				}
				if !secretAppliesTo(sec, ctx) {
					continue
				}
				blob, rerr := reg.Resolve(expandUser(sec.Ref))
				if rerr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, rerr))
					continue
				}
				maps.Copy(resolved, parseEnvBlob(blob))
			}
		}
	}
	return resolved, failures
}

// syncSecrets resolves every secret referenced by the resolved locks and the
// manifest at dir and materializes it. It runs as the final step of
// `ainfra install`, which passes the in-memory resolved locks so a secret
// added to ainfra.yaml syncs even while the committed lockfile is stale.
//
// A secret is materialized by its destination:
//   - a single-value secret (lock)     -> one env var in the settings file
//   - envFile: true                    -> a .env blob expanded into many env vars
//   - path: <file>                     -> the resolved value written to that file
//
// Process-environment delivery (what ${VAR} expansion in HTTP MCP server
// headers needs) is not persisted at all: the launcher shim written here
// re-resolves secrets through `ainfra exec` at every launch.
func syncSecrets(dir string, reg *secret.Registry, committed, personal *lockfile.Lock, ctx resolve.ResolutionContext) (syncResult, error) {
	resolved, failures := resolveSecretEnvFor(dir, reg, committed, personal, ctx)

	// path secrets write the resolved value to a credential file.
	fileSet := map[string]bool{}
	if layers, lerr := manifest.LoadLayers(dir); lerr == nil {
		for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
			m := layers[ln]
			if m == nil {
				continue
			}
			for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
				sec := m.Secrets[id]
				if sec.Path == "" || sec.EnvFile {
					continue
				}
				if !secretAppliesTo(sec, ctx) {
					continue
				}
				blob, rerr := reg.Resolve(expandUser(sec.Ref))
				if rerr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, rerr))
					continue
				}
				dest := expandTilde(sec.Path)
				if werr := writeCredentialFile(dest, blob); werr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, werr))
					continue
				}
				fileSet[dest] = true
			}
		}
	}

	if len(failures) > 0 {
		return syncResult{}, fmt.Errorf("could not resolve secrets:\n%s", strings.Join(failures, "\n"))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return syncResult{}, err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
	if err := writeSettingsEnv(settingsPath, resolved); err != nil {
		return syncResult{}, err
	}

	// The settings env block only reaches stdio MCP servers. Claude Code
	// expands ${VAR} in HTTP server headers from the real process environment,
	// so a launcher shim re-execs claude through `ainfra exec`, which resolves
	// secrets fresh into the process env at every launch. Skipped entirely
	// when the manifest declares no secrets — a secretless install must not
	// touch the user's shell config.
	shimPath, shimNote := "", ""
	if len(resolved) > 0 {
		shimPath, shimNote, err = writeLauncherShim(home, dir)
		if err != nil {
			return syncResult{}, err
		}
		if err := ensurePathLine(filepath.Join(home, ".zshenv"), filepath.Dir(shimPath)); err != nil {
			return syncResult{}, err
		}
	}
	// The pre-shim mechanism exported credential values into every shell via
	// an env.sh sourced from ~/.zshenv. Remove it on every install so upgraded
	// machines stop carrying secrets in shell config.
	if err := cleanupLegacyShellEnv(home); err != nil {
		return syncResult{}, err
	}

	return syncResult{
		EnvCount:     len(resolved),
		SettingsPath: settingsPath,
		ShimPath:     shimPath,
		ShimNote:     shimNote,
		Files:        slices.Sorted(maps.Keys(fileSet)),
	}, nil
}

// writeLauncherShim writes an executable `claude` wrapper that re-execs the
// real binary through `ainfra exec`, so every launch gets freshly-resolved
// secrets in its process environment. The manifest dir is baked in at install
// time because the shim runs from any working directory. The shim holds no
// secret values, so 0755 is fine.
//
// The baked path is first redirected out of any linked git worktree
// (durableManifestDir). There is one shim per machine, so installing from a
// throwaway worktree would otherwise pin every future claude launch — from
// every directory — to a path that disappears when that worktree is removed,
// silently dropping secret injection until the next install. The returned note
// is non-empty when the user needs to know which dir was baked and why; the
// caller is responsible for surfacing it.
//
// A second shim, `claude-app`, targets the real claude binary by absolute
// path (resolved at install time). It exists for GUI hosts that launch claude
// by a configured path instead of PATH lookup — e.g. cmux's "Claude Binary
// Path" setting — where the name-based shim either never runs or would
// resolve the host's own wrapper. Skipped when no native binary is found.
func writeLauncherShim(home, manifestDir string) (string, string, error) {
	binDir := filepath.Join(home, ".config", "ainfra", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", err
	}
	manifestDir, note := durableManifestDir(manifestDir)
	// The ainfra binary is referenced absolutely: GUI-spawned processes (the
	// exact audience of these shims) get a minimal PATH without /opt/homebrew
	// /bin, so a bare `ainfra` fails with "not found" there. LookPath gives
	// the stable brew symlink; the running executable is the fallback.
	ainfraBin := "ainfra"
	if p, err := exec.LookPath("ainfra"); err == nil {
		ainfraBin = p
	} else if p, err := os.Executable(); err == nil {
		ainfraBin = p
	}
	shim := filepath.Join(binDir, "claude")
	content := fmt.Sprintf(`#!/bin/sh
# Generated by ainfra. Do not edit by hand.
# Resolves managed secrets into the process environment, then launches claude.
exec %q --chdir %q exec -- claude "$@"
`, ainfraBin, manifestDir)
	if err := os.WriteFile(shim, []byte(content), 0o755); err != nil {
		return "", "", err
	}
	if real := findRealClaude(binDir); real != "" {
		app := filepath.Join(binDir, "claude-app")
		appContent := fmt.Sprintf(`#!/bin/sh
# Generated by ainfra. Do not edit by hand.
# Absolute-target variant of the claude shim, for GUI hosts (e.g. cmux's
# Claude Binary Path setting) that must not re-resolve claude via PATH.
exec %q --chdir %q exec -- %q "$@"
`, ainfraBin, manifestDir, real)
		if err := os.WriteFile(app, []byte(appContent), 0o755); err != nil {
			return "", "", err
		}
		if err := os.Chmod(app, 0o755); err != nil {
			return "", "", err
		}
	}
	// WriteFile's mode only applies on creation; re-assert on updates.
	return shim, note, os.Chmod(shim, 0o755)
}

// manifestInputs are every file read from the baked dir when secrets are
// resolved from it. Redirecting the shim is only safe when all of them are
// byte-identical between the two dirs.
//
// The list must stay in step with what resolution actually opens, or the
// redirect starts guessing again: ainfra.lock is what single-value secrets
// resolve from, ainfra.personal.lock is per-checkout, and
// ainfra.personal.yaml carries envFile/path secrets that are declared in the
// manifest and never appear in any lock (manifest.LoadLayers). Comparing
// ainfra.yaml alone would miss all three.
var manifestInputs = []string{"ainfra.yaml", "ainfra.lock", "ainfra.personal.yaml", "ainfra.personal.lock"}

// durableManifestDir maps a manifest dir that lives in a linked git worktree
// onto the repo's main worktree, which outlives it. It returns the dir to bake
// and a note for the user, empty when nothing worth reporting happened.
//
// Worktrees are per-task and get deleted; the shim baked from one is
// per-machine and permanent, so a worktree path silently stops injecting
// secrets the day that worktree is removed.
//
// The redirect is only safe when it changes nothing about what exec will
// resolve, so it requires every file in manifestInputs to be byte-identical
// across the two dirs. A linked worktree usually sits on its own branch and
// may add, drop, or retarget secrets; baking main's path then would inject a
// different branch's credentials at every future launch. Divergence therefore
// keeps the worktree path and says so — the caller surfaces the note, because
// that path is the known-ephemeral one and the user needs to re-run install
// from the main checkout to get a durable shim.
//
// Everything git cannot vouch for falls through unchanged: no git on PATH,
// not a repo, a bare repo, or a main worktree with no manifest at all.
func durableManifestDir(manifestDir string) (string, string) {
	gitDir, err := gitRevParse(manifestDir, "--git-dir")
	if err != nil {
		return manifestDir, ""
	}
	commonDir, err := gitRevParse(manifestDir, "--git-common-dir")
	if err != nil {
		return manifestDir, ""
	}
	// Equal paths mean the main worktree — nothing to redirect. They differ
	// only inside a linked worktree, where --git-dir is
	// <common>/worktrees/<name>.
	if gitDir == commonDir {
		return manifestDir, ""
	}
	mainWorktree := filepath.Dir(commonDir)
	if !fileExists(filepath.Join(mainWorktree, "ainfra.yaml")) && !fileExists(filepath.Join(mainWorktree, "ainfra.lock")) {
		return manifestDir, ""
	}
	if differing, ok := firstDifferingInput(manifestDir, mainWorktree); !ok {
		return manifestDir, fmt.Sprintf(
			"%s and %s differ — the launcher shim stays pinned to this worktree, and stops injecting secrets when it is removed.\nRun `ainfra install` from %s once this work is merged.",
			filepath.Join(manifestDir, differing), filepath.Join(mainWorktree, differing), mainWorktree)
	}
	return mainWorktree, ""
}

// firstDifferingInput compares every manifestInputs file across two dirs. It
// returns ok=true when all match, otherwise the name of the first that does
// not. A file missing from both sides counts as matching; missing from one
// only does not.
func firstDifferingInput(a, b string) (string, bool) {
	for _, name := range manifestInputs {
		// An unreadable file is treated as differing, not as absent: failing
		// closed here costs a redirect, failing open could bake a path whose
		// secrets were never actually compared.
		aBytes, aErr := os.ReadFile(filepath.Join(a, name))
		bBytes, bErr := os.ReadFile(filepath.Join(b, name))
		if os.IsNotExist(aErr) && os.IsNotExist(bErr) {
			continue
		}
		if aErr != nil || bErr != nil || !bytes.Equal(aBytes, bBytes) {
			return name, false
		}
	}
	return "", true
}

// gitRevParse returns one absolute rev-parse path for dir. --path-format keeps
// the answer absolute; git otherwise reports --git-dir as a bare ".git"
// relative to dir, which would make the caller's comparison meaningless.
func gitRevParse(dir, flag string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", flag)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("git rev-parse %s: empty result for %s", flag, dir)
	}
	return filepath.Clean(path), nil
}

// findRealClaude returns the absolute path of the first claude on PATH that
// is neither in the shim dir nor a wrapper script (anything starting with
// "#!") — third-party wrappers like cmux's must not be baked into claude-app,
// or the shim and the wrapper would launch each other forever. Empty when no
// native binary is found. A symlink (the standard ~/.local/bin/claude) is
// returned as the symlink path, so self-updates that repoint it keep working.
func findRealClaude(shimDir string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || dir == shimDir {
			continue
		}
		cand := filepath.Join(dir, "claude")
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		f, err := os.Open(cand)
		if err != nil {
			continue
		}
		var magic [2]byte
		n, _ := f.Read(magic[:])
		f.Close()
		if n == 2 && magic[0] == '#' && magic[1] == '!' {
			continue
		}
		return cand
	}
	return ""
}

// ensurePathLine makes ~/.zshenv put the ainfra shim dir first on PATH,
// creating the rc file when missing. The line carries no secret values.
// Idempotent: a file that already references the shim dir (in any form) is
// left untouched, so a user can move or rewrite the line without ainfra
// re-appending it.
func ensurePathLine(rcPath, binDir string) error {
	marker := binDir
	ref := fmt.Sprintf("%q", binDir)
	if home, err := os.UserHomeDir(); err == nil {
		if rel, rerr := filepath.Rel(home, binDir); rerr == nil && !strings.HasPrefix(rel, "..") {
			marker = rel
			ref = fmt.Sprintf(`"$HOME/%s"`, rel)
		}
	}
	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), marker) {
		return nil
	}
	line := fmt.Sprintf("\n# Added by ainfra — puts the secret-injecting launcher shims on PATH.\nexport PATH=%s:\"$PATH\"\n", ref)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		line = "\n" + line
	}
	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// cleanupLegacyShellEnv removes the pre-shim secret delivery: the env.sh file
// of export lines and the ~/.zshenv line that sourced it (written by ainfra
// <= 0.2.11). Both put credential values into the environment of every shell.
// Idempotent, and a no-op on machines that never had them. Other content in
// ~/.zshenv — including the shim PATH line — is preserved.
func cleanupLegacyShellEnv(home string) error {
	envPath := filepath.Join(home, ".config", "ainfra", "env.sh")
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	rcPath := filepath.Join(home, ".zshenv")
	raw, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	removed := false
	for _, l := range lines {
		if strings.Contains(l, ".config/ainfra/env.sh") ||
			strings.Contains(l, "Added by ainfra — exports managed secrets into the shell environment.") {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return nil
	}
	return os.WriteFile(rcPath, []byte(strings.Join(kept, "\n")), 0o644)
}

// expandTilde resolves a leading ~/ against the user's home directory.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}

// writeCredentialFile writes a resolved secret value to a file: the parent
// directory is created 0700 and the file 0600 — both hold a credential.
func writeCredentialFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// parseEnvBlob parses a .env-style blob (KEY=value lines) into a map. Blank
// lines and # comments are skipped, a leading `export ` is ignored, and a
// double-quoted value has its quotes removed and \n \t \" \\ escapes decoded —
// so a multi-line PEM key or JSON document can be stored on a single line.
func parseEnvBlob(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.TrimPrefix(t, "export ")
		eq := strings.IndexByte(t, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(t[:eq])
		val := strings.TrimSpace(t[eq+1:])
		switch {
		case len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"':
			val = val[1 : len(val)-1]
			val = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(val)
		case len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'':
			val = val[1 : len(val)-1]
		}
		out[key] = val
	}
	return out
}

// writeSettingsEnv merges the resolved secrets into the "env" object of the
// Claude Code settings file, preserving every other key in the file and every
// env entry ainfra does not manage. The file is written 0600 — it holds
// credential values.
func writeSettingsEnv(path string, env map[string]string) error {
	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	envObj, _ := doc["env"].(map[string]any)
	if envObj == nil {
		envObj = map[string]any{}
	}
	for k, v := range env {
		envObj[k] = v
	}
	doc["env"] = envObj

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return err
	}
	// WriteFile's mode only applies when creating the file. Claude Code may have
	// created settings.local.json at 0644 first, so chmod explicitly — it holds
	// credential values and must not be world-readable.
	return os.Chmod(path, 0o600)
}

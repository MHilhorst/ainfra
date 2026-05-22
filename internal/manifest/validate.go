package manifest

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/diag"
)

// packageLaunchers are commands that launch a server from a package registry;
// such servers must pin an exact version (spec §5.1).
var packageLaunchers = map[string]bool{"npx": true, "uvx": true, "pipx": true}

// hookEvents are the Claude Code lifecycle events a hook may bind to (spec §11).
var hookEvents = map[string]bool{
	"SessionStart": true, "SessionEnd": true, "UserPromptSubmit": true,
	"PreToolUse": true, "PostToolUse": true, "Notification": true,
	"Stop": true, "SubagentStop": true, "PreCompact": true,
}

// isRemoteSource reports whether a source string fetches from a remote
// registry (git or npm) and therefore must pin an exact version — the same
// drift-detection rule MCP servers follow (spec §5.1). A local path does not.
func isRemoteSource(src string) bool {
	return strings.HasPrefix(src, "git+") || strings.HasPrefix(src, "npm:")
}

// hasEmbeddedVersion reports whether a source string already encodes a pinned
// version inline — e.g. "npm:@scope/pkg@1.2.3". For npm sources, a version
// suffix in the package specifier counts. Git sources must declare a separate
// version field; a branch ref like "@main" does not satisfy the requirement.
func hasEmbeddedVersion(src string) bool {
	if strings.HasPrefix(src, "npm:") {
		// npm:@scope/pkg@version or npm:pkg@version
		rest := strings.TrimPrefix(src, "npm:")
		// Drop a leading @ (scoped package) and look for a second @
		if strings.HasPrefix(rest, "@") {
			rest = rest[1:] // skip the scope's @
		}
		return strings.Contains(rest, "@")
	}
	return false
}

// Validate runs static checks on a single manifest layer. It returns the first
// problem found as a *diag.Diagnostic; entries are checked in sorted-key order
// so that first problem is deterministic. The diagnostic's File is left empty
// — ValidateAll fills it from the layer. When a layer references a template
// declared in another layer, the caller (ValidateAll) injects the merged
// template map before calling Validate.
func Validate(m *Manifest) error {
	for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
		srv := m.MCPServers[id]
		if srv.Template != "" {
			if _, ok := m.Templates[srv.Template]; !ok {
				return &diag.Diagnostic{
					Summary: fmt.Sprintf("unknown template %q", srv.Template),
					Path:    "mcpServers." + id,
					Detail:  fmt.Sprintf("Server %q references template %q, which is not defined.", id, srv.Template),
					Hint:    "Define it under templates:, or correct the name.",
				}
			}
			continue
		}
		if d := validateMCPTransport(srv, "mcpServers."+id); d != nil {
			return d
		}
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return &diag.Diagnostic{
				Summary: "package-launched server must pin an exact version",
				Path:    "mcpServers." + id,
				Detail:  fmt.Sprintf("Server %q launches via %s but declares no version.", id, srv.Command),
				Hint:    `Add a version field, e.g.  version: "1.2.3"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Templates)) {
		tmpl := m.Templates[id]
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if d := validateMCPTransport(*srv, "templates."+id); d != nil {
				return d
			}
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return &diag.Diagnostic{
					Summary: "package-launched server must pin an exact version",
					Path:    "templates." + id,
					Detail:  fmt.Sprintf("Template %q produces a server launched via %s with no version.", id, srv.Command),
					Hint:    `Add a version field to the template body, e.g.  version: "1.2.3"`,
				}
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
		h := m.Hooks[id]
		if !hookEvents[h.Event] {
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("unknown or missing hook event %q", h.Event),
				Path:    "hooks." + id,
				Detail:  "A hook must bind to a Claude Code lifecycle event.",
				Hint:    "Valid events: SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop, SubagentStop, PreCompact.",
			}
		}
		if h.Command == "" {
			return &diag.Diagnostic{
				Summary: "hook declares no command",
				Path:    "hooks." + id,
				Detail:  fmt.Sprintf("Hook %q binds to %s but has nothing to run.", id, h.Event),
				Hint:    "Add a command field.",
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		if m.Commands[id].Source == "" {
			return &diag.Diagnostic{
				Summary: "command declares no source",
				Path:    "commands." + id,
				Detail:  fmt.Sprintf("Command %q has no source file.", id),
				Hint:    "Add a source field pointing at the command's .md file.",
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
		s := m.Skills[id]
		if s.Source == "" {
			return &diag.Diagnostic{
				Summary: "skill declares no source",
				Path:    "skills." + id,
				Detail:  fmt.Sprintf("Skill %q has no source. ainfra reconciles externally-sourced skills; a skill committed to the repo's own .claude/skills/ does not belong here.", id),
				Hint:    `Add a source field, e.g.  source: "github:acme/claude-skills/incident-response"`,
			}
		}
		if isRemoteSource(s.Source) && s.Version == "" && !hasEmbeddedVersion(s.Source) {
			return &diag.Diagnostic{
				Summary: "remote skill must pin an exact version",
				Path:    "skills." + id,
				Detail:  fmt.Sprintf("Skill %q fetches from a remote source but declares no version.", id),
				Hint:    `Add a version field, e.g.  version: "1.4.0"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Marketplaces)) {
		mp := m.Marketplaces[id]
		if mp.Source == "" {
			return &diag.Diagnostic{
				Summary: "marketplace declares no source",
				Path:    "marketplaces." + id,
				Detail:  fmt.Sprintf("Marketplace %q has no source.", id),
				Hint:    `Add a source field, e.g.  source: "github:owner/repo"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
		p := m.Plugins[id]
		if p.Marketplace == "" {
			return &diag.Diagnostic{
				Summary: "plugin declares no marketplace",
				Path:    "plugins." + id,
				Detail:  fmt.Sprintf("Plugin %q has no marketplace.", id),
				Hint:    `Add a marketplace field referencing a declared marketplace, e.g.  marketplace: my-org`,
			}
		}
		if _, ok := m.Marketplaces[p.Marketplace]; !ok {
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("plugin %q references undeclared marketplace %q", id, p.Marketplace),
				Path:    "plugins." + id,
				Detail:  fmt.Sprintf("Plugin %q names marketplace %q, but no marketplace with that name is declared.", id, p.Marketplace),
				Hint:    fmt.Sprintf("Declare the marketplace under marketplaces: %s, or correct the marketplace field.", p.Marketplace),
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Vars)) {
		v := m.Vars[id]
		switch v.From {
		case "value", "env", "command":
			// valid
		default:
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("unknown var source %q", v.From),
				Path:    "vars." + id,
				Detail:  fmt.Sprintf("Var %q declares from: %q, which is not a known source.", id, v.From),
				Hint:    "Valid sources: value, env, command.",
			}
		}
		if v.From == "env" && v.Env == "" {
			return &diag.Diagnostic{
				Summary: "var with from: env declares no env field",
				Path:    "vars." + id,
				Detail:  fmt.Sprintf("Var %q uses from: env but has no env field naming the environment variable.", id),
				Hint:    "Add an env field, e.g.  env: HOME",
			}
		}
		if v.From == "command" && v.Command == "" {
			return &diag.Diagnostic{
				Summary: "var with from: command declares no command field",
				Path:    "vars." + id,
				Detail:  fmt.Sprintf("Var %q uses from: command but has no command field.", id),
				Hint:    `Add a command field, e.g.  command: "git config user.name"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
		r := m.Rules[id]
		if r.Source == "" {
			return &diag.Diagnostic{
				Summary: "rule declares no source",
				Path:    "rules." + id,
				Detail:  fmt.Sprintf("Rule %q has no source file.", id),
				Hint:    "Add a source field pointing at the context file (e.g. ./rules/team-claude.md).",
			}
		}
		if isRemoteSource(r.Source) && r.Version == "" && !hasEmbeddedVersion(r.Source) {
			return &diag.Diagnostic{
				Summary: "remote rule must pin an exact version",
				Path:    "rules." + id,
				Detail:  fmt.Sprintf("Rule %q fetches from a remote source but declares no version.", id),
				Hint:    `Add a version field, e.g.  version: "1"`,
			}
		}
	}
	if err := validateTools(m.Tools); err != nil {
		return err
	}
	if d := validateSecrets(m); d != nil {
		return d
	}
	return nil
}

// validateSecrets checks every secrets: entry for a structurally malformed
// reference. It is offline-only — it does not resolve the secret, just checks
// the shape of a ref that uses a scheme ainfra knows. Entries are checked in
// sorted-key order so the first reported problem is deterministic.
func validateSecrets(m *Manifest) *diag.Diagnostic {
	for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
		s := m.Secrets[id]
		if s.Ref == "" {
			continue
		}
		scheme, rest, ok := strings.Cut(s.Ref, "://")
		if !ok {
			continue // not a scheme-style ref; nothing structural to check
		}
		switch scheme {
		case "op":
			// op://<vault>/<item>/<field> — at least three non-empty segments.
			segs := strings.Split(rest, "/")
			nonEmpty := 0
			for _, seg := range segs {
				if seg != "" {
					nonEmpty++
				}
			}
			if nonEmpty < 3 {
				return &diag.Diagnostic{
					Summary: "malformed op:// secret reference",
					Path:    "secrets." + id,
					Detail:  fmt.Sprintf("Secret %q has ref %q; a 1Password reference is op://<vault>/<item>/<field>.", id, s.Ref),
					Hint:    `Use three segments, e.g.  ref: "op://Engineering/Database/password"`,
				}
			}
		case "env":
			if strings.TrimSpace(rest) == "" {
				return &diag.Diagnostic{
					Summary: "malformed env:// secret reference",
					Path:    "secrets." + id,
					Detail:  fmt.Sprintf("Secret %q has ref %q but names no environment variable.", id, s.Ref),
					Hint:    `Name the variable, e.g.  ref: "env://API_TOKEN"`,
				}
			}
		}
	}
	return nil
}

// validateMCPTransport enforces the disjoint field sets of the two MCP
// transports (spec §5.2): a transport: http server needs a url and rejects the
// stdio launch fields; a stdio server (the default) rejects the http-only url
// and headers fields. Both transports share one struct, so this is an explicit
// check, not a structural guarantee.
func validateMCPTransport(srv MCPServer, path string) *diag.Diagnostic {
	if srv.Transport == "http" {
		if srv.URL == "" {
			return &diag.Diagnostic{
				Summary: "http MCP server declares no url",
				Path:    path,
				Detail:  "A transport: http server is reached over HTTP and needs an endpoint.",
				Hint:    "Add a url field, e.g.  url: https://mcp.example.com/sse",
			}
		}
		if srv.Command != "" || len(srv.Args) > 0 || srv.Version != "" {
			return &diag.Diagnostic{
				Summary: "http MCP server declares stdio-only fields",
				Path:    path,
				Detail:  "command, args, and version apply only to transport: stdio.",
				Hint:    "Remove them, or set transport: stdio.",
			}
		}
		return nil
	}
	if srv.URL != "" || len(srv.Headers) > 0 {
		return &diag.Diagnostic{
			Summary: "stdio MCP server declares http-only fields",
			Path:    path,
			Detail:  "url and headers apply only to transport: http.",
			Hint:    "Remove them, or set transport: http.",
		}
	}
	return nil
}

// validateTools rejects empty patterns in the tools channel. An empty allow,
// ask, deny, or disabled entry is almost always an editing mistake, and a
// blank permission pattern silently matches nothing — a quiet footgun.
func validateTools(t *Tools) error {
	if t == nil {
		return nil
	}
	blank := func(field string, list []string) error {
		for _, pattern := range list {
			if strings.TrimSpace(pattern) == "" {
				return &diag.Diagnostic{
					Summary: "tools." + field + " contains an empty entry",
					Path:    "tools." + field,
					Detail:  "A blank pattern matches nothing and is almost always a mistake.",
					Hint:    "Remove the empty entry, or replace it with a real pattern.",
				}
			}
		}
		return nil
	}
	if t.Builtins != nil {
		if err := blank("builtins.disabled", t.Builtins.Disabled); err != nil {
			return err
		}
	}
	if p := t.Permissions; p != nil {
		// Checked in a fixed order so the first reported problem is deterministic.
		for _, tier := range []struct {
			field string
			list  []string
		}{
			{"permissions.allow", p.Allow},
			{"permissions.ask", p.Ask},
			{"permissions.deny", p.Deny},
		} {
			if err := blank(tier.field, tier.list); err != nil {
				return err
			}
		}
	}
	return nil
}

// agentFileFor names the source file for each layer, used to tag diagnostics
// raised by the cross-layer agent checks.
var agentFileFor = map[Layer]string{
	LayerRepo:     "ainfra.yaml",
	LayerPersonal: "ainfra.personal.yaml",
	LayerTeam:     "(team layer)",
}

// channelEntry is one channel entry flattened for the capability check.
type channelEntry struct {
	channel string
	id      string // empty for the singleton tools channel
	agents  []string
}

// path renders the diagnostic Path for an entry.
func (e channelEntry) path() string {
	if e.id == "" {
		return e.channel
	}
	return e.channel + "." + e.id
}

// collectEntries flattens every channel entry of m into a slice. Entries
// within each channel are sorted by id; channels themselves are emitted in a
// fixed order so the capability check always reports the same first error.
func collectEntries(m *Manifest) []channelEntry {
	var out []channelEntry
	for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
		out = append(out, channelEntry{agent.ChannelMCPServers, id, m.MCPServers[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
		out = append(out, channelEntry{agent.ChannelSkills, id, m.Skills[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Marketplaces)) {
		out = append(out, channelEntry{agent.ChannelMarketplaces, id, nil})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
		out = append(out, channelEntry{agent.ChannelPlugins, id, m.Plugins[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
		out = append(out, channelEntry{agent.ChannelRules, id, m.Rules[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.CLITools)) {
		out = append(out, channelEntry{agent.ChannelCLITools, id, m.CLITools[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
		out = append(out, channelEntry{agent.ChannelHooks, id, m.Hooks[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		out = append(out, channelEntry{agent.ChannelCommands, id, m.Commands[id].Agents})
	}
	if m.Tools != nil {
		out = append(out, channelEntry{agent.ChannelTools, "", m.Tools.Agents})
	}
	return out
}

// checkEntryAgent applies the spec §3.2 gating rules to one entry against the
// resolved target agent. It returns nil when the entry is acceptable.
func checkEntryAgent(e channelEntry, target agent.ID) *diag.Diagnostic {
	for _, a := range e.agents {
		if !agent.Known(a) {
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("unknown agent %q in agents:", a),
				Path:    e.path(),
				Detail:  fmt.Sprintf("Entry %q gates to agent %q, which ainfra does not know.", e.path(), a),
				Hint:    "Valid agents: claude-code, codex.",
			}
		}
	}
	// A non-empty agents: list that omits the target deliberately scopes this
	// entry away from the target — cleanly skipped, not an error.
	if len(e.agents) > 0 && !slices.Contains(e.agents, string(target)) {
		return nil
	}
	if agent.Supports(target, e.channel) {
		return nil
	}
	if len(e.agents) > 0 {
		// agents: lists the target, yet the target cannot render this channel.
		return &diag.Diagnostic{
			Summary: fmt.Sprintf("agent %q cannot render the %s channel", target, e.channel),
			Path:    e.path(),
			Detail:  fmt.Sprintf("Entry %q is gated to agent %q, but %q has no %s channel.", e.path(), target, target, e.channel),
			Hint:    fmt.Sprintf("Remove %q from this entry's agents: list.", target),
		}
	}
	return &diag.Diagnostic{
		Summary: fmt.Sprintf("the %s channel is not supported by agent %q", e.channel, target),
		Path:    e.path(),
		Detail:  fmt.Sprintf("The resolved agent is %q, which cannot render the %s channel.", target, e.channel),
		Hint:    "Gate this entry away with  agents: [claude-code]  — or change the agent field.",
	}
}

// validateAgentCapabilities resolves the target agent, rejects an unknown
// agent id, and checks every channel entry against the agent's capabilities
// (spec §3.1, §3.2).
func validateAgentCapabilities(layers map[Layer]*Manifest) error {
	id, setLayer, _ := ResolveAgent(layers)
	if !agent.Known(id) {
		return &diag.Diagnostic{
			Summary: fmt.Sprintf("unknown agent %q", id),
			File:    agentFileFor[setLayer],
			Path:    "agent",
			Detail:  fmt.Sprintf("The agent field selects which AI agent ainfra renders for; %q is not one ainfra knows.", id),
			Hint:    "Valid agents: claude-code, codex.",
		}
	}
	target := agent.ID(id)
	for _, ln := range []Layer{LayerTeam, LayerRepo, LayerPersonal} {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		for _, e := range collectEntries(m) {
			if d := checkEntryAgent(e, target); d != nil {
				d.File = agentFileFor[ln]
				return d
			}
		}
	}
	return nil
}

// ValidateAll validates every present layer. It builds a cross-layer template
// and marketplace map first, so a lower layer may reference a template or
// marketplace defined in a higher one, then tags each diagnostic with the
// offending layer's file name.
func ValidateAll(layers map[Layer]*Manifest) error {
	order := []Layer{LayerTeam, LayerRepo, LayerPersonal}
	allTemplates := map[string]Template{}
	for _, ln := range order {
		if m, ok := layers[ln]; ok {
			for name, tmpl := range m.Templates {
				if _, exists := allTemplates[name]; !exists {
					allTemplates[name] = tmpl
				}
			}
		}
	}
	allMarketplaces := map[string]Marketplace{}
	for _, ln := range order {
		if m, ok := layers[ln]; ok {
			for name, mp := range m.Marketplaces {
				if _, exists := allMarketplaces[name]; !exists {
					allMarketplaces[name] = mp
				}
			}
		}
	}
	for _, ln := range order {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		toValidate := m
		needsCopy := len(m.Templates) < len(allTemplates) || len(m.Marketplaces) < len(allMarketplaces)
		if needsCopy {
			copied := *m
			copied.Templates = allTemplates
			copied.Marketplaces = allMarketplaces
			toValidate = &copied
		}
		if err := Validate(toValidate); err != nil {
			if d, ok := err.(*diag.Diagnostic); ok && d.File == "" {
				d.File = agentFileFor[ln]
			}
			return err
		}
	}
	return validateAgentCapabilities(layers)
}

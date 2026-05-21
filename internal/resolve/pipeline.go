package resolve

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/MHilhorst/ainfra/internal/graph"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
)

// portBase is the lowest local port ainfra allocates for tunnels and other
// allocated-port resolved fields. 13306 sits just above the default MySQL port.
const portBase = 13306

// RunLock executes the full resolve pipeline for the repo at dir and writes
// ainfra.lock (team+repo entries) and ainfra.personal.lock (personal).
func RunLock(dir string) error {
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return err
	}
	// Merge templates across layers, so a lower layer can reference a template
	// declared higher up. The resolve phase below reuses this map.
	allTemplates := map[string]manifest.Template{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		if m, ok := layers[layerName]; ok {
			for name, tmpl := range m.Templates {
				if _, exists := allTemplates[name]; !exists {
					allTemplates[name] = tmpl
				}
			}
		}
	}

	// Validate every layer. ValidateAll merges templates across layers and
	// tags each diagnostic with its source file — the same check
	// `ainfra validate` runs.
	if err := manifest.ValidateAll(layers); err != nil {
		return err
	}

	prior, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		return err
	}
	priorPorts := portsFromLock(prior)

	type tagged struct {
		id    string
		layer manifest.Layer
		inst  manifest.MCPServer
		tmpl  manifest.Template
	}
	var insts []tagged
	var portReqs []PortRequest
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for id, srv := range m.MCPServers {
			if srv.Template == "" {
				// Only templated instances are resolved in this phase;
				// fully-inlined mcpServers are handled by the follow-up plan.
				continue
			}
			tmpl := allTemplates[srv.Template]
			insts = append(insts, tagged{id, layerName, srv, tmpl})
			for field, rf := range tmpl.Resolved {
				if rf.Kind == "allocated-port" {
					portReqs = append(portReqs, PortRequest{Instance: id, Field: field})
				}
			}
		}
	}
	sort.Slice(insts, func(i, j int) bool { return insts[i].id < insts[j].id })

	ports, err := AllocatePorts(portReqs, priorPorts, portBase)
	if err != nil {
		return err
	}

	g := graph.New()
	lock := &lockfile.Lock{Version: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: lockfile.Entries{
			MCPServers:         map[string]lockfile.Entry{},
			BackgroundServices: map[string]lockfile.Entry{},
			Hooks:              map[string]lockfile.Entry{},
			Commands:           map[string]lockfile.Entry{},
			CLITools:           map[string]lockfile.Entry{},
			Skills:             map[string]lockfile.Entry{},
			Plugins:            map[string]lockfile.Entry{},
			Rules:              map[string]lockfile.Entry{},
			Tools:              map[string]lockfile.Entry{},
		}}

	for _, ti := range insts {
		resolved := map[string]any{}
		// Populate all resolved fields. allocated-port gets the assigned port;
		// other kinds get a placeholder so template interpolation succeeds.
		for f, rf := range ti.tmpl.Resolved {
			switch rf.Kind {
			case "allocated-port":
				if p, ok := ports[ti.id][f]; ok {
					resolved[f] = p
				}
			default:
				resolved[f] = fmt.Sprintf("<resolved:%s.%s>", ti.id, f)
			}
		}
		out, err := Instantiate(ti.id, ti.inst, ti.tmpl, resolved)
		if err != nil {
			return err
		}
		g.AddNode("mcp:" + ti.id)
		entry := lockfile.Entry{Layer: string(ti.layer), FromTemplate: ti.inst.Template, Resolved: resolved}
		if out.MCPServer != nil {
			entry.Version = out.MCPServer.Version
			entry.ContentHash = lockfile.ContentHash(map[string]any{
				"command": out.MCPServer.Command, "version": out.MCPServer.Version,
				"env": toAnyMap(out.MCPServer.Env),
			})
			addRequireEdges(g, "mcp:"+ti.id, out.MCPServer.Requires)
		}
		lock.Entries.MCPServers[ti.id] = entry
		if out.Service != nil {
			g.AddNode("svc:" + out.Service.ID)
			lock.Entries.BackgroundServices[out.Service.ID] = lockfile.Entry{
				Layer: string(ti.layer), Resolved: resolved,
				ContentHash: lockfile.ContentHash(out.Service.Spec),
			}
			addRequireEdges(g, "svc:"+out.Service.ID, out.Service.Requires)
		}
	}
	// Resolve the hooks and commands channels. Neither is templated: each entry
	// is hashed and recorded, and its requires edges are added to the graph so
	// the cycle check and topo-sort cover them too.
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
			h := m.Hooks[id]
			node := "hook:" + id
			g.AddNode(node)
			addRequireEdges(g, node, h.Requires)
			lock.Entries.Hooks[id] = lockfile.Entry{
				Layer: string(layerName),
				ContentHash: lockfile.ContentHash(map[string]any{
					"event": h.Event, "matcher": h.Matcher, "command": h.Command,
					"source": h.Source, "timeout": h.Timeout,
				}),
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
			c := m.Commands[id]
			node := "cmd:" + id
			g.AddNode(node)
			addRequireEdges(g, node, c.Requires)
			lock.Entries.Commands[id] = lockfile.Entry{
				Layer:   string(layerName),
				Version: c.Version,
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": c.Source, "description": c.Description, "version": c.Version,
				}),
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
			s := m.Skills[id]
			node := "skill:" + id
			g.AddNode(node)
			addRequireEdges(g, node, s.Requires)
			lock.Entries.Skills[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  s.Version,
				Requires: requireRefs(s.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": s.Source, "version": s.Version,
				}),
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
			p := m.Plugins[id]
			node := "plugin:" + id
			g.AddNode(node)
			addRequireEdges(g, node, p.Requires)
			lock.Entries.Plugins[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  p.Version,
				Requires: requireRefs(p.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": p.Source, "version": p.Version,
				}),
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
			r := m.Rules[id]
			node := "rule:" + id
			g.AddNode(node)
			addRequireEdges(g, node, r.Requires)
			lock.Entries.Rules[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  r.Version,
				Requires: requireRefs(r.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": r.Source, "version": r.Version, "target": r.Target,
				}),
			}
		}
		if m.Tools != nil {
			node := "tools:" + string(layerName)
			g.AddNode(node)
			lock.Entries.Tools[string(layerName)] = lockfile.Entry{
				Layer: string(layerName),
				ContentHash: lockfile.ContentHash(map[string]any{
					"disabled": m.Tools.Builtins.Disabled,
					"allow":    m.Tools.Permissions.Allow,
					"deny":     m.Tools.Permissions.Deny,
				}),
			}
		}
	}

	if _, err := g.TopoSort(); err != nil {
		return fmt.Errorf("dependency graph invalid: %w", err)
	}

	committed, personal := splitByLayer(lock)
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), committed); err != nil {
		return err
	}
	return lockfile.Write(filepath.Join(dir, "ainfra.personal.lock"), personal)
}

func toAnyMap(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// requireRefs converts an entry's requires edges into the node-ref strings the
// dependency graph uses ("cli:node", "svc:tunnel", "pre:internet"). The lock
// stores these per entry so plan/apply/check can rebuild the graph without
// re-reading the manifest.
func requireRefs(reqs []manifest.Require) []string {
	var refs []string
	for _, r := range reqs {
		switch {
		case r.Service != "":
			refs = append(refs, "svc:"+r.Service)
		case r.CLITool != "":
			refs = append(refs, "cli:"+r.CLITool)
		case r.Precondition != "":
			refs = append(refs, "pre:"+r.Precondition)
		}
	}
	return refs
}

func addRequireEdges(g *graph.Graph, fromNode string, reqs []manifest.Require) {
	for _, ref := range requireRefs(reqs) {
		g.AddNode(ref)
		g.AddEdge(fromNode, ref)
	}
}

func portsFromLock(l *lockfile.Lock) map[string]map[string]int {
	out := map[string]map[string]int{}
	for id, e := range l.Entries.MCPServers {
		for f, v := range e.Resolved {
			if p, ok := v.(int); ok {
				if out[id] == nil {
					out[id] = map[string]int{}
				}
				out[id][f] = p
			}
		}
	}
	return out
}

// splitByLayer divides a lock into the committed (team+repo) and personal locks
// (spec §7 — the layered lockfile).
func splitByLayer(l *lockfile.Lock) (committed, personal *lockfile.Lock) {
	mk := func() *lockfile.Lock {
		return &lockfile.Lock{Version: 1, GeneratedAt: l.GeneratedAt, Entries: lockfile.Entries{
			MCPServers: map[string]lockfile.Entry{}, BackgroundServices: map[string]lockfile.Entry{},
			Hooks: map[string]lockfile.Entry{}, Commands: map[string]lockfile.Entry{},
			CLITools: map[string]lockfile.Entry{}, Skills: map[string]lockfile.Entry{},
			Plugins: map[string]lockfile.Entry{}, Rules: map[string]lockfile.Entry{},
			Tools: map[string]lockfile.Entry{}}}
	}
	committed, personal = mk(), mk()
	route := func(dst func(*lockfile.Lock) map[string]lockfile.Entry, src map[string]lockfile.Entry) {
		for id, e := range src {
			if e.Layer == string(manifest.LayerPersonal) {
				dst(personal)[id] = e
			} else {
				dst(committed)[id] = e
			}
		}
	}
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.MCPServers }, l.Entries.MCPServers)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.BackgroundServices }, l.Entries.BackgroundServices)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Hooks }, l.Entries.Hooks)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Commands }, l.Entries.Commands)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Skills }, l.Entries.Skills)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Plugins }, l.Entries.Plugins)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Rules }, l.Entries.Rules)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Tools }, l.Entries.Tools)
	return committed, personal
}

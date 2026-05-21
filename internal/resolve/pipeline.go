package resolve

import (
	"fmt"
	"path/filepath"
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
	// Build a merged template map so lower layers can reference templates from
	// higher layers (e.g. personal layer reusing a repo-layer template).
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

	// Validate each layer. For layers that reference cross-layer templates,
	// inject the merged template map so the existence check passes.
	for _, m := range layers {
		toValidate := m
		if len(m.Templates) < len(allTemplates) {
			// Shallow copy with merged templates so cross-layer refs validate.
			copied := *m
			copied.Templates = allTemplates
			toValidate = &copied
		}
		if err := manifest.Validate(toValidate); err != nil {
			return err
		}
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
			CLITools:           map[string]lockfile.Entry{},
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
			for _, r := range out.MCPServer.Requires {
				if r.Service != "" {
					g.AddNode("svc:" + r.Service)
					g.AddEdge("mcp:"+ti.id, "svc:"+r.Service)
				}
				if r.CLITool != "" {
					g.AddNode("cli:" + r.CLITool)
					g.AddEdge("mcp:"+ti.id, "cli:"+r.CLITool)
				}
			}
		}
		lock.Entries.MCPServers[ti.id] = entry
		if out.Service != nil {
			g.AddNode("svc:" + out.Service.ID)
			lock.Entries.BackgroundServices[out.Service.ID] = lockfile.Entry{
				Layer: string(ti.layer), Resolved: resolved,
				ContentHash: lockfile.ContentHash(out.Service.Spec),
			}
			for _, r := range out.Service.Requires {
				if r.CLITool != "" {
					g.AddNode("cli:" + r.CLITool)
					g.AddEdge("svc:"+out.Service.ID, "cli:"+r.CLITool)
				}
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
			CLITools: map[string]lockfile.Entry{}}}
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
	return committed, personal
}

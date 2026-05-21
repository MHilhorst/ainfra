package resolve

import (
	"fmt"

	"github.com/MHilhorst/aistack/internal/manifest"
)

// Instance is the resolved output of instantiating one template.
type Instance struct {
	ID        string
	MCPServer *manifest.MCPServer
	Service   *manifest.BackgroundService
}

// Instantiate expands a template for one instance. resolved holds the
// tool-owned field values (allocated in AllocatePorts).
func Instantiate(id string, inst manifest.MCPServer, tmpl manifest.Template, resolved map[string]any) (Instance, error) {
	params := map[string]any{}
	for name, p := range tmpl.Params {
		if v, ok := inst.Params[name]; ok {
			params[name] = v
		} else if p.Default != nil {
			params[name] = p.Default
		} else if p.Required {
			return Instance{}, fmt.Errorf("%s: missing required param %q", id, name)
		}
	}
	secret := map[string]any{}
	for name := range tmpl.Secrets {
		secret[name] = fmt.Sprintf("<secret:%s.%s>", id, name)
	}
	scope := Scope{
		Params:   params,
		Instance: map[string]any{"id": id},
		Resolved: resolved,
		Secret:   secret,
	}

	out := Instance{ID: id}
	if src := tmpl.Produces.MCPServer; src != nil {
		srv := *src
		srv.Env = map[string]string{}
		for k, v := range src.Env {
			ev, err := Interpolate(v, scope)
			if err != nil {
				return Instance{}, err
			}
			srv.Env[k] = ev
		}
		srv.Requires = interpolateRequires(src.Requires, scope)
		out.MCPServer = &srv
	}
	if src := tmpl.Produces.BackgroundService; src != nil {
		svc := *src
		bid, err := Interpolate(src.ID, scope)
		if err != nil {
			return Instance{}, err
		}
		svc.ID = bid
		spec, err := InterpolateMap(src.Spec, scope)
		if err != nil {
			return Instance{}, err
		}
		svc.Spec = spec
		svc.Requires = interpolateRequires(src.Requires, scope)
		out.Service = &svc
	}
	return out, nil
}

func interpolateRequires(reqs []manifest.Require, scope Scope) []manifest.Require {
	out := make([]manifest.Require, len(reqs))
	for i, r := range reqs {
		r.Service, _ = Interpolate(r.Service, scope)
		r.CLITool, _ = Interpolate(r.CLITool, scope)
		r.Precondition, _ = Interpolate(r.Precondition, scope)
		out[i] = r
	}
	return out
}

package resolve

import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/manifest"
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
		srv.Headers = map[string]string{}
		for k, v := range src.Headers {
			hv, err := Interpolate(v, scope)
			if err != nil {
				return Instance{}, err
			}
			srv.Headers[k] = hv
		}
		var err error
		srv.Requires, err = interpolateRequires(src.Requires, scope)
		if err != nil {
			return Instance{}, err
		}
		if len(src.Args) > 0 {
			srv.Args = append([]string(nil), src.Args...)
		}
		if src.Enabled != nil {
			b := *src.Enabled
			srv.Enabled = &b
		}
		srv.Params = nil
		srv.Secret = nil
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
		svc.Requires, err = interpolateRequires(src.Requires, scope)
		if err != nil {
			return Instance{}, err
		}
		if svc.Lifecycle, err = InterpolateMap(src.Lifecycle, scope); err != nil {
			return Instance{}, err
		}
		if svc.Check, err = InterpolateMap(src.Check, scope); err != nil {
			return Instance{}, err
		}
		out.Service = &svc
	}
	return out, nil
}

// interpolateRequires returns a fresh slice of Require with every ${...}
// reference in each edge expanded; it fails on the first bad reference.
func interpolateRequires(reqs []manifest.Require, scope Scope) ([]manifest.Require, error) {
	out := make([]manifest.Require, len(reqs))
	for i, r := range reqs {
		var err error
		if r.Service, err = Interpolate(r.Service, scope); err != nil {
			return nil, err
		}
		if r.CLITool, err = Interpolate(r.CLITool, scope); err != nil {
			return nil, err
		}
		if r.Precondition, err = Interpolate(r.Precondition, scope); err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

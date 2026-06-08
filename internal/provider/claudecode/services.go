package claudecode

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Services reconciles background service definitions under
// <root>/.ainfra/services/<id>/. ainfra generates the service definition only;
// starting and supervising the process is out of scope (design §7).
//
// Resource.Payload keys: "kind" (string) and "spec" (map[string]any).
type Services struct{}

// Channel returns the channel name this provider manages.
func (Services) Channel() string { return "backgroundServices" }

func servicesDir(env provider.Env) string {
	return filepath.Join(env.Root, ".ainfra", "services")
}

func serviceDir(env provider.Env, id string) string {
	return filepath.Join(servicesDir(env), id)
}

// Observe lists subdirectories of <root>/.ainfra/services/ and returns a
// Resource per service directory present. A missing directory is treated as no
// resources.
func (Services) Observe(env provider.Env) ([]provider.Resource, error) {
	entries, err := env.FS.ReadDir(servicesDir(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var resources []provider.Resource
	for _, name := range entries {
		info, err := env.FS.Stat(filepath.Join(servicesDir(env), name))
		if err != nil || !info.IsDir() {
			continue
		}
		resources = append(resources, provider.Resource{
			ID:      name,
			Channel: "backgroundServices",
		})
	}
	return resources, nil
}

// Apply executes the channel plan. For Create/Update it writes start.sh and
// stop.sh under the service directory via fsmerge.WriteOwnedFile. For Delete it
// removes the service directory. Honors env.DryRun.
func (Services) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			switch c.Kind {
			case provider.ChangeCreate, provider.ChangeUpdate:
				id := c.ID
				kind, _ := c.Resource.Payload["kind"].(string)
				spec, _ := c.Resource.Payload["spec"].(map[string]any)

				startScript := buildStartScript(id, kind, spec)
				stopScript := buildStopScript(id, kind, spec)

				dir := serviceDir(env, id)
				if err := fsmerge.WriteOwnedFile(env.FS, filepath.Join(dir, "start.sh"), []byte(startScript)); err != nil {
					return provider.ApplyResult{}, fmt.Errorf("backgroundServices: writing start.sh for %q: %w", id, err)
				}
				if err := fsmerge.WriteOwnedFile(env.FS, filepath.Join(dir, "stop.sh"), []byte(stopScript)); err != nil {
					return provider.ApplyResult{}, fmt.Errorf("backgroundServices: writing stop.sh for %q: %w", id, err)
				}
			case provider.ChangeDelete:
				dir := serviceDir(env, c.ID)
				if err := env.FS.RemoveAll(dir); err != nil {
					return provider.ApplyResult{}, fmt.Errorf("backgroundServices: removing service dir for %q: %w", c.ID, err)
				}
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "backgroundServices",
		Applied: applied,
	}, nil
}

func buildStartScript(id, kind string, spec map[string]any) string {
	s := "#!/bin/sh\n"
	s += fmt.Sprintf("# ainfra-generated start script for service %s (kind %s)\n", id, kind)
	if spec["command"] != nil {
		s += specString(spec, "command") + "\n"
	} else {
		s += "# TODO: add start command\n"
	}
	return s
}

func buildStopScript(id, kind string, spec map[string]any) string {
	s := "#!/bin/sh\n"
	s += fmt.Sprintf("# ainfra-generated stop script for service %s (kind %s)\n", id, kind)
	if spec["stopCommand"] != nil {
		s += specString(spec, "stopCommand") + "\n"
	} else {
		s += "# TODO: add stop command\n"
	}
	return s
}

// specString coerces a spec value to a string. Spec values arrive as `any`
// because they survive YAML decode and template interpolation, so a port may be
// an int (3306) or a string ("${resolved.tunnelPort}" once resolved).
func specString(spec map[string]any, key string) string {
	v, ok := spec[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

package manifest

import "fmt"

// packageLaunchers are commands that launch a server from a package registry;
// such servers must pin an exact version (spec §5.1).
var packageLaunchers = map[string]bool{"npx": true, "uvx": true, "pipx": true}

// Validate runs static checks on a single resolved manifest.
func Validate(m *Manifest) error {
	for id, srv := range m.MCPServers {
		if srv.Template != "" {
			if _, ok := m.Templates[srv.Template]; !ok {
				return fmt.Errorf("mcpServers.%s: unknown template %q", id, srv.Template)
			}
			continue
		}
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return fmt.Errorf("mcpServers.%s: package-launched servers must pin an exact version", id)
		}
	}
	for id, tmpl := range m.Templates {
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return fmt.Errorf("templates.%s: package-launched servers must pin an exact version", id)
			}
		}
	}
	return nil
}

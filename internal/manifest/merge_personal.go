package manifest

// mergePersonal combines a repo-level personal manifest (more specific) and a
// global personal manifest (less specific, shared across repos) into one
// personal layer. The repo file wins per key on every map; for singletons and
// scalar fields a non-zero repo value wins.
//
// Either input may be nil. A non-nil result is returned whenever at least one
// input is non-nil.
func mergePersonal(repo, global *Manifest) *Manifest {
	if repo == nil && global == nil {
		return nil
	}
	if repo == nil {
		return global
	}
	if global == nil {
		return repo
	}
	out := *repo // start from repo so its scalar fields win
	if out.Agent == "" {
		out.Agent = global.Agent
	}
	if len(out.Extends) == 0 {
		out.Extends = global.Extends
	}
	out.Preconditions = mergeMap(out.Preconditions, global.Preconditions)
	out.CLITools = mergeMap(out.CLITools, global.CLITools)
	out.BackgroundServices = mergeMap(out.BackgroundServices, global.BackgroundServices)
	out.Secrets = mergeMap(out.Secrets, global.Secrets)
	out.Templates = mergeMap(out.Templates, global.Templates)
	out.MCPServers = mergeMap(out.MCPServers, global.MCPServers)
	out.Hooks = mergeMap(out.Hooks, global.Hooks)
	out.Commands = mergeMap(out.Commands, global.Commands)
	out.Skills = mergeMap(out.Skills, global.Skills)
	out.Marketplaces = mergeMap(out.Marketplaces, global.Marketplaces)
	out.Plugins = mergeMap(out.Plugins, global.Plugins)
	out.Rules = mergeMap(out.Rules, global.Rules)
	out.Vars = mergeMap(out.Vars, global.Vars)
	if out.Tools == nil {
		out.Tools = global.Tools
	}
	if out.Publish == nil {
		out.Publish = global.Publish
	}
	return &out
}

// mergeMap returns a map where repo entries shadow global entries with the
// same key. Both inputs may be nil; the result is nil only when both are
// empty.
func mergeMap[K comparable, V any](repo, global map[K]V) map[K]V {
	if len(repo) == 0 && len(global) == 0 {
		return repo
	}
	out := make(map[K]V, len(repo)+len(global))
	for k, v := range global {
		out[k] = v
	}
	for k, v := range repo {
		out[k] = v // repo wins
	}
	return out
}

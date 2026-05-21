package manifest

import "github.com/MHilhorst/ainfra/internal/agent"

// ResolveAgent determines the target agent across the config layers. The
// highest-authority layer that declares a non-empty agent wins (team, then
// repo, then personal); when no layer declares one the default agent is used.
// It returns the agent id, the layer that set it (empty when defaulted), and
// whether any layer set it explicitly. ResolveAgent does not check that the
// id is a known agent — validateAgentCapabilities does.
func ResolveAgent(layers map[Layer]*Manifest) (id string, layer Layer, explicit bool) {
	for _, ln := range []Layer{LayerTeam, LayerRepo, LayerPersonal} {
		if m, ok := layers[ln]; ok && m.Agent != "" {
			return m.Agent, ln, true
		}
	}
	return string(agent.Default), "", false
}

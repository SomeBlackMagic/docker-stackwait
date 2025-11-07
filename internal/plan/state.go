package plan

import (
	"stackman/internal/compose"
)

// BuildDesiredState creates DesiredState from a parsed compose file
func BuildDesiredState(composeFile *compose.ComposeFile) *DesiredState {
	state := &DesiredState{
		Services: make(map[string]*compose.Service),
		Networks: make(map[string]*compose.Network),
		Volumes:  make(map[string]*compose.Volume),
		Configs:  make(map[string]*compose.Config),
		Secrets:  make(map[string]*compose.Secret),
	}

	// Copy services
	for name, svc := range composeFile.Services {
		state.Services[name] = svc
	}

	// Copy networks
	for name, net := range composeFile.Networks {
		state.Networks[name] = net
	}

	// Copy volumes
	for name, vol := range composeFile.Volumes {
		state.Volumes[name] = vol
	}

	// Copy configs
	for name, cfg := range composeFile.Configs {
		state.Configs[name] = cfg
	}

	// Copy secrets
	for name, sec := range composeFile.Secrets {
		state.Secrets[name] = sec
	}

	return state
}

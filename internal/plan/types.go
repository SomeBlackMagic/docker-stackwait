package plan

import (
	"stackman/internal/compose"

	"github.com/docker/docker/api/types/swarm"
)

// ActionType represents the type of change needed
type ActionType string

const (
	ActionCreate ActionType = "create"
	ActionUpdate ActionType = "update"
	ActionDelete ActionType = "delete"
	ActionNone   ActionType = "none"
)

// ServiceAction represents a planned change to a service
type ServiceAction struct {
	Name          string
	Action        ActionType
	CurrentSpec   *swarm.ServiceSpec
	DesiredSpec   *swarm.ServiceSpec
	CurrentMeta   *swarm.Meta
	ServiceID     string
	Changes       []string // Human-readable list of changes
}

// NetworkAction represents a planned change to a network
type NetworkAction struct {
	Name        string
	Action      ActionType
	NetworkID   string
	Driver      string
	Labels      map[string]string
}

// VolumeAction represents a planned change to a volume
type VolumeAction struct {
	Name      string
	Action    ActionType
	VolumeID  string
	Driver    string
	Labels    map[string]string
}

// SecretAction represents a planned change to a secret
type SecretAction struct {
	Name      string
	Action    ActionType
	SecretID  string
	Labels    map[string]string
	Data      []byte // Only set for create/update
}

// ConfigAction represents a planned change to a config
type ConfigAction struct {
	Name      string
	Action    ActionType
	ConfigID  string
	Labels    map[string]string
	Data      []byte // Only set for create/update
}

// Plan represents the full deployment plan
type Plan struct {
	StackName string

	// Resources (applied first, in order)
	Networks []NetworkAction
	Volumes  []VolumeAction
	Configs  []ConfigAction
	Secrets  []SecretAction

	// Services (applied after resources)
	Services []ServiceAction

	// Cleanup (applied last, only with --prune)
	OrphanedServices []string
	OrphanedNetworks []string
	OrphanedVolumes  []string
	OrphanedConfigs  []string
	OrphanedSecrets  []string
}

// IsEmpty returns true if the plan has no changes
func (p *Plan) IsEmpty() bool {
	hasChanges := false

	for _, net := range p.Networks {
		if net.Action != ActionNone {
			hasChanges = true
			break
		}
	}

	for _, vol := range p.Volumes {
		if vol.Action != ActionNone {
			hasChanges = true
			break
		}
	}

	for _, cfg := range p.Configs {
		if cfg.Action != ActionNone {
			hasChanges = true
			break
		}
	}

	for _, sec := range p.Secrets {
		if sec.Action != ActionNone {
			hasChanges = true
			break
		}
	}

	for _, svc := range p.Services {
		if svc.Action != ActionNone {
			hasChanges = true
			break
		}
	}

	return !hasChanges && len(p.OrphanedServices) == 0 &&
		len(p.OrphanedNetworks) == 0 && len(p.OrphanedVolumes) == 0 &&
		len(p.OrphanedConfigs) == 0 && len(p.OrphanedSecrets) == 0
}

// CurrentState represents the current state of the stack in Swarm
type CurrentState struct {
	Services map[string]swarm.Service
	Networks map[string]swarm.Network
	Volumes  map[string]struct{} // Just names, volumes don't have full specs in swarm
	Configs  map[string]swarm.Config
	Secrets  map[string]swarm.Secret
}

// DesiredState represents the desired state from compose file
type DesiredState struct {
	Services map[string]*compose.Service
	Networks map[string]*compose.Network
	Volumes  map[string]*compose.Volume
	Configs  map[string]*compose.Config
	Secrets  map[string]*compose.Secret
}

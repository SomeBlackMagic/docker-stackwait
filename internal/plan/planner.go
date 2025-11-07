package plan

import (
	"context"

	"github.com/docker/docker/client"
)

// Planner creates deployment plans by comparing current and desired state
type Planner struct {
	client    *client.Client
	stackName string
}

// NewPlanner creates a new deployment planner
func NewPlanner(cli *client.Client, stackName string) *Planner {
	return &Planner{
		client:    cli,
		stackName: stackName,
	}
}

// CreatePlan compares current and desired state and creates a deployment plan
func (p *Planner) CreatePlan(ctx context.Context, current *CurrentState, desired *DesiredState) (*Plan, error) {
	plan := &Plan{
		StackName: p.stackName,
	}

	// Plan network changes
	plan.Networks = p.planNetworks(current, desired)

	// Plan volume changes
	plan.Volumes = p.planVolumes(current, desired)

	// Plan config changes
	plan.Configs = p.planConfigs(current, desired)

	// Plan secret changes
	plan.Secrets = p.planSecrets(current, desired)

	// Plan service changes
	plan.Services = p.planServices(current, desired)

	return plan, nil
}

// planNetworks determines network changes
func (p *Planner) planNetworks(current *CurrentState, desired *DesiredState) []NetworkAction {
	var actions []NetworkAction

	// Check for creates and updates
	for name, desiredNet := range desired.Networks {
		if currentNet, exists := current.Networks[name]; exists {
			// Network exists - check if update needed
			// For now, we don't update networks (would require recreate)
			actions = append(actions, NetworkAction{
				Name:      name,
				Action:    ActionNone,
				NetworkID: currentNet.ID,
				Driver:    currentNet.Driver,
				Labels:    currentNet.Labels,
			})
		} else {
			// Network doesn't exist - create
			actions = append(actions, NetworkAction{
				Name:   name,
				Action: ActionCreate,
				Driver: desiredNet.Driver,
				Labels: desiredNet.Labels,
			})
		}
	}

	// Check for deletes (orphaned networks)
	for name, currentNet := range current.Networks {
		if _, exists := desired.Networks[name]; !exists {
			actions = append(actions, NetworkAction{
				Name:      name,
				Action:    ActionDelete,
				NetworkID: currentNet.ID,
			})
		}
	}

	return actions
}

// planVolumes determines volume changes
func (p *Planner) planVolumes(current *CurrentState, desired *DesiredState) []VolumeAction {
	var actions []VolumeAction

	// Check for creates
	for name, desiredVol := range desired.Volumes {
		if _, exists := current.Volumes[name]; exists {
			// Volume exists - no action needed (volumes are immutable)
			actions = append(actions, VolumeAction{
				Name:   name,
				Action: ActionNone,
				Driver: desiredVol.Driver,
				Labels: desiredVol.Labels,
			})
		} else {
			// Volume doesn't exist - create
			actions = append(actions, VolumeAction{
				Name:   name,
				Action: ActionCreate,
				Driver: desiredVol.Driver,
				Labels: desiredVol.Labels,
			})
		}
	}

	// Check for deletes (orphaned volumes)
	for name := range current.Volumes {
		if _, exists := desired.Volumes[name]; !exists {
			actions = append(actions, VolumeAction{
				Name:   name,
				Action: ActionDelete,
			})
		}
	}

	return actions
}

// planConfigs determines config changes
func (p *Planner) planConfigs(current *CurrentState, desired *DesiredState) []ConfigAction {
	var actions []ConfigAction

	// Check for creates and updates
	for name, desiredCfg := range desired.Configs {
		if currentCfg, exists := current.Configs[name]; exists {
			// Config exists - configs are immutable, so we'd need to create new version
			// For now, treat as no change
			actions = append(actions, ConfigAction{
				Name:     name,
				Action:   ActionNone,
				ConfigID: currentCfg.ID,
				Labels:   currentCfg.Spec.Labels,
			})
		} else {
			// Config doesn't exist - create
			actions = append(actions, ConfigAction{
				Name:   name,
				Action: ActionCreate,
				Labels: desiredCfg.Labels,
				Data:   desiredCfg.Data,
			})
		}
	}

	// Check for deletes (orphaned configs)
	for name, currentCfg := range current.Configs {
		if _, exists := desired.Configs[name]; !exists {
			actions = append(actions, ConfigAction{
				Name:     name,
				Action:   ActionDelete,
				ConfigID: currentCfg.ID,
			})
		}
	}

	return actions
}

// planSecrets determines secret changes
func (p *Planner) planSecrets(current *CurrentState, desired *DesiredState) []SecretAction {
	var actions []SecretAction

	// Check for creates and updates
	for name, desiredSec := range desired.Secrets {
		if currentSec, exists := current.Secrets[name]; exists {
			// Secret exists - secrets are immutable, so we'd need to create new version
			// For now, treat as no change
			actions = append(actions, SecretAction{
				Name:     name,
				Action:   ActionNone,
				SecretID: currentSec.ID,
				Labels:   currentSec.Spec.Labels,
			})
		} else {
			// Secret doesn't exist - create
			actions = append(actions, SecretAction{
				Name:   name,
				Action: ActionCreate,
				Labels: desiredSec.Labels,
				Data:   desiredSec.Data,
			})
		}
	}

	// Check for deletes (orphaned secrets)
	for name, currentSec := range current.Secrets {
		if _, exists := desired.Secrets[name]; !exists {
			actions = append(actions, SecretAction{
				Name:     name,
				Action:   ActionDelete,
				SecretID: currentSec.ID,
			})
		}
	}

	return actions
}

// planServices determines service changes
func (p *Planner) planServices(current *CurrentState, desired *DesiredState) []ServiceAction {
	var actions []ServiceAction

	// Check for creates and updates
	for name, desiredSvc := range desired.Services {
		if currentSvc, exists := current.Services[name]; exists {
			// Service exists - check if update needed
			changes := compareServices(&currentSvc, desiredSvc)
			action := ActionNone
			if len(changes) > 0 {
				action = ActionUpdate
			}

			actions = append(actions, ServiceAction{
				Name:        name,
				Action:      action,
				ServiceID:   currentSvc.ID,
				CurrentSpec: &currentSvc.Spec,
				CurrentMeta: &currentSvc.Meta,
				Changes:     changes,
			})
		} else {
			// Service doesn't exist - create
			actions = append(actions, ServiceAction{
				Name:   name,
				Action: ActionCreate,
			})
		}
	}

	// Check for deletes (orphaned services)
	for name, currentSvc := range current.Services {
		if _, exists := desired.Services[name]; !exists {
			actions = append(actions, ServiceAction{
				Name:      name,
				Action:    ActionDelete,
				ServiceID: currentSvc.ID,
			})
		}
	}

	return actions
}

// compareServices compares current and desired service specs and returns list of changes
func compareServices(current *swarm.Service, desired *compose.Service) []string {
	var changes []string

	// For MVP, we'll do a simple comparison
	// In full implementation, this would compare all fields

	// Compare image
	if current.Spec.TaskTemplate.ContainerSpec != nil {
		currentImage := current.Spec.TaskTemplate.ContainerSpec.Image
		// Strip digest from current image for comparison
		// Docker adds @sha256:... to images, but compose files don't have it
		// This is a simplified comparison
		if currentImage != desired.Image {
			changes = append(changes, "image")
		}
	}

	// Compare replicas
	if current.Spec.Mode.Replicated != nil && current.Spec.Mode.Replicated.Replicas != nil {
		currentReplicas := *current.Spec.Mode.Replicated.Replicas
		if desired.Deploy != nil && desired.Deploy.Replicas != currentReplicas {
			changes = append(changes, "replicas")
		}
	}

	// More comparisons would go here...
	// For now, if we have any uncertainty, mark as changed
	if len(changes) == 0 && desired != nil {
		// Conservative approach: assume service needs update if we can't verify it's the same
		changes = append(changes, "configuration")
	}

	return changes
}

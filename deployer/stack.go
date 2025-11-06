package deployer

import (
	"context"
	"fmt"
	"log"

	"stackwait/compose"
)

type StackDeployer struct {
	cli                DockerClient
	stackName          string
	MaxFailedTaskCount int // Maximum number of failed tasks before giving up
}

func NewStackDeployer(cli DockerClient, stackName string, maxFailedTaskCount int) *StackDeployer {
	if maxFailedTaskCount <= 0 {
		maxFailedTaskCount = 3 // Default value
	}
	return &StackDeployer{
		cli:                cli,
		stackName:          stackName,
		MaxFailedTaskCount: maxFailedTaskCount,
	}
}

// Deploy deploys a complete stack from a compose file
func (d *StackDeployer) Deploy(ctx context.Context, composeFile *compose.ComposeFile) error {
	log.Printf("Starting deployment of stack: %s", d.stackName)

	// 1. Remove exited containers from previous deployments
	if err := d.RemoveExitedContainers(ctx); err != nil {
		return fmt.Errorf("failed to remove exited containers: %w", err)
	}

	// 2. Check for obsolete services and remove them
	if err := d.removeObsoleteServices(ctx, composeFile.Services); err != nil {
		return fmt.Errorf("failed to remove obsolete services: %w", err)
	}

	// 3. Pull images
	if err := d.pullImages(ctx, composeFile.Services); err != nil {
		return fmt.Errorf("failed to pull images: %w", err)
	}

	// 4. Create networks
	if err := d.createNetworks(ctx, composeFile.Networks); err != nil {
		return fmt.Errorf("failed to create networks: %w", err)
	}

	// 5. Create volumes
	if err := d.createVolumes(ctx, composeFile.Volumes); err != nil {
		return fmt.Errorf("failed to create volumes: %w", err)
	}

	// 6. Create/update services
	if err := d.deployServices(ctx, composeFile.Services); err != nil {
		return fmt.Errorf("failed to deploy services: %w", err)
	}

	log.Printf("Stack %s deployed successfully", d.stackName)
	return nil
}

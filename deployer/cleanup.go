package deployer

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"stackwait/compose"
)

// removeObsoleteServices removes services that exist in the stack but not in the compose file
func (d *StackDeployer) removeObsoleteServices(ctx context.Context, services map[string]*compose.Service) error {
	// Get current services in stack
	currentServices, err := d.GetStackServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list current services: %w", err)
	}

	if len(currentServices) == 0 {
		// No existing services, nothing to remove
		return nil
	}

	// Build map of desired service names
	desiredServices := make(map[string]bool)
	for name := range services {
		fullName := fmt.Sprintf("%s_%s", d.stackName, name)
		desiredServices[fullName] = true
	}

	// Find services to remove
	var servicesToRemove []swarm.Service
	for _, svc := range currentServices {
		if !desiredServices[svc.Spec.Name] {
			servicesToRemove = append(servicesToRemove, svc)
		}
	}

	if len(servicesToRemove) == 0 {
		log.Printf("No obsolete services to remove")
		return nil
	}

	// Remove obsolete services
	log.Printf("Found %d obsolete service(s) to remove", len(servicesToRemove))
	for _, svc := range servicesToRemove {
		log.Printf("Removing obsolete service: %s", svc.Spec.Name)
		if err := d.cli.ServiceRemove(ctx, svc.ID); err != nil {
			return fmt.Errorf("failed to remove service %s: %w", svc.Spec.Name, err)
		}
		log.Printf("Service %s marked for removal", svc.Spec.Name)
	}

	// Wait for services to be fully removed
	log.Printf("Waiting for services to be fully removed...")
	if err := d.waitForServicesRemoval(ctx, servicesToRemove); err != nil {
		return fmt.Errorf("failed to wait for service removal: %w", err)
	}
	log.Printf("All obsolete services removed successfully")

	return nil
}

// waitForServicesRemoval waits for services to be completely removed
func (d *StackDeployer) waitForServicesRemoval(ctx context.Context, services []swarm.Service) error {
	for _, svc := range services {
		log.Printf("Waiting for service %s to be removed...", svc.Spec.Name)

		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout waiting for service %s removal: %w", svc.Spec.Name, ctx.Err())
			default:
				// Check if service still exists
				_, _, err := d.cli.ServiceInspectWithRaw(ctx, svc.ID, types.ServiceInspectOptions{})
				if err != nil {
					// Service not found - it's been removed
					log.Printf("Service %s has been removed", svc.Spec.Name)
					break
				}

				// Service still exists, wait a bit
				continue
			}
			break
		}
	}

	return nil
}

// RemoveStack removes all resources associated with the stack
func (d *StackDeployer) RemoveStack(ctx context.Context) error {
	log.Printf("Removing stack: %s", d.stackName)

	// Remove services
	services, err := d.GetStackServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services {
		log.Printf("Removing service: %s", svc.Spec.Name)
		if err := d.cli.ServiceRemove(ctx, svc.ID); err != nil {
			log.Printf("Warning: failed to remove service %s: %v", svc.Spec.Name, err)
		}
	}

	// Remove networks
	networks, err := d.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", d.stackName)),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networks {
		log.Printf("Removing network: %s", net.Name)
		if err := d.cli.NetworkRemove(ctx, net.ID); err != nil {
			log.Printf("Warning: failed to remove network %s: %v", net.Name, err)
		}
	}

	log.Printf("Stack %s removed", d.stackName)
	return nil
}

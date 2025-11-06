package deployer

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"

	"stackwait/compose"
)

func (d *StackDeployer) deployServices(ctx context.Context, services map[string]*compose.Service) error {
	for name, svc := range services {
		if err := d.deployService(ctx, name, svc); err != nil {
			return fmt.Errorf("failed to deploy service %s: %w", name, err)
		}
	}

	return nil
}

func (d *StackDeployer) deployService(ctx context.Context, serviceName string, service *compose.Service) error {
	fullName := fmt.Sprintf("%s_%s", d.stackName, serviceName)

	// Convert compose service to swarm spec
	spec, err := compose.ConvertToSwarmSpec(serviceName, service, d.stackName)
	if err != nil {
		return fmt.Errorf("failed to convert service spec: %w", err)
	}

	// Attach to default network if no networks specified
	if service.Networks == nil {
		defaultNetwork := fmt.Sprintf("%s_default", d.stackName)
		spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{
			{Target: defaultNetwork},
		}
	}

	// Get registry auth for the image
	registryAuth := getRegistryAuth(service.Image)

	// Check if service exists
	existingServices, err := d.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("name", fullName),
		),
		Status: true,
	})

	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	if len(existingServices) > 0 {
		// Update existing service
		existing := existingServices[0]
		log.Printf("Updating service: %s", fullName)

		// Get current tasks before update to track recreation
		oldTasks, err := d.cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", existing.ID),
				filters.Arg("desired-state", "running"),
			),
		})
		if err != nil {
			return fmt.Errorf("failed to list old tasks: %w", err)
		}

		_, err = d.cli.ServiceUpdate(
			ctx,
			existing.ID,
			existing.Version,
			*spec,
			types.ServiceUpdateOptions{
				EncodedRegistryAuth: registryAuth,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to update service: %w", err)
		}

		// Wait a bit for Docker to process the update
		time.Sleep(1 * time.Second)

		// Check if tasks were actually recreated by comparing task IDs
		newTasks, err := d.cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", existing.ID),
				filters.Arg("desired-state", "running"),
			),
		})
		if err != nil {
			return fmt.Errorf("failed to list new tasks: %w", err)
		}

		// Build map of old task IDs
		oldTaskIDs := make(map[string]bool)
		for _, task := range oldTasks {
			oldTaskIDs[task.ID] = true
		}

		// Check if any new task appeared
		hasNewTasks := false
		for _, task := range newTasks {
			if !oldTaskIDs[task.ID] {
				hasNewTasks = true
				break
			}
		}

		if hasNewTasks {
			log.Printf("Service %s updated, waiting for tasks to be recreated...", fullName)
			// Wait for old tasks to be replaced with new ones
			if err := d.waitForServiceUpdate(ctx, existing.ID, oldTasks); err != nil {
				return fmt.Errorf("failed to wait for service update: %w", err)
			}
			log.Printf("Service %s update completed", fullName)
		} else {
			log.Printf("Service %s: no changes detected (tasks not recreated)", fullName)
		}
	} else {
		// Create new service
		log.Printf("Creating service: %s", fullName)

		_, err = d.cli.ServiceCreate(ctx, *spec, types.ServiceCreateOptions{
			EncodedRegistryAuth: registryAuth,
		})
		if err != nil {
			return fmt.Errorf("failed to create service: %w", err)
		}
		log.Printf("Service %s created", fullName)
	}

	return nil
}

// GetStackServices returns all services in the stack
func (d *StackDeployer) GetStackServices(ctx context.Context) ([]swarm.Service, error) {
	return d.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", d.stackName)),
		),
		Status: true,
	})
}

// waitForServiceUpdate waits for service tasks to be recreated after update
func (d *StackDeployer) waitForServiceUpdate(ctx context.Context, serviceID string, oldTasks []swarm.Task) error {
	// Build map of old task IDs
	oldTaskIDs := make(map[string]bool)
	for _, task := range oldTasks {
		oldTaskIDs[task.ID] = true
	}

	log.Printf("Tracking %d old tasks for service update", len(oldTasks))

	start := time.Now()
	timeout := 5 * time.Minute
	seenFailedTasks := make(map[string]bool)
	newFailedTaskCount := 0
	lastStatusLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for service update")
		default:
		}

		// Check timeout
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for service update after %v", timeout)
		}

		// Get current tasks
		currentTasks, err := d.cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", serviceID),
			),
		})
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		// Log task summary every 10 seconds
		if time.Since(lastStatusLog) > 10*time.Second {
			var taskStates []string
			newTaskCount := 0
			oldTaskCount := 0
			for _, task := range currentTasks {
				if oldTaskIDs[task.ID] {
					oldTaskCount++
					taskStates = append(taskStates, fmt.Sprintf("OLD-%s:%s", task.ID[:12], task.Status.State))
				} else {
					newTaskCount++
					taskStates = append(taskStates, fmt.Sprintf("NEW-%s:%s", task.ID[:12], task.Status.State))
				}
			}
			log.Printf("Task status: %d old, %d new. States: %v", oldTaskCount, newTaskCount, taskStates)
			lastStatusLog = time.Now()
		}

		// Check for failed new tasks
		for _, task := range currentTasks {
			// Skip old tasks
			if oldTaskIDs[task.ID] {
				continue
			}

			// Check if new task failed or completed abnormally
			// Tasks in 'complete' state with DesiredState=shutdown are tasks that were replaced
			// This happens when healthcheck fails
			isFailed := task.Status.State == swarm.TaskStateFailed ||
				task.Status.State == swarm.TaskStateRejected ||
				(task.Status.State == swarm.TaskStateShutdown && task.Status.Err != "") ||
				(task.Status.State == swarm.TaskStateComplete && task.DesiredState == swarm.TaskStateShutdown)

			if isFailed {
				// Log each failed task once
				if !seenFailedTasks[task.ID] {
					seenFailedTasks[task.ID] = true
					newFailedTaskCount++

					if task.Status.Err != "" {
						log.Printf("ERROR: New task %s failed with state %s (desired: %s): %s",
							task.ID[:12], task.Status.State, task.DesiredState, task.Status.Err)
					} else {
						log.Printf("ERROR: New task %s failed with state %s (desired: %s)",
							task.ID[:12], task.Status.State, task.DesiredState)
					}

					if task.Status.ContainerStatus.ExitCode != 0 {
						log.Printf("  Container exit code: %d", task.Status.ContainerStatus.ExitCode)
					}

					// Explain why task is considered failed
					if task.Status.State == swarm.TaskStateComplete && task.DesiredState == swarm.TaskStateShutdown {
						log.Printf("  Task was shutdown and replaced (likely healthcheck failure)")
					}
				}

				// If we have enough failed new tasks, give up
				if newFailedTaskCount >= d.MaxFailedTaskCount {
					return fmt.Errorf("service update failed: %d new tasks failed (healthcheck or startup failures)", newFailedTaskCount)
				}
			}
		}

		// Check if all old tasks are shutdown/completed
		oldTasksShutdown := true
		for _, task := range currentTasks {
			if oldTaskIDs[task.ID] {
				// Old task still exists - check if it's running
				if task.Status.State == swarm.TaskStateRunning {
					oldTasksShutdown = false
					break
				}
			}
		}

		// Check if new tasks exist and are running
		hasNewRunningTasks := false
		newTasksHealthy := true
		for _, task := range currentTasks {
			// Skip old tasks
			if oldTaskIDs[task.ID] {
				continue
			}

			// This is a new task
			if task.DesiredState == swarm.TaskStateRunning {
				if task.Status.State == swarm.TaskStateRunning {
					hasNewRunningTasks = true

					// Check container health if task has a container
					if task.Status.ContainerStatus.ContainerID != "" {
						containerID := task.Status.ContainerStatus.ContainerID
						inspect, err := d.cli.ContainerInspect(ctx, containerID)
						if err == nil {
							// If container has healthcheck, wait for it to be healthy
							if inspect.State.Health != nil {
								healthStatus := inspect.State.Health.Status
								// Allow "starting" as transitional state
								if healthStatus != "healthy" && healthStatus != "starting" {
									newTasksHealthy = false
								}
							}
						}
					}
				} else if task.Status.State != swarm.TaskStateComplete {
					// Task is not running yet and not complete - still starting
					hasNewRunningTasks = false
					break
				}
			}
		}

		// If old tasks are shutdown and new tasks are running and healthy, we're done
		if oldTasksShutdown && hasNewRunningTasks && newTasksHealthy {
			log.Printf("Service update completed: old tasks shutdown, new tasks running and healthy")
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}

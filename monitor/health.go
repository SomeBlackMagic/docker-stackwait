package monitor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	WaitHealthTimeout = 10 * time.Minute
	WaitInterval      = 3 * time.Second
	StatusLogInterval = 10 * time.Second
)

type HealthMonitor struct {
	cli                *client.Client
	stackName          string
	MaxFailedTaskCount int // Maximum number of failed tasks before giving up
}

func NewHealthMonitor(cli *client.Client, stackName string, maxFailedTaskCount int) *HealthMonitor {
	if maxFailedTaskCount <= 0 {
		maxFailedTaskCount = 3 // Default value
	}
	return &HealthMonitor{
		cli:                cli,
		stackName:          stackName,
		MaxFailedTaskCount: maxFailedTaskCount,
	}
}

// WaitHealthy waits for all containers in the stack to become healthy
func (m *HealthMonitor) WaitHealthy(ctx context.Context) bool {
	start := time.Now()
	lastLogTime := time.Now()
	failedTasks := make(map[string]int)      // Track failed tasks per service
	seenFailedTasks := make(map[string]bool) // Track which failed tasks we've already logged

	log.Println("Waiting for services to become healthy...")

	for {
		allRunning := true
		allHealthy := true
		hasHealthChecks := false

		// Get services in the stack
		services, err := m.cli.ServiceList(ctx, types.ServiceListOptions{
			Filters: filters.NewArgs(
				filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", m.stackName)),
			),
		})
		if err != nil {
			log.Printf("failed to list services: %v", err)
			return false
		}

		if len(services) == 0 {
			if time.Since(start) > 30*time.Second {
				log.Printf("No services found for stack %s after 30 seconds", m.stackName)
				return false
			}
			time.Sleep(WaitInterval)
			continue
		}

		count := 0
		var containerStatuses []string

		// Check containers for each service
		for _, svc := range services {
			// Get ALL tasks for this service (not just desired-state=running)
			// Retry with exponential backoff for API timeouts
			var allTasks []swarm.Task
			var err error
			maxRetries := 3
			for retry := 0; retry < maxRetries; retry++ {
				allTasks, err = m.cli.TaskList(ctx, types.TaskListOptions{
					Filters: filters.NewArgs(
						filters.Arg("service", svc.ID),
					),
				})
				if err == nil {
					break
				}
				if retry < maxRetries-1 {
					waitTime := time.Duration(retry+1) * time.Second
					log.Printf("failed to list tasks for service %s (attempt %d/%d): %v, retrying in %v",
						svc.Spec.Name, retry+1, maxRetries, err, waitTime)
					time.Sleep(waitTime)
				}
			}
			if err != nil {
				log.Printf("ERROR: failed to list tasks for service %s: %v", svc.Spec.Name, err)
				allRunning = false
				continue
			}

			// Check for failed/rejected tasks
			currentFailedCount := 0

			for _, task := range allTasks {
				// Count tasks that failed after the deployment started
				if task.Status.State == swarm.TaskStateFailed ||
					task.Status.State == swarm.TaskStateRejected ||
					(task.Status.State == swarm.TaskStateShutdown && task.Status.Err != "") {
					currentFailedCount++

					// Log each failed task once
					taskKey := fmt.Sprintf("%s-%s", svc.ID, task.ID)
					if !seenFailedTasks[taskKey] {
						seenFailedTasks[taskKey] = true

						if task.Status.Err != "" {
							log.Printf("ERROR: Service %s task %s failed with state %s: %s",
								svc.Spec.Name, task.ID[:12], task.Status.State, task.Status.Err)
						} else {
							log.Printf("ERROR: Service %s task %s failed with state %s",
								svc.Spec.Name, task.ID[:12], task.Status.State)
						}

						// Log container exit code if available
						if task.Status.ContainerStatus.ExitCode != 0 {
							log.Printf("  Container exit code: %d", task.Status.ContainerStatus.ExitCode)
						}
					}
				}
			}

			// Track if failed tasks are accumulating (sign of persistent failure)
			if currentFailedCount > 0 {
				prevCount, exists := failedTasks[svc.ID]
				failedTasks[svc.ID] = currentFailedCount

				// If we have enough failed tasks, it's a persistent failure
				if currentFailedCount >= m.MaxFailedTaskCount {
					log.Printf("Service %s has %d failed tasks - deployment likely failing", svc.Spec.Name, currentFailedCount)
					return false
				}

				// If failed count keeps growing, it's not recovering
				if exists && currentFailedCount > prevCount && time.Since(start) > 2*time.Minute {
					log.Printf("Service %s: failed tasks increasing (%d -> %d) - deployment not recovering",
						svc.Spec.Name, prevCount, currentFailedCount)
					return false
				}
			}

			// Now check running tasks
			for _, task := range allTasks {
				if task.Status.State != swarm.TaskStateRunning || task.DesiredState != swarm.TaskStateRunning {
					continue
				}

				// Get container ID from task
				containerID := task.Status.ContainerStatus.ContainerID
				if containerID == "" {
					continue
				}

				count++

				inspect, err := m.cli.ContainerInspect(ctx, containerID)
				if err != nil {
					log.Printf("failed to inspect container %s: %v", containerID[:12], err)
					allRunning = false
					continue
				}

				containerName := strings.TrimPrefix(inspect.Name, "/")

				// Check container status
				if inspect.State.Status != "running" {
					containerStatuses = append(containerStatuses, fmt.Sprintf("%s: %s", containerName, inspect.State.Status))
					allRunning = false
					continue
				}

				// Check container health if healthcheck exists
				if inspect.State.Health != nil {
					hasHealthChecks = true
					healthStatus := inspect.State.Health.Status
					containerStatuses = append(containerStatuses, fmt.Sprintf("%s: running/%s", containerName, healthStatus))

					// Allow "starting" as intermediate state
					if healthStatus != "healthy" && healthStatus != "starting" {
						allHealthy = false
					}
				} else {
					// Container without healthcheck is considered healthy if running
					containerStatuses = append(containerStatuses, fmt.Sprintf("%s: running/no-healthcheck", containerName))
				}
			}
		}

		// Log statuses every 10 seconds
		if time.Since(lastLogTime) > StatusLogInterval {
			if len(containerStatuses) > 0 {
				log.Printf("Container statuses: %s", strings.Join(containerStatuses, ", "))
			}
			lastLogTime = time.Now()
		}

		// If no containers found, wait for them to appear
		if count == 0 {
			if time.Since(start) > 30*time.Second {
				log.Printf("No running containers found for stack %s after 30 seconds", m.stackName)
				return false
			}
			time.Sleep(WaitInterval)
			continue
		}

		// Check if ready
		if allRunning && (allHealthy || !hasHealthChecks) {
			log.Printf("All containers are healthy (checked %d containers)", count)
			return true
		}

		// Check timeout
		if time.Since(start) > WaitHealthTimeout {
			log.Printf("Health check timeout reached. Final statuses: %s", strings.Join(containerStatuses, ", "))
			return false
		}

		time.Sleep(WaitInterval)
	}
}

// WaitServicesReady waits for all services to have running tasks
func (m *HealthMonitor) WaitServicesReady(ctx context.Context) error {
	services, err := m.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", m.stackName)),
		),
		Status: true,
	})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	if len(services) == 0 {
		return fmt.Errorf("no services found for stack %s", m.stackName)
	}

	log.Printf("Waiting for %d services to be ready...", len(services))

	start := time.Now()
	for {
		allReady := true

		for _, svc := range services {
			// Retry with exponential backoff for API timeouts
			var tasks []swarm.Task
			var err error
			maxRetries := 3
			for retry := 0; retry < maxRetries; retry++ {
				tasks, err = m.cli.TaskList(ctx, types.TaskListOptions{
					Filters: filters.NewArgs(
						filters.Arg("service", svc.ID),
						filters.Arg("desired-state", "running"),
					),
				})
				if err == nil {
					break
				}
				if retry < maxRetries-1 {
					waitTime := time.Duration(retry+1) * time.Second
					log.Printf("failed to list tasks for service %s (attempt %d/%d): %v, retrying in %v",
						svc.Spec.Name, retry+1, maxRetries, err, waitTime)
					time.Sleep(waitTime)
				}
			}
			if err != nil {
				return fmt.Errorf("failed to list tasks for service %s: %w", svc.Spec.Name, err)
			}

			runningTasks := 0
			for _, task := range tasks {
				if task.Status.State == swarm.TaskStateRunning {
					runningTasks++
				}
			}

			var desired uint64 = 1
			if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
				desired = *svc.Spec.Mode.Replicated.Replicas
			}

			if uint64(runningTasks) < desired {
				allReady = false
				log.Printf("Service %s: %d/%d tasks running", svc.Spec.Name, runningTasks, desired)
			}
		}

		if allReady {
			log.Println("All services have running tasks")
			return nil
		}

		if time.Since(start) > 5*time.Minute {
			return fmt.Errorf("timeout waiting for services to be ready")
		}

		time.Sleep(5 * time.Second)
	}
}

// GetServiceStatus returns the status of a specific service
func (m *HealthMonitor) GetServiceStatus(ctx context.Context, serviceName string) (*ServiceStatus, error) {
	fullName := fmt.Sprintf("%s_%s", m.stackName, serviceName)

	services, err := m.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("name", fullName),
		),
		Status: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("service %s not found", fullName)
	}

	svc := services[0]

	// Get tasks for the service
	// Retry with exponential backoff for API timeouts
	var tasks []swarm.Task
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		tasks, err = m.cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", svc.ID),
			),
		})
		if err == nil {
			break
		}
		if retry < maxRetries-1 {
			waitTime := time.Duration(retry+1) * time.Second
			log.Printf("failed to list tasks for service %s (attempt %d/%d): %v, retrying in %v",
				svc.Spec.Name, retry+1, maxRetries, err, waitTime)
			time.Sleep(waitTime)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	status := &ServiceStatus{
		Name:         svc.Spec.Name,
		ID:           svc.ID,
		Image:        svc.Spec.TaskTemplate.ContainerSpec.Image,
		Replicas:     0,
		DesiredTasks: 0,
		RunningTasks: 0,
		Tasks:        make([]TaskStatus, 0),
	}

	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		status.Replicas = *svc.Spec.Mode.Replicated.Replicas
	}

	for _, task := range tasks {
		taskStatus := TaskStatus{
			ID:           task.ID,
			State:        string(task.Status.State),
			DesiredState: string(task.DesiredState),
			Error:        task.Status.Err,
		}

		if task.DesiredState == swarm.TaskStateRunning {
			status.DesiredTasks++
		}

		if task.Status.State == swarm.TaskStateRunning {
			status.RunningTasks++
		}

		status.Tasks = append(status.Tasks, taskStatus)
	}

	return status, nil
}

type ServiceStatus struct {
	Name         string
	ID           string
	Image        string
	Replicas     uint64
	DesiredTasks int
	RunningTasks int
	Tasks        []TaskStatus
}

type TaskStatus struct {
	ID           string
	State        string
	DesiredState string
	Error        string
}

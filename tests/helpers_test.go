//go:build integration
// +build integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	testStackName = "stackman-test"
	composeFile   = "testdata/simple-stack.yml"
	testTimeout   = 3 * time.Minute
)

// isDockerAvailable checks if Docker is available and in swarm mode
func isDockerAvailable(t *testing.T) bool {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Logf("Docker client creation failed: %v", err)
		return false
	}
	defer cli.Close()

	// Check if swarm is initialized
	ctx := context.Background()
	info, err := cli.Info(ctx)
	if err != nil {
		t.Logf("Docker info failed: %v", err)
		return false
	}

	if !info.Swarm.ControlAvailable {
		t.Log("Docker is not in swarm mode")
		return false
	}

	return true
}

// cleanup removes test stack resources
func cleanup(t *testing.T) {
	cleanupStack(t, testStackName)
}

// cleanupStack removes all resources for a specific stack
func cleanupStack(t *testing.T, stackName string) {
	t.Logf("Cleaning up stack: %s", stackName)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Logf("Warning: Failed to create client for cleanup: %v", err)
		return
	}
	defer cli.Close()

	ctx := context.Background()

	// Remove all services in the stack
	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)

	services, err := cli.ServiceList(ctx, types.ServiceListOptions{Filters: filter})
	if err != nil {
		t.Logf("Warning: Failed to list services: %v", err)
		return
	}

	for _, service := range services {
		t.Logf("Removing service: %s", service.Spec.Name)
		if err := cli.ServiceRemove(ctx, service.ID); err != nil {
			t.Logf("Warning: Failed to remove service %s: %v", service.Spec.Name, err)
		}
	}

	// Wait for services to be removed
	time.Sleep(5 * time.Second)

	// Remove networks
	networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: filter})
	if err != nil {
		t.Logf("Warning: Failed to list networks: %v", err)
		return
	}

	for _, network := range networks {
		t.Logf("Removing network: %s", network.Name)
		if err := cli.NetworkRemove(ctx, network.ID); err != nil {
			t.Logf("Warning: Failed to remove network %s: %v", network.Name, err)
		}
	}
}

// verifyStack checks that the expected number of services exist for a stack
func verifyStack(t *testing.T, stackName string, expectedServices int) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	ctx := context.Background()
	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)

	services, err := cli.ServiceList(ctx, types.ServiceListOptions{Filters: filter})
	if err != nil {
		return err
	}

	if len(services) != expectedServices {
		t.Errorf("Expected %d services, found %d", expectedServices, len(services))
	}

	for _, service := range services {
		t.Logf("Service: %s (ID: %s)", service.Spec.Name, service.ID[:12])
	}

	return nil
}

// checkServicesHealth verifies all services in a stack are healthy
func checkServicesHealth(ctx context.Context, cli *client.Client, stackName string) (bool, error) {
	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)

	services, err := cli.ServiceList(ctx, types.ServiceListOptions{Filters: filter})
	if err != nil {
		return false, err
	}

	allHealthy := true
	for _, service := range services {
		// Get tasks for this service
		taskFilter := filters.NewArgs()
		taskFilter.Add("service", service.ID)
		taskFilter.Add("desired-state", "running")

		tasks, err := cli.TaskList(ctx, types.TaskListOptions{Filters: taskFilter})
		if err != nil {
			return false, err
		}

		serviceHealthy := false
		for _, task := range tasks {
			if task.Status.State == "running" {
				// Check container health if available
				if task.Status.ContainerStatus != nil && task.Status.ContainerStatus.ContainerID != "" {
					container, err := cli.ContainerInspect(ctx, task.Status.ContainerStatus.ContainerID)
					if err == nil {
						if container.State.Health == nil || container.State.Health.Status == "healthy" {
							serviceHealthy = true
							break
						}
					}
				} else {
					// No health check defined, running is enough
					serviceHealthy = true
					break
				}
			}
		}

		if !serviceHealthy {
			allHealthy = false
		}
	}

	return allHealthy, nil
}

// readFile reads file content
func readFile(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", path, err)
	}
	return string(data)
}

// writeFile writes content to file
func writeFile(t *testing.T, path, content string) {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", path, err)
	}
}

//go:build integration
// +build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	testStackName = "stackman-test"
	composeFile   = "testdata/simple-stack.yml"
	testTimeout   = 3 * time.Minute
)

// TestIntegration_FullDeploymentCycle tests complete deployment lifecycle
func TestIntegration_FullDeploymentCycle(t *testing.T) {
	// Check if Docker is available
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	// Ensure clean state
	cleanup(t)
	defer cleanup(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Build stackman binary
	t.Log("Building stackman binary...")
	buildCmd := exec.Command("go", "build", "-o", "stackman")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build stackman: %v\n%s", err, output)
	}

	// Test 1: Initial deployment
	t.Run("InitialDeployment", func(t *testing.T) {
		t.Log("Testing initial deployment...")

		cmd := exec.CommandContext(ctx, "./stackman", "apply",
			"-n", testStackName,
			"-f", composeFile,
			"-timeout", "2m",
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("Initial deployment failed: %v", err)
		}

		// Verify deployment
		if err := verifyStack(t, testStackName, 2); err != nil {
			t.Errorf("Stack verification failed: %v", err)
		}
	})

	// Test 2: Check services are healthy
	t.Run("VerifyServicesHealthy", func(t *testing.T) {
		t.Log("Verifying services are healthy...")

		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			t.Fatalf("Failed to create Docker client: %v", err)
		}
		defer cli.Close()

		// Wait for services to become healthy
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			healthy, err := checkServicesHealth(ctx, cli, testStackName)
			if err != nil {
				t.Logf("Health check error: %v", err)
			}
			if healthy {
				t.Log("All services are healthy!")
				return
			}
			time.Sleep(5 * time.Second)
		}

		t.Error("Services did not become healthy within timeout")
	})

	// Test 3: Update deployment (change replicas)
	t.Run("UpdateDeployment", func(t *testing.T) {
		t.Log("Testing deployment update...")

		// Create modified compose file with different replica count
		modifiedCompose := strings.ReplaceAll(readFile(t, composeFile),
			"replicas: 1", "replicas: 2")
		modifiedFile := "testdata/simple-stack-modified.yml"
		writeFile(t, modifiedFile, modifiedCompose)
		defer os.Remove(modifiedFile)

		cmd := exec.CommandContext(ctx, "./stackman", "apply",
			"-n", testStackName,
			"-f", modifiedFile,
			"-timeout", "2m",
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("Update deployment failed: %v", err)
		}

		// Verify updated replicas
		time.Sleep(10 * time.Second)
		if err := verifyStack(t, testStackName, 2); err != nil {
			t.Errorf("Stack verification after update failed: %v", err)
		}
	})

	// Test 4: Cleanup (implicit in defer)
	t.Run("Cleanup", func(t *testing.T) {
		t.Log("Testing cleanup...")
		// cleanup() will be called by defer
	})
}

// TestIntegration_PathResolution tests volume path resolution
func TestIntegration_PathResolution(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	// Create compose file with volume mounts
	composeWithVolumes := `version: "3.8"
services:
  test:
    image: alpine:latest
    command: sleep 300
    volumes:
      - ./testdata:/data:ro
      - test-volume:/app
    deploy:
      replicas: 1

volumes:
  test-volume:
    driver: local
`
	volumeComposeFile := "testdata/volume-test.yml"
	writeFile(t, volumeComposeFile, composeWithVolumes)
	defer os.Remove(volumeComposeFile)

	stackName := "stackman-volume-test"
	defer cleanupStack(t, stackName)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./stackman", "apply",
		"-n", stackName,
		"-f", volumeComposeFile,
		"-timeout", "1m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Deployment with volumes failed: %v", err)
	}

	// Verify service was created
	if err := verifyStack(t, stackName, 1); err != nil {
		t.Errorf("Stack with volumes verification failed: %v", err)
	}
}

// Helper functions

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

func cleanup(t *testing.T) {
	cleanupStack(t, testStackName)
}

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
	networks, err := cli.NetworkList(ctx, types.NetworkListOptions{Filters: filter})
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

func readFile(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", path, err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", path, err)
	}
}

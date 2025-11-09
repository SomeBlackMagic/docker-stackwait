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
	buildCmd := exec.Command("go", "build", "-o", "../stackman", "../")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build stackman: %v\n%s", err, output)
	}

	// Test 1: Initial deployment
	t.Run("InitialDeployment", func(t *testing.T) {
		t.Log("Testing initial deployment...")

		cmd := exec.CommandContext(ctx, "../stackman", "apply",
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

		cmd := exec.CommandContext(ctx, "../stackman", "apply",
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

// TestIntegration_UpdateConfig tests service update with update_config parameters
func TestIntegration_UpdateConfig(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-update-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Deploy initial version
	composeWithUpdate := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 2
      update_config:
        parallelism: 1
        delay: 5s
        order: start-first
        monitor: 10s
        max_failure_ratio: 0.2
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3

networks:
  default:
    driver: overlay
`
	updateComposeFile := "testdata/update-config.yml"
	writeFile(t, updateComposeFile, composeWithUpdate)
	defer os.Remove(updateComposeFile)

	t.Log("Deploying initial version...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", updateComposeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Initial deployment failed: %v", err)
	}

	// Update with new image
	t.Log("Updating to new image version...")
	updatedCompose := strings.ReplaceAll(composeWithUpdate, "nginx:alpine", "nginx:stable-alpine")
	writeFile(t, updateComposeFile, updatedCompose)

	cmd = exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", updateComposeFile,
		"-timeout", "3m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Update deployment failed: %v", err)
	}

	// Verify UpdateStatus.State == "completed"
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)
	services, err := cli.ServiceList(ctx, types.ServiceListOptions{Filters: filter})
	if err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}

	for _, service := range services {
		svc, _, err := cli.ServiceInspectWithRaw(ctx, service.ID, types.ServiceInspectOptions{})
		if err != nil {
			t.Fatalf("Failed to inspect service: %v", err)
		}

		if svc.UpdateStatus != nil {
			t.Logf("Service %s UpdateStatus.State: %s", service.Spec.Name, svc.UpdateStatus.State)
			if svc.UpdateStatus.State != "completed" && svc.UpdateStatus.State != "" {
				t.Errorf("Expected UpdateStatus.State to be 'completed', got '%s'", svc.UpdateStatus.State)
			}
		}
	}
}

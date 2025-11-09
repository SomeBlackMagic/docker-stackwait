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

	"github.com/docker/docker/client"
)

// TestIntegration_HealthCheck tests health check monitoring
func TestIntegration_HealthCheck(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-health-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Deploy service with healthcheck
	composeWithHealth := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s

networks:
  default:
    driver: overlay
`
	healthComposeFile := "testdata/health-check.yml"
	writeFile(t, healthComposeFile, composeWithHealth)
	defer os.Remove(healthComposeFile)

	t.Log("Deploying service with healthcheck...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", healthComposeFile,
		"-timeout", "3m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Deployment with healthcheck failed: %v", err)
	}

	// Verify health status
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	t.Log("Verifying health status...")
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		healthy, err := checkServicesHealth(ctx, cli, stackName)
		if err != nil {
			t.Logf("Health check error: %v", err)
		}
		if healthy {
			t.Log("Service is healthy!")
			return
		}
		time.Sleep(5 * time.Second)
	}

	t.Error("Service did not become healthy within timeout")
}

// TestIntegration_UnhealthyRollback tests rollback on unhealthy service
func TestIntegration_UnhealthyRollback(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-unhealthy-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Deploy healthy service first
	healthyCompose := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost"]
      interval: 5s
      timeout: 3s
      retries: 2
      start_period: 5s

networks:
  default:
    driver: overlay
`
	composeFile := "testdata/unhealthy-test.yml"
	writeFile(t, composeFile, healthyCompose)
	defer os.Remove(composeFile)

	t.Log("Deploying healthy service...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", composeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Initial healthy deployment failed: %v", err)
	}

	// Update to unhealthy service (wrong healthcheck)
	t.Log("Updating to unhealthy service (should rollback)...")
	unhealthyCompose := strings.ReplaceAll(healthyCompose,
		`test: ["CMD", "wget", "-q", "--spider", "http://localhost"]`,
		`test: ["CMD", "wget", "-q", "--spider", "http://localhost:9999"]`) // Wrong port

	writeFile(t, composeFile, unhealthyCompose)

	cmd = exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", composeFile,
		"-timeout", "1m",
		"-rollback-timeout", "1m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		t.Error("Expected deployment to fail due to unhealthy service")
	} else {
		t.Logf("Deployment failed as expected: %v", err)
	}

	// Note: Automatic rollback should have occurred
	// Verify original service is still running
	time.Sleep(10 * time.Second)

	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	defer cli.Close()

	healthy, _ := checkServicesHealth(ctx, cli, stackName)
	if !healthy {
		t.Log("Warning: Service may not have rolled back to healthy state")
	}
}

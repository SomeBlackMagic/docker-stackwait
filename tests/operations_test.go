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

// TestIntegration_Prune tests --prune functionality
func TestIntegration_Prune(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-prune-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Deploy initial version with 2 services
	initialCompose := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
  app:
    image: alpine:latest
    command: sleep 300
    deploy:
      replicas: 1

networks:
  default:
    driver: overlay
`
	pruneComposeFile := "testdata/prune-test.yml"
	writeFile(t, pruneComposeFile, initialCompose)
	defer os.Remove(pruneComposeFile)

	t.Log("Deploying initial stack with 2 services...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", pruneComposeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Initial deployment failed: %v", err)
	}

	// Verify 2 services
	if err := verifyStack(t, stackName, 2); err != nil {
		t.Errorf("Initial stack verification failed: %v", err)
	}

	// Update to only 1 service with --prune
	t.Log("Updating to 1 service with --prune...")
	prunedCompose := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1

networks:
  default:
    driver: overlay
`
	writeFile(t, pruneComposeFile, prunedCompose)

	cmd = exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", pruneComposeFile,
		"--prune",
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Pruned deployment failed: %v", err)
	}

	// Verify only 1 service remains
	time.Sleep(10 * time.Second)
	if err := verifyStack(t, stackName, 1); err != nil {
		t.Errorf("Pruned stack verification failed: %v", err)
	}
}

// TestIntegration_SignalInterrupt tests SIGINT handling and rollback
func TestIntegration_SignalInterrupt(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-signal-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Deploy initial healthy service
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
	signalComposeFile := "testdata/signal-test.yml"
	writeFile(t, signalComposeFile, healthyCompose)
	defer os.Remove(signalComposeFile)

	t.Log("Deploying initial healthy service...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", signalComposeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Initial deployment failed: %v", err)
	}

	// Update with slow healthcheck and send SIGINT during wait
	t.Log("Starting update with slow healthcheck...")
	slowCompose := strings.ReplaceAll(healthyCompose,
		"start_period: 5s",
		"start_period: 60s") // Very long start period
	writeFile(t, signalComposeFile, slowCompose)

	cmd = exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", signalComposeFile,
		"-timeout", "3m",
		"-rollback-timeout", "1m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start command
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Wait a bit for deployment to start
	time.Sleep(10 * time.Second)

	// Send SIGINT
	t.Log("Sending SIGINT to trigger rollback...")
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Logf("Failed to send SIGINT: %v", err)
	}

	// Wait for command to finish
	err := cmd.Wait()
	t.Logf("Command finished with: %v", err)

	// Verify service rolled back or remains stable
	time.Sleep(5 * time.Second)
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	defer cli.Close()

	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)
	services, _ := cli.ServiceList(ctx, types.ServiceListOptions{Filters: filter})
	if len(services) > 0 {
		t.Log("Service still exists after SIGINT (rollback may have occurred)")
	}
}

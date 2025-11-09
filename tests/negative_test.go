//go:build integration
// +build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestIntegration_NegativeTimeout tests timeout behavior
func TestIntegration_NegativeTimeout(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-timeout-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Deploy service that won't become healthy in time
	timeoutCompose := `version: "3.8"
services:
  slow:
    image: alpine:latest
    command: sh -c "sleep 200"  # Takes longer than timeout
    deploy:
      replicas: 1
    healthcheck:
      test: ["CMD", "false"]  # Always fails
      interval: 5s
      timeout: 3s
      retries: 2
      start_period: 5s

networks:
  default:
    driver: overlay
`
	timeoutComposeFile := "testdata/timeout-test.yml"
	writeFile(t, timeoutComposeFile, timeoutCompose)
	defer os.Remove(timeoutComposeFile)

	t.Log("Deploying service that will timeout (short timeout)...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", timeoutComposeFile,
		"-timeout", "30s",
		"-rollback-timeout", "30s",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		t.Error("Expected deployment to fail due to timeout")
	} else {
		t.Logf("Deployment timed out as expected: %v", err)
	}

	// Exit code should be 2 (timeout with successful rollback)
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode := exitErr.ExitCode()
		t.Logf("Exit code: %d", exitCode)
		if exitCode != 2 && exitCode != 0 {
			t.Logf("Warning: Expected exit code 2 or 0, got %d", exitCode)
		}
	}
}

// TestIntegration_NegativeInvalidCompose tests invalid compose handling
func TestIntegration_NegativeInvalidCompose(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-invalid-test"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Invalid compose (missing required fields)
	invalidCompose := `version: "3.8"
services:
  web:
    # Missing image!
    deploy:
      replicas: 1
`
	invalidComposeFile := "testdata/invalid-test.yml"
	writeFile(t, invalidComposeFile, invalidCompose)
	defer os.Remove(invalidComposeFile)

	t.Log("Trying to deploy invalid compose...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", invalidComposeFile,
		"-timeout", "1m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		t.Error("Expected deployment to fail due to invalid compose")
		cleanupStack(t, stackName)
	} else {
		t.Logf("Deployment failed as expected: %v", err)
	}

	// Exit code should be 1 (validation error)
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode := exitErr.ExitCode()
		t.Logf("Exit code: %d", exitCode)
		if exitCode != 1 {
			t.Logf("Warning: Expected exit code 1 (validation error), got %d", exitCode)
		}
	}
}

// TestIntegration_NegativeImageLatest tests --allow-latest protection
func TestIntegration_NegativeImageLatest(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-latest-test"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Compose with :latest tag
	latestCompose := `version: "3.8"
services:
  web:
    image: nginx:latest  # Should be rejected
    deploy:
      replicas: 1

networks:
  default:
    driver: overlay
`
	latestComposeFile := "testdata/latest-test.yml"
	writeFile(t, latestComposeFile, latestCompose)
	defer os.Remove(latestComposeFile)

	t.Log("Trying to deploy with :latest tag (should fail without --allow-latest)...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", latestComposeFile,
		"-timeout", "1m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		t.Error("Expected deployment to fail due to :latest tag without --allow-latest")
		cleanupStack(t, stackName)
	} else {
		t.Logf("Deployment rejected as expected: %v", err)
	}
}

//go:build integration
// +build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// TestIntegration_SecretsAndConfigs tests secrets and configs management
func TestIntegration_SecretsAndConfigs(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-secrets-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create test secret and config files
	secretContent := "my-secret-password"
	configContent := "server { listen 80; }"

	os.MkdirAll("testdata/secrets", 0755)
	secretFile := "testdata/secrets/db-password.txt"
	configFile := "testdata/secrets/nginx.conf"

	writeFile(t, secretFile, secretContent)
	writeFile(t, configFile, configContent)
	defer os.Remove(secretFile)
	defer os.Remove(configFile)

	composeWithSecrets := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    secrets:
      - source: db-password
        target: /run/secrets/db-password
        mode: 0400
    configs:
      - source: nginx-config
        target: /etc/nginx/conf.d/default.conf
        mode: 0644

secrets:
  db-password:
    file: ./testdata/secrets/db-password.txt

configs:
  nginx-config:
    file: ./testdata/secrets/nginx.conf

networks:
  default:
    driver: overlay
`
	secretsComposeFile := "testdata/secrets-test.yml"
	writeFile(t, secretsComposeFile, composeWithSecrets)
	defer os.Remove(secretsComposeFile)

	t.Log("Deploying service with secrets and configs...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", secretsComposeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Deployment with secrets/configs failed: %v", err)
	}

	// Verify secrets and configs were created
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)

	secrets, err := cli.SecretList(ctx, types.SecretListOptions{Filters: filter})
	if err != nil {
		t.Fatalf("Failed to list secrets: %v", err)
	}
	if len(secrets) == 0 {
		t.Error("No secrets found for stack")
	} else {
		t.Logf("Found %d secret(s)", len(secrets))
	}

	configs, err := cli.ConfigList(ctx, types.ConfigListOptions{Filters: filter})
	if err != nil {
		t.Fatalf("Failed to list configs: %v", err)
	}
	if len(configs) == 0 {
		t.Error("No configs found for stack")
	} else {
		t.Logf("Found %d config(s)", len(configs))
	}
}

// TestIntegration_VolumesAndPaths tests volume mounting and path resolution
func TestIntegration_VolumesAndPaths(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-volumes-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create test data directory
	os.MkdirAll("testdata/data", 0755)
	testFile := "testdata/data/test.txt"
	writeFile(t, testFile, "test data")
	defer os.Remove(testFile)

	composeWithVolumes := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    volumes:
      - ./testdata/data:/usr/share/nginx/html:ro
      - app-data:/var/lib/data

volumes:
  app-data:
    driver: local

networks:
  default:
    driver: overlay
`
	volumesComposeFile := "testdata/volumes-test.yml"
	writeFile(t, volumesComposeFile, composeWithVolumes)
	defer os.Remove(volumesComposeFile)

	t.Log("Deploying service with volumes...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", volumesComposeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Deployment with volumes failed: %v", err)
	}

	// Verify volumes were created
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)

	volumes, err := cli.VolumeList(ctx, volume.ListOptions{Filters: filter})
	if err != nil {
		t.Fatalf("Failed to list volumes: %v", err)
	}
	if len(volumes.Volumes) == 0 {
		t.Error("No volumes found for stack")
	} else {
		t.Logf("Found %d volume(s)", len(volumes.Volumes))
	}

	// Verify service has mounts
	if err := verifyStack(t, stackName, 1); err != nil {
		t.Errorf("Stack verification failed: %v", err)
	}
}

// TestIntegration_OverlayNetworks tests overlay network creation
func TestIntegration_OverlayNetworks(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-networks-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	composeWithNetworks := `version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    networks:
      - frontend
      - backend

  app:
    image: alpine:latest
    command: sleep 300
    deploy:
      replicas: 1
    networks:
      - backend

networks:
  frontend:
    driver: overlay
    labels:
      description: "Frontend network"
  backend:
    driver: overlay
    labels:
      description: "Backend network"
`
	networksComposeFile := "testdata/networks-test.yml"
	writeFile(t, networksComposeFile, composeWithNetworks)
	defer os.Remove(networksComposeFile)

	t.Log("Deploying services with overlay networks...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", networksComposeFile,
		"-timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Deployment with networks failed: %v", err)
	}

	// Verify networks were created
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+stackName)

	networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: filter})
	if err != nil {
		t.Fatalf("Failed to list networks: %v", err)
	}

	expectedNetworks := 2
	if len(networks) < expectedNetworks {
		t.Errorf("Expected at least %d networks, found %d", expectedNetworks, len(networks))
	} else {
		t.Logf("Found %d network(s)", len(networks))
		for _, net := range networks {
			if net.Driver != "overlay" {
				t.Errorf("Expected overlay network, got %s", net.Driver)
			}
		}
	}
}

package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseComposeFile(t *testing.T) {
	// Create temporary compose file
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")

	composeContent := `version: '3.8'
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    deploy:
      replicas: 2
networks:
  default:
    driver: overlay
volumes:
  data:
    driver: local
`

	err := os.WriteFile(composeFile, []byte(composeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test compose file: %v", err)
	}

	// Parse the file
	result, err := ParseComposeFile(composeFile)
	if err != nil {
		t.Fatalf("ParseComposeFile failed: %v", err)
	}

	// Verify results
	if result.Version != "3.8" {
		t.Errorf("Expected version 3.8, got %s", result.Version)
	}

	if len(result.Services) != 1 {
		t.Errorf("Expected 1 service, got %d", len(result.Services))
	}

	webService, ok := result.Services["web"]
	if !ok {
		t.Fatal("Expected 'web' service not found")
	}

	if webService.Image != "nginx:latest" {
		t.Errorf("Expected image nginx:latest, got %s", webService.Image)
	}

	if webService.Deploy == nil || webService.Deploy.Replicas == nil || *webService.Deploy.Replicas != 2 {
		t.Error("Expected replicas to be 2")
	}

	if len(result.Networks) != 1 {
		t.Errorf("Expected 1 network, got %d", len(result.Networks))
	}

	if len(result.Volumes) != 1 {
		t.Errorf("Expected 1 volume, got %d", len(result.Volumes))
	}
}

func TestParseComposeFile_NonExistent(t *testing.T) {
	_, err := ParseComposeFile("/non/existent/file.yml")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestParseComposeFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "invalid.yml")

	invalidContent := `
services:
  web:
    image: nginx
    invalid yaml structure [[[
`

	err := os.WriteFile(composeFile, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err = ParseComposeFile(composeFile)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

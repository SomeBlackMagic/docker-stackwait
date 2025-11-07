package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConvertVolumes_PathResolution(t *testing.T) {
	// Save and restore working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	tests := []struct {
		name           string
		volumeSpec     string
		expectedSource string // relative to tmpDir
		isAbsolute     bool
	}{
		{
			name:           "relative with ./",
			volumeSpec:     "./data:/app/data",
			expectedSource: "data",
			isAbsolute:     false,
		},
		{
			name:           "simple relative",
			volumeSpec:     "data:/app/data",
			expectedSource: "data",
			isAbsolute:     false,
		},
		{
			name:           "absolute path",
			volumeSpec:     "/var/log:/logs",
			expectedSource: "/var/log",
			isAbsolute:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volumes := []interface{}{tt.volumeSpec}
			mounts, err := convertVolumes(volumes)

			if err != nil {
				t.Fatalf("convertVolumes() error = %v", err)
			}

			if len(mounts) != 1 {
				t.Fatalf("Expected 1 mount, got %d", len(mounts))
			}

			mount := mounts[0]

			if tt.isAbsolute {
				if mount.Source != tt.expectedSource {
					t.Errorf("Expected source %q, got %q", tt.expectedSource, mount.Source)
				}
			} else {
				expectedAbs := filepath.Join(tmpDir, tt.expectedSource)
				if mount.Source != expectedAbs {
					t.Errorf("Expected source %q, got %q", expectedAbs, mount.Source)
				}
			}
		})
	}
}

func TestConvertVolumes_WithSWARMSTACKPATH(t *testing.T) {
	// Save and restore env
	originalEnv := os.Getenv("SWARM_STACK_PATH")
	defer func() {
		if originalEnv != "" {
			os.Setenv("SWARM_STACK_PATH", originalEnv)
		} else {
			os.Unsetenv("SWARM_STACK_PATH")
		}
	}()

	// Set custom base path
	customPath := "/custom/base"
	os.Setenv("SWARM_STACK_PATH", customPath)

	volumes := []interface{}{"./data:/app/data"}
	mounts, err := convertVolumes(volumes)

	if err != nil {
		t.Fatalf("convertVolumes() error = %v", err)
	}

	if len(mounts) != 1 {
		t.Fatalf("Expected 1 mount, got %d", len(mounts))
	}

	expected := filepath.Join(customPath, "data")
	if mounts[0].Source != expected {
		t.Errorf("Expected source %q, got %q", expected, mounts[0].Source)
	}
}

func TestConvertVolumes_ReadOnly(t *testing.T) {
	volumes := []interface{}{"/var/log:/logs:ro"}
	mounts, err := convertVolumes(volumes)

	if err != nil {
		t.Fatalf("convertVolumes() error = %v", err)
	}

	if len(mounts) != 1 {
		t.Fatalf("Expected 1 mount, got %d", len(mounts))
	}

	if !mounts[0].ReadOnly {
		t.Error("Expected mount to be read-only")
	}
}

package compose

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParseComposeFile reads and parses a docker-compose.yml file
func ParseComposeFile(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Set defaults
	if compose.Services == nil {
		compose.Services = make(map[string]*Service)
	}
	if compose.Networks == nil {
		compose.Networks = make(map[string]*Network)
	}
	if compose.Volumes == nil {
		compose.Volumes = make(map[string]*Volume)
	}

	return &compose, nil
}

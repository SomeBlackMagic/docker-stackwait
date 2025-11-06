package monitor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type HealthLogStreamer struct {
	cli                *client.Client
	stackName          string
	lastLogs           map[string]int  // containerID -> last processed log index
	existingContainers map[string]bool // containerID -> exists (to ignore initial logs)
}

func NewHealthLogStreamer(cli *client.Client, stackName string) *HealthLogStreamer {
	return &HealthLogStreamer{
		cli:                cli,
		stackName:          stackName,
		lastLogs:           make(map[string]int),
		existingContainers: make(map[string]bool),
	}
}

// StreamHealthLogs monitors and outputs health check logs for containers with health checks
func (hls *HealthLogStreamer) StreamHealthLogs(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	log.Println("Starting health log monitoring...")

	// First scan: identify existing containers and skip their logs
	hls.initExistingContainers(ctx)

	// Check immediately on start
	hls.checkHealthLogs(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping health log monitoring")
			return
		case <-ticker.C:
			hls.checkHealthLogs(ctx)
		}
	}
}

func (hls *HealthLogStreamer) initExistingContainers(ctx context.Context) {
	// Get all containers
	list, err := hls.cli.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return
	}

	for _, c := range list {
		// Filter by service name prefix
		serviceName := c.Labels["com.docker.swarm.service.name"]
		if !strings.HasPrefix(serviceName, hls.stackName+"_") {
			continue
		}

		// Mark as existing and skip to end of health logs
		hls.existingContainers[c.ID] = true

		// Inspect to get current health log count
		inspect, err := hls.cli.ContainerInspect(ctx, c.ID)
		if err == nil && inspect.State.Health != nil {
			hls.lastLogs[c.ID] = len(inspect.State.Health.Log)
		}
	}

	if len(hls.existingContainers) > 0 {
		log.Printf("HealthLogStreamer: Found %d existing containers for stack %s (will not stream their logs)", len(hls.existingContainers), hls.stackName)
	}
}

func (hls *HealthLogStreamer) checkHealthLogs(ctx context.Context) {
	// Get all containers (including non-running ones)
	list, err := hls.cli.ContainerList(ctx, container.ListOptions{
		All: true, // Include all containers, not just running ones
	})
	if err != nil {
		log.Printf("[HealthLog] failed to list containers: %v", err)
		return
	}

	totalContainers := 0
	containersWithHealth := 0
	containersWithLogs := 0
	totalNewLogs := 0

	for _, c := range list {
		// Filter by service name prefix (same approach as LogStreamer)
		serviceName := c.Labels["com.docker.swarm.service.name"]
		if !strings.HasPrefix(serviceName, hls.stackName+"_") {
			continue
		}

		totalContainers++
		// Inspect container to get health check info
		inspect, err := hls.cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			log.Printf("[HealthLog] failed to inspect container %s: %v", c.ID[:12], err)
			continue
		}

		containerName := strings.TrimPrefix(inspect.Name, "/")

		// Skip containers without health checks
		if inspect.State.Health == nil {
			continue
		}

		containersWithHealth++
		health := inspect.State.Health

		// Check if there are health check logs
		if len(health.Log) == 0 {
			continue
		}

		containersWithLogs++

		// Get the last processed log index for this container
		lastProcessedIndex := hls.lastLogs[c.ID]

		// Process all new log entries for this container
		newLogsCount := 0
		for i := lastProcessedIndex; i < len(health.Log); i++ {
			logEntry := health.Log[i]
			hls.outputHealthLog(containerName, health.Status, logEntry)
			newLogsCount++
			totalNewLogs++
		}

		// Update the last processed index for this container
		hls.lastLogs[c.ID] = len(health.Log)
	}
}

func (hls *HealthLogStreamer) outputHealthLog(containerName, status string, logEntry *container.HealthcheckResult) {
	// Format the timestamp
	start := logEntry.Start.Format("2006-01-02T15:04:05")
	end := logEntry.End.Format("2006-01-02T15:04:05")

	// Prepare the output message
	exitCodeStr := fmt.Sprintf("exit_code=%d", logEntry.ExitCode)

	// Trim and clean the output
	output := strings.TrimSpace(logEntry.Output)

	// Output health check result
	if logEntry.ExitCode == 0 {
		// Successful health check
		if output != "" {
			firstLine := strings.Split(output, "\n")[0]
			if len(firstLine) > 100 {
				firstLine = firstLine[:100] + "..."
			}
			log.Printf("[HEALTH] %s | status=%s %s | start=%s end=%s | output: %s",
				containerName, status, exitCodeStr, start, end, firstLine)
		} else {
			log.Printf("[HEALTH] %s | status=%s %s | start=%s end=%s",
				containerName, status, exitCodeStr, start, end)
		}
	} else {
		// Failed health check
		if output != "" {
			lines := strings.Split(output, "\n")
			maxLines := 5
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				output = strings.Join(lines, "\n") + "\n... (truncated)"
			} else {
				output = strings.Join(lines, "\n")
			}

			log.Printf("[HEALTH] %s | status=%s %s | start=%s end=%s | OUTPUT:\n%s",
				containerName, status, exitCodeStr, start, end, output)
		} else {
			log.Printf("[HEALTH] %s | status=%s %s | start=%s end=%s | (no output)",
				containerName, status, exitCodeStr, start, end)
		}
	}
}

// GetHealthStatus returns the current health status of all containers in the stack
func (hls *HealthLogStreamer) GetHealthStatus(ctx context.Context) (map[string]*HealthStatus, error) {
	result := make(map[string]*HealthStatus)

	list, err := hls.cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", hls.stackName)),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range list {
		inspect, err := hls.cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}

		if inspect.State.Health == nil {
			continue
		}

		containerName := strings.TrimPrefix(inspect.Name, "/")
		health := inspect.State.Health

		status := &HealthStatus{
			ContainerName: containerName,
			Status:        health.Status,
			FailingStreak: health.FailingStreak,
		}

		if len(health.Log) > 0 {
			latestLog := health.Log[len(health.Log)-1]
			status.LastCheck = latestLog.End
			status.LastExitCode = latestLog.ExitCode
			status.LastOutput = strings.TrimSpace(latestLog.Output)
		}

		result[containerName] = status
	}

	return result, nil
}

type HealthStatus struct {
	ContainerName string
	Status        string
	FailingStreak int
	LastCheck     time.Time
	LastExitCode  int
	LastOutput    string
}

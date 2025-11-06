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
	cli       *client.Client
	stackName string
	lastLogs  map[string]int // containerID -> last processed log index
}

func NewHealthLogStreamer(cli *client.Client, stackName string) *HealthLogStreamer {
	return &HealthLogStreamer{
		cli:       cli,
		stackName: stackName,
		lastLogs:  make(map[string]int),
	}
}

// StreamHealthLogs monitors and outputs health check logs for containers with health checks
func (hls *HealthLogStreamer) StreamHealthLogs(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	log.Println("Starting health log monitoring...")

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

func (hls *HealthLogStreamer) checkHealthLogs(ctx context.Context) {
	// Get all containers for the stack
	list, err := hls.cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", hls.stackName)),
		),
	})
	if err != nil {
		log.Printf("failed to list containers for health logs: %v", err)
		return
	}

	for _, c := range list {
		// Inspect container to get health check info
		inspect, err := hls.cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}

		// Skip containers without health checks
		if inspect.State.Health == nil {
			continue
		}

		containerName := strings.TrimPrefix(inspect.Name, "/")
		health := inspect.State.Health

		// Check if there are health check logs
		if len(health.Log) == 0 {
			continue
		}

		// Get the last processed log index for this container
		lastProcessedIndex := hls.lastLogs[c.ID]

		// Process all new log entries for this container
		for i := lastProcessedIndex; i < len(health.Log); i++ {
			logEntry := health.Log[i]
			hls.outputHealthLog(containerName, health.Status, logEntry)
		}

		// Update the last processed index for this container
		hls.lastLogs[c.ID] = len(health.Log)
	}
}

func (hls *HealthLogStreamer) outputHealthLog(containerName, status string, logEntry *container.HealthcheckResult) {
	// Format the timestamp
	timestamp := logEntry.End.Format("15:04:05")

	// Prepare the output message
	exitCodeStr := fmt.Sprintf("exit_code=%d", logEntry.ExitCode)

	// Trim and clean the output
	output := strings.TrimSpace(logEntry.Output)

	// For successful health checks, show a compact message
	if logEntry.ExitCode == 0 {
		if output != "" {
			// Show first line of output if available
			firstLine := strings.Split(output, "\n")[0]
			if len(firstLine) > 100 {
				firstLine = firstLine[:100] + "..."
			}
			fmt.Printf("[health:%s] %s | status=%s %s | %s\n",
				containerName, timestamp, status, exitCodeStr, firstLine)
		} else {
			fmt.Printf("[health:%s] %s | status=%s %s\n",
				containerName, timestamp, status, exitCodeStr)
		}
	} else {
		// For failed health checks, show more detail
		if output != "" {
			// Show multiple lines for errors, but limit to reasonable size
			lines := strings.Split(output, "\n")
			maxLines := 5
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				output = strings.Join(lines, "\n") + "\n... (truncated)"
			} else {
				output = strings.Join(lines, "\n")
			}

			fmt.Printf("[health:%s] %s | status=%s %s | OUTPUT:\n%s\n",
				containerName, timestamp, status, exitCodeStr, output)
		} else {
			fmt.Printf("[health:%s] %s | status=%s %s | (no output)\n",
				containerName, timestamp, status, exitCodeStr)
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

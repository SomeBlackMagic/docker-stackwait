package monitor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type LogStreamer struct {
	cli       *client.Client
	stackName string
}

func NewLogStreamer(cli *client.Client, stackName string) *LogStreamer {
	return &LogStreamer{
		cli:       cli,
		stackName: stackName,
	}
}

// StreamLogs streams logs from all containers in the stack
func (ls *LogStreamer) StreamLogs(ctx context.Context) {
	// Continuously monitor for new containers and stream their logs
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	trackedContainers := make(map[string]time.Time) // Track container ID -> creation time

	// First scan: identify existing containers but don't stream their logs
	containers, err := ls.cli.ContainerList(ctx, container.ListOptions{
		All: false,
	})
	if err == nil {
		for _, c := range containers {
			serviceName := c.Labels["com.docker.swarm.service.name"]
			if strings.HasPrefix(serviceName, ls.stackName+"_") {
				// Mark as tracked with creation time, but don't stream logs
				trackedContainers[c.ID] = time.Unix(c.Created, 0)
			}
		}
		log.Printf("LogStreamer: Found %d existing containers for stack %s (will not stream their logs)", len(trackedContainers), ls.stackName)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get list of ALL containers, then filter by service name prefix
			containers, err := ls.cli.ContainerList(ctx, container.ListOptions{
				All: false, // Only running containers
			})
			if err != nil {
				log.Printf("failed to list containers for stack %s: %v", ls.stackName, err)
				continue
			}

			// Filter containers by service name prefix
			var stackContainers []types.Container
			for _, c := range containers {
				serviceName := c.Labels["com.docker.swarm.service.name"]
				if strings.HasPrefix(serviceName, ls.stackName+"_") {
					stackContainers = append(stackContainers, c)
				}
			}

			// Start streaming logs for new containers only
			for _, c := range stackContainers {
				if _, exists := trackedContainers[c.ID]; !exists {
					// New container detected
					trackedContainers[c.ID] = time.Unix(c.Created, 0)
					serviceName := c.Labels["com.docker.swarm.service.name"]
					containerName := c.Names[0]
					if len(containerName) > 0 && containerName[0] == '/' {
						containerName = containerName[1:]
					}
					log.Printf("Found new container: %s (service: %s)", containerName, serviceName)
					go ls.streamContainerLogs(ctx, c.ID, serviceName, containerName)
				}
			}
		}
	}
}

func (ls *LogStreamer) streamContainerLogs(ctx context.Context, containerID, serviceName, containerName string) {
	// Get container creation time
	inspect, err := ls.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		log.Printf("failed to inspect container %s: %v", containerName, err)
		return
	}

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Since:      inspect.Created, // Show logs from container creation time
		Timestamps: false,
	}

	reader, err := ls.cli.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		log.Printf("failed to get logs for container %s: %v", containerName, err)
		return
	}
	defer reader.Close()

	// Create pipe readers for stdout and stderr
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	// Demultiplex Docker stream in background
	go func() {
		defer stdoutWriter.Close()
		defer stderrWriter.Close()
		_, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, reader)
		if err != nil && err != io.EOF && ctx.Err() == nil {
			log.Printf("stdcopy error for container %s: %v", containerName, err)
		}
	}()

	// Stream stdout
	go ls.streamPipe(ctx, stdoutReader, serviceName, "stdout")

	// Stream stderr
	ls.streamPipe(ctx, stderrReader, serviceName, "stderr")
}

func (ls *LogStreamer) streamPipe(ctx context.Context, r io.Reader, serviceName, streamType string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				fmt.Printf("[logs:%s] %s\n", serviceName, line)
			}
		}
	}
}

// PrintLogWithService outputs lines from reader with specified prefix
func PrintLogWithService(prefix string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Printf("[%s] %s\n", prefix, line)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("scanner error for %s: %v", prefix, err)
	}
}

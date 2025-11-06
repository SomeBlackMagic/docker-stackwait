package monitor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
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

// StreamLogs streams logs from all services in the stack
func (ls *LogStreamer) StreamLogs(ctx context.Context) {
	// Small delay to let services start
	time.Sleep(3 * time.Second)

	// Get list of services for the stack
	services, err := ls.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", ls.stackName)),
		),
	})
	if err != nil {
		log.Printf("failed to list services for stack %s: %v", ls.stackName, err)
		return
	}

	if len(services) == 0 {
		log.Printf("no services found for stack %s", ls.stackName)
		return
	}

	log.Printf("Starting log streaming for %d services...", len(services))

	// Stream logs for each service
	for _, service := range services {
		go ls.streamServiceLogs(ctx, service.ID, service.Spec.Name)
	}
}

func (ls *LogStreamer) streamServiceLogs(ctx context.Context, serviceID, serviceName string) {
	log.Printf("Starting logs for service: %s", serviceName)

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "0", // Only show new logs
		Timestamps: false,
	}

	reader, err := ls.cli.ServiceLogs(ctx, serviceID, opts)
	if err != nil {
		log.Printf("failed to get logs for service %s: %v", serviceName, err)
		return
	}
	defer reader.Close()

	// Create a scanner to read logs line by line
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Printf("Stopping logs for service: %s", serviceName)
			return
		default:
			line := scanner.Text()
			if line != "" {
				// Docker service logs are prefixed with 8 bytes header
				// Skip the header if present
				if len(line) > 8 {
					line = line[8:]
				}
				fmt.Printf("[logs:%s] %s\n", serviceName, line)
			}
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		log.Printf("scanner error for service %s: %v", serviceName, err)
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

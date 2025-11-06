package monitor

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type EventStreamer struct {
	cli                *client.Client
	stackName          string
	pendingExecStart   map[string]*events.Message // Track exec_start events waiting for exec_die
	existingContainers map[string]bool            // Track existing containers to ignore their events
}

func NewEventStreamer(cli *client.Client, stackName string) *EventStreamer {
	return &EventStreamer{
		cli:                cli,
		stackName:          stackName,
		pendingExecStart:   make(map[string]*events.Message),
		existingContainers: make(map[string]bool),
	}
}

// StreamEvents streams Docker events for containers, services, and tasks in the stack
func (es *EventStreamer) StreamEvents(ctx context.Context) {
	// First scan: identify existing containers to ignore their events
	containers, err := es.cli.ContainerList(ctx, container.ListOptions{
		All: false,
	})
	if err == nil {
		for _, c := range containers {
			serviceName := c.Labels["com.docker.swarm.service.name"]
			if strings.HasPrefix(serviceName, es.stackName+"_") {
				// Mark as existing - we'll ignore events from these containers
				es.existingContainers[c.ID] = true
			}
		}
		log.Printf("EventStreamer: Found %d existing containers for stack %s (will ignore their events)", len(es.existingContainers), es.stackName)
	}

	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("type", "service")
	f.Add("type", "node")

	msgs, errs := es.cli.Events(ctx, events.ListOptions{Filters: f})

	for {
		select {
		case e := <-msgs:
			switch e.Type {
			case events.ContainerEventType:
				svc := e.Actor.Attributes["com.docker.swarm.service.name"]
				stackNs := e.Actor.Attributes["com.docker.stack.namespace"]

				// Check both service name prefix and stack namespace
				if stackNs != es.stackName && !strings.HasPrefix(svc, es.stackName+"_") {
					continue
				}

				// Get container ID from the event
				// For exec events, the Actor.ID is the exec ID, not container ID
				// For container events, Actor.ID is the container ID
				containerID := e.Actor.ID

				name := e.Actor.Attributes["name"]
				if name == "" {
					name = e.Actor.ID[:12]
				}

				// For exec events, we need to get the actual container ID
				// The Actor.ID for exec events is the exec ID, not container ID
				// We need to find the container ID by inspecting
				if strings.HasPrefix(string(e.Action), "exec_") {
					// For exec events, we'll need to check container by inspecting the exec
					// But Docker doesn't provide easy way to get container ID from exec ID in events
					// So we'll extract container ID from the container name in attributes
					// Container name format: stackname_servicename.replica.taskid
					// We need to find this container and get its ID

					// Try to find container by name
					if name != "" {
						foundContainers, err := es.cli.ContainerList(ctx, container.ListOptions{
							All:     true,
							Filters: filters.NewArgs(filters.Arg("name", name)),
						})
						if err == nil && len(foundContainers) > 0 {
							actualContainerID := foundContainers[0].ID
							// Check if this is an existing container
							if es.existingContainers[actualContainerID] {
								// Ignore healthcheck events from existing containers
								continue
							}
						}
					}
				} else {
					// For non-exec container events, check if this is an existing container
					if e.Action == "create" {
						// New container created - remove from existing containers list if present
						delete(es.existingContainers, containerID)
					} else if es.existingContainers[containerID] {
						// Ignore events from existing containers
						continue
					}
				}

				// Handle exec events (healthcheck)
				if strings.HasPrefix(string(e.Action), "exec_start:") {
					// execID is in the Actor.ID for exec events
					execID := e.Actor.ID
					// Store exec_start event to pair with exec_die
					es.pendingExecStart[execID] = &e
					continue
				}

				if e.Action == "exec_die" {
					// Match exec_die with exec_start using Actor.ID
					execID := e.Actor.ID
					if startEvent, ok := es.pendingExecStart[execID]; ok {
						// Found matching exec_start, combine them
						exitCode := e.Actor.Attributes["exitCode"]
						status := "failed"
						if exitCode == "0" {
							status = "passed"
						}
						// Extract command from the action (exec_start: <command>)
						cmd := strings.TrimPrefix(string(startEvent.Action), "exec_start: ")
						if cmd == "" {
							cmd = "healthcheck"
						}
						fmt.Printf("[event:container:%s] healthcheck %s (exit %s): %s\n", name, status, exitCode, cmd)
						delete(es.pendingExecStart, execID)
						continue
					}
				}

				// Log other important container events
				if shouldLogEvent(string(e.Action)) {
					fmt.Printf("[event:container:%s] %s\n", name, e.Action)
				}

			case events.ServiceEventType:
				svcName := e.Actor.Attributes["name"]
				if !strings.HasPrefix(svcName, es.stackName+"_") {
					continue
				}

				// Log all service events
				fmt.Printf("[event:service:%s] %s\n", svcName, e.Action)

				// Show additional details for updates
				if e.Action == "update" {
					if updateState := e.Actor.Attributes["updatestate.new"]; updateState != "" {
						fmt.Printf("[event:service:%s] update state: %s\n", svcName, updateState)
					}
				}

			case events.NodeEventType:
				nodeName := e.Actor.Attributes["name"]
				if nodeName == "" {
					nodeName = e.Actor.ID[:12]
				}

				// Log node events (availability, state changes)
				if shouldLogNodeEvent(string(e.Action)) {
					fmt.Printf("[event:node:%s] %s\n", nodeName, e.Action)
				}
			}

		case err := <-errs:
			if err != nil && ctx.Err() == nil {
				fmt.Printf("[event:error] %v\n", err)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

// shouldLogEvent determines if a container event should be logged
func shouldLogEvent(action string) bool {
	// Skip exec events as they are handled separately
	if strings.HasPrefix(action, "exec_") {
		return false
	}

	importantEvents := []string{
		"create",
		"start",
		"die",
		"kill",
		"stop",
		"restart",
		"oom",
	}

	for _, event := range importantEvents {
		if strings.Contains(action, event) {
			return true
		}
	}

	return false
}

// shouldLogNodeEvent determines if a node event should be logged
func shouldLogNodeEvent(action string) bool {
	importantEvents := []string{
		"update",
		"remove",
	}

	for _, event := range importantEvents {
		if strings.Contains(action, event) {
			return true
		}
	}

	return false
}

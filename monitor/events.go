package monitor

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type EventStreamer struct {
	cli              *client.Client
	stackName        string
	pendingExecStart map[string]*events.Message // Track exec_start events waiting for exec_die
}

func NewEventStreamer(cli *client.Client, stackName string) *EventStreamer {
	return &EventStreamer{
		cli:              cli,
		stackName:        stackName,
		pendingExecStart: make(map[string]*events.Message),
	}
}

// StreamEvents streams Docker events for containers, services, and tasks in the stack
func (es *EventStreamer) StreamEvents(ctx context.Context) {
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
				if !strings.HasPrefix(svc, es.stackName+"_") {
					continue
				}
				name := e.Actor.Attributes["name"]

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

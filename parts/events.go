package parts

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// StreamEvents обрабатывает события Docker контейнеров для указанного стека
func StreamEvents(ctx context.Context, cli *client.Client, stack string) {
	f := filters.NewArgs()
	f.Add("type", "container")

	msgs, errs := cli.Events(ctx, events.ListOptions{Filters: f})
	for {
		select {
		case e := <-msgs:
			if e.Type == events.ContainerEventType {
				svc := e.Actor.Attributes["com.docker.swarm.service.name"]
				if !strings.HasPrefix(svc, stack+"_") {
					continue
				}
				//name := e.Actor.Attributes["name"]
				//fmt.Printf("[event:%s] %s\n", name, e.Action)
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

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// -----------------------------------
var (
	version  string = "dev"
	revision string = "000000000000000000000000000000"
)

//-----------------------------------

type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
	Networks map[string]ComposeNetwork `yaml:"networks,omitempty"`
}

type ComposeService struct {
	Image       string            `yaml:"image"`
	Command     []string          `yaml:"command,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Deploy      *DeployConfig     `yaml:"deploy,omitempty"`
	Ports       []any             `yaml:"ports,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
}

type DeployConfig struct {
	Replicas *uint64 `yaml:"replicas,omitempty"`
}

type ComposeNetwork struct {
	Driver string `yaml:"driver,omitempty"`
}

const (
	waitHealthTimeout = 10 * time.Minute
	waitInterval      = 3 * time.Second
)

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Usage: %s <stack-name> <docker-compose.yml>", os.Args[0])
	}

	log.Printf("Start Docker Stack Wait version=%s revision=%s", version, revision)

	stack := os.Args[1]
	file := os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), waitHealthTimeout)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatalf("docker client init: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		log.Fatalf("cannot read file: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		log.Fatalf("yaml parse error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go streamEvents(ctx, cli, stack, &wg)
	go streamLogs(ctx, cli, stack)

	prevImages := map[string]string{}

	// --- Networks ---
	for name, netCfg := range compose.Networks {
		netName := fmt.Sprintf("%s_%s", stack, name)
		fmt.Printf("Ensuring network %s (driver=%s)\n", netName, netCfg.Driver)
		_, err := cli.NetworkInspect(ctx, netName, types.NetworkInspectOptions{})
		if err != nil {
			_, err = cli.NetworkCreate(ctx, netName, types.NetworkCreate{
				Driver:     netCfg.Driver,
				Attachable: true,
				Labels: map[string]string{
					"com.docker.stack.namespace": stack,
				},
			})
			if err != nil {
				log.Printf("network %s: %v\n", netName, err)
			}
		}
	}

	// --- Services (create or update) ---
	for name, svc := range compose.Services {
		serviceName := fmt.Sprintf("%s_%s", stack, name)
		spec := buildSpec(stack, serviceName, svc)
		existing, _, err := cli.ServiceInspectWithRaw(ctx, serviceName, types.ServiceInspectOptions{})
		if err == nil && existing.ID != "" {
			prevImages[serviceName] = existing.Spec.TaskTemplate.ContainerSpec.Image
			fmt.Printf("Updating service %s (%s)\n", serviceName, svc.Image)
			_, err = cli.ServiceUpdate(ctx, existing.ID, existing.Version, spec, types.ServiceUpdateOptions{})
			if err != nil {
				log.Printf("update %s failed: %v\n", serviceName, err)
			} else {
				fmt.Printf("Service %s updated.\n", serviceName)
			}
		} else {
			fmt.Printf("Creating service %s (%s)\n", serviceName, svc.Image)
			_, err = cli.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
			if err != nil {
				log.Printf("service %s create error: %v\n", serviceName, err)
			} else {
				fmt.Printf("Service %s created.\n", serviceName)
			}
		}
	}

	fmt.Println("Waiting for services to become healthy...")

	if !waitHealthy(ctx, cli, stack) {
		fmt.Println("ERROR: Services failed healthcheck or didn't start in time.")
		rollback(ctx, cli, prevImages)
		os.Exit(1)
	}

	fmt.Println("All containers healthy.")
	cancel()
	wg.Wait()
}

// --- Service builder ---
func buildSpec(stack, name string, svc ComposeService) swarm.ServiceSpec {
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: name,
			Labels: map[string]string{
				"com.docker.stack.namespace": stack,
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:   svc.Image,
				Env:     toEnvList(svc.Environment),
				Command: svc.Command,
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{
				Replicas: replicas(svc.Deploy),
			},
		},
	}
	if len(svc.Networks) > 0 {
		spec.TaskTemplate.Networks = make([]swarm.NetworkAttachmentConfig, len(svc.Networks))
		for i, n := range svc.Networks {
			spec.TaskTemplate.Networks[i] = swarm.NetworkAttachmentConfig{
				Target: fmt.Sprintf("%s_%s", stack, n),
			}
		}
	}
	if len(svc.Ports) > 0 {
		spec.EndpointSpec = &swarm.EndpointSpec{
			Mode:  swarm.ResolutionModeVIP,
			Ports: parsePorts(svc.Ports),
		}
	}
	return spec
}

func replicas(d *DeployConfig) *uint64 {
	if d != nil && d.Replicas != nil {
		return d.Replicas
	}
	v := uint64(1)
	return &v
}

func toEnvList(m map[string]string) []string {
	res := make([]string, 0, len(m))
	for k, v := range m {
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res
}

// --- Rollback ---
func rollback(ctx context.Context, cli *client.Client, old map[string]string) {
	fmt.Println("Starting rollback...")
	for svc, img := range old {
		fmt.Printf("Rollback %s → %s\n", svc, img)
		existing, _, err := cli.ServiceInspectWithRaw(ctx, svc, types.ServiceInspectOptions{})
		if err != nil {
			fmt.Printf("rollback %s failed: %v\n", svc, err)
			continue
		}
		existing.Spec.TaskTemplate.ContainerSpec.Image = img
		_, err = cli.ServiceUpdate(ctx, existing.ID, existing.Version, existing.Spec, types.ServiceUpdateOptions{})
		if err != nil {
			fmt.Printf("rollback update %s failed: %v\n", svc, err)
		}
	}
}

// --- Healthcheck watcher ---
func waitHealthy(ctx context.Context, cli *client.Client, stack string) bool {
	start := time.Now()
	for {
		allHealthy := true
		list, err := cli.ContainerList(ctx, container.ListOptions{})
		if err != nil {
			return false
		}
		count := 0
		for _, c := range list {
			if !hasLabel(c.Labels, "com.docker.stack.namespace", stack) {
				continue
			}
			count++
			inspect, err := cli.ContainerInspect(ctx, c.ID)
			if err != nil {
				allHealthy = false
				continue
			}
			if inspect.State.Status != "running" {
				allHealthy = false
			}
			if inspect.State.Health != nil && inspect.State.Health.Status != "healthy" {
				allHealthy = false
			}
		}
		if allHealthy && count > 0 {
			return true
		}
		if time.Since(start) > waitHealthTimeout {
			return false
		}
		time.Sleep(waitInterval)
	}
}

func hasLabel(m map[string]string, key, val string) bool {
	v, ok := m[key]
	return ok && v == val
}

// --- Events ---
func streamEvents(ctx context.Context, cli *client.Client, stack string, wg *sync.WaitGroup) {
	defer wg.Done()
	f := filters.NewArgs()
	f.Add("type", "container")

	msgs, errs := cli.Events(ctx, types.EventsOptions{Filters: f})
	for {
		select {
		case e := <-msgs:
			if e.Type == events.ContainerEventType {
				svc := e.Actor.Attributes["com.docker.swarm.service.name"]
				if !strings.HasPrefix(svc, stack+"_") {
					continue
				}
				name := e.Actor.Attributes["name"]
				fmt.Printf("[event:%s] %s\n", name, e.Action)
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

// --- Logs ---
func streamLogs(ctx context.Context, cli *client.Client, stack string) {
	known := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			list, err := cli.ContainerList(ctx, container.ListOptions{})
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			for _, c := range list {
				svc := c.Labels["com.docker.swarm.service.name"]
				if !strings.HasPrefix(svc, stack+"_") {
					continue
				}
				if known[c.ID] {
					continue
				}
				known[c.ID] = true
				go followContainerLogs(ctx, cli, c.ID, strings.TrimPrefix(c.Names[0], "/"))
			}
			time.Sleep(3 * time.Second)
		}
	}
}

func followContainerLogs(ctx context.Context, cli *client.Client, id, name string) {
	r, err := cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "10",
	})
	if err != nil {
		return
	}
	defer r.Close()

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	go func() {
		defer stdoutWriter.Close()
		defer stderrWriter.Close()
		_, _ = stdcopy.StdCopy(stdoutWriter, stderrWriter, r)
	}()
	go printLog(fmt.Sprintf("logs:%s", name), stdoutReader)
	go printLog(fmt.Sprintf("logs:%s", name), stderrReader)
}

func printLog(prefix string, r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		fmt.Printf("[%s] %s\n", prefix, line)
	}
}

// --- Ports parser ---
func parsePorts(entries []any) []swarm.PortConfig {
	var res []swarm.PortConfig
	for _, e := range entries {
		switch v := e.(type) {
		case string:
			var proto string = "tcp"
			var pub, tgt uint32
			if n, _ := fmt.Sscanf(v, "%d:%d/%s", &pub, &tgt, &proto); n >= 2 {
				res = append(res, swarm.PortConfig{
					Protocol:      swarm.PortConfigProtocol(proto),
					PublishedPort: pub,
					TargetPort:    tgt,
				})
			} else if n, _ := fmt.Sscanf(v, "%d:%d", &pub, &tgt); n == 2 {
				res = append(res, swarm.PortConfig{
					Protocol:      "tcp",
					PublishedPort: pub,
					TargetPort:    tgt,
				})
			}
		case map[string]any:
			pc := swarm.PortConfig{Protocol: "tcp", PublishMode: swarm.PortConfigPublishModeIngress}
			if val, ok := v["target"].(int); ok {
				pc.TargetPort = uint32(val)
			}
			if val, ok := v["published"].(int); ok {
				pc.PublishedPort = uint32(val)
			}
			if val, ok := v["protocol"].(string); ok {
				pc.Protocol = swarm.PortConfigProtocol(val)
			}
			if val, ok := v["mode"].(string); ok && val == "host" {
				pc.PublishMode = swarm.PortConfigPublishModeHost
			}
			res = append(res, pc)
		}
	}
	return res
}

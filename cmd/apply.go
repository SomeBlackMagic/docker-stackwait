package cmd

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"stackman/internal/compose"
	"stackman/internal/health"
	"stackman/internal/snapshot"
	"stackman/internal/swarm"
)

// ExecuteApply runs the apply command
func ExecuteApply(args []string) {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)

	// Required flags
	stackName := fs.String("n", "", "Stack name (required)")
	composeFile := fs.String("f", "", "Compose file path (required)")

	// Optional flags
	valuesFile := fs.String("values", "", "Values file for templating")
	setValues := fs.String("set", "", "Set values (comma-separated key=value pairs)")
	timeout := fs.Duration("timeout", 15*time.Minute, "Deployment timeout")
	rollbackTimeout := fs.Duration("rollback-timeout", 10*time.Minute, "Rollback timeout")
	noWait := fs.Bool("no-wait", false, "Don't wait for deployment to complete")
	prune := fs.Bool("prune", false, "Remove orphaned resources")
	allowLatest := fs.Bool("allow-latest", false, "Allow 'latest' tag in images")
	parallel := fs.Int("parallel", 1, "Number of parallel service updates")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: stackman apply -n <stack> -f <compose-file> [flags]

Deploy or update a Docker Swarm stack.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Validate required flags
	if *stackName == "" {
		fmt.Fprintf(os.Stderr, "Error: -n (stack name) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	if *composeFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -f (compose file) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	// Run apply logic
	if err := runApply(*stackName, *composeFile, &ApplyOptions{
		ValuesFile:      *valuesFile,
		SetValues:       *setValues,
		Timeout:         *timeout,
		RollbackTimeout: *rollbackTimeout,
		NoWait:          *noWait,
		Prune:           *prune,
		AllowLatest:     *allowLatest,
		Parallel:        *parallel,
	}); err != nil {
		log.Fatalf("Apply failed: %v", err)
	}
}

// ApplyOptions contains options for the apply command
type ApplyOptions struct {
	ValuesFile      string
	SetValues       string
	Timeout         time.Duration
	RollbackTimeout time.Duration
	NoWait          bool
	Prune           bool
	AllowLatest     bool
	Parallel        int
}

// runApply performs the actual deployment
func runApply(stackName, composeFile string, opts *ApplyOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout+5*time.Minute)
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client init: %w", err)
	}
	defer cli.Close()

	// Parse compose file
	log.Printf("Parsing compose file: %s", composeFile)
	composeSpec, err := compose.ParseComposeFile(composeFile)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	// TODO: Apply templating if valuesFile or setValues provided

	// Create deployer
	stackDeployer := swarm.NewStackDeployer(cli, stackName, 3)

	// Create snapshot before deployment
	snap := snapshot.CreateSnapshot(ctx, stackDeployer)

	// Track deployment state
	deploymentComplete := make(chan bool, 1)

	// Handle signals
	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)

		select {
		case <-deploymentComplete:
			log.Println("Deployment already completed, exiting...")
			os.Exit(0)
		default:
			log.Println("Deployment interrupted, initiating rollback...")
			snapshot.Rollback(context.Background(), stackDeployer, snap)
			os.Exit(130)
		}
	}()

	// Deploy stack
	log.Printf("Deploying stack: %s", stackName)
	deployResult, err := stackDeployer.Deploy(ctx, composeSpec)
	if err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	fmt.Println("Stack deployed successfully.")

	// If --no-wait, exit now
	if opts.NoWait {
		deploymentComplete <- true
		return nil
	}

	// Wait for services to become healthy
	if len(deployResult.UpdatedServices) > 0 {
		log.Printf("Waiting for %d service(s) to become healthy...", len(deployResult.UpdatedServices))

		// Wait for service update to complete
		if err := waitForServiceUpdates(ctx, cli, deployResult.UpdatedServices); err != nil {
			log.Printf("Service update failed: %v", err)
			snapshot.Rollback(context.Background(), stackDeployer, snap)
			return err
		}

		// Wait for tasks to become healthy
		healthCtx, healthCancel := context.WithTimeout(ctx, opts.Timeout)
		defer healthCancel()

		if err := waitForAllTasksHealthy(healthCtx, cli, stackName, deployResult.UpdatedServices); err != nil {
			log.Printf("Health check failed: %v", err)
			snapshot.Rollback(context.Background(), stackDeployer, snap)
			return err
		}

		log.Println("All services are healthy!")
	} else {
		log.Println("No services were changed during this deployment")
	}

	// Mark deployment as successful
	deploymentComplete <- true

	return nil
}

// waitForServiceUpdates waits for all service updates to complete
func waitForServiceUpdates(ctx context.Context, cli *client.Client, services []swarm.ServiceUpdateResult) error {
	for _, svc := range services {
		monitor := health.NewServiceUpdateMonitor(cli, svc.ServiceID, svc.ServiceName)
		if err := monitor.WaitForUpdateComplete(ctx); err != nil {
			return fmt.Errorf("service %s update failed: %w", svc.ServiceName, err)
		}
		log.Printf("✅ Service %s update completed", svc.ServiceName)
	}
	return nil
}

// waitForAllTasksHealthy waits for all tasks of updated services to become healthy
func waitForAllTasksHealthy(ctx context.Context, cli *client.Client, stackName string, updatedServices []swarm.ServiceUpdateResult) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime).Round(time.Second)
			return fmt.Errorf("timeout after %v waiting for services to become healthy", elapsed)

		case <-ticker.C:
			allHealthy := true
			unhealthyTasks := []string{}

			for _, svc := range updatedServices {
				filter := filters.NewArgs()
				filter.Add("service", svc.ServiceID)
				filter.Add("desired-state", "running")

				tasks, err := cli.TaskList(ctx, types.TaskListOptions{
					Filters: filter,
				})
				if err != nil {
					log.Printf("Failed to list tasks for service %s: %v", svc.ServiceName, err)
					allHealthy = false
					continue
				}

				serviceHealthy := false
				for _, t := range tasks {
					// Skip old tasks
					if t.Version.Index < svc.Version.Index {
						continue
					}

					// Check if task is running
					if t.Status.State != dockerswarm.TaskStateRunning {
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s (state: %s)", svc.ServiceName, t.Status.State))
						continue
					}

					// Check container health if available
					if t.Status.ContainerStatus != nil && t.Status.ContainerStatus.ContainerID != "" {
						containerInfo, err := cli.ContainerInspect(ctx, t.Status.ContainerStatus.ContainerID)
						if err != nil {
							log.Printf("Failed to inspect container for %s: %v", svc.ServiceName, err)
							allHealthy = false
							continue
						}

						// If health check is defined, wait for healthy status
						if containerInfo.State.Health != nil {
							if containerInfo.State.Health.Status != "healthy" {
								allHealthy = false
								unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s (health: %s)", svc.ServiceName, containerInfo.State.Health.Status))
							} else {
								serviceHealthy = true
							}
						} else {
							// No health check, running is enough
							serviceHealthy = true
						}
					}
				}

				if !serviceHealthy {
					allHealthy = false
				}
			}

			if allHealthy {
				return nil
			}

			if len(unhealthyTasks) > 0 {
				log.Printf("Waiting for: %v", unhealthyTasks)
			}
		}
	}
}

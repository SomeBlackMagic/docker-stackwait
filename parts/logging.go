package parts

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// StreamLogs запускает потоковый вывод логов для всех сервисов в стеке
func StreamLogs(ctx context.Context, cli *client.Client, stack string) {
	// Небольшая задержка чтобы дать время сервисам запуститься
	time.Sleep(3 * time.Second)

	// Получаем список сервисов для стека
	services, err := cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stack)),
	})
	if err != nil {
		log.Printf("failed to list services for stack %s: %v", stack, err)
		return
	}

	if len(services) == 0 {
		log.Printf("no services found for stack %s", stack)
		return
	}

	fmt.Printf("Starting log streaming for %d services...\n", len(services))

	// Запускаем логи для каждого сервиса
	for _, service := range services {
		go func(serviceName string) {
			// Используем --tail=0 --follow для получения только новых логов (без истории)
			cmd := exec.Command("docker", "service", "logs", "--follow", "--no-task-ids", "--tail", "0", serviceName)

			fmt.Printf("Starting logs for service: %s\n", serviceName)

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				log.Printf("failed to create stdout pipe for service %s: %v", serviceName, err)
				return
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				log.Printf("failed to create stderr pipe for service %s: %v", serviceName, err)
				return
			}

			if err := cmd.Start(); err != nil {
				log.Printf("failed to start logs command for service %s: %v", serviceName, err)
				return
			}

			go printLogWithService(fmt.Sprintf("logs:%s", serviceName), stdout)
			go printLogWithService(fmt.Sprintf("logs:%s", serviceName), stderr)

			go func() {
				defer func() {
					if cmd.Process != nil {
						cmd.Process.Kill()
					}
				}()
				select {
				case <-ctx.Done():
					fmt.Printf("Stopping logs for service: %s\n", serviceName)
					return
				}
			}()

			err = cmd.Wait()
			if err != nil && ctx.Err() == nil {
				log.Printf("logs command for service %s finished with error: %v", serviceName, err)
			}
		}(service.Spec.Name)
	}
}

// printLog выводит строки из reader с указанным префиксом
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

// printLogWithService выводит строки из reader с указанным префиксом и обрабатывает ошибки сканера
func printLogWithService(prefix string, r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		fmt.Printf("[%s] %s\n", prefix, line)
	}
	// Если сканер завершился с ошибкой, логируем это
	if err := s.Err(); err != nil {
		log.Printf("scanner error for %s: %v", prefix, err)
	}
}

// FilterDockerOutput фильтрует повторяющиеся сообщения от docker stack deploy
func FilterDockerOutput(prefix string, r io.Reader) {
	s := bufio.NewScanner(r)
	lastLine := ""
	lastVerifySeconds := ""

	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}

		// Фильтруем повторяющиеся сообщения о прогрессе
		if strings.Contains(line, "overall progress:") && line == lastLine {
			continue
		}

		// Фильтруем другие сообщения о прогрессе
		if strings.Contains(line, "progress:") && line == lastLine {
			continue
		}

		// Фильтруем повторяющиеся сообщения verify
		if strings.Contains(line, "verify: Waiting") && strings.Contains(line, "seconds to verify that tasks are stable") {
			// Извлекаем количество секунд из сообщения
			parts := strings.Split(line, " ")
			if len(parts) >= 3 {
				currentSeconds := parts[2] // "Waiting X seconds..."
				if currentSeconds == lastVerifySeconds {
					continue // Пропускаем, если количество секунд не изменилось
				}
				lastVerifySeconds = currentSeconds
			}
		}

		fmt.Printf("[%s] %s\n", prefix, line)
		lastLine = line
	}
}

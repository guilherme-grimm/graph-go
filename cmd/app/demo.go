package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const demoComposeFile = "docker-compose.demo.yml"

var (
	lookPath     = exec.LookPath
	runDemoCheck = func(cmd *exec.Cmd) error {
		return cmd.Run()
	}
	runDemoOutput = func(cmd *exec.Cmd) ([]byte, error) {
		return cmd.Output()
	}
)

var demoPorts = []int{8080, 5432, 27017, 9000, 9001}

func newDemoCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "demo",
		Short:        "Boot the seeded demo stack with Docker Compose.",
		Long:         "demo is a repo-scoped onboarding command. It runs the seeded Docker Compose stack from docker-compose.demo.yml so you can explore graph-go against a realistic environment immediately.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDemo(cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func runDemo(stdout, stderr io.Writer) error {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}

	if err := checkDockerCLI(); err != nil {
		return err
	}
	if err := checkDockerCompose(repoRoot); err != nil {
		return err
	}
	if err := checkDemoPorts(repoRoot); err != nil {
		return err
	}

	composeFile := filepath.Join(repoRoot, demoComposeFile)
	if err := writeDemoIntro(stdout, repoRoot, composeFile); err != nil {
		return fmt.Errorf("write demo intro: %w", err)
	}

	cmd := exec.Command("docker", "compose", "-f", composeFile, "up", "--build")
	cmd.Dir = repoRoot
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run demo stack: %w\nhint: ensure ports 8080, 5432, 27017, 9000, and 9001 are free, then tear down any prior demo stack with `docker compose -f docker-compose.demo.yml down`", err)
	}

	return nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	return findRepoRootFrom(wd)
}

func findRepoRootFrom(start string) (string, error) {
	current := start
	for {
		composeFile := filepath.Join(current, demoComposeFile)
		goMod := filepath.Join(current, "go.mod")
		if fileExists(composeFile) && fileExists(goMod) {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("repo root not found: run this command from the graph-go repository")
		}
		current = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func checkDockerCLI() error {
	if _, err := lookPath("docker"); err != nil {
		return fmt.Errorf("docker CLI not found in PATH: install Docker with Docker Compose support to use `graph-go demo`")
	}

	return nil
}

func checkDockerCompose(repoRoot string) error {
	cmd := exec.Command("docker", "compose", "version")
	cmd.Dir = repoRoot
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := runDemoCheck(cmd); err != nil {
		return fmt.Errorf("`docker compose` is unavailable or Docker is not running: %w", err)
	}

	return nil
}

func checkDemoPorts(repoRoot string) error {
	busyPorts, err := listBusyDemoPorts(repoRoot)
	if err != nil {
		return err
	}
	if len(busyPorts) == 0 {
		return nil
	}

	ports := make([]int, 0, len(busyPorts))
	for port := range busyPorts {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, fmt.Sprintf("%d (%s)", port, busyPorts[port]))
	}

	return fmt.Errorf("demo ports already in use: %s\nfree those ports or tear down the prior stack first with `docker compose -f docker-compose.demo.yml down`", strings.Join(parts, ", "))
}

func listBusyDemoPorts(repoRoot string) (map[int]string, error) {
	cmd := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Ports}}")
	cmd.Dir = repoRoot
	out, err := runDemoOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("inspect running Docker ports: %w", err)
	}

	return parseBusyDemoPorts(string(out)), nil
}

func parseBusyDemoPorts(output string) map[int]string {
	busyPorts := make(map[int]string)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, ports, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}

		for _, segment := range strings.Split(ports, ",") {
			segment = strings.TrimSpace(segment)
			for _, marker := range []string{"0.0.0.0:", ":::"} {
				idx := strings.Index(segment, marker)
				if idx == -1 {
					continue
				}
				start := idx + len(marker)
				end := start
				for end < len(segment) && segment[end] >= '0' && segment[end] <= '9' {
					end++
				}
				if end == start {
					continue
				}
				port, err := strconv.Atoi(segment[start:end])
				if err != nil {
					continue
				}
				if containsPort(demoPorts, port) {
					busyPorts[port] = name
				}
			}
		}
	}

	return busyPorts
}

func containsPort(ports []int, want int) bool {
	for _, port := range ports {
		if port == want {
			return true
		}
	}

	return false
}

func writeDemoIntro(w io.Writer, repoRoot, composeFile string) error {
	lines := []string{
		"Starting graph-go demo stack with Docker Compose...",
		fmt.Sprintf("Repo: %s", repoRoot),
		fmt.Sprintf("Compose file: %s", composeFile),
		"First run note: image pulls and builds can take several minutes on a cold machine.",
		"Subsequent runs are much faster once the images are cached.",
		"Required host ports: 8080, 5432, 27017, 9000, 9001.",
		"Open http://localhost:8080 once the services are healthy.",
		"Press Ctrl+C to stop the attached Compose session.",
		"To remove the stack later: docker compose -f docker-compose.demo.yml down",
	}

	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	return nil
}

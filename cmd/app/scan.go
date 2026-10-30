package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/guilherme-grimm/graph-go/internal/adapters/catalog"
	"github.com/guilherme-grimm/graph-go/internal/config"
	"github.com/guilherme-grimm/graph-go/internal/graph/render"
	"github.com/guilherme-grimm/graph-go/internal/logging"
	"github.com/guilherme-grimm/graph-go/internal/server"
)

var scanFormats = []string{"json", "mermaid", "markdown", "dot"}

// largeDiagramNodes is where diagrams stop being readable, matching the UI's
// threshold for collapsing namespaces and switching to swimlane.
const largeDiagramNodes = 300

func newScanCmd() *cobra.Command {
	var (
		format     string
		withHealth bool
		pretty     bool
	)

	cmd := &cobra.Command{
		Use:          "scan",
		Aliases:      []string{"export"},
		Short:        "Run discovery once and emit the graph to stdout.",
		Long:         "scan runs one discovery pass against Docker, Kubernetes, and any YAML-declared services, then writes the discovered graph to stdout and exits. --format picks JSON (the /api/graph envelope), a Mermaid or Graphviz DOT diagram, or Mermaid fenced for a Markdown file. Logs go to stderr - combine with --log-level=warn (or LOG_LEVEL=warn) for quieter stderr when piping.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			logLevel, _ := cmd.Flags().GetString("log-level")
			logFormat, _ := cmd.Flags().GetString("log-format")
			return runScan(configPath, logLevel, logFormat, format, withHealth, pretty)
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json|mermaid|markdown|dot")
	cmd.Flags().BoolVar(&withHealth, "health", false, "run one health sweep and merge adapter status onto nodes")
	cmd.Flags().BoolVar(&pretty, "pretty", false, "indent output for humans (default: compact)")

	return cmd
}

func runScan(configPath, logLevel, logFormat, format string, withHealth, pretty bool) error {
	if !slices.Contains(scanFormats, format) {
		return fmt.Errorf("unsupported format %q (supported: %s)", format, strings.Join(scanFormats, ", "))
	}

	logger, err := logging.New(logLevel, logFormat)
	if err != nil {
		return err
	}
	defer logger.Sync() //nolint:errcheck // stderr sync may fail on some TTYs

	cfg, err := config.Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debugw("no config file - auto-discovery mode", "path", configPath)
		} else {
			logger.Warnw("config load failed - auto-discovery mode", "path", configPath, "err", err)
		}
		cfg = &config.Config{}
	}

	reg, _, cleanup := server.BuildRegistry(cfg, logger, catalog.Default())
	defer cleanup()

	if format == "json" {
		if err := server.WriteGraphJSON(os.Stdout, reg, withHealth, pretty); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		return nil
	}

	g, err := server.ScanGraph(reg, withHealth)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if len(g.Nodes) > largeDiagramNodes {
		logger.Warnw("large diagram: renderers like GitHub's Mermaid may refuse it; narrow discovery with kubernetes.namespaces or docker.network",
			"nodes", len(g.Nodes), "edges", len(g.Edges))
	}

	switch format {
	case "mermaid":
		err = render.Mermaid(os.Stdout, g, false)
	case "markdown":
		err = render.Mermaid(os.Stdout, g, true)
	case "dot":
		err = render.DOT(os.Stdout, g)
	}
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	return nil
}

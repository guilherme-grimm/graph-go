package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/adapters/catalog"
	"github.com/guilherme-grimm/graph-go/internal/config"
	"github.com/guilherme-grimm/graph-go/internal/logging"
	"github.com/guilherme-grimm/graph-go/internal/mcp"
	"github.com/guilherme-grimm/graph-go/internal/server"
)

func newMCPCmd() *cobra.Command {
	var maxBytes int

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the discovered topology to AI agents over MCP (stdio).",
		Long: "mcp runs a Model Context Protocol server on stdin/stdout so coding agents can query your " +
			"infrastructure with three read-only tools: get_topology, get_node and find_nodes. Discovery starts " +
			"in the background at launch and stays warm for the session. Logs go to stderr at warn level unless " +
			"--log-level or LOG_LEVEL says otherwise.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			logFormat, _ := cmd.Flags().GetString("log-format")
			logLevel, _ := cmd.Flags().GetString("log-level")
			// Clients show the server's stderr; info-level discovery chatter
			// is noise there, so default quieter than serve.
			if !cmd.Flags().Changed("log-level") && os.Getenv("LOG_LEVEL") == "" {
				logLevel = "warn"
			}
			if maxBytes < mcp.MinMaxResponseBytes {
				return fmt.Errorf("--max-response-bytes must be at least %d", mcp.MinMaxResponseBytes)
			}
			return runMCP(cmd.Context(), configPath, logLevel, logFormat, maxBytes)
		},
	}
	cmd.Flags().IntVar(&maxBytes, "max-response-bytes", mcp.DefaultMaxResponseBytes, "cap on each tool response, in bytes")
	return cmd
}

func runMCP(ctx context.Context, configPath, logLevel, logFormat string, maxBytes int) error {
	logger, err := logging.New(logLevel, logFormat)
	if err != nil {
		return err
	}
	defer logger.Sync() //nolint:errcheck // stderr sync may fail on some TTYs

	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logger.Debugw("no config file - auto-discovery mode", "path", configPath)
		} else {
			logger.Warnw("config load failed - auto-discovery mode", "path", configPath, "err", err)
		}
		cfg = &config.Config{}
	}

	backend := mcp.StartBackend(func() (adapters.Registry, func()) {
		return server.StartRegistry(cfg, logger, catalog.Default())
	}, logger)
	defer backend.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s := mcp.NewServer(backend, mcp.Options{Version: Version, MaxResponseBytes: maxBytes})
	if err := s.Run(ctx, &sdk.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

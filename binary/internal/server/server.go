package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"binary/internal/adapters"
	"binary/internal/config"
	"binary/internal/database"
)

type Server struct {
	port     int
	db       database.Service
	registry adapters.Registry
}

// NewServer returns the HTTP server and a cleanup function that should
// be called during graceful shutdown to close adapter connections.
func NewServer(cfg *config.Config) (*http.Server, func()) {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port == 0 {
		port = 8080
		log.Printf("PORT not set or invalid, defaulting to %d", port)
	}

	reg := adapters.NewRegistry()

	// Register adapters from config
	if cfg != nil {
		for _, entry := range cfg.Connections {
			adapter, err := config.AdapterFactory(entry.Type)
			if err != nil {
				log.Printf("WARNING: %v", err)
				continue
			}

			connCfg := entry.ToConnectionConfig()
			if err := reg.Register(entry.Name, adapter, connCfg); err != nil {
				log.Printf("WARNING: failed to register %q adapter: %v", entry.Name, err)
			} else {
				log.Printf("%s adapter %q registered successfully", entry.Type, entry.Name)
			}
		}
	}

	s := &Server{
		port:     port,
		db:       database.New(),
		registry: reg,
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	cleanup := func() {
		if err := reg.CloseAll(); err != nil {
			log.Printf("error closing adapters: %v", err)
		}
		if err := s.db.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}

	return server, cleanup
}

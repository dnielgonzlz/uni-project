package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/config"
)

// Examples:
//   go run ./cmd/migrate up         – apply all pending migrations
//   go run ./cmd/migrate down 1     – roll back the most recent migration
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down> [N]")
		os.Exit(1)
	}

	direction := os.Args[1]

	m, err := migrate.New("file://migrations", cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to create migrator", "error", err)
		os.Exit(1)
	}
	defer m.Close()

	switch direction {
	case "up":
		err = m.Up()
	case "down":
		steps := 1
		if len(os.Args) >= 3 {
			if _, scanErr := fmt.Sscanf(os.Args[2], "%d", &steps); scanErr != nil {
				fmt.Fprintf(os.Stderr, "invalid step count: %s\n", os.Args[2])
				os.Exit(1)
			}
		}
		err = m.Steps(-steps)
	default:
		fmt.Fprintf(os.Stderr, "unknown direction: %s (use 'up' or 'down')\n", direction)
		os.Exit(1)
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	slog.Info("migration complete", "direction", direction)
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/weareprogmatic/laminar/internal/config"
	"github.com/weareprogmatic/laminar/internal/server"
	"github.com/weareprogmatic/laminar/internal/version"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "laminar.json", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.StringVar(configPath, "c", "laminar.json", "Path to configuration file (shorthand)")
	flag.BoolVar(showVersion, "v", false, "Show version information (shorthand)")

	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("Laminar %s\n", version.Version)
		fmt.Printf("Commit: %s\n", version.Commit)
		fmt.Printf("Built:  %s\n", version.Date)
		os.Exit(0)
	}

	// Load configuration
	services, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if *verbose {
		log.Printf("Loaded %d service(s) from %s", len(services), *configPath)
		for _, svc := range services {
			log.Printf("  - %s: %s on port %d", svc.Name, svc.Binary, svc.Port)
		}
	}

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down gracefully...", sig)
		cancel()
	}()

	// Start all servers
	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		go func(cfg config.ServiceConfig) {
			defer wg.Done()
			if err := server.Start(ctx, cfg); err != nil {
				log.Printf("Server error for %s: %v", cfg.Name, err)
			}
		}(svc)
	}

	// Wait for all servers to finish
	wg.Wait()
	log.Println("All servers stopped. Goodbye!")
}

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
	"github.com/weareprogmatic/laminar/internal/invoke"
	"github.com/weareprogmatic/laminar/internal/secrets"
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
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if *verbose {
		log.Printf("Loaded %d service(s) from %s", len(cfg.Services), *configPath)
		for _, svc := range cfg.Services {
			log.Printf("  - %s: %s on port %d", svc.Name, svc.Binary, svc.Port)
		}
	}

	// Start the mock Lambda Service API so that Lambda-to-Lambda calls
	// (e.g. lambda.Invoke / lambda.InvokeAsync) resolve to local binaries.
	cleanup, err := setupInvokeServer(cfg.Services)
	if err != nil {
		log.Fatalf("Failed to start Lambda Service API: %v", err)
	}
	defer cleanup()

	// Start the mock Secrets Manager API so that AWS SDK GetSecretValue calls
	// resolve to values configured in laminar.json.
	secretsCleanup, err := setupSecretsServer(cfg.Secrets, cfg.Services)
	if err != nil {
		log.Fatalf("Failed to start Secrets Manager API: %v", err)
	}
	defer secretsCleanup()

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
	for _, svc := range cfg.Services {
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

// setupInvokeServer starts the mock Lambda Service API and injects AWS_ENDPOINT_URL_LAMBDA
// into each service's environment so Lambda-to-Lambda SDK calls resolve locally.
// It returns a cleanup function that shuts down the server.
func setupInvokeServer(services []config.ServiceConfig) (func(), error) {
	srv, err := invoke.NewServer(services)
	if err != nil {
		return nil, err
	}
	srv.Start()
	log.Printf("Lambda Service API listening on http://%s", srv.Addr())

	endpoint := fmt.Sprintf("http://%s", srv.Addr())
	for i := range services {
		if services[i].Env == nil {
			services[i].Env = make(map[string]string)
		}
		services[i].Env["AWS_ENDPOINT_URL_LAMBDA"] = endpoint
		services[i].Env["AWS_LAMBDA_ENDPOINT"] = endpoint
	}

	return func() { _ = srv.Close() }, nil
}

// setupSecretsServer starts the mock Secrets Manager API and injects
// AWS_ENDPOINT_URL_SECRETS_MANAGER into each service's environment so that
// AWS SDK GetSecretValue calls resolve to values configured in laminar.json.
// It returns a cleanup function that shuts down the server.
func setupSecretsServer(globalSecrets map[string]string, services []config.ServiceConfig) (func(), error) {
	srv, err := secrets.NewServer(globalSecrets)
	if err != nil {
		return nil, err
	}
	srv.Start()
	log.Printf("Secrets Manager API listening on http://%s", srv.Addr())

	endpoint := fmt.Sprintf("http://%s", srv.Addr())
	for i := range services {
		if services[i].Env == nil {
			services[i].Env = make(map[string]string)
		}
		services[i].Env["AWS_ENDPOINT_URL_SECRETS_MANAGER"] = endpoint
	}

	return func() { _ = srv.Close() }, nil
}

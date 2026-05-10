package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/nathan77886/gitea2github/internal/config"
	"github.com/nathan77886/gitea2github/internal/git"
	"github.com/nathan77886/gitea2github/internal/handler"
	"github.com/nathan77886/gitea2github/internal/logger"
	"github.com/nathan77886/gitea2github/internal/queue"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config YAML file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	// Ensure required directories exist.
	for _, dir := range []string{cfg.WorkDir, cfg.QueueDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("creating directory %s: %v", dir, err)
		}
	}

	l := logger.New(cfg.LogFile)
	syncer := git.New(cfg.WorkDir, cfg)

	// Build the queue handler that syncs and logs results.
	qHandler := func(t queue.Task) error {
		proj := findProject(cfg, t.Project)
		if proj == nil {
			return fmt.Errorf("unknown project: %s", t.Project)
		}
		log.Printf("syncing project %s (ref=%s after=%s)", t.Project, t.Ref, t.After)
		if err := syncer.Sync(proj); err != nil {
			l.Log("ERROR project=%s ref=%s after=%s err=%v", t.Project, t.Ref, t.After, err)
			return err
		}
		l.Log("OK project=%s ref=%s after=%s", t.Project, t.Ref, t.After)
		log.Printf("synced project %s successfully", t.Project)
		return nil
	}

	q, err := queue.New(cfg.QueueDir, qHandler)
	if err != nil {
		log.Fatalf("creating queue: %v", err)
	}

	done := make(chan struct{})
	go q.Run(done)

	mux := http.NewServeMux()
	wh := handler.New(cfg, q)
	mux.Handle("/webhook", wh)
	mux.Handle("/webhook/", wh)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server: %v", err)
		}
	}()

	// Wait for SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	close(done)
}

func findProject(cfg *config.Config, name string) *config.Project {
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == name {
			return &cfg.Projects[i]
		}
	}
	return nil
}

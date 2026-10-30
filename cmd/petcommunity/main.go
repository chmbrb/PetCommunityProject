// SPDX-License-Identifier: AGPL-3.0-or-later

// Command petcommunity runs the PetCommunityProject backend. One binary provides
// every mode (ADR 0003):
//
//	petcommunity serve            run the HTTP API
//	petcommunity worker           run background jobs (sending email, cleanup)
//	petcommunity migrate [up]     apply pending database migrations
//	petcommunity migrate status   list migrations and whether they are applied
//	petcommunity version          print the build version
//	petcommunity healthcheck      exit 0 if the local server answers /healthz (for container health checks)
//
// Configuration comes from PC_* environment variables (see .env.example).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/jobs"
	"github.com/chmbrb/PetCommunityProject/internal/mail"
	"github.com/chmbrb/PetCommunityProject/internal/server"
	"github.com/chmbrb/PetCommunityProject/internal/version"
)

const usage = `usage: petcommunity <command>

commands:
  serve            run the HTTP API
  worker           run background jobs (sending email, cleanup)
  migrate [up]     apply pending database migrations
  migrate status   list migrations and whether they are applied
  version          print the build version
  healthcheck      exit 0 if the local server answers /healthz
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.Version)
		return 0
	case "healthcheck":
		if err := healthcheck(ctx, getenv("PC_HTTP_ADDR")); err != nil {
			fmt.Fprintf(stderr, "unhealthy: %v\n", err)
			return 1
		}
		return 0
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "invalid configuration:\n%v\n", err)
		return 1
	}
	logger := newLogger(cfg, stderr)

	switch args[0] {
	case "serve":
		err = serve(ctx, cfg, logger)
	case "worker":
		err = work(ctx, cfg, logger)
	case "migrate":
		err = migrate(ctx, cfg, args[1:], stdout, logger)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		logger.Error(args[0]+" failed", "error", err)
		return 1
	}
	return 0
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	if cfg.Env == config.Production {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	st := database.NewStore(pool)
	queue, err := jobs.NewInserter(pool, logger)
	if err != nil {
		return err
	}
	authSvc, err := auth.New(cfg, st, queue, logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.New(cfg, st, authSvc, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr, "version", version.Version, "env", cfg.Env)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func work(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	st := database.NewStore(pool)
	emailer := auth.NewEmailer(cfg, st, mail.NewSMTPSender(cfg.Mail), logger)
	client, err := jobs.NewWorker(pool, st, emailer, logger)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	logger.Info("worker started", "version", version.Version, "smtp_host", cfg.Mail.Host)

	<-ctx.Done()
	logger.Info("worker stopping")
	// Let running jobs finish; a fresh context because ctx is already cancelled.
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.Stop(stopCtx)
}

func migrate(ctx context.Context, cfg config.Config, args []string, stdout io.Writer, logger *slog.Logger) error {
	sub := "up"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "up":
		applied, err := database.Migrate(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		for _, m := range applied {
			logger.Info("applied migration", "version", m.Version, "source", m.Source)
		}
		logger.Info("migrations up to date", "applied", len(applied))
		return nil
	case "status":
		statuses, err := database.Status(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			state := "pending"
			if s.Applied {
				state = "applied"
			}
			fmt.Fprintf(stdout, "%05d  %-8s %s\n", s.Version, state, s.Source)
		}
		return nil
	default:
		return fmt.Errorf("unknown migrate subcommand %q (want up or status)", sub)
	}
}

// healthcheck asks the server on this host for /healthz. The production image has
// no shell or curl, so container health checks run this instead.
func healthcheck(ctx context.Context, addr string) error {
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse PC_HTTP_ADDR: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

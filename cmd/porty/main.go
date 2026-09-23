package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/msoldin/porty/internal/app"
	portyauth "github.com/msoldin/porty/internal/auth"
	"github.com/msoldin/porty/internal/config"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		if err := resetPassword(os.Args[2:], os.LookupEnv); err != nil {
			slog.Error("reset password", "error", err)
			os.Exit(1)
		}
		slog.Info("administrator password reset")
		return
	}
	cfg, err := config.Load(os.Args[1:], os.LookupEnv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	db, err := portysqlite.Open(context.Background(), filepath.Join(cfg.DataDir, "porty.db"))
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := app.New(ctx, db, cfg)
	if err != nil {
		slog.Error("construct application", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("Porty listening", "address", cfg.Server.Listen)
	err = serve(ctx, server, func() error {
		if cfg.Server.TLSCert != "" {
			return server.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
		}
		return server.ListenAndServe()
	})
	if err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func resetPassword(args []string, lookupEnv func(string) (string, bool)) error {
	cfg, err := config.Load(args, lookupEnv)
	if err != nil {
		return err
	}
	password, ok := lookupEnv("PORTY_RESET_PASSWORD")
	if !ok {
		return errors.New("PORTY_RESET_PASSWORD is required")
	}
	db, err := portysqlite.Open(context.Background(), filepath.Join(cfg.DataDir, "porty.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	authService := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	return authService.ResetPassword(context.Background(), password)
}

// serve runs the HTTP server until it fails or the process context is cancelled.
func serve(ctx context.Context, server *http.Server, start func() error) error {
	stopped := make(chan struct{})
	shutdownDone := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			shutdownDone <- server.Shutdown(shutdownCtx)
		case <-stopped:
			shutdownDone <- nil
		}
	}()
	err := start()
	close(stopped)
	if shutdownErr := <-shutdownDone; shutdownErr != nil {
		return shutdownErr
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

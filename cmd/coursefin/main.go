package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"coursefin/internal/config"
	"coursefin/internal/server"
	"coursefin/internal/updater"
)

var version = "1.2.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-version", "version":
			fmt.Println(normalizedVersion())
			os.Exit(0)
		}
	}

	cfg := config.Load()
	cfg.Version = version
	logger := newLogger(cfg.LogLevel)

	appSrv, err := server.New(cfg)
	if err != nil {
		logger.Error("failed to initialize server", "error", err)
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		logger.Error("failed to bind listener", "addr", ":"+cfg.Port, "error", err)
		_ = appSrv.Close()
		os.Exit(1)
	}

	if cfg.ManagedBinDir != "" {
		pendingPath := filepath.Join(cfg.ManagedBinDir, ".pending_update")
		if err := os.Remove(pendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("failed to remove pending update marker", "path", pendingPath, "error", err)
		}
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           appSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("CourseFin listening", "version", normalizedVersion(), "addr", httpSrv.Addr, "data_dir", cfg.DataDir)
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	var restartReq *updater.RestartRequest
	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("http server stopped with error", "error", err)
			_ = appSrv.Close()
			os.Exit(1)
		}
	case <-sigCtx.Done():
		logger.Info("shutdown signal received")
	case req := <-appSrv.RestartCh():
		logger.Info("hot-exec restart requested", "exec_path", req.ExecPath)
		restartReq = &req
	}

	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server graceful shutdown error", "error", err)
		_ = httpSrv.Close()
	}
	if err := appSrv.Close(); err != nil {
		logger.Error("application close error", "error", err)
	}

	if restartReq != nil {
		if err := updater.ExecReplace(restartReq.ExecPath, os.Args, os.Environ()); err != nil {
			logger.Error("hot-exec failed, attempting backup rollback", "exec_path", restartReq.ExecPath, "backup_path", restartReq.BackupPath, "error", err)
			if restartReq.BackupPath != "" {
				if _, statErr := os.Stat(restartReq.BackupPath); statErr == nil {
					if renameErr := os.Rename(restartReq.BackupPath, restartReq.ExecPath); renameErr != nil {
						logger.Error("failed to restore backup binary", "backup_path", restartReq.BackupPath, "exec_path", restartReq.ExecPath, "error", renameErr)
						os.Exit(1)
					}
					if cfg.ManagedBinDir != "" {
						pendingPath := filepath.Join(cfg.ManagedBinDir, ".pending_update")
						if removeErr := os.Remove(pendingPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
							logger.Warn("failed to remove pending update marker", "path", pendingPath, "error", removeErr)
						}
					}
					if execErr := updater.ExecReplace(restartReq.ExecPath, os.Args, os.Environ()); execErr != nil {
						logger.Error("hot-exec on restored backup binary failed", "exec_path", restartReq.ExecPath, "error", execErr)
					}
				} else if !errors.Is(statErr, os.ErrNotExist) {
					logger.Error("failed to stat backup binary for rollback", "backup_path", restartReq.BackupPath, "error", statErr)
				}
			}
			os.Exit(1)
		}
	}

	logger.Info("CourseFin stopped cleanly")
}

func normalizedVersion() string {
	if v := updater.NormalizeVersion(version); v != "" {
		return v
	}
	return "0.0.0-dev"
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/server"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "reset-admin":
			os.Exit(runResetAdmin(os.Args[2:]))
		case "serve", "run":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		}
	}

	addr := flag.String("listen", "", "listen address (overrides LISTEN_ADDR)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if *addr != "" {
		cfg.ListenAddr = *addr
	}

	logPath := filepath.Join(cfg.LogsDir, "app.log")
	_ = cfg.EnsureDirs()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	var log *slog.Logger
	if err == nil {
		defer logFile.Close()
		log = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo}))
	} else {
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	slog.SetDefault(log)

	app, err := server.New(cfg, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}
	defer app.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.StartCleanup(ctx)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "data", cfg.DataDir)
		fmt.Printf("飞船已启动: http://127.0.0.1%s\n", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server: %v\n", err)
			os.Exit(1)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func runResetAdmin(args []string) int {
	fs := flag.NewFlagSet("reset-admin", flag.ExitOnError)
	_ = fs.Parse(args)
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	app, err := server.New(cfg, slog.Default())
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	defer app.Close()
	if err := app.Auth.ResetAdmin(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "reset-admin: %v\n", err)
		return 1
	}
	fmt.Println("管理员已清除，下次启动可重新注册。")
	return 0
}

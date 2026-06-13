// Command server runs the kungal-link-live-checker HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KunMoe/kungal-link-live-checker/internal/checker"
	"github.com/KunMoe/kungal-link-live-checker/internal/config"
	"github.com/KunMoe/kungal-link-live-checker/internal/httpapi"
	"github.com/KunMoe/kungal-link-live-checker/internal/provider/baidu"
	"github.com/KunMoe/kungal-link-live-checker/internal/provider/caiyun"
	"github.com/KunMoe/kungal-link-live-checker/internal/provider/pan123"
	"github.com/KunMoe/kungal-link-live-checker/internal/provider/quark"
	"github.com/KunMoe/kungal-link-live-checker/internal/provider/uc"
	"github.com/KunMoe/kungal-link-live-checker/internal/ratelimit"
	"github.com/KunMoe/kungal-link-live-checker/internal/service"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	httpClient := &http.Client{
		Timeout: cfg.CheckTimeout,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	checkers := []checker.Checker{
		quark.New(quark.Options{Client: httpClient, Logger: log, BlockedAsDead: cfg.QuarkBlockedAsDead}),
	}
	if cfg.UCEnabled {
		checkers = append(checkers, uc.New(uc.Options{
			TokenURL: cfg.UCTokenURL, Client: httpClient, Logger: log, BlockedAsDead: cfg.QuarkBlockedAsDead,
		}))
	}
	if cfg.BaiduEnabled {
		checkers = append(checkers, baidu.New(baidu.Options{Client: httpClient, Logger: log}))
	}
	if cfg.CaiyunEnabled {
		checkers = append(checkers, caiyun.New(caiyun.Options{Client: httpClient, Logger: log}))
	}
	if cfg.Pan123Enabled {
		checkers = append(checkers, pan123.New(pan123.Options{Client: httpClient, Logger: log}))
	}

	registry := checker.NewRegistry(checkers...)
	limiters := ratelimit.NewRegistry(cfg.RateRPS, cfg.RateBurst)
	svc := service.New(registry, limiters, service.Options{
		CheckTimeout: cfg.CheckTimeout,
		TTLAlive:     cfg.TTLAlive,
		TTLDead:      cfg.TTLDead,
		TTLUnknown:   cfg.TTLUnknown,
	}, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go svc.RunJanitor(ctx, 10*time.Minute)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewServer(svc, cfg.APIKeys, cfg.BatchMax, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.CheckTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.Addr, "providers", registry.Names(), "auth_keys", len(cfg.APIKeys))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
}

// Package main is the entry point for the YFS User Quota Update service.
// It periodically reads user storage usage from the Files database and updates
// file_users.quota_utilized (in GB) in the main database.
package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"
	"yfs-user-quota-update-cron/internal/config"
	"yfs-user-quota-update-cron/internal/filesdb"
	"yfs-user-quota-update-cron/internal/logger"
	"yfs-user-quota-update-cron/internal/maindb"
	"yfs-user-quota-update-cron/internal/pgpool"
)

// main is the application entry point.
// It initializes configuration, logging, and database connections,
// then runs the quota sync every SYNC_INTERVAL until stopped.
func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid configuration")
	}
	// initialize the logger
	logger.Init(
		logger.Options{
			File:       cfg.LogFile,
			MaxSizeMB:  cfg.LogMaxSizeMB,
			MaxBackups: cfg.LogMaxBackups,
			MaxAgeDays: cfg.LogMaxAgeDays,
			Compress:   cfg.LogCompress,
			Level:      cfg.LogLevel,
			Console:    cfg.LogShowConsole,
		})

	// stop gracefully on docker stop / Ctrl+C
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	filesPool, err := pgpool.New(ctx, cfg.FilesPGURL, cfg.FilesPGMaxConns, cfg.FilesPGMinConns)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize Files database connection pool")
	}
	defer filesPool.Close()

	mainPool, err := pgpool.New(ctx, cfg.MainPGURL, cfg.MainPGMaxConns, cfg.MainPGMinConns)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize main database connection pool")
	}
	defer mainPool.Close()

	filesRepo := filesdb.NewFilesQuotaRepo(filesPool)
	mainRepo := maindb.NewFileUsersRepo(mainPool)

	logger.Info().Str("interval", cfg.SyncInterval.String()).Msg("quota update service started")

	ticker := time.NewTicker(cfg.SyncInterval)
	defer ticker.Stop()
	for {
		runSync(ctx, cfg, filesRepo, mainRepo)

		select {
		case <-ctx.Done():
			logger.Info().Msg("shutdown signal received, quota update service stopped")
			return
		case <-ticker.C:
		}
	}
}

// runSync performs a single quota sync from the Files database to the main database.
func runSync(ctx context.Context, cfg config.Config, filesRepo *filesdb.FilesQuotaRepo, mainRepo *maindb.FileUsersRepo) {
	startTime := time.Now()

	quotas, err := filesRepo.GetUserQuotas(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to fetch user quotas from files database")
		return
	}
	if len(quotas) == 0 {
		logger.Info().Msg("no user quotas found in files database, skipping quota update")
		return
	}

	mainRepo.UpdateQuotaParallel(ctx, quotas, cfg.PGUpdateChunkSize, cfg.PGWorkers)

	logger.Info().
		Int("total_users", len(quotas)).
		Str("execution_time", time.Since(startTime).String()).
		Msg("quota update run completed")
}

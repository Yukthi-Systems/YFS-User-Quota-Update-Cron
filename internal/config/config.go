// Package config handles the application configuration loading and mapping.
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration structure.
// All values are read from environment variables (set via docker-compose).
type Config struct {
	// SyncInterval is how often the quota sync runs (e.g. "5m", "1h").
	SyncInterval time.Duration `mapstructure:"sync_interval"`

	// LogFile is the path to the log file. Empty disables file logging.
	LogFile string `mapstructure:"log_file"`
	// LogMaxSizeMB is the maximum size of the log file in megabytes before it gets rotated.
	LogMaxSizeMB int `mapstructure:"log_max_size_mb"`
	// LogMaxBackups is the maximum number of old log files to retain.
	LogMaxBackups int `mapstructure:"log_max_backups"`
	// LogMaxAgeDays is the maximum number of days to retain old log files.
	LogMaxAgeDays int `mapstructure:"log_max_age_days"`
	// LogCompress determines if the rotated log files should be compressed.
	LogCompress bool `mapstructure:"log_compress"`
	// LogLevel sets the logging severity (e.g., "debug", "info", "warn", "error").
	LogLevel string `mapstructure:"log_level"`
	// LogShowConsole if true, logs will also be printed to the console (stderr).
	LogShowConsole bool `mapstructure:"log_show_console"`

	// FilesPGURL is the connection string of the Files database (source of user_quotas).
	FilesPGURL string `mapstructure:"files_pg_url"`
	// FilesPGMaxConns is the maximum number of connections in the Files database pool.
	FilesPGMaxConns int32 `mapstructure:"files_pg_max_conns"`
	// FilesPGMinConns is the minimum number of connections in the Files database pool.
	FilesPGMinConns int32 `mapstructure:"files_pg_min_conns"`

	// MainPGURL is the connection string of the main database (target file_users).
	MainPGURL string `mapstructure:"main_pg_url"`
	// MainPGMaxConns is the maximum number of connections in the main database pool.
	MainPGMaxConns int32 `mapstructure:"main_pg_max_conns"`
	// MainPGMinConns is the minimum number of connections in the main database pool.
	MainPGMinConns int32 `mapstructure:"main_pg_min_conns"`
	// PGUpdateChunkSize is the number of records to update in a single batch.
	PGUpdateChunkSize int `mapstructure:"pg_update_chunk_size"`
	// PGWorkers is the number of parallel workers for PostgreSQL updates.
	PGWorkers int `mapstructure:"pg_workers"`
}

// defaults lists every config key with its default value. Each key is read
// from the environment variable of the same name in upper case
// (e.g. "sync_interval" -> SYNC_INTERVAL).
var defaults = map[string]any{
	"sync_interval":        "5m",
	"log_file":             "",
	"log_max_size_mb":      250,
	"log_max_backups":      10,
	"log_max_age_days":     20,
	"log_compress":         true,
	"log_level":            "info",
	"log_show_console":     true,
	"files_pg_url":         "",
	"files_pg_max_conns":   2,
	"files_pg_min_conns":   1,
	"main_pg_url":          "",
	"main_pg_max_conns":    5,
	"main_pg_min_conns":    1,
	"pg_update_chunk_size": 200,
	"pg_workers":           4,
}

// LoadConfig initializes and returns the application configuration from environment variables.
//
// Example:
//
//	cfg, err := config.LoadConfig()
//	fmt.Println(cfg.SyncInterval)
func LoadConfig() (Config, error) {
	vp := viper.New()
	for key, val := range defaults {
		vp.SetDefault(key, val)
	}
	vp.AutomaticEnv()

	var config Config
	if err := vp.Unmarshal(&config); err != nil {
		return config, err
	}

	if config.FilesPGURL == "" {
		return config, fmt.Errorf("FILES_PG_URL is required")
	}
	if config.MainPGURL == "" {
		return config, fmt.Errorf("MAIN_PG_URL is required")
	}
	if config.SyncInterval <= 0 {
		return config, fmt.Errorf("SYNC_INTERVAL must be greater than 0")
	}
	if config.PGUpdateChunkSize <= 0 || config.PGWorkers <= 0 {
		return config, fmt.Errorf("PG_UPDATE_CHUNK_SIZE and PG_WORKERS must be greater than 0")
	}
	return config, nil
}

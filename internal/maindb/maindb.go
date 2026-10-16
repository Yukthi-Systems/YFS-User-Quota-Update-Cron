// Package maindb handles updating file_users.quota_utilized in the main PostgreSQL database.
package maindb

import (
	"context"
	"sync"
	"time"
	"yfs-user-quota-update-cron/internal/logger"
	"yfs-user-quota-update-cron/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FileUsersRepo provides methods to update file user quotas in the main database.
type FileUsersRepo struct {
	db *pgxpool.Pool
}

// quotaJob represents a single task for a worker to update a chunk of user quotas.
type quotaJob struct {
	id    string            // Unique identifier for the job
	index int               // Index of the chunk
	chunk []model.UserQuota // List of user quotas to update
}

// NewFileUsersRepo initializes a new main database repository with the provided pool.
func NewFileUsersRepo(pool *pgxpool.Pool) *FileUsersRepo {
	return &FileUsersRepo{db: pool}
}

// chunkProducer splits the quota data into chunks and sends them to the jobs channel.
func chunkProducer(ctx context.Context, data []model.UserQuota, chunkSize int, jobs chan<- quotaJob) {
	defer close(jobs)
	jobIndex := 0

	for start := 0; start < len(data); start += chunkSize {
		end := min(start+chunkSize, len(data))

		select {
		case jobs <- quotaJob{
			id:    uuid.NewString(), // unique job id
			index: jobIndex,
			chunk: data[start:end],
		}:
		case <-ctx.Done():
			return
		}

		jobIndex++
	}
}

// worker processes quota update jobs from the jobs channel.
func (p *FileUsersRepo) worker(ctx context.Context, jobs <-chan quotaJob, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		start := time.Now()
		total := len(job.chunk)
		logger.Debug().Interface("user_quotas", job.chunk).Msg("Updating values to the database.")

		updatedCount, missingEmails, err := p.updateQuotaChunk(ctx, job.chunk)
		if err != nil {
			logger.Error().
				Err(err).
				Str("job_id", job.id).
				Int("chunk_index", job.index).
				Int("total_records", total).
				Str("duration", time.Since(start).String()).
				Msg("quota chunk update failed")
			continue
		}

		logEvent := logger.Info().
			Str("job_id", job.id).
			Int("chunk_index", job.index).
			Int("total_records", total).
			Int64("updated_rows", updatedCount).
			Int64("unchanged_rows", int64(total-len(missingEmails))-updatedCount).
			Int("missing_records", len(missingEmails)).
			Str("duration", time.Since(start).String())

		if len(missingEmails) > 0 {
			logEvent.
				Strs("missing_emails", missingEmails).
				Msg("quota chunk update completed with some emails not found in file_users")
		} else {
			logEvent.Msg("quota chunk update completed")
		}
	}
}

// updateQuotaChunk performs a batch update of file_users.quota_utilized (in GB).
// Rows whose value is already up to date are not rewritten.
//
// Returns:
//   - int64: Number of rows actually updated.
//   - []string: Emails from the chunk that do not exist in file_users.
//   - error: Any error encountered during the update.
func (p *FileUsersRepo) updateQuotaChunk(ctx context.Context, chunk []model.UserQuota) (int64, []string, error) {
	emails := make([]string, len(chunk))
	usedBytes := make([]int64, len(chunk))
	for i, u := range chunk {
		emails[i] = u.Email
		usedBytes[i] = u.UsedStorageBytes
	}

	// bytes -> GB (1024^3), rounded to 2 decimals to match NUMERIC(10,2)
	query := `
		WITH data AS (
			SELECT d.email, round(d.used_bytes::numeric / 1073741824, 2) AS gb
			FROM unnest($1::text[], $2::bigint[]) AS d(email, used_bytes)
		), upd AS (
			UPDATE file_users f
			SET quota_utilized = data.gb
			FROM data
			WHERE f.email = data.email
				AND f.quota_utilized IS DISTINCT FROM data.gb
			RETURNING f.email
		)
		SELECT
			(SELECT count(*) FROM upd),
			COALESCE((
				SELECT array_agg(data.email)
				FROM data
				WHERE NOT EXISTS (SELECT 1 FROM file_users f WHERE f.email = data.email)
			), '{}');`

	var updatedCount int64
	var missingEmails []string
	if err := p.db.QueryRow(ctx, query, emails, usedBytes).Scan(&updatedCount, &missingEmails); err != nil {
		return 0, nil, err
	}
	return updatedCount, missingEmails, nil
}

// UpdateQuotaParallel coordinates parallel quota updates using a worker pool.
//
// Parameters:
//   - ctx: Context to control the execution.
//   - data: The user quotas to update.
//   - chunkSize: The number of records each worker processes at once.
//   - workers: The number of concurrent workers to spawn.
//
// Example:
//
//	repo.UpdateQuotaParallel(ctx, quotas, 200, 4)
func (p *FileUsersRepo) UpdateQuotaParallel(
	ctx context.Context,
	data []model.UserQuota,
	chunkSize int,
	workers int,
) {
	logger.Debug().Msgf("starting parallel quota update with %d workers", workers)
	jobs := make(chan quotaJob, workers*2) // buffered for throughput

	var wg sync.WaitGroup

	// Start workers
	for range workers {
		wg.Add(1)
		go p.worker(ctx, jobs, &wg)
	}

	// Produce chunks
	go chunkProducer(ctx, data, chunkSize, jobs)

	// Wait for completion
	wg.Wait()
}

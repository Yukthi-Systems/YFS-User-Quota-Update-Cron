// Package filesdb reads user storage usage from the Files PostgreSQL database.
package filesdb

import (
	"context"
	"yfs-user-quota-update-cron/internal/logger"
	"yfs-user-quota-update-cron/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FilesQuotaRepo provides methods to fetch user quota details from the Files database.
type FilesQuotaRepo struct {
	db *pgxpool.Pool
}

// NewFilesQuotaRepo initializes a new Files database repository with the provided pool.
func NewFilesQuotaRepo(pool *pgxpool.Pool) *FilesQuotaRepo {
	return &FilesQuotaRepo{db: pool}
}

// GetUserQuotas fetches the used storage of every user from user_quotas,
// resolving user_id to the user's email through the users table.
//
// Returns:
//   - []model.UserQuota: The fetched user quota details.
//   - error: Any error encountered during the fetch.
func (r *FilesQuotaRepo) GetUserQuotas(ctx context.Context) ([]model.UserQuota, error) {
	logger.Debug().Msg("fetching user quotas from files database")

	query := `
		SELECT u.email, q.used_storage_bytes
		FROM user_quotas q
		JOIN users u ON u.user_id = q.user_id
		WHERE u.email <> '';`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var quotas []model.UserQuota
	for rows.Next() {
		var q model.UserQuota
		if err := rows.Scan(&q.Email, &q.UsedStorageBytes); err != nil {
			return nil, err
		}
		if q.UsedStorageBytes < 0 {
			logger.Warn().Str("email", q.Email).Int64("used_storage_bytes", q.UsedStorageBytes).
				Msg("negative used storage, treating as 0")
			q.UsedStorageBytes = 0
		}
		quotas = append(quotas, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	logger.Info().Msgf("fetched %d user quotas from files database", len(quotas))
	return quotas, nil
}

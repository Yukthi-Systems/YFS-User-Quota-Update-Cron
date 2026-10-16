// Package model contains the data structures used across the application.
package model

// UserQuota represents the storage usage of a Files user.
type UserQuota struct {
	// Email is the user's email address, the key of file_users in the main database.
	Email string
	// UsedStorageBytes is the amount of storage currently used by the user, in bytes.
	UsedStorageBytes int64
}

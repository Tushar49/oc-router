package config

import (
	"errors"
	"strings"
)

// UsageConfig controls the local usage database. The database is resolved
// against the configuration directory when Database is relative.
type UsageConfig struct {
	Enabled         bool   `json:"enabled"`
	Database        string `json:"database,omitempty"`
	RetentionDays   int    `json:"retention_days,omitempty"`
	BatchSize       int    `json:"batch_size,omitempty"`
	FlushIntervalMS int    `json:"flush_interval_ms,omitempty"`
	QueryTimeoutMS  int    `json:"query_timeout_ms,omitempty"`
}

// DefaultUsageConfig is the built-in usage configuration.
func DefaultUsageConfig() UsageConfig {
	return UsageConfig{
		Enabled: true, RetentionDays: 365, BatchSize: 100,
		FlushIntervalMS: 100, QueryTimeoutMS: 5000,
	}
}

// Normalize fills defaults and validates the ranges the plan documents.
func (u *UsageConfig) Normalize() error {
	defaults := DefaultUsageConfig()
	if u.RetentionDays == 0 {
		u.RetentionDays = defaults.RetentionDays
	}
	if u.BatchSize == 0 {
		u.BatchSize = defaults.BatchSize
	}
	if u.FlushIntervalMS == 0 {
		u.FlushIntervalMS = defaults.FlushIntervalMS
	}
	if u.QueryTimeoutMS == 0 {
		u.QueryTimeoutMS = defaults.QueryTimeoutMS
	}
	u.Database = strings.TrimSpace(u.Database)
	if u.RetentionDays < 7 || u.RetentionDays > 3650 {
		return errors.New("usage.retention_days must be between 7 and 3650")
	}
	if u.BatchSize < 1 || u.BatchSize > 1000 {
		return errors.New("usage.batch_size must be between 1 and 1000")
	}
	if u.FlushIntervalMS < 10 || u.FlushIntervalMS > 60000 {
		return errors.New("usage.flush_interval_ms must be between 10 and 60000")
	}
	if u.QueryTimeoutMS < 100 || u.QueryTimeoutMS > 60000 {
		return errors.New("usage.query_timeout_ms must be between 100 and 60000")
	}
	return nil
}

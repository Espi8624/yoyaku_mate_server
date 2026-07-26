package models

type DBMetrics struct {
	ActiveConnections int64   `json:"active_connections"`
	DatabaseSizeMB    float64 `json:"database_size_mb"`
	SlowQueries24h    int64   `json:"slow_queries_24h"`
}

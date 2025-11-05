package profiling

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// QueryStats tracks statistics for a single query
type QueryStats struct {
	Query         string
	Count         int64
	TotalDuration time.Duration
	MinDuration   time.Duration
	MaxDuration   time.Duration
	AvgDuration   time.Duration
	mu            sync.RWMutex
}

// DBProfiler profiles database queries
type DBProfiler struct {
	enabled bool
	queries map[string]*QueryStats
	slowLog time.Duration // Log queries slower than this
	mu      sync.RWMutex
}

// NewDBProfiler creates a new database profiler
func NewDBProfiler(slowQueryThreshold time.Duration) *DBProfiler {
	return &DBProfiler{
		enabled: true,
		queries: make(map[string]*QueryStats),
		slowLog: slowQueryThreshold,
	}
}

// Enable enables profiling
func (dbp *DBProfiler) Enable() {
	dbp.mu.Lock()
	defer dbp.mu.Unlock()
	dbp.enabled = true
}

// Disable disables profiling
func (dbp *DBProfiler) Disable() {
	dbp.mu.Lock()
	defer dbp.mu.Unlock()
	dbp.enabled = false
}

// IsEnabled returns whether profiling is enabled
func (dbp *DBProfiler) IsEnabled() bool {
	dbp.mu.RLock()
	defer dbp.mu.RUnlock()
	return dbp.enabled
}

// TrackQuery tracks a query execution
func (dbp *DBProfiler) TrackQuery(query string, duration time.Duration) {
	if !dbp.IsEnabled() {
		return
	}

	dbp.mu.Lock()
	defer dbp.mu.Unlock()

	stats, exists := dbp.queries[query]
	if !exists {
		stats = &QueryStats{
			Query:       query,
			MinDuration: duration,
			MaxDuration: duration,
		}
		dbp.queries[query] = stats
	}

	stats.mu.Lock()
	stats.Count++
	stats.TotalDuration += duration

	if duration < stats.MinDuration {
		stats.MinDuration = duration
	}
	if duration > stats.MaxDuration {
		stats.MaxDuration = duration
	}
	stats.AvgDuration = time.Duration(int64(stats.TotalDuration) / stats.Count)
	stats.mu.Unlock()

	// Log slow queries
	if duration > dbp.slowLog {
		log.Printf("🐌 SLOW QUERY (%v): %s", duration, truncateQuery(query, 100))
	}
}

// ProfiledDB wraps a sql.DB with profiling
type ProfiledDB struct {
	*sql.DB
	profiler *DBProfiler
}

// WrapDB wraps a database connection with profiling
func WrapDB(db *sql.DB, profiler *DBProfiler) *ProfiledDB {
	return &ProfiledDB{
		DB:       db,
		profiler: profiler,
	}
}

// QueryContext executes a query with profiling
func (pdb *ProfiledDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	rows, err := pdb.DB.QueryContext(ctx, query, args...)
	duration := time.Since(start)

	pdb.profiler.TrackQuery(query, duration)

	return rows, err
}

// QueryRowContext executes a query row with profiling
func (pdb *ProfiledDB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	start := time.Now()
	row := pdb.DB.QueryRowContext(ctx, query, args...)
	duration := time.Since(start)

	pdb.profiler.TrackQuery(query, duration)

	return row
}

// ExecContext executes a statement with profiling
func (pdb *ProfiledDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	result, err := pdb.DB.ExecContext(ctx, query, args...)
	duration := time.Since(start)

	pdb.profiler.TrackQuery(query, duration)

	return result, err
}

// GetStats returns query statistics sorted by total duration
func (dbp *DBProfiler) GetStats() []*QueryStats {
	dbp.mu.RLock()
	defer dbp.mu.RUnlock()

	stats := make([]*QueryStats, 0, len(dbp.queries))
	for _, s := range dbp.queries {
		stats = append(stats, s)
	}

	// Sort by total duration (descending)
	for i := 0; i < len(stats); i++ {
		for j := i + 1; j < len(stats); j++ {
			if stats[j].TotalDuration > stats[i].TotalDuration {
				stats[i], stats[j] = stats[j], stats[i]
			}
		}
	}

	return stats
}

// PrintStats prints query statistics
func (dbp *DBProfiler) PrintStats() {
	stats := dbp.GetStats()

	log.Println(">Database Query Statistics:")
	log.Println("================================================================================")

	if len(stats) == 0 {
		log.Println("No queries tracked yet")
		return
	}

	for i, s := range stats {
		if i >= 20 { // Show top 20 queries
			break
		}

		s.mu.RLock()
		log.Printf("#%d Query: %s", i+1, truncateQuery(s.Query, 80))
		log.Printf("   Count: %d, Total: %v, Avg: %v, Min: %v, Max: %v",
			s.Count, s.TotalDuration, s.AvgDuration, s.MinDuration, s.MaxDuration)
		s.mu.RUnlock()
		log.Println()
	}

	log.Println("================================================================================")
}

// Reset resets all statistics
func (dbp *DBProfiler) Reset() {
	dbp.mu.Lock()
	defer dbp.mu.Unlock()
	dbp.queries = make(map[string]*QueryStats)
	log.Println("🔄 Database profiler statistics reset")
}

// GetSlowQueries returns queries slower than threshold
func (dbp *DBProfiler) GetSlowQueries(threshold time.Duration) []*QueryStats {
	dbp.mu.RLock()
	defer dbp.mu.RUnlock()

	var slow []*QueryStats
	for _, s := range dbp.queries {
		s.mu.RLock()
		if s.AvgDuration > threshold {
			slow = append(slow, s)
		}
		s.mu.RUnlock()
	}

	return slow
}

// EnablePostgresSQLLogging enables PostgreSQL query logging
// This should be run once at startup
func EnablePostgresSQLLogging(db *sql.DB) error {
	queries := []string{
		// Log all queries (WARNING: Very verbose, only for debugging)
		// "ALTER SYSTEM SET log_statement = 'all'",

		// Log slow queries only (recommended)
		"ALTER SYSTEM SET log_min_duration_statement = 100", // Log queries > 100ms

		// Log query execution plan
		"SET auto_explain.log_min_duration = 100",
		"SET auto_explain.log_analyze = true",
		"SET auto_explain.log_buffers = true",

		// Reload configuration
		"SELECT pg_reload_conf()",
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			log.Printf("Warning: Failed to enable PostgreSQL logging: %v", err)
			// Don't return error, continue anyway
		}
	}

	log.Println("PostgreSQL query logging enabled (threshold: 100ms)")
	return nil
}

// AnalyzeTable runs ANALYZE on a table to update statistics
func AnalyzeTable(db *sql.DB, tableName string) error {
	query := fmt.Sprintf("ANALYZE %s", tableName)
	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to analyze table %s: %w", tableName, err)
	}
	log.Printf("Analyzed table: %s", tableName)
	return nil
}

// GetTableStats returns table statistics
func GetTableStats(db *sql.DB, tableName string) error {
	query := `
		SELECT 
			schemaname,
			tablename,
			pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size,
			n_live_tup AS rows,
			n_dead_tup AS dead_rows,
			last_vacuum,
			last_autovacuum,
			last_analyze,
			last_autoanalyze
		FROM pg_stat_user_tables
		WHERE tablename = $1
	`

	var (
		schema          string
		table           string
		size            string
		rows            int64
		deadRows        int64
		lastVacuum      sql.NullTime
		lastAutoVacuum  sql.NullTime
		lastAnalyze     sql.NullTime
		lastAutoAnalyze sql.NullTime
	)

	err := db.QueryRow(query, tableName).Scan(
		&schema, &table, &size, &rows, &deadRows,
		&lastVacuum, &lastAutoVacuum, &lastAnalyze, &lastAutoAnalyze,
	)

	if err != nil {
		return fmt.Errorf("failed to get stats for table %s: %w", tableName, err)
	}

	log.Printf(">Table Statistics: %s.%s", schema, table)
	log.Printf("   Size: %s", size)
	log.Printf("   Live rows: %d", rows)
	log.Printf("   Dead rows: %d", deadRows)
	log.Printf("   Last vacuum: %v", lastVacuum.Time)
	log.Printf("   Last analyze: %v", lastAnalyze.Time)

	return nil
}

// GetIndexUsage returns index usage statistics
func GetIndexUsage(db *sql.DB, tableName string) error {
	query := `
		SELECT
			schemaname,
			tablename,
			indexname,
			idx_scan AS scans,
			idx_tup_read AS tuples_read,
			idx_tup_fetch AS tuples_fetched,
			pg_size_pretty(pg_relation_size(indexrelid)) AS size
		FROM pg_stat_user_indexes
		WHERE tablename = $1
		ORDER BY idx_scan DESC
	`

	rows, err := db.Query(query, tableName)
	if err != nil {
		return fmt.Errorf("failed to get index usage for table %s: %w", tableName, err)
	}
	defer rows.Close()

	log.Printf(">Index Usage Statistics: %s", tableName)
	log.Println("================================================================================")

	for rows.Next() {
		var (
			schema        string
			table         string
			indexName     string
			scans         int64
			tuplesRead    int64
			tuplesFetched int64
			size          string
		)

		if err := rows.Scan(&schema, &table, &indexName, &scans, &tuplesRead, &tuplesFetched, &size); err != nil {
			return err
		}

		log.Printf("Index: %s", indexName)
		log.Printf("   Scans: %d, Tuples Read: %d, Tuples Fetched: %d, Size: %s",
			scans, tuplesRead, tuplesFetched, size)

		if scans == 0 {
			log.Printf("    WARNING: Index never used - consider dropping")
		}
		log.Println()
	}

	log.Println("================================================================================")
	return nil
}

// truncateQuery truncates a query string for display
func truncateQuery(query string, maxLen int) string {
	// Remove extra whitespace
	query = fmt.Sprintf("%s", query)

	if len(query) > maxLen {
		return query[:maxLen] + "..."
	}
	return query
}

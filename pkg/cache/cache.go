package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Cache provides SQLite-based caching for GitHub data
type Cache struct {
	db      *sql.DB
	maxAge  time.Duration
	dbPath  string
}

// CacheEntry represents a cached item
type CacheEntry struct {
	Key       string    `json:"key"`
	Data      []byte    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// New creates a new cache instance
func New(dbPath string, maxAgeDays int) (*Cache, error) {
	slog.Debug("Creating cache", "path", dbPath, "max_age_days", maxAgeDays)
	
	// Ensure parent directory exists
	cacheDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		slog.Error("Failed to create cache directory", "dir", cacheDir, "error", err)
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		slog.Error("Failed to open cache database", "path", dbPath, "error", err)
		return nil, fmt.Errorf("failed to open cache database: %w", err)
	}

	cache := &Cache{
		db:     db,
		maxAge: time.Duration(maxAgeDays) * 24 * time.Hour,
		dbPath: dbPath,
	}

	if err := cache.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize cache: %w", err)
	}

	return cache, nil
}

// initialize creates the cache table if it doesn't exist
func (c *Cache) initialize() error {
	query := `
		CREATE TABLE IF NOT EXISTS cache_entries (
			key TEXT PRIMARY KEY,
			data BLOB NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		);
		
		CREATE INDEX IF NOT EXISTS idx_expires_at ON cache_entries(expires_at);
	`

	if _, err := c.db.Exec(query); err != nil {
		return fmt.Errorf("failed to create cache table: %w", err)
	}

	return nil
}

// Get retrieves a cached value by key
func (c *Cache) Get(key string, dest interface{}) error {
	query := `
		SELECT data, expires_at 
		FROM cache_entries 
		WHERE key = ? AND expires_at > datetime('now')
	`

	var data []byte
	var expiresAt string
	
	err := c.db.QueryRow(query, key).Scan(&data, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrCacheMiss
		}
		return fmt.Errorf("failed to get cache entry: %w", err)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("failed to unmarshal cached data: %w", err)
	}

	return nil
}

// Set stores a value in the cache with the configured TTL
func (c *Cache) Set(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(c.maxAge)

	query := `
		INSERT OR REPLACE INTO cache_entries (key, data, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`

	if _, err := c.db.Exec(query, key, data, now, expiresAt); err != nil {
		return fmt.Errorf("failed to set cache entry: %w", err)
	}

	return nil
}

// Delete removes a cache entry by key
func (c *Cache) Delete(key string) error {
	query := `DELETE FROM cache_entries WHERE key = ?`
	
	if _, err := c.db.Exec(query, key); err != nil {
		return fmt.Errorf("failed to delete cache entry: %w", err)
	}

	return nil
}

// Clear removes all cache entries
func (c *Cache) Clear() error {
	query := `DELETE FROM cache_entries`
	
	if _, err := c.db.Exec(query); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	return nil
}

// Cleanup removes expired entries from the cache
func (c *Cache) Cleanup() error {
	query := `DELETE FROM cache_entries WHERE expires_at <= datetime('now')`
	
	result, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to cleanup cache: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		// Vacuum database after cleanup to reclaim space
		if _, err := c.db.Exec("VACUUM"); err != nil {
			// Log but don't fail on vacuum error
			fmt.Printf("Warning: failed to vacuum cache database: %v\n", err)
		}
	}

	return nil
}

// Stats returns cache statistics
func (c *Cache) Stats() (*CacheStats, error) {
	var total, expired int64

	// Get total entries
	err := c.db.QueryRow("SELECT COUNT(*) FROM cache_entries").Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to get total cache entries: %w", err)
	}

	// Get expired entries
	err = c.db.QueryRow("SELECT COUNT(*) FROM cache_entries WHERE expires_at <= datetime('now')").Scan(&expired)
	if err != nil {
		return nil, fmt.Errorf("failed to get expired cache entries: %w", err)
	}

	return &CacheStats{
		TotalEntries:   total,
		ExpiredEntries: expired,
		ValidEntries:   total - expired,
		DatabasePath:   c.dbPath,
	}, nil
}

// Close closes the cache database connection
func (c *Cache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// CacheStats represents cache statistics
type CacheStats struct {
	TotalEntries   int64  `json:"total_entries"`
	ExpiredEntries int64  `json:"expired_entries"`
	ValidEntries   int64  `json:"valid_entries"`
	DatabasePath   string `json:"database_path"`
}

// Cache-specific errors
var (
	ErrCacheMiss = fmt.Errorf("cache miss")
)

// CacheKey generates cache keys for different GitHub data types
func CacheKey(dataType, identifier string) string {
	return fmt.Sprintf("%s:%s", dataType, identifier)
}

// PRCacheKey generates a cache key for PR data
func PRCacheKey(owner, repo string, number int) string {
	return CacheKey("pr", fmt.Sprintf("%s/%s#%d", owner, repo, number))
}

// DiffStatsCacheKey generates a cache key for diff stats
func DiffStatsCacheKey(owner, repo string, number int) string {
	return CacheKey("diff", fmt.Sprintf("%s/%s#%d", owner, repo, number))
}

// CheckStatusCacheKey generates a cache key for check status
func CheckStatusCacheKey(owner, repo string, number int) string {
	return CacheKey("checks", fmt.Sprintf("%s/%s#%d", owner, repo, number))
}

// ReviewsCacheKey generates a cache key for reviews
func ReviewsCacheKey(owner, repo string, number int) string {
	return CacheKey("reviews", fmt.Sprintf("%s/%s#%d", owner, repo, number))
}

// SearchCacheKey generates a cache key for search results
func SearchCacheKey(query string) string {
	return CacheKey("search", query)
}
package cache

// Cache defines the interface for cache operations
type Cache interface {
	Get(key string, value interface{}) error
	Set(key string, value interface{}) error
	Delete(key string) error
	Cleanup() error
	Close() error
}

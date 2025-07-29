package cache

// NoOpCache is a no-operation cache that does nothing
// Used when caching is disabled
type NoOpCache struct{}

// NewNoOpCache creates a new no-op cache instance
func NewNoOpCache() Cache {
	return &NoOpCache{}
}

// Get always returns an error indicating cache miss
func (n *NoOpCache) Get(key string, value interface{}) error {
	return ErrCacheMiss
}

// Set always succeeds without storing anything
func (n *NoOpCache) Set(key string, value interface{}) error {
	return nil
}

// Delete always succeeds without doing anything
func (n *NoOpCache) Delete(key string) error {
	return nil
}

// Cleanup always succeeds without doing anything
func (n *NoOpCache) Cleanup() error {
	return nil
}

// Close always succeeds without doing anything
func (n *NoOpCache) Close() error {
	return nil
}

// Ensure NoOpCache implements Cache interface
var _ Cache = (*NoOpCache)(nil)

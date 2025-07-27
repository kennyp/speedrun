package backoffconfig

import (
	"time"

	"github.com/cenkalti/backoff/v4"
)

// Config holds backoff configuration
type Config struct {
	MaxElapsedTime  time.Duration
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	RandomizationFactor float64
}

// GlobalConfig holds backoff configuration for different services
type GlobalConfig struct {
	Default Config
	GitHub  Config
	OpenAI  Config
}

// DefaultGlobalConfig returns default backoff configuration
func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Default: Config{
			MaxElapsedTime:      30 * time.Second,
			InitialInterval:     1 * time.Second,
			MaxInterval:         10 * time.Second,
			Multiplier:          2.0,
			RandomizationFactor: 0.1,
		},
		GitHub: Config{
			MaxElapsedTime:      60 * time.Second,
			InitialInterval:     1 * time.Second,
			MaxInterval:         15 * time.Second,
			Multiplier:          2.0,
			RandomizationFactor: 0.2,
		},
		OpenAI: Config{
			MaxElapsedTime:      90 * time.Second,
			InitialInterval:     2 * time.Second,
			MaxInterval:         30 * time.Second,
			Multiplier:          2.0,
			RandomizationFactor: 0.3,
		},
	}
}

// ToExponentialBackoff converts Config to cenkalti/backoff ExponentialBackOff
func (c *Config) ToExponentialBackoff() *backoff.ExponentialBackOff {
	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.MaxElapsedTime = c.MaxElapsedTime
	exponentialBackoff.InitialInterval = c.InitialInterval
	exponentialBackoff.MaxInterval = c.MaxInterval
	exponentialBackoff.Multiplier = c.Multiplier
	exponentialBackoff.RandomizationFactor = c.RandomizationFactor
	return exponentialBackoff
}

// WithDefaults returns a config with default values filled in for zero values
func (c *Config) WithDefaults(defaults Config) Config {
	result := *c
	
	if result.MaxElapsedTime == 0 {
		result.MaxElapsedTime = defaults.MaxElapsedTime
	}
	if result.InitialInterval == 0 {
		result.InitialInterval = defaults.InitialInterval
	}
	if result.MaxInterval == 0 {
		result.MaxInterval = defaults.MaxInterval
	}
	if result.Multiplier == 0 {
		result.Multiplier = defaults.Multiplier
	}
	if result.RandomizationFactor == 0 {
		result.RandomizationFactor = defaults.RandomizationFactor
	}
	
	return result
}
package tsbridge

import (
	"time"
)

type Config struct {
	Options ConfigOptions
}

// ConfigOptions is a set of global options required to initialize configuration.
type ConfigOptions struct {
	CounterResetInterval     time.Duration
	EnableStatusPage         bool
	Filename                 string
	MinPointAge              time.Duration
	SDInternalMetricsProject string
	SDLookBackInterval       time.Duration
	StorageEngine            string
	UpdateParallelism        int
	SyncPeriod               time.Duration
	SyncCleanupAfter         int
}

func NewConfig(options *ConfigOptions) *Config {
	return &Config{
		Options: *options,
	}
}

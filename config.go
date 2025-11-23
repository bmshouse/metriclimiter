package metriclimiterprocessor

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/collector/component"
)

// Config defines the configuration options for the metric limiter processor.
type Config struct {
	// MetricNames is the list of metric names to rate limit.
	MetricNames []string `mapstructure:"metric_names"`

	// RateIntervalSeconds is the rate interval in seconds.
	// A metric matching the list will only be allowed once per this interval.
	RateIntervalSeconds int `mapstructure:"rate_interval_seconds"`

	// PerLabelSet enables per-label-set rate limiting.
	// When true: Rate limits based on metric name + attribute values
	// When false: Rate limits based on metric name only (default, backward compatible)
	PerLabelSet bool `mapstructure:"per_label_set"`

	// MaxCardinalityPerMetric is the maximum number of unique label sets to track per metric.
	// Prevents unbounded memory growth. Uses LRU eviction when exceeded.
	// Default: 200000 (approximately 12.8 MB per metric at 64 bytes per entry)
	// Set to 0 to disable (not recommended for per_label_set mode)
	MaxCardinalityPerMetric int `mapstructure:"max_cardinality_per_metric"`

	// CardinalityTTLSeconds is the TTL for inactive label sets in seconds.
	// Label sets not seen within this duration are removed by background cleanup.
	// 0 = disabled (use LRU eviction only). Default: 0
	CardinalityTTLSeconds int `mapstructure:"cardinality_ttl_seconds"`

	// MetricConfigs contains per-metric configuration overrides.
	// Overrides global settings for specific metrics.
	MetricConfigs []MetricConfig `mapstructure:"metric_configs"`
}

// MetricConfig defines per-metric rate limiting configuration.
type MetricConfig struct {
	// Name is the metric name (must match an entry in MetricNames)
	Name string `mapstructure:"name"`

	// PerLabelSet overrides the global PerLabelSet setting for this metric
	PerLabelSet *bool `mapstructure:"per_label_set"`

	// RateIntervalSeconds overrides the global rate interval for this metric
	RateIntervalSeconds *int `mapstructure:"rate_interval_seconds"`

	// MaxCardinality overrides the global max cardinality for this metric
	MaxCardinality *int `mapstructure:"max_cardinality"`

	// CardinalityTTLSeconds overrides the global TTL for this metric
	CardinalityTTLSeconds *int `mapstructure:"cardinality_ttl_seconds"`
}

// Validate checks that the configuration is valid.
func (cfg *Config) Validate() error {
	if len(cfg.MetricNames) == 0 {
		return errors.New("metric_names cannot be empty")
	}

	if cfg.RateIntervalSeconds <= 0 {
		return fmt.Errorf("rate_interval_seconds must be positive, got %d", cfg.RateIntervalSeconds)
	}

	// Ensure all metric names are non-empty
	for i, name := range cfg.MetricNames {
		if name == "" {
			return fmt.Errorf("metric_names[%d] cannot be empty", i)
		}
	}

	// Validate cardinality settings
	if cfg.MaxCardinalityPerMetric < 0 {
		return fmt.Errorf("max_cardinality_per_metric must be >= 0, got %d", cfg.MaxCardinalityPerMetric)
	}

	if cfg.MaxCardinalityPerMetric > 0 && cfg.MaxCardinalityPerMetric < 1000 {
		return fmt.Errorf("max_cardinality_per_metric must be >= 1000 if set, got %d", cfg.MaxCardinalityPerMetric)
	}

	if cfg.CardinalityTTLSeconds < 0 {
		return fmt.Errorf("cardinality_ttl_seconds must be >= 0, got %d", cfg.CardinalityTTLSeconds)
	}

	// Validate per-metric configs
	for i, mc := range cfg.MetricConfigs {
		if mc.Name == "" {
			return fmt.Errorf("metric_configs[%d].name cannot be empty", i)
		}

		// Check if metric name is in MetricNames
		found := false
		for _, name := range cfg.MetricNames {
			if name == mc.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("metric_configs[%d].name %q is not in metric_names", i, mc.Name)
		}

		// Validate metric-specific cardinality
		if mc.MaxCardinality != nil && *mc.MaxCardinality > 0 && *mc.MaxCardinality < 1000 {
			return fmt.Errorf("metric_configs[%d].max_cardinality must be >= 1000 if set, got %d", i, *mc.MaxCardinality)
		}

		// Validate metric-specific rate interval
		if mc.RateIntervalSeconds != nil && *mc.RateIntervalSeconds <= 0 {
			return fmt.Errorf("metric_configs[%d].rate_interval_seconds must be positive, got %d", i, *mc.RateIntervalSeconds)
		}

		// Validate metric-specific TTL
		if mc.CardinalityTTLSeconds != nil && *mc.CardinalityTTLSeconds < 0 {
			return fmt.Errorf("metric_configs[%d].cardinality_ttl_seconds must be >= 0, got %d", i, *mc.CardinalityTTLSeconds)
		}
	}

	return nil
}

var _ component.Config = (*Config)(nil)

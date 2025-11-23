package metriclimiterprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"
)

const (
	typeStr   = "metriclimiter"
	stability = component.StabilityLevelBeta
)

// NewFactory creates a new factory for the metric limiter processor.
// The factory follows the standard OpenTelemetry Collector factory pattern
// and supports configuration validation, default configuration creation,
// and processor instantiation.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithMetrics(createMetricsProcessor, stability),
	)
}

// createDefaultConfig creates the default configuration for the processor.
// It returns a Config with sensible defaults: 60-second rate interval,
// name-only rate limiting mode, and 200K max cardinality.
func createDefaultConfig() component.Config {
	return &Config{
		RateIntervalSeconds:     60, // Default: 1 per minute
		PerLabelSet:             false,
		MaxCardinalityPerMetric: 200000, // Default: 200K entries
		CardinalityTTLSeconds:   0,      // Disabled by default
	}
}

// createMetricsProcessor creates a new metric limiter processor instance.
// It initializes the processor with the provided configuration, creates metric-specific
// configurations (handling per-metric overrides), and wraps it with the standard
// processorhelper.NewMetrics for lifecycle management and metrics handling.
func createMetricsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (processor.Metrics, error) {
	config := cfg.(*Config)

	p := &metricLimiterProcessor{
		config:            config,
		logger:            set.Logger,
		limitedMetrics:    make(map[string]*metricConfig, len(config.MetricNames)),
		rateIntervalNanos: int64(config.RateIntervalSeconds) * 1e9,
	}

	// Initialize metric configs
	for _, metricName := range config.MetricNames {
		mc, err := p.getMetricConfig(metricName, config)
		if err != nil {
			set.Logger.Error("failed to create metric config",
				zap.String("metric", metricName),
				zap.Error(err),
			)
			return nil, err
		}
		p.limitedMetrics[metricName] = mc
	}

	// Log configuration
	set.Logger.Info("creating metric limiter processor",
		zap.Int("metric_count", len(config.MetricNames)),
		zap.Int("rate_interval_seconds", config.RateIntervalSeconds),
		zap.Bool("per_label_set", config.PerLabelSet),
		zap.Int("max_cardinality_per_metric", config.MaxCardinalityPerMetric),
		zap.Int("cardinality_ttl_seconds", config.CardinalityTTLSeconds),
	)

	return processorhelper.NewMetrics(
		ctx,
		set,
		cfg,
		nextConsumer,
		p.processMetrics,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}

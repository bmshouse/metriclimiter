// Package metriclimiterprocessor provides a processor for rate limiting metrics
// in the OpenTelemetry Collector.
//
// The metric limiter processor allows you to limit the rate at which specific metrics
// are processed. It supports two rate limiting modes:
//
// 1. Name-Only Mode (default): Rate limit based on metric name only
// 2. Per-Label-Set Mode: Rate limit based on metric name + label combinations
//
// Configuration:
//
//	processors:
//	  metriclimiter:
//	    metric_names:
//	      - "system.cpu.utilization"
//	      - "system.memory.usage"
//	    rate_interval_seconds: 60
//	    per_label_set: false              # Default: name-only mode
//
// Per-Label-Set Mode Example:
//
//	processors:
//	  metriclimiter:
//	    metric_names:
//	      - "http.request.duration"
//	    rate_interval_seconds: 60
//	    per_label_set: true               # Enable per-label-set tracking
//	    max_cardinality_per_metric: 100000 # Track up to 100K unique label sets
//
// Features:
// - O(1) metric name lookup performance
// - Per-label-set rate limiting with xxHash64 for efficient hashing
// - Thread-safe concurrent access (mutex for name-only, LRU cache for per-label-set)
// - Zero-copy metric filtering using RemoveIf()
// - Minimal memory overhead:
//   * Name-only mode: ~8 bytes per tracked metric
//   * Per-label-set mode: ~8 bytes per unique label combination (vs. 100+ with string concat)
// - Pre-compiled metric name lookup maps
// - Pre-calculated rate intervals for efficiency
// - LRU cache with automatic eviction for bounded memory usage
// - Per-metric configuration overrides for fine-grained control
//
// Memory Efficiency Example (84K unique label combinations):
// - String concatenation approach: ~10+ MB
// - xxHash64 + LRU cache: ~672 KB (93% reduction)
//
// Example:
//
//	factory := metriclimiterprocessor.NewFactory()
//	processor, err := factory.CreateMetricsProcessor(
//	    context.Background(),
//	    processorSettings,
//	    &Config{
//	        MetricNames: []string{"http.request.duration"},
//	        RateIntervalSeconds: 60,
//	        PerLabelSet: true,
//	        MaxCardinalityPerMetric: 100000,
//	    },
//	    nextConsumer,
//	)
//
// The processor integrates seamlessly with the OpenTelemetry Collector framework
// and follows standard processor patterns for configuration and lifecycle management.
package metriclimiterprocessor

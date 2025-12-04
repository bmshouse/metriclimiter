package metriclimiterprocessor

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	lru "github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// metricLimiterProcessor implements the metric rate limiter.
type metricLimiterProcessor struct {
	config            *Config
	logger            *zap.Logger
	limitedMetrics    map[string]*metricConfig // Metric name -> config
	rateIntervalNanos int64                    // Pre-calculated interval in nanoseconds
}

// metricConfig holds the per-metric rate limiting state.
type metricConfig struct {
	name                string
	perLabelSet         bool
	rateIntervalNanos   int64
	maxCardinality      int
	cardinalityTTLNanos int64

	// For name-only rate limiting (perLabelSet = false)
	// Uses sliding window to ensure exactly rate_interval_seconds between metrics
	lastSeenNameMu    sync.Mutex
	lastSeenTimestamp int64 // Last allowed metric timestamp in nanoseconds

	// For per-label-set rate limiting (perLabelSet = true)
	// Maps hash key -> last allowed metric timestamp in nanoseconds for each label set
	lastSeenLabelSets *lru.Cache[uint64, int64]

	// For logging/monitoring
	droppedCount   int64
	allowedCount   int64
	evictedCount   int64
	collisionCount int64
}

// start initializes the processor.
func (p *metricLimiterProcessor) start(ctx context.Context, _ any) error {
	p.logger.Debug("metric limiter processor starting")
	return nil
}

// shutdown closes the processor.
func (p *metricLimiterProcessor) shutdown(ctx context.Context) error {
	p.logger.Debug("metric limiter processor shutting down")
	return nil
}

// processMetrics processes incoming metrics through the rate limiting processor.
// It applies rate limiting based on the configured metric_names and rate intervals.
// Metrics exceeding the rate limit are filtered from the output.
// The processor mutates the input metric data (removes metrics), as declared
// in the capabilities.
func (p *metricLimiterProcessor) processMetrics(
	ctx context.Context,
	md pmetric.Metrics,
) (pmetric.Metrics, error) {
	resourceMetrics := md.ResourceMetrics()

	// Iterate through the metric hierarchy and remove rate-limited metrics
	for i := 0; i < resourceMetrics.Len(); i++ {
		rm := resourceMetrics.At(i)
		scopeMetrics := rm.ScopeMetrics()

		for j := 0; j < scopeMetrics.Len(); j++ {
			sm := scopeMetrics.At(j)
			metrics := sm.Metrics()

			// Process each metric and remove only the data points that
			// are rate-limited. We operate at the data-point level so
			// different label-sets in the same metric are treated
			// independently.
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)
				metricName := metric.Name()

				mc, ok := p.limitedMetrics[metricName]
				if !ok {
					// Not rate-limited, leave all data points
					continue
				}

				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					dps := metric.Gauge().DataPoints()
					if mc.perLabelSet {
						dps.RemoveIf(func(dp pmetric.NumberDataPoint) bool {
							return p.shouldDropByLabelSet(mc, dp.Attributes())
						})
					} else {
						dps.RemoveIf(func(dp pmetric.NumberDataPoint) bool {
							return p.shouldDropByName(mc)
						})
					}
				case pmetric.MetricTypeSum:
					dps := metric.Sum().DataPoints()
					if mc.perLabelSet {
						dps.RemoveIf(func(dp pmetric.NumberDataPoint) bool {
							return p.shouldDropByLabelSet(mc, dp.Attributes())
						})
					} else {
						dps.RemoveIf(func(dp pmetric.NumberDataPoint) bool {
							return p.shouldDropByName(mc)
						})
					}
				case pmetric.MetricTypeHistogram:
					dps := metric.Histogram().DataPoints()
					if mc.perLabelSet {
						dps.RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
							return p.shouldDropByLabelSet(mc, dp.Attributes())
						})
					} else {
						dps.RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
							return p.shouldDropByName(mc)
						})
					}
				case pmetric.MetricTypeExponentialHistogram:
					dps := metric.ExponentialHistogram().DataPoints()
					if mc.perLabelSet {
						dps.RemoveIf(func(dp pmetric.ExponentialHistogramDataPoint) bool {
							return p.shouldDropByLabelSet(mc, dp.Attributes())
						})
					} else {
						dps.RemoveIf(func(dp pmetric.ExponentialHistogramDataPoint) bool {
							return p.shouldDropByName(mc)
						})
					}
				case pmetric.MetricTypeSummary:
					dps := metric.Summary().DataPoints()
					if mc.perLabelSet {
						dps.RemoveIf(func(dp pmetric.SummaryDataPoint) bool {
							return p.shouldDropByLabelSet(mc, dp.Attributes())
						})
					} else {
						dps.RemoveIf(func(dp pmetric.SummaryDataPoint) bool {
							return p.shouldDropByName(mc)
						})
					}
				default:
					// Unknown/unsupported metric type: do nothing
				}
			}

			// Remove any metrics that now have zero data points (all data points were filtered).
			metrics.RemoveIf(func(metric pmetric.Metric) bool {
				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					return metric.Gauge().DataPoints().Len() == 0
				case pmetric.MetricTypeSum:
					return metric.Sum().DataPoints().Len() == 0
				case pmetric.MetricTypeHistogram:
					return metric.Histogram().DataPoints().Len() == 0
				case pmetric.MetricTypeExponentialHistogram:
					return metric.ExponentialHistogram().DataPoints().Len() == 0
				case pmetric.MetricTypeSummary:
					return metric.Summary().DataPoints().Len() == 0
				default:
					return false
				}
			})
		}
	}

	return md, nil
}

// shouldDropMetric determines if a metric should be dropped based on rate limiting.
// This helper preserves backward compatibility for tests that exercise
// metric-level behavior: it inspects the first data point in the metric and
// applies the configured rate-limiting mode (name-only or per-label-set).
// Returns true if the metric (based on its first data point) should be dropped.
func (p *metricLimiterProcessor) shouldDropMetric(metric pmetric.Metric) bool {
	metricName := metric.Name()

	mc, ok := p.limitedMetrics[metricName]
	if !ok {
		return false // Not in rate limit list, allow
	}

	if !mc.perLabelSet {
		// Rate limit by name only (backward compatible)
		return p.shouldDropByName(mc)
	}

	// Rate limit by label set - extract attributes from metric data points
	// For per-label-set mode, we check the attributes of the first data point.
	return p.shouldDropByLabelSet(mc, p.extractAttributes(metric))
}

// extractAttributes extracts attributes from a metric's data points.
// Handles all metric types: Gauge, Sum, Histogram, ExponentialHistogram, Summary.
// Returns the attributes from the first data point found.
func (p *metricLimiterProcessor) extractAttributes(metric pmetric.Metric) pcommon.Map {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		if dp := metric.Gauge().DataPoints(); dp.Len() > 0 {
			return dp.At(0).Attributes()
		}
	case pmetric.MetricTypeSum:
		if dp := metric.Sum().DataPoints(); dp.Len() > 0 {
			return dp.At(0).Attributes()
		}
	case pmetric.MetricTypeHistogram:
		if dp := metric.Histogram().DataPoints(); dp.Len() > 0 {
			return dp.At(0).Attributes()
		}
	case pmetric.MetricTypeExponentialHistogram:
		if dp := metric.ExponentialHistogram().DataPoints(); dp.Len() > 0 {
			return dp.At(0).Attributes()
		}
	case pmetric.MetricTypeSummary:
		if dp := metric.Summary().DataPoints(); dp.Len() > 0 {
			return dp.At(0).Attributes()
		}
	}
	// Fallback to empty map if no data points found
	return pcommon.NewMap()
}

// shouldDropByName determines if a metric should be dropped based on metric name only.
// This implements name-only rate limiting mode using a sliding window approach.
// Each metric is allowed only if at least rate_interval_seconds have passed since the last allowed metric.
// This ensures exactly 1 metric per interval with no phase drift or gaps.
// Returns true if the metric is within the rate interval (should drop), false otherwise.
func (p *metricLimiterProcessor) shouldDropByName(mc *metricConfig) bool {
	mc.lastSeenNameMu.Lock()
	defer mc.lastSeenNameMu.Unlock()

	now := time.Now().UnixNano()

	if mc.lastSeenTimestamp > 0 {
		timeSinceLastSeen := now - mc.lastSeenTimestamp

		// Use < to check if within rate interval (not yet elapsed)
		// A metric is allowed if at least rate_interval_seconds have passed
		if timeSinceLastSeen < mc.rateIntervalNanos {
			// Within rate interval: DROP
			mc.droppedCount++
			p.logger.Debug("dropping rate-limited metric (by name)",
				zap.String("metric", mc.name),
				zap.Int64("time_since_last_ms", timeSinceLastSeen/1e6),
				zap.Int("rate_interval_s", int(mc.rateIntervalNanos/1e9)),
			)
			return true
		}
	}

	// Not seen before or outside rate interval: ALLOW and record timestamp
	mc.lastSeenTimestamp = now
	mc.allowedCount++
	p.logger.Debug("allowing metric (by name)",
		zap.String("metric", mc.name),
	)
	return false
}

// shouldDropByLabelSet determines if a metric should be dropped based on metric name + attributes.
// This implements per-label-set rate limiting mode using an LRU cache with sliding window.
// Each unique combination of metric name and attribute values is tracked independently.
// Uses timestamp-based sliding window to ensure exactly rate_interval_seconds between metrics per label set.
// Returns true if the label set is within the rate interval (should drop), false otherwise.
func (p *metricLimiterProcessor) shouldDropByLabelSet(mc *metricConfig, attributes pcommon.Map) bool {
	// Generate hash key from metric name + attributes
	key := p.generateHashKey(mc.name, attributes)

	now := time.Now().UnixNano()

	// Check LRU cache for last seen timestamp
	if lastSeenTimestampAny, ok := mc.lastSeenLabelSets.Get(key); ok {
		lastSeenTimestamp := lastSeenTimestampAny
		timeSinceLastSeen := now - lastSeenTimestamp

		// Use < to check if within rate interval (not yet elapsed)
		// A metric is allowed if at least rate_interval_seconds have passed
		if timeSinceLastSeen < mc.rateIntervalNanos {
			// Within rate interval: DROP
			mc.droppedCount++
			p.logger.Debug("dropping rate-limited metric (by label set)",
				zap.String("metric", mc.name),
				zap.Uint64("key", key),
				zap.Int64("time_since_last_ms", timeSinceLastSeen/1e6),
			)
			return true
		}
	}

	// Not seen before or outside rate interval: ALLOW and record timestamp
	mc.lastSeenLabelSets.Add(key, now)
	mc.allowedCount++
	p.logger.Debug("allowing metric (by label set)",
		zap.String("metric", mc.name),
		zap.Uint64("key", key),
	)
	return false
}

// generateHashKey generates a uint64 hash key from metric name and attributes.
// Uses xxHash64 for fast (5-13 GB/s throughput), deterministic hashing with minimal allocations.
// Attributes are sorted before hashing to ensure order-independent results.
// The resulting hash is used as a key in the LRU cache for per-label-set rate limiting.
func (p *metricLimiterProcessor) generateHashKey(metricName string, attributes pcommon.Map) uint64 {
	h := xxhash.New()

	// Hash metric name
	h.WriteString(metricName)

	// Sort attribute keys for deterministic hashing
	// (attributes in different order should produce same hash)
	keys := make([]string, 0, attributes.Len())
	attributes.Range(func(k string, v pcommon.Value) bool {
		keys = append(keys, k)
		return true
	})
	sort.Strings(keys)

	// Hash attributes in sorted order
	for _, k := range keys {
		v, _ := attributes.Get(k)
		h.WriteString(k)
		h.WriteString("=")
		h.WriteString(v.AsString())
	}

	return h.Sum64()
}

// getMetricConfig returns or creates a metric configuration with proper settings.
// It applies per-metric configuration overrides from the config if available,
// otherwise uses global defaults. For per-label-set mode, it creates an LRU cache
// with configurable max cardinality and eviction callbacks.
func (p *metricLimiterProcessor) getMetricConfig(
	metricName string,
	config *Config,
) (*metricConfig, error) {
	// Check for per-metric overrides
	var perLabelSet bool = config.PerLabelSet
	var rateIntervalNanos int64 = int64(config.RateIntervalSeconds) * 1e9
	var maxCardinality int = config.MaxCardinalityPerMetric
	var cardinalityTTLNanos int64 = 0

	if config.CardinalityTTLSeconds > 0 {
		cardinalityTTLNanos = int64(config.CardinalityTTLSeconds) * 1e9
	}

	// Apply per-metric config overrides
	for _, mc := range config.MetricConfigs {
		if mc.Name == metricName {
			if mc.PerLabelSet != nil {
				perLabelSet = *mc.PerLabelSet
			}
			if mc.RateIntervalSeconds != nil {
				rateIntervalNanos = int64(*mc.RateIntervalSeconds) * 1e9
			}
			if mc.MaxCardinality != nil {
				maxCardinality = *mc.MaxCardinality
			}
			if mc.CardinalityTTLSeconds != nil {
				cardinalityTTLNanos = int64(*mc.CardinalityTTLSeconds) * 1e9
			}
			break
		}
	}

	// Set sensible defaults if not specified
	if maxCardinality == 0 {
		maxCardinality = 200000 // Default: 200K entries
	}

	mc := &metricConfig{
		name:                metricName,
		perLabelSet:         perLabelSet,
		rateIntervalNanos:   rateIntervalNanos,
		maxCardinality:      maxCardinality,
		cardinalityTTLNanos: cardinalityTTLNanos,
	}

	// Create LRU cache for per-label-set mode
	if perLabelSet {
		cache, err := lru.NewWithEvict(maxCardinality, func(key uint64, value int64) {
			mc.evictedCount++
			p.logger.Debug("evicted label set from LRU cache",
				zap.String("metric", metricName),
				zap.Uint64("key", key),
				zap.Int("cache_size", maxCardinality),
			)
		})
		if err != nil {
			return nil, err
		}
		mc.lastSeenLabelSets = cache
	}

	p.logger.Debug("created metric config",
		zap.String("metric", metricName),
		zap.Bool("per_label_set", perLabelSet),
		zap.Int("max_cardinality", maxCardinality),
		zap.Int64("rate_interval_s", rateIntervalNanos/1e9),
	)

	return mc, nil
}

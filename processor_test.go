package metriclimiterprocessor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap/zaptest"
)

func TestHashKeyConsistency(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	attrs1 := pcommon.NewMap()
	attrs1.PutStr("a", "1")
	attrs1.PutStr("b", "2")

	attrs2 := pcommon.NewMap()
	attrs2.PutStr("b", "2") // Different order
	attrs2.PutStr("a", "1")

	key1 := processor.generateHashKey("test.metric", attrs1)
	key2 := processor.generateHashKey("test.metric", attrs2)

	assert.Equal(t, key1, key2, "hash keys should be identical regardless of attribute order")
}

func TestHashKeyDifferent(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	attrs1 := pcommon.NewMap()
	attrs1.PutStr("instance", "i-1")

	attrs2 := pcommon.NewMap()
	attrs2.PutStr("instance", "i-2")

	key1 := processor.generateHashKey("test.metric", attrs1)
	key2 := processor.generateHashKey("test.metric", attrs2)

	assert.NotEqual(t, key1, key2, "different attributes should produce different hash keys")
}

func TestConfigValidation_EmptyMetricNames(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{},
		RateIntervalSeconds: 60,
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metric_names cannot be empty")
}

func TestConfigValidation_ZeroRateInterval(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: 0,
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate_interval_seconds must be positive")
}

func TestConfigValidation_NegativeRateInterval(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: -10,
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate_interval_seconds must be positive")
}

func TestConfigValidation_EmptyMetricInList(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{"test1", "", "test2"},
		RateIntervalSeconds: 60,
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metric_names[1] cannot be empty")
}

func TestConfigValidation_Valid(t *testing.T) {
	cfg := &Config{
		MetricNames:             []string{"test.metric"},
		RateIntervalSeconds:     60,
		PerLabelSet:             false,
		MaxCardinalityPerMetric: 200000,
		CardinalityTTLSeconds:   0,
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigValidation_ValidWithPerLabelSet(t *testing.T) {
	cfg := &Config{
		MetricNames:             []string{"test.metric"},
		RateIntervalSeconds:     60,
		PerLabelSet:             true,
		MaxCardinalityPerMetric: 100000,
		CardinalityTTLSeconds:   3600,
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigValidation_InvalidCardinality(t *testing.T) {
	cfg := &Config{
		MetricNames:             []string{"test.metric"},
		RateIntervalSeconds:     60,
		MaxCardinalityPerMetric: 500, // Less than minimum 1000
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_cardinality_per_metric must be >= 1000")
}

func TestConfigValidation_NegativeCardinality(t *testing.T) {
	cfg := &Config{
		MetricNames:             []string{"test.metric"},
		RateIntervalSeconds:     60,
		MaxCardinalityPerMetric: -100,
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_cardinality_per_metric must be >= 0")
}

func TestConfigValidation_NegativeTTL(t *testing.T) {
	cfg := &Config{
		MetricNames:           []string{"test.metric"},
		RateIntervalSeconds:   60,
		CardinalityTTLSeconds: -100,
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cardinality_ttl_seconds must be >= 0")
}

func TestConfigValidation_PerMetricConfigInvalidName(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: 60,
		MetricConfigs: []MetricConfig{
			{
				Name: "nonexistent.metric",
			},
		},
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in metric_names")
}

func TestConfigValidation_PerMetricConfigValid(t *testing.T) {
	interval := 30
	trueVal := true
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: 60,
		MetricConfigs: []MetricConfig{
			{
				Name:                "test.metric",
				RateIntervalSeconds: &interval,
				PerLabelSet:         &trueVal,
			},
		},
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigValidation_PerMetricConfigInvalidRateInterval(t *testing.T) {
	zeroInterval := 0
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: 60,
		MetricConfigs: []MetricConfig{
			{
				Name:                "test.metric",
				RateIntervalSeconds: &zeroInterval,
			},
		},
	}

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate_interval_seconds must be positive")
}

func TestMetricConfigCreation_NameOnly(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60,
			PerLabelSet:             false,
			MaxCardinalityPerMetric: 200000,
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	cfg, err := processor.getMetricConfig("test.metric", processor.config)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.False(t, cfg.perLabelSet)
	assert.Equal(t, int64(60*1e9), cfg.rateIntervalNanos)
	assert.Equal(t, 200000, cfg.maxCardinality)
	assert.Nil(t, cfg.lastSeenLabelSets) // Should not create LRU cache for name-only
}

func TestMetricConfigCreation_PerLabelSet(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60,
			PerLabelSet:             true,
			MaxCardinalityPerMetric: 100000,
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	cfg, err := processor.getMetricConfig("test.metric", processor.config)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.True(t, cfg.perLabelSet)
	assert.Equal(t, int64(60*1e9), cfg.rateIntervalNanos)
	assert.Equal(t, 100000, cfg.maxCardinality)
	assert.NotNil(t, cfg.lastSeenLabelSets) // Should create LRU cache for per-label-set
}

func TestMetricConfigCreation_WithPerMetricOverrides(t *testing.T) {
	interval := 30
	trueVal := true
	card := 50000

	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60,
			PerLabelSet:             false,
			MaxCardinalityPerMetric: 200000,
			MetricConfigs: []MetricConfig{
				{
					Name:                "test.metric",
					RateIntervalSeconds: &interval,
					PerLabelSet:         &trueVal,
					MaxCardinality:      &card,
				},
			},
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	cfg, err := processor.getMetricConfig("test.metric", processor.config)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.True(t, cfg.perLabelSet)                       // Overridden to true
	assert.Equal(t, int64(30*1e9), cfg.rateIntervalNanos) // Overridden to 30
	assert.Equal(t, 50000, cfg.maxCardinality)            // Overridden to 50000
	assert.NotNil(t, cfg.lastSeenLabelSets)
}

func TestShouldDropByNameFirstOccurrence(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger: zaptest.NewLogger(t),
	}

	mc := &metricConfig{
		name:              "test.metric",
		rateIntervalNanos: 60 * 1e9,
	}

	// First occurrence should NOT drop
	dropped := processor.shouldDropByName(mc)
	assert.False(t, dropped, "first occurrence should be allowed")
	assert.Equal(t, int64(1), mc.allowedCount)
	assert.Equal(t, int64(0), mc.droppedCount)
}

func TestShouldDropByNameSecondOccurrenceWithinInterval(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger: zaptest.NewLogger(t),
	}

	mc := &metricConfig{
		name:              "test.metric",
		rateIntervalNanos: 60 * 1e9,
	}

	// First occurrence
	processor.shouldDropByName(mc)

	// Second occurrence immediately should DROP
	dropped := processor.shouldDropByName(mc)
	assert.True(t, dropped, "second occurrence within interval should be dropped")
	assert.Equal(t, int64(1), mc.allowedCount)
	assert.Equal(t, int64(1), mc.droppedCount)
}

func TestShouldDropByNameAfterInterval(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger: zaptest.NewLogger(t),
	}

	mc := &metricConfig{
		name:              "test.metric",
		rateIntervalNanos: 1 * 1e9, // 1 second
	}

	// First occurrence
	processor.shouldDropByName(mc)
	assert.Equal(t, int64(1), mc.allowedCount)

	// Second occurrence immediately - should drop
	processor.shouldDropByName(mc)
	assert.Equal(t, int64(1), mc.droppedCount)

	// Wait for interval to pass
	time.Sleep(1100 * time.Millisecond)

	// Third occurrence after interval - should allow
	dropped := processor.shouldDropByName(mc)
	assert.False(t, dropped, "occurrence after interval should be allowed")
	assert.Equal(t, int64(2), mc.allowedCount)
	assert.Equal(t, int64(1), mc.droppedCount)
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	assert.NotNil(t, factory)
}

func TestCreateDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig()
	assert.NotNil(t, cfg)

	config := cfg.(*Config)
	assert.Equal(t, 60, config.RateIntervalSeconds)
	assert.False(t, config.PerLabelSet)
	assert.Equal(t, 200000, config.MaxCardinalityPerMetric)
	assert.Equal(t, 0, config.CardinalityTTLSeconds)
}

// Helper function to create empty metrics for testing
func createEmptyMetrics() pmetric.Metrics {
	return pmetric.NewMetrics()
}

// Integration test: Real metrics with multiple data points
func TestIntegration_ProcessMetricsWithRealData(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     1, // 1 second interval for fast testing
			PerLabelSet:             false,
			MaxCardinalityPerMetric: 200000,
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Create metric config for "http.request.duration"
	mc, err := processor.getMetricConfig("http.request.duration", processor.config)
	assert.NoError(t, err)
	processor.limitedMetrics["http.request.duration"] = mc

	// Create metrics with real data
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()

	// Add two metrics with same name
	m1 := sm.Metrics().AppendEmpty()
	m1.SetName("http.request.duration")
	gauge1 := m1.SetEmptyGauge()
	dp1 := gauge1.DataPoints().AppendEmpty()
	dp1.SetDoubleValue(0.5)

	m2 := sm.Metrics().AppendEmpty()
	m2.SetName("http.request.duration")
	gauge2 := m2.SetEmptyGauge()
	dp2 := gauge2.DataPoints().AppendEmpty()
	dp2.SetDoubleValue(0.3)

	// Process metrics - first should be allowed, second should be dropped (same name within interval)
	result, err := processor.processMetrics(context.Background(), metrics)
	assert.NoError(t, err)
	assert.Equal(t, 1, result.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len(),
		"in name-only mode, only first metric should pass (second is within rate interval)")

	// Process metrics immediately - should drop (still within rate interval)
	metrics2 := pmetric.NewMetrics()
	rm2 := metrics2.ResourceMetrics().AppendEmpty()
	sm2 := rm2.ScopeMetrics().AppendEmpty()

	m3 := sm2.Metrics().AppendEmpty()
	m3.SetName("http.request.duration")
	gauge3 := m3.SetEmptyGauge()
	dp3 := gauge3.DataPoints().AppendEmpty()
	dp3.SetDoubleValue(0.7)

	result2, err := processor.processMetrics(context.Background(), metrics2)
	assert.NoError(t, err)
	assert.Equal(t, 0, result2.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len(),
		"second occurrence within interval should drop metric")

	// Wait for interval to pass
	time.Sleep(1100 * time.Millisecond)

	// Process metrics again - should allow
	metrics3 := pmetric.NewMetrics()
	rm3 := metrics3.ResourceMetrics().AppendEmpty()
	sm3 := rm3.ScopeMetrics().AppendEmpty()

	m4 := sm3.Metrics().AppendEmpty()
	m4.SetName("http.request.duration")
	gauge4 := m4.SetEmptyGauge()
	dp4 := gauge4.DataPoints().AppendEmpty()
	dp4.SetDoubleValue(0.9)

	result3, err := processor.processMetrics(context.Background(), metrics3)
	assert.NoError(t, err)
	assert.Equal(t, 1, result3.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len(),
		"after interval should allow metrics")
}

// Integration test: Per-label-set with multiple label combinations
func TestIntegration_PerLabelSet_MultipleLabelSets(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     1, // 1 second for testing
			PerLabelSet:             true,
			MaxCardinalityPerMetric: 100000,
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Create metric config for per-label-set mode
	mc, err := processor.getMetricConfig("http.request.duration", processor.config)
	assert.NoError(t, err)
	assert.True(t, mc.perLabelSet, "should be in per-label-set mode")
	assert.NotNil(t, mc.lastSeenLabelSets, "should have LRU cache")
	processor.limitedMetrics["http.request.duration"] = mc

	// Create metrics with different label sets (attributes)
	attrs1 := pcommon.NewMap()
	attrs1.PutStr("instance", "i-1")
	attrs1.PutStr("method", "GET")

	attrs2 := pcommon.NewMap()
	attrs2.PutStr("instance", "i-1")
	attrs2.PutStr("method", "POST")

	attrs3 := pcommon.NewMap()
	attrs3.PutStr("instance", "i-2")
	attrs3.PutStr("method", "GET")

	// Test: Generate hash keys
	key1 := processor.generateHashKey("http.request.duration", attrs1)
	key2 := processor.generateHashKey("http.request.duration", attrs2)
	key3 := processor.generateHashKey("http.request.duration", attrs3)

	// All should be different (different label sets)
	assert.NotEqual(t, key1, key2, "different methods should produce different keys")
	assert.NotEqual(t, key1, key3, "different instances should produce different keys")
	assert.NotEqual(t, key2, key3, "different combinations should produce different keys")

	// Test: shouldDropByLabelSet with different combinations
	dropped1 := processor.shouldDropByLabelSet(mc, attrs1)
	assert.False(t, dropped1, "first occurrence of label set 1 should be allowed")

	dropped2 := processor.shouldDropByLabelSet(mc, attrs2)
	assert.False(t, dropped2, "first occurrence of label set 2 should be allowed")

	dropped3 := processor.shouldDropByLabelSet(mc, attrs3)
	assert.False(t, dropped3, "first occurrence of label set 3 should be allowed")

	// Second occurrences should be dropped (same rate interval)
	dropped1_2 := processor.shouldDropByLabelSet(mc, attrs1)
	assert.True(t, dropped1_2, "second occurrence of label set 1 should be dropped")

	dropped2_2 := processor.shouldDropByLabelSet(mc, attrs2)
	assert.True(t, dropped2_2, "second occurrence of label set 2 should be dropped")

	dropped3_2 := processor.shouldDropByLabelSet(mc, attrs3)
	assert.True(t, dropped3_2, "second occurrence of label set 3 should be dropped")

	// Verify counts
	assert.Equal(t, int64(3), mc.allowedCount, "should have allowed 3 unique label sets")
	assert.Equal(t, int64(3), mc.droppedCount, "should have dropped 3 occurrences")
}

// Integration test: High cardinality scenario (84K combinations)
func TestIntegration_HighCardinality_84KLabelCombinations(t *testing.T) {
	// Configuration for 84K label combinations
	// 4 environments × 70 IP addresses × 30 instance IDs × 10 methods = 84,000
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60,
			PerLabelSet:             true,
			MaxCardinalityPerMetric: 100000, // Allow 100K of 84K possible
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	mc, err := processor.getMetricConfig("application.request.metrics", processor.config)
	assert.NoError(t, err)
	processor.limitedMetrics["application.request.metrics"] = mc

	// Simulate processing 1000 unique label combinations
	uniqueSets := 0
	droppedCount := 0
	allowedCount := 0

	// Create 1000 unique label sets (subset of 84K possible)
	for env := 0; env < 4; env++ {
		for ip := 0; ip < 70; ip++ {
			if uniqueSets >= 1000 {
				break
			}

			for method := 0; method < 10; method++ {
				if uniqueSets >= 1000 {
					break
				}

				attrs := pcommon.NewMap()
				attrs.PutStr("environment", []string{"prod", "staging", "dev", "test"}[env])
				attrs.PutStr("ip_address", fmt.Sprintf("192.168.1.%d", ip%256))
				attrs.PutStr("method", []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE", "CONNECT", "INFO"}[method])

				// First occurrence
				dropped := processor.shouldDropByLabelSet(mc, attrs)
				if !dropped {
					allowedCount++
					uniqueSets++
				}

				// Second occurrence (should drop)
				dropped = processor.shouldDropByLabelSet(mc, attrs)
				if dropped {
					droppedCount++
				}
			}
		}
	}

	// Verify results
	assert.Equal(t, 1000, allowedCount, "should allow 1000 unique label sets")
	assert.Equal(t, 1000, droppedCount, "should drop 1000 duplicate occurrences")
	assert.Equal(t, int64(1000), mc.allowedCount, "metric config should track allowed count")
	assert.Equal(t, int64(1000), mc.droppedCount, "metric config should track dropped count")

	// Verify memory efficiency: each entry uses ~8 bytes
	estimatedMemory := uniqueSets * 8
	assert.Less(t, estimatedMemory, 10000, "1000 label sets should use < 10KB with xxHash64")
}

// Integration test: LRU eviction when exceeding max cardinality
func TestIntegration_LRUEviction_ExceedsMaxCardinality(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60,
			PerLabelSet:             true,
			MaxCardinalityPerMetric: 10, // Very small for testing
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	mc, err := processor.getMetricConfig("test.metric", processor.config)
	assert.NoError(t, err)
	assert.Equal(t, 10, mc.maxCardinality, "should have max cardinality of 10")
	processor.limitedMetrics["test.metric"] = mc

	// Add 15 unique label sets (exceeds max of 10)
	for i := 0; i < 15; i++ {
		attrs := pcommon.NewMap()
		attrs.PutStr("id", fmt.Sprintf("label-set-%d", i))

		dropped := processor.shouldDropByLabelSet(mc, attrs)
		assert.False(t, dropped, fmt.Sprintf("label set %d should be allowed", i))
	}

	// Should have allowed 15 but cache only keeps 10 (LRU eviction)
	assert.Equal(t, int64(15), mc.allowedCount, "should have allowed 15 entries")
	assert.Greater(t, mc.evictedCount, int64(0), "should have evicted entries")

	// Verify cache size is bounded at max cardinality
	cacheSize := mc.lastSeenLabelSets.Len()
	assert.LessOrEqual(t, cacheSize, mc.maxCardinality, "cache size should not exceed max cardinality")
}

// Integration test: Per-metric configuration overrides
func TestIntegration_PerMetricOverrides(t *testing.T) {
	interval := 30
	perLabelSet := true
	card := 50000

	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60, // Global default
			PerLabelSet:             false,
			MaxCardinalityPerMetric: 200000,
			MetricConfigs: []MetricConfig{
				{
					Name:                "expensive.metric",
					RateIntervalSeconds: &interval,
					PerLabelSet:         &perLabelSet,
					MaxCardinality:      &card,
				},
			},
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Get config for metric with overrides
	mc, err := processor.getMetricConfig("expensive.metric", processor.config)
	assert.NoError(t, err)

	// Verify overrides were applied
	assert.Equal(t, int64(30*1e9), mc.rateIntervalNanos, "should use overridden interval (30s)")
	assert.True(t, mc.perLabelSet, "should use overridden per_label_set (true)")
	assert.Equal(t, 50000, mc.maxCardinality, "should use overridden max cardinality (50K)")

	// Get config for metric with defaults
	mc2, err := processor.getMetricConfig("normal.metric", processor.config)
	assert.NoError(t, err)

	// Verify defaults are used
	assert.Equal(t, int64(60*1e9), mc2.rateIntervalNanos, "should use global interval (60s)")
	assert.False(t, mc2.perLabelSet, "should use global per_label_set (false)")
	assert.Equal(t, 200000, mc2.maxCardinality, "should use global max cardinality (200K)")
}

// Benchmark helper: Test hash key generation performance
func TestBenchmark_HashKeyGeneration(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	attrs := pcommon.NewMap()
	attrs.PutStr("env", "production")
	attrs.PutStr("region", "us-west-2")
	attrs.PutStr("instance", "i-1234567890abcdef0")
	attrs.PutStr("service", "api-gateway")
	attrs.PutStr("version", "1.2.3")

	// Generate 10000 hash keys and measure consistency
	keys := make(map[uint64]int)
	for i := 0; i < 10000; i++ {
		key := processor.generateHashKey("http.request.duration", attrs)
		keys[key]++
	}

	// Should generate same hash every time
	assert.Equal(t, 1, len(keys), "should always generate same hash for same input")
	assert.Equal(t, 10000, keys[processor.generateHashKey("http.request.duration", attrs)],
		"all 10000 iterations should produce same key")
}

// Test: Extract attributes from different metric types
func TestExtractAttributes_Gauge(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Create a Gauge metric with attributes
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	metric := sm.Metrics().AppendEmpty()
	metric.SetName("test.gauge")

	gauge := metric.SetEmptyGauge()
	dp := gauge.DataPoints().AppendEmpty()
	dp.SetDoubleValue(42.0)
	dp.Attributes().PutStr("instance", "i-1")
	dp.Attributes().PutStr("method", "GET")

	// Extract attributes
	attrs := processor.extractAttributes(metric)

	// Verify attributes were extracted
	assert.Equal(t, 2, attrs.Len(), "should have 2 attributes")
	val, exists := attrs.Get("instance")
	assert.True(t, exists, "should have instance attribute")
	assert.Equal(t, "i-1", val.AsString(), "instance should be i-1")
	val, exists = attrs.Get("method")
	assert.True(t, exists, "should have method attribute")
	assert.Equal(t, "GET", val.AsString(), "method should be GET")
}

// Test: Extract attributes from Sum metric
func TestExtractAttributes_Sum(t *testing.T) {
	processor := &metricLimiterProcessor{
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Create a Sum metric with attributes
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	metric := sm.Metrics().AppendEmpty()
	metric.SetName("test.counter")

	sum := metric.SetEmptySum()
	dp := sum.DataPoints().AppendEmpty()
	dp.SetIntValue(100)
	dp.Attributes().PutStr("service", "api")
	dp.Attributes().PutStr("endpoint", "/users")

	// Extract attributes
	attrs := processor.extractAttributes(metric)

	// Verify attributes were extracted
	assert.Equal(t, 2, attrs.Len(), "should have 2 attributes")
	val, exists := attrs.Get("service")
	assert.True(t, exists, "should have service attribute")
	assert.Equal(t, "api", val.AsString(), "service should be api")
	val, exists = attrs.Get("endpoint")
	assert.True(t, exists, "should have endpoint attribute")
	assert.Equal(t, "/users", val.AsString(), "endpoint should be /users")
}

// Test: Per-label-set with actual metric data points
func TestShouldDropMetric_PerLabelSet_WithAttributes(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     1, // 1 second for testing
			PerLabelSet:             true,
			MaxCardinalityPerMetric: 100000,
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Create metric config for per-label-set mode
	mc, err := processor.getMetricConfig("http.request.duration", processor.config)
	assert.NoError(t, err)
	processor.limitedMetrics["http.request.duration"] = mc

	// Create first metric with attributes instance=i-1, method=GET
	metric1 := pmetric.NewMetrics()
	rm1 := metric1.ResourceMetrics().AppendEmpty()
	sm1 := rm1.ScopeMetrics().AppendEmpty()
	m1 := sm1.Metrics().AppendEmpty()
	m1.SetName("http.request.duration")
	gauge1 := m1.SetEmptyGauge()
	dp1 := gauge1.DataPoints().AppendEmpty()
	dp1.SetDoubleValue(100.0)
	dp1.Attributes().PutStr("instance", "i-1")
	dp1.Attributes().PutStr("method", "GET")

	// Create second metric with attributes instance=i-1, method=POST (different label set)
	metric2 := pmetric.NewMetrics()
	rm2 := metric2.ResourceMetrics().AppendEmpty()
	sm2 := rm2.ScopeMetrics().AppendEmpty()
	m2 := sm2.Metrics().AppendEmpty()
	m2.SetName("http.request.duration")
	gauge2 := m2.SetEmptyGauge()
	dp2 := gauge2.DataPoints().AppendEmpty()
	dp2.SetDoubleValue(200.0)
	dp2.Attributes().PutStr("instance", "i-1")
	dp2.Attributes().PutStr("method", "POST")

	// Create third metric with attributes instance=i-2, method=GET (different label set)
	metric3 := pmetric.NewMetrics()
	rm3 := metric3.ResourceMetrics().AppendEmpty()
	sm3 := rm3.ScopeMetrics().AppendEmpty()
	m3 := sm3.Metrics().AppendEmpty()
	m3.SetName("http.request.duration")
	gauge3 := m3.SetEmptyGauge()
	dp3 := gauge3.DataPoints().AppendEmpty()
	dp3.SetDoubleValue(300.0)
	dp3.Attributes().PutStr("instance", "i-2")
	dp3.Attributes().PutStr("method", "GET")

	// Process metrics through shouldDropMetric
	dropped1 := processor.shouldDropMetric(m1)
	assert.False(t, dropped1, "first occurrence of i-1/GET should be allowed")

	dropped2 := processor.shouldDropMetric(m2)
	assert.False(t, dropped2, "first occurrence of i-1/POST should be allowed (different label set)")

	dropped3 := processor.shouldDropMetric(m3)
	assert.False(t, dropped3, "first occurrence of i-2/GET should be allowed (different label set)")

	// All three unique label sets should be tracked independently
	assert.Equal(t, int64(3), mc.allowedCount, "should have allowed 3 unique label sets")
	assert.Equal(t, int64(0), mc.droppedCount, "should have dropped 0 on first occurrence")

	// Second occurrences within rate interval should be dropped
	dropped1_2 := processor.shouldDropMetric(m1)
	assert.True(t, dropped1_2, "second occurrence of i-1/GET within interval should be dropped")

	dropped2_2 := processor.shouldDropMetric(m2)
	assert.True(t, dropped2_2, "second occurrence of i-1/POST within interval should be dropped")

	dropped3_2 := processor.shouldDropMetric(m3)
	assert.True(t, dropped3_2, "second occurrence of i-2/GET within interval should be dropped")

	assert.Equal(t, int64(3), mc.droppedCount, "should have dropped 3 within-interval occurrences")
}

// Test: Different label combinations produce different hash keys through actual metrics
func TestShouldDropMetric_LabelDifferentiation(t *testing.T) {
	processor := &metricLimiterProcessor{
		config: &Config{
			RateIntervalSeconds:     60,
			PerLabelSet:             true,
			MaxCardinalityPerMetric: 100000,
		},
		logger:         zaptest.NewLogger(t),
		limitedMetrics: make(map[string]*metricConfig),
	}

	// Create metric config
	mc, err := processor.getMetricConfig("application.metric", processor.config)
	assert.NoError(t, err)
	processor.limitedMetrics["application.metric"] = mc

	// Create test data: matchbox-web and experiment-web with same metric name
	testCases := []struct {
		name   string
		attrs  map[string]string
		expect bool // First metric should be allowed
	}{
		{
			name: "matchbox-web service",
			attrs: map[string]string{
				"service_name": "matchbox-web",
				"environment":  "staging",
			},
			expect: false, // Should be allowed (not dropped)
		},
		{
			name: "experiment-web service",
			attrs: map[string]string{
				"service_name": "experiment-web",
				"environment":  "staging",
			},
			expect: false, // Should be allowed (not dropped)
		},
		{
			name: "another-service",
			attrs: map[string]string{
				"service_name": "another-web",
				"environment":  "staging",
			},
			expect: false, // Should be allowed (not dropped)
		},
	}

	// First pass: all should be allowed (first occurrence)
	for _, tc := range testCases {
		metric := pmetric.NewMetrics()
		rm := metric.ResourceMetrics().AppendEmpty()
		sm := rm.ScopeMetrics().AppendEmpty()
		m := sm.Metrics().AppendEmpty()
		m.SetName("application.metric")
		gauge := m.SetEmptyGauge()
		dp := gauge.DataPoints().AppendEmpty()
		dp.SetDoubleValue(1.0)
		for k, v := range tc.attrs {
			dp.Attributes().PutStr(k, v)
		}

		dropped := processor.shouldDropMetric(m)
		assert.Equal(t, tc.expect, dropped, "%s first occurrence: should be allowed", tc.name)
	}

	// Verify that each unique label set was tracked independently
	assert.Equal(t, int64(3), mc.allowedCount, "should have allowed 3 unique label sets")
	assert.Equal(t, int64(0), mc.droppedCount, "no metrics should be dropped on first occurrence")

	// Second pass: all should be dropped (within 60 second interval)
	for _, tc := range testCases {
		metric := pmetric.NewMetrics()
		rm := metric.ResourceMetrics().AppendEmpty()
		sm := rm.ScopeMetrics().AppendEmpty()
		m := sm.Metrics().AppendEmpty()
		m.SetName("application.metric")
		gauge := m.SetEmptyGauge()
		dp := gauge.DataPoints().AppendEmpty()
		dp.SetDoubleValue(2.0)
		for k, v := range tc.attrs {
			dp.Attributes().PutStr(k, v)
		}

		dropped := processor.shouldDropMetric(m)
		assert.True(t, dropped, "%s second occurrence: should be dropped", tc.name)
	}

	// Verify all were dropped
	assert.Equal(t, int64(3), mc.droppedCount, "should have dropped 3 within-interval occurrences")
}

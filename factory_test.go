package metriclimiterprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
)

func TestFactory_NewFactory(t *testing.T) {
	factory := NewFactory()
	assert.NotNil(t, factory)
}

func TestFactory_CreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	assert.NotNil(t, cfg)
	assert.IsType(t, &Config{}, cfg)

	config := cfg.(*Config)
	assert.Equal(t, 60, config.RateIntervalSeconds, "default rate interval should be 60 seconds")
	assert.False(t, config.PerLabelSet, "default per_label_set should be false")
	assert.Equal(t, 200000, config.MaxCardinalityPerMetric, "default max cardinality should be 200000")
	assert.Empty(t, config.MetricNames, "default metric names should be empty")
}

func TestFactory_Type(t *testing.T) {
	factory := NewFactory()
	expectedType := component.MustNewType("metriclimiter")
	assert.Equal(t, expectedType, factory.Type())
}

func TestFactory_ValidConfig(t *testing.T) {
	factory := NewFactory()
	assert.NotNil(t, factory)

	// Verify factory type
	assert.Equal(t, component.MustNewType("metriclimiter"), factory.Type())
}

func TestFactory_DefaultConfigValidation(t *testing.T) {
	cfg := createDefaultConfig()
	config := cfg.(*Config)

	// Default config should have empty metric names, so Validate should fail
	err := config.Validate()
	assert.Error(t, err, "default config with empty metric names should fail validation")
}

func TestFactory_ConfigValidation_ValidWithPerLabelSet(t *testing.T) {
	cfg := &Config{
		MetricNames:             []string{"test.metric"},
		RateIntervalSeconds:     60,
		PerLabelSet:             true,
		MaxCardinalityPerMetric: 100000,
		CardinalityTTLSeconds:   3600,
	}

	err := cfg.Validate()
	assert.NoError(t, err, "valid config with per_label_set should pass validation")
}

func TestFactory_ConfigValidation_InvalidMetricNames(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{},
		RateIntervalSeconds: 60,
	}

	err := cfg.Validate()
	assert.Error(t, err, "empty metric names should fail validation")
	assert.Contains(t, err.Error(), "metric_names cannot be empty")
}

func TestFactory_ConfigValidation_InvalidRateInterval(t *testing.T) {
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: 0,
	}

	err := cfg.Validate()
	assert.Error(t, err, "zero rate interval should fail validation")
	assert.Contains(t, err.Error(), "rate_interval_seconds must be positive")
}

func TestFactory_ConfigValidation_NegativeCardinality(t *testing.T) {
	cfg := &Config{
		MetricNames:             []string{"test.metric"},
		RateIntervalSeconds:     60,
		MaxCardinalityPerMetric: -100,
	}

	err := cfg.Validate()
	assert.Error(t, err, "negative cardinality should fail validation")
	assert.Contains(t, err.Error(), "max_cardinality_per_metric must be >= 0")
}

func TestFactory_ConfigValidation_PerMetricConfig_InvalidName(t *testing.T) {
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
	assert.Error(t, err, "metric config with non-existent name should fail validation")
	assert.Contains(t, err.Error(), "not in metric_names")
}

func TestFactory_ConfigValidation_PerMetricConfig_ValidOverride(t *testing.T) {
	interval := 30
	cfg := &Config{
		MetricNames:         []string{"test.metric"},
		RateIntervalSeconds: 60,
		MetricConfigs: []MetricConfig{
			{
				Name:                "test.metric",
				RateIntervalSeconds: &interval,
			},
		},
	}

	err := cfg.Validate()
	assert.NoError(t, err, "valid metric config should pass validation")
}

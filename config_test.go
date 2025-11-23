package metriclimiterprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				MetricNames:         []string{"cpu.usage", "memory.usage"},
				RateIntervalSeconds: 60,
			},
			wantErr: false,
		},
		{
			name: "valid config with single metric",
			config: Config{
				MetricNames:         []string{"system.cpu.utilization"},
				RateIntervalSeconds: 30,
			},
			wantErr: false,
		},
		{
			name: "empty metric names",
			config: Config{
				MetricNames:         []string{},
				RateIntervalSeconds: 60,
			},
			wantErr: true,
			errMsg:  "metric_names cannot be empty",
		},
		{
			name: "nil metric names",
			config: Config{
				MetricNames:         nil,
				RateIntervalSeconds: 60,
			},
			wantErr: true,
			errMsg:  "metric_names cannot be empty",
		},
		{
			name: "zero rate interval",
			config: Config{
				MetricNames:         []string{"cpu.usage"},
				RateIntervalSeconds: 0,
			},
			wantErr: true,
			errMsg:  "rate_interval_seconds must be positive",
		},
		{
			name: "negative rate interval",
			config: Config{
				MetricNames:         []string{"cpu.usage"},
				RateIntervalSeconds: -10,
			},
			wantErr: true,
			errMsg:  "rate_interval_seconds must be positive",
		},
		{
			name: "empty metric name in list",
			config: Config{
				MetricNames:         []string{"cpu.usage", "", "memory.usage"},
				RateIntervalSeconds: 60,
			},
			wantErr: true,
			errMsg:  "metric_names[1] cannot be empty",
		},
		{
			name: "large rate interval",
			config: Config{
				MetricNames:         []string{"rare.metric"},
				RateIntervalSeconds: 3600, // 1 hour
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err, "expected error for %s", tt.name)
				assert.Contains(t, err.Error(), tt.errMsg, "error message should contain %s", tt.errMsg)
			} else {
				assert.NoError(t, err, "expected no error for %s", tt.name)
			}
		})
	}
}

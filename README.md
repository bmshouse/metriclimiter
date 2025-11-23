# Metric Limiter Processor

The metric limiter processor limits the rate at which specific metrics are processed by the OpenTelemetry Collector. When a metric name matches the configured list, the processor will only allow it to pass through once per the configured rate interval. Additional metrics within that interval will be dropped.

## Configuration

```yaml
processors:
  metriclimiter:
    metric_names:
      - "system.cpu.utilization"
      - "system.memory.usage"
      - "expensive.custom.metric"
    rate_interval_seconds: 60  # Allow once per minute
```

## Configuration Options

### `metric_names` (required)

A list of metric names to rate limit. When a metric's name matches an entry in this list, rate limiting is applied.

- Type: `[]string`
- Required: Yes
- Example: `["system.cpu.utilization", "system.memory.usage"]`

### `rate_interval_seconds` (optional)

The rate interval in seconds. A metric in the `metric_names` list will only be allowed to pass once per this interval. Additional metrics with the same name within this interval will be dropped.

- Type: `int`
- Default: `60`
- Minimum: `1`
- Example: `30` (allow metric once per 30 seconds)

### `per_label_set` (optional)

When enabled, the processor tracks and limits metrics by both metric name AND label values (attributes). This allows different label combinations to be rate-limited independently. Disabled by default for backward compatibility.

- Type: `bool`
- Default: `false`
- When enabled: Each unique combination of metric name + attribute values is tracked separately
- When disabled: Only metric name is considered for rate limiting
- Example: `true` (enable per-label-set rate limiting)

**Use Case:** High-cardinality metrics where you want to limit based on specific label combinations rather than all occurrences of a metric name.

### `max_cardinality_per_metric` (optional)

When `per_label_set` is enabled, this controls the maximum number of unique label combinations to track per metric. Once exceeded, least recently used combinations are evicted from the cache.

- Type: `int`
- Default: `200000`
- Minimum: `1000`
- Maximum: Unlimited (limited only by available memory)
- Example: `100000` (track up to 100K unique label combinations)

**Memory Efficiency:** Each cached label combination uses only 8 bytes (using xxHash64 with LRU cache), so 100,000 combinations = ~800KB of memory per metric.

### `cardinality_ttl_seconds` (optional)

When `per_label_set` is enabled, this sets an optional TTL (time-to-live) for cardinality tracking. Entries older than this TTL may be evicted.

- Type: `int`
- Default: `0` (TTL disabled, use only LRU eviction)
- Minimum: `0`
- Example: `3600` (evict entries not seen in last hour)

### `metric_configs` (optional)

Per-metric configuration overrides. Allows different metrics to have different rate limits and settings.

- Type: `[]MetricConfig`
- Default: Empty (use global defaults for all metrics)

**MetricConfig structure:**
```yaml
metric_configs:
  - name: "expensive.metric"
    rate_interval_seconds: 120      # Override: 2 minutes instead of default 60s
    per_label_set: true              # Override: track per label set
    max_cardinality: 50000           # Override: 50K unique label sets
    cardinality_ttl_seconds: 1800    # Override: 30 minute TTL
```

## Example Configurations

### Simple Rate Limiting

Allow expensive metrics only once per minute:

```yaml
processors:
  metriclimiter:
    metric_names:
      - "expensive.database.query"
    rate_interval_seconds: 60
```

### Multiple Metrics with Different Purposes

```yaml
processors:
  metriclimiter:
    metric_names:
      - "app.error.rate"
      - "db.slow.query.time"
      - "custom.expensive.calculation"
    rate_interval_seconds: 120  # Allow once per 2 minutes
```

### Per-Label-Set Rate Limiting

Limit metrics based on label combinations (e.g., track each instance separately):

```yaml
processors:
  metriclimiter:
    metric_names:
      - "http.request.duration"
    rate_interval_seconds: 60
    per_label_set: true                  # Enable per-label-set tracking
    max_cardinality_per_metric: 100000   # Track up to 100K unique label sets
```

**Example behavior:**
- Instance i-1 with method GET: allowed
- Instance i-1 with method POST: allowed separately (different label set)
- Instance i-2 with method GET: allowed separately (different instance label)
- Instance i-1 with method GET again (within 60s): dropped

### Per-Label-Set with High Cardinality

For metrics with many label combinations (like 84,000 unique combinations):

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

processors:
  metriclimiter:
    metric_names:
      - "application.request.metrics"
    rate_interval_seconds: 30
    per_label_set: true
    max_cardinality_per_metric: 100000  # Allow 100K of 84K possible combinations
    cardinality_ttl_seconds: 3600       # Evict unseen entries after 1 hour

  batch:
    send_batch_size: 1024
    timeout: 10s

exporters:
  otlp:
    endpoint: localhost:4317

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [metriclimiter, batch]
      exporters: [otlp]
```

**Memory efficiency:** ~800KB per metric for 100K label sets (vs. potentially MB with string-based tracking).

### Per-Metric Configuration Overrides

Different metrics with different rate limits:

```yaml
processors:
  metriclimiter:
    metric_names:
      - "expensive.database.query"
      - "normal.cpu.usage"
    rate_interval_seconds: 60           # Global default: 60s
    per_label_set: false                # Global default: name-only

    metric_configs:
      - name: "expensive.database.query"
        rate_interval_seconds: 300      # Override: 5 minutes
        per_label_set: true             # Override: track per label set
        max_cardinality: 50000

      - name: "normal.cpu.usage"
        # Uses global defaults: 60s, per_label_set=false
```

### Complete Pipeline Example

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

processors:
  metriclimiter:
    metric_names:
      - "system.cpu.utilization"
      - "system.memory.usage"
    rate_interval_seconds: 60

  batch:
    send_batch_size: 1024
    timeout: 10s

exporters:
  otlp:
    endpoint: localhost:4317

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [metriclimiter, batch]
      exporters: [otlp]
```

## How It Works

### Name-Only Mode (default: `per_label_set=false`)

1. **Metric Name Matching**: When a metric arrives, the processor checks if its name is in the configured `metric_names` list.

2. **Rate Limit Check**: If matched, the processor checks whether the metric has been seen within the `rate_interval_seconds` window.

3. **Decision**:
   - **First occurrence or outside interval**: Metric is allowed to pass through, and the current timestamp is recorded.
   - **Within interval**: Metric is dropped and does not continue through the pipeline.

4. **Non-matching Metrics**: Metrics whose names don't match the `metric_names` list always pass through unchanged.

### Per-Label-Set Mode (`per_label_set=true`)

1. **Label Set Identification**: For each metric, a hash key is generated from:
   - Metric name
   - Metric attributes (labels) in sorted order for deterministic hashing

2. **LRU Cache Lookup**: The processor uses an LRU cache to track which label sets have been seen recently.

3. **Decision**:
   - **New label set or outside interval**: Metric is allowed to pass through, and the label set is cached.
   - **Cached label set within interval**: Metric is dropped.
   - **Cache Full**: Least recently used entries are evicted automatically.

4. **Memory Efficiency**: Each cached label set uses only 8 bytes (one 64-bit timestamp), enabling tracking of thousands of unique label combinations with minimal memory.

## Performance Characteristics

### Name-Only Mode
- **Metric Name Lookup**: O(1) - pre-compiled hash map
- **Timestamp Lookup**: O(1) - lock-free concurrent access via `sync.Map`
- **Memory Overhead**: ~8 bytes per tracked metric (one int64 timestamp)
- **Thread Safety**: Lock-free concurrent access for high-performance multi-threaded environments

### Per-Label-Set Mode
- **Hash Generation**: O(k) where k = number of attributes - sorted attribute hashing
- **Cache Lookup**: O(1) - LRU cache with O(1) average access
- **Memory Overhead**: ~8 bytes per tracked label set (one 64-bit hash + timestamp)
- **Cache Eviction**: O(1) with automatic LRU eviction when max_cardinality reached
- **Concurrency**: Thread-safe LRU cache supports concurrent reads/writes

### Example: 84,000 Label Combinations
- **Cardinality**: 4 environments × 70 IP addresses × 30 instance IDs × 10 methods = 84,000 unique combinations
- **Memory Usage**: ~100K combinations × 8 bytes = ~800 KB per metric
- **Lookup Time**: < 1 microsecond per metric (hash generation + cache lookup)
- **CPU Overhead**: Negligible - dominated by sorting attributes (typically < 5 attributes)

## Use Cases

### High-Volume Metrics

When you have expensive metrics that are generated frequently but don't need to be exported at high frequency:

```yaml
processors:
  metriclimiter:
    metric_names:
      - "application.request.histogram"  # High cardinality metric
      - "jvm.gc.duration"                 # Expensive to generate
    rate_interval_seconds: 60
```

### Cost Optimization

Reduce the volume of metrics sent to expensive backends:

```yaml
processors:
  metriclimiter:
    metric_names:
      - "detailed.trace.metrics"
      - "debug.memory.allocation"
    rate_interval_seconds: 300  # Only send every 5 minutes
```

### Uneven Load Distribution

Ensure that metrics don't overwhelm downstream systems:

```yaml
processors:
  metriclimiter:
    metric_names:
      - "real.time.sensor.data"
      - "event.stream.metrics"
    rate_interval_seconds: 30
```

## Limitations

- Timestamp tracking is in-memory (resets on collector restart)
- Per-label-set mode requires explicit attribute configuration (future enhancement)
- No regex pattern matching for metric names (feature request for future version)

## Future Enhancements

Potential improvements for future versions:

- Attribute-based filtering (match on attribute values)
- Per-metric rate intervals
- Regex pattern matching for metric names
- TTL-based cleanup of stale entries
- Metrics/telemetry on dropped metric counts
- Support for sampling strategies

## Testing

The processor includes comprehensive tests:

- Configuration validation tests (`config_test.go`)
- Rate limiting logic tests (`processor_test.go`)
- Factory integration tests (`factory_test.go`)

Run tests with:

```bash
go test ./...
```

## Integration with OpenTelemetry Collector

This processor follows the standard OpenTelemetry Collector processor pattern and can be integrated into any collector build.

### Building the Collector with This Processor

Add to your `builder.yaml`:

```yaml
dist:
  name: otelcontribcol

processors:
  - gomod: github.com/bmshouse/metriclimiter v0.1.0

receivers:
  - gomod: go.opentelemetry.io/collector/receiver/otlpreceiver v0.104.0

exporters:
  - gomod: go.opentelemetry.io/collector/exporter/otlpexporter v0.104.0
```

Then build with the OpenTelemetry Collector Builder:

```bash
ocb --config=builder.yaml
```

This creates a binary at `./dist/otelcontribcol` (or `.exe` on Windows) with the metric limiter processor included.

## License

Same as OpenTelemetry Collector (Apache 2.0)

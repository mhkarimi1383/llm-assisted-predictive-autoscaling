# Monitoring

This directory contains monitoring configurations for the LLM-Based Intelligent Autoscaling System.

## Directory Structure

- `grafana/` - Grafana dashboards and provisioning files
- `prometheus/` - Prometheus configuration and rules

## Key Files

- `grafana/dashboard.json` - Main dashboard for system overview
- `prometheus/prometheus.yml` - Prometheus server configuration
- `docker-compose.yml` - Local development environment with monitoring stack

## Metrics Collected

The system exposes the following Prometheus metrics:

### Latency Metrics
- `llm_autoscaler_agent_latency_seconds` - Histogram of agent processing latency
- `llm_autoscaler_total_latency_seconds` - Histogram of end-to-end processing latency

### Scaling Metrics
- `llm_autoscaler_scaling_actions_total` - Counter of scaling actions by type and direction
- `llm_autoscaler_scaling_events_total` - Counter of scaling events
- `llm_autoscaler_replica_count` - Current replica count for managed workloads

### System Metrics
- `llm_autoscaler_up` - Service availability (1 = up, 0 = down)
- `llm_autoscaler_goroutines` - Number of active goroutines
- `llm_autoscaler_memory_alloc_bytes` - Memory allocation in bytes

### Business Metrics
- `llm_autoscaler_workload_prediction_accuracy` - Gauge of prediction accuracy
- `llm_autoscaler_safety_policy_violations` - Counter of safety policy violations
- `llm_autoscaler_resource_waste_reduction` - Gauge of estimated resource savings

## Alerting Rules

Key alerting rules include:
- System downtime
- High latency (>5s average)
- Safety policy violations
- Resource prediction inaccuracies
- Scaling oscillation detection
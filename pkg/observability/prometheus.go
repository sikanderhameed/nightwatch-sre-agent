package observability

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PrometheusClient wraps the official Prometheus API client
type PrometheusClient struct {
	api v1.API
}

// NewPrometheusClient creates a new client connected to PROMETHEUS_URL
func NewPrometheusClient() (*PrometheusClient, error) {
	url := os.Getenv("PROMETHEUS_URL")
	if url == "" {
		return nil, fmt.Errorf("PROMETHEUS_URL env var is not set")
	}

	client, err := api.NewClient(api.Config{
		Address: url,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}

	return &PrometheusClient{
		api: v1.NewAPI(client),
	}, nil
}

// IsHighErrorRate checks if 5xx errors > 1% in the last 2 minutes
func (c *PrometheusClient) IsHighErrorRate(ctx context.Context, namespace, appName string) (bool, string, error) {
	// Query: rate of 5xx errors / total rate
	// rate(http_requests_total{status=~"5.*", app="<app>"}[2m]) 
	// / 
	// rate(http_requests_total{app="<app>"}[2m]) > 0.01

	query := fmt.Sprintf(`
		sum(rate(http_requests_total{namespace="%s", app="%s", status=~"5.."}[2m])) 
		/ 
		sum(rate(http_requests_total{namespace="%s", app="%s"}[2m])) > 0.01
	`, namespace, appName, namespace, appName)

	result, warnings, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return false, "", fmt.Errorf("prometheus query failed: %w", err)
	}
	if len(warnings) > 0 {
		fmt.Printf("Prometheus Warnings: %v\n", warnings)
	}

	// If result is empty, no high error rate
	vector, ok := result.(model.Vector)
	if !ok {
		return false, "", fmt.Errorf("unexpected result format: %T", result)
	}

	if len(vector) > 0 {
		return true, fmt.Sprintf("High Error Rate Detected: %.2f%% (Current value: %s)", float64(vector[0].Value)*100, vector[0].Value), nil
	}

	return false, "", nil
}

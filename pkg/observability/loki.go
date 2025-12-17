package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// LokiClient wraps a simple HTTP client for LogQL queries
type LokiClient struct {
	baseURL string
	client  *http.Client
}

// NewLokiClient creates a new client connected to LOKI_URL
func NewLokiClient() (*LokiClient, error) {
	url := os.Getenv("LOKI_URL")
	if url == "" {
		return nil, fmt.Errorf("LOKI_URL env var is not set")
	}

	return &LokiClient{
		baseURL: url,
		client:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// FetchLogs retrieves errors logs for the app from the last 15 minutes
func (c *LokiClient) FetchLogs(namespace, appName string) (string, error) {
	// Query: {namespace="<ns>", app="<app>"} |= "error"
	query := fmt.Sprintf(`{namespace="%s", app="%s"} |= "error"`, namespace, appName)
	
	// API: /loki/api/v1/query_range
	u, err := url.Parse(c.baseURL + "/loki/api/v1/query_range")
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("limit", "50") // Limit to 50 lines
	
	// Time range: now - 15m
	end := time.Now()
	start := end.Add(-15 * time.Minute)
	q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	q.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	
	u.RawQuery = q.Encode()

	resp, err := c.client.Get(u.String())
	if err != nil {
		return "", fmt.Errorf("loki request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse Loki Response
	var lokiResp LokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return "", fmt.Errorf("failed to parse loki response: %w", err)
	}

	// Format logs into a string
	var logs string
	for _, stream := range lokiResp.Data.Result {
		for _, entry := range stream.Values {
			// entry is [timestamp, logline]
			if len(entry) >= 2 {
				logs += fmt.Sprintf("[%s] %s\n", parseLokiTime(entry[0]), entry[1])
			}
		}
	}

	if logs == "" {
		return "No error logs found in Loki for the last 15 minutes.", nil
	}

	return logs, nil
}

func parseLokiTime(tsStr string) string {
	tsInt, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return tsStr
	}
	return time.Unix(0, tsInt).Format(time.RFC3339)
}

// Loki Response Structures
type LokiQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"` // [ [timestamp, line], ... ]
		} `json:"result"`
	} `json:"data"`
}

package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"
)

type metricsCollector struct {
	mu           sync.Mutex
	startTime    time.Time
	lastActivity time.Time
	inFlight     int64
	httpRequests map[httpMetricKey]int64
	rpcRequests  map[rpcMetricKey]int64
	toolCalls    map[toolMetricKey]int64
}

type httpMetricKey struct {
	route  string
	method string
	status string
}

type rpcMetricKey struct {
	method string
	status string
}

type toolMetricKey struct {
	tool   string
	status string
}

func newMetricsCollector() *metricsCollector {
	now := time.Now().UTC()
	return &metricsCollector{
		startTime:    now,
		lastActivity: now,
		httpRequests: map[httpMetricKey]int64{},
		rpcRequests:  map[rpcMetricKey]int64{},
		toolCalls:    map[toolMetricKey]int64{},
	}
}

func (m *metricsCollector) beginHTTP() {
	m.mu.Lock()
	m.inFlight++
	m.mu.Unlock()
}

func (m *metricsCollector) endHTTP(route, method string, status int, countActivity bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inFlight--
	m.httpRequests[httpMetricKey{
		route:  route,
		method: method,
		status: strconv.Itoa(status),
	}]++
	if countActivity {
		m.lastActivity = time.Now().UTC()
	}
}

func (m *metricsCollector) observeRPC(method string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := "ok"
	if !ok {
		status = "error"
	}
	m.rpcRequests[rpcMetricKey{method: method, status: status}]++
	m.lastActivity = time.Now().UTC()
}

func (m *metricsCollector) observeTool(tool string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := "ok"
	if !ok {
		status = "error"
	}
	m.toolCalls[toolMetricKey{tool: tool, status: status}]++
	m.lastActivity = time.Now().UTC()
}

func (m *metricsCollector) render(w io.Writer) error {
	m.mu.Lock()
	startTime := m.startTime
	lastActivity := m.lastActivity
	inFlight := m.inFlight
	httpRequests := make(map[httpMetricKey]int64, len(m.httpRequests))
	for k, v := range m.httpRequests {
		httpRequests[k] = v
	}
	rpcRequests := make(map[rpcMetricKey]int64, len(m.rpcRequests))
	for k, v := range m.rpcRequests {
		rpcRequests[k] = v
	}
	toolCalls := make(map[toolMetricKey]int64, len(m.toolCalls))
	for k, v := range m.toolCalls {
		toolCalls[k] = v
	}
	m.mu.Unlock()

	writeFamily := func(name, help, metricType string, lines []string) error {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, help); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType); err != nil {
			return err
		}
		for _, line := range lines {
			if _, err := io.WriteString(w, line+"\n"); err != nil {
				return err
			}
		}
		return nil
	}

	if err := writeFamily("mcpserver_up", "Whether the MCP server is up.", "gauge", []string{"mcpserver_up 1"}); err != nil {
		return err
	}
	if err := writeFamily("mcpserver_start_time_seconds", "Unix time when the MCP server started.", "gauge", []string{
		fmt.Sprintf("mcpserver_start_time_seconds %.0f", float64(startTime.Unix())),
	}); err != nil {
		return err
	}
	if err := writeFamily("mcpserver_last_activity_seconds", "Unix time of the most recent observed activity.", "gauge", []string{
		fmt.Sprintf("mcpserver_last_activity_seconds %.0f", float64(lastActivity.Unix())),
	}); err != nil {
		return err
	}
	if err := writeFamily("mcpserver_http_in_flight_requests", "Current number of in-flight HTTP requests.", "gauge", []string{
		fmt.Sprintf("mcpserver_http_in_flight_requests %d", inFlight),
	}); err != nil {
		return err
	}

	httpLines := make([]string, 0, len(httpRequests))
	httpKeys := make([]httpMetricKey, 0, len(httpRequests))
	for k := range httpRequests {
		httpKeys = append(httpKeys, k)
	}
	sort.Slice(httpKeys, func(i, j int) bool {
		a, b := httpKeys[i], httpKeys[j]
		if a.route != b.route {
			return a.route < b.route
		}
		if a.method != b.method {
			return a.method < b.method
		}
		return a.status < b.status
	})
	for _, k := range httpKeys {
		httpLines = append(httpLines, fmt.Sprintf(
			"mcpserver_http_requests_total{route=%q,method=%q,status=%q} %d",
			k.route, k.method, k.status, httpRequests[k],
		))
	}
	if err := writeFamily("mcpserver_http_requests_total", "Total HTTP requests handled by the MCP server.", "counter", httpLines); err != nil {
		return err
	}

	rpcLines := make([]string, 0, len(rpcRequests))
	rpcKeys := make([]rpcMetricKey, 0, len(rpcRequests))
	for k := range rpcRequests {
		rpcKeys = append(rpcKeys, k)
	}
	sort.Slice(rpcKeys, func(i, j int) bool {
		a, b := rpcKeys[i], rpcKeys[j]
		if a.method != b.method {
			return a.method < b.method
		}
		return a.status < b.status
	})
	for _, k := range rpcKeys {
		rpcLines = append(rpcLines, fmt.Sprintf(
			"mcpserver_rpc_requests_total{method=%q,status=%q} %d",
			k.method, k.status, rpcRequests[k],
		))
	}
	if err := writeFamily("mcpserver_rpc_requests_total", "Total JSON-RPC requests handled by the MCP server.", "counter", rpcLines); err != nil {
		return err
	}

	toolLines := make([]string, 0, len(toolCalls))
	toolKeys := make([]toolMetricKey, 0, len(toolCalls))
	for k := range toolCalls {
		toolKeys = append(toolKeys, k)
	}
	sort.Slice(toolKeys, func(i, j int) bool {
		a, b := toolKeys[i], toolKeys[j]
		if a.tool != b.tool {
			return a.tool < b.tool
		}
		return a.status < b.status
	})
	for _, k := range toolKeys {
		toolLines = append(toolLines, fmt.Sprintf(
			"mcpserver_tool_calls_total{tool=%q,status=%q} %d",
			k.tool, k.status, toolCalls[k],
		))
	}
	if err := writeFamily("mcpserver_tool_calls_total", "Total tool calls handled by the MCP server.", "counter", toolLines); err != nil {
		return err
	}

	return nil
}

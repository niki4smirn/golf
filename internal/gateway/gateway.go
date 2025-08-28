package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/niki4smirn/golf/internal/database"
	"github.com/niki4smirn/golf/internal/types"
)

// Gateway handles JSON-RPC requests and audit logging
type Gateway struct {
	db         *database.Database
	tinybirdDB *database.TinybirdDatabase
	targetURL  string
	httpClient *http.Client
}

// New creates a new Gateway instance
func New(db *database.Database, targetURL string) *Gateway {
	return &Gateway{
		db:        db,
		targetURL: targetURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetTinybirdLogger adds Tinybird logging capability
func (g *Gateway) SetTinybirdLogger(tinybirdDB *database.TinybirdDatabase) {
	g.tinybirdDB = tinybirdDB
}

// ProxyJSONRPC handles incoming JSON-RPC requests, forwards them, and logs everything
func (g *Gateway) ProxyJSONRPC(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Generate a unique request ID for tracking
	requestID := generateRequestID()

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Parse JSON-RPC request to extract method
	var jsonRPCReq types.JSONRPCRequest
	var method string = "unknown"
	if err := json.Unmarshal(body, &jsonRPCReq); err == nil && jsonRPCReq.Method != "" {
		method = jsonRPCReq.Method
	}

	// Capture headers
	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0] // Take first value for simplicity
		}
	}
	headersJSON, _ := json.Marshal(headers)

	// Store the request immediately - this ensures we capture everything even if processing fails
	auditRequest := &types.AuditRequest{
		Timestamp: startTime,
		Method:    method,
		RequestID: requestID,
		IPAddress: getClientIP(r),
		UserAgent: r.UserAgent(),
		Request:   json.RawMessage(body),
		Headers:   json.RawMessage(headersJSON),
	}

	// Log the request immediately
	if err := g.db.InsertAuditRequest(auditRequest); err != nil {
		log.Printf("Failed to insert audit request: %v", err)
		// Continue processing even if audit logging fails
	}

	// Also log to Tinybird if configured
	if g.tinybirdDB != nil {
		if err := g.tinybirdDB.InsertAuditRequest(auditRequest); err != nil {
			log.Printf("Failed to insert audit request to Tinybird: %v", err)
		}
	}

	// Forward the request to the target service
	if g.targetURL == "" {
		g.handleError(w, "No target URL configured", requestID, startTime, http.StatusServiceUnavailable)
		return
	}

	g.forwardRequest(w, r, body, requestID, startTime)
}

func (g *Gateway) forwardRequest(w http.ResponseWriter, r *http.Request, requestBody []byte, requestID string, startTime time.Time) {
	// Create a new request to forward
	req, err := http.NewRequest("POST", g.targetURL, bytes.NewReader(requestBody))
	if err != nil {
		g.handleError(w, "Failed to create forward request", requestID, startTime, http.StatusInternalServerError)
		return
	}

	// Copy all original headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Add gateway-specific headers
	req.Header.Set("X-Forwarded-For", getClientIP(r))
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("X-Gateway", "golf-audit-gateway")

	// Forward the request
	resp, err := g.httpClient.Do(req)
	if err != nil {
		g.handleError(w, fmt.Sprintf("Failed to forward request: %v", err), requestID, startTime, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read the response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		g.handleError(w, "Failed to read response", requestID, startTime, http.StatusInternalServerError)
		return
	}

	// Store the response
	auditResponse := &types.AuditResponse{
		RequestID:   requestID,
		Timestamp:   time.Now(),
		Response:    json.RawMessage(responseBody),
		StatusCode:  resp.StatusCode,
		ProcessTime: time.Since(startTime).Milliseconds(),
	}

	if err := g.db.InsertAuditResponse(auditResponse); err != nil {
		log.Printf("Failed to insert audit response: %v", err)
	}

	// Also log to Tinybird if configured
	if g.tinybirdDB != nil {
		if err := g.tinybirdDB.InsertAuditResponse(auditResponse); err != nil {
			log.Printf("Failed to insert audit response to Tinybird: %v", err)
		}
	}

	// Forward response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Send the response
	w.WriteHeader(resp.StatusCode)
	w.Write(responseBody)

	// Response logging is already done above
}

func (g *Gateway) sendResponse(w http.ResponseWriter, response types.JSONRPCResponse, requestID string, startTime time.Time, statusCode int) {
	responseBody, err := json.Marshal(response)
	if err != nil {
		g.handleError(w, "Failed to marshal response", requestID, startTime, http.StatusInternalServerError)
		return
	}

	// Store the response
	auditResponse := &types.AuditResponse{
		RequestID:   requestID,
		Timestamp:   time.Now(),
		Response:    json.RawMessage(responseBody),
		StatusCode:  statusCode,
		ProcessTime: time.Since(startTime).Milliseconds(),
	}

	if err := g.db.InsertAuditResponse(auditResponse); err != nil {
		log.Printf("Failed to insert audit response: %v", err)
	}

	// Also log to Tinybird if configured
	if g.tinybirdDB != nil {
		if err := g.tinybirdDB.InsertAuditResponse(auditResponse); err != nil {
			log.Printf("Failed to insert audit response to Tinybird: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(responseBody)
}

func (g *Gateway) handleError(w http.ResponseWriter, errorMsg string, requestID string, startTime time.Time, statusCode int) {
	errorResp := types.JSONRPCResponse{
		ID:      nil,
		JSONRPC: "2.0",
		Error: &types.JSONRPCError{
			Code:    -32603,
			Message: "Internal error",
			Data:    errorMsg,
		},
	}

	responseBody, _ := json.Marshal(errorResp)

	// Store the error response
	auditResponse := &types.AuditResponse{
		RequestID:   requestID,
		Timestamp:   time.Now(),
		Response:    json.RawMessage(responseBody),
		StatusCode:  statusCode,
		ProcessTime: time.Since(startTime).Milliseconds(),
		Error:       errorMsg,
	}

	if err := g.db.InsertAuditResponse(auditResponse); err != nil {
		log.Printf("Failed to insert audit response: %v", err)
	}

	// Also log to Tinybird if configured
	if g.tinybirdDB != nil {
		if err := g.tinybirdDB.InsertAuditResponse(auditResponse); err != nil {
			log.Printf("Failed to insert audit response to Tinybird: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(responseBody)
}

// logRequest is no longer needed as we store requests and responses separately

// GetAuditRequests returns audit requests with pagination
func (g *Gateway) GetAuditRequests(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	requests, err := g.db.GetAuditRequests(limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve audit requests: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"requests": requests,
		"limit":    limit,
		"offset":   offset,
		"count":    len(requests),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAuditResponses returns audit responses with pagination
func (g *Gateway) GetAuditResponses(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	responses, err := g.db.GetAuditResponses(limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve audit responses: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"responses": responses,
		"limit":     limit,
		"offset":    offset,
		"count":     len(responses),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetOrphanedRequests returns requests without responses
func (g *Gateway) GetOrphanedRequests(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	requests, err := g.db.GetOrphanedRequests(limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve orphaned requests: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"orphaned_requests": requests,
		"limit":             limit,
		"offset":            offset,
		"count":             len(requests),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAuditLogs returns audit logs with pagination (backward compatibility - combined view)
func (g *Gateway) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	method := r.URL.Query().Get("method")

	var logs []types.AuditLog
	var err error

	if method != "" {
		logs, err = g.db.GetAuditLogsByMethod(method, limit, offset)
	} else {
		logs, err = g.db.GetAuditLogs(limit, offset)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve audit logs: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"logs":   logs,
		"limit":  limit,
		"offset": offset,
		"count":  len(logs),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetStats returns statistics about the audit logs
func (g *Gateway) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := g.db.GetStats()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve stats: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// HealthCheck endpoint
func (g *Gateway) HealthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"version":   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// SetupRoutes configures the HTTP routes
func (g *Gateway) SetupRoutes() *mux.Router {
	r := mux.NewRouter()

	// JSON-RPC endpoint
	r.HandleFunc("/rpc", g.ProxyJSONRPC).Methods("POST", "OPTIONS")
	r.HandleFunc("/mcp", g.ProxyJSONRPC).Methods("POST", "OPTIONS")

	// Management endpoints
	r.HandleFunc("/audit/logs", g.GetAuditLogs).Methods("GET")            // Combined view (backward compatibility)
	r.HandleFunc("/audit/requests", g.GetAuditRequests).Methods("GET")    // Requests only
	r.HandleFunc("/audit/responses", g.GetAuditResponses).Methods("GET")  // Responses only
	r.HandleFunc("/audit/orphaned", g.GetOrphanedRequests).Methods("GET") // Failed/orphaned requests
	r.HandleFunc("/audit/stats", g.GetStats).Methods("GET")
	r.HandleFunc("/health", g.HealthCheck).Methods("GET")

	// Serve static dashboard
	r.PathPrefix("/").Handler(http.HandlerFunc(serveDashboard))

	return r
}

// Utility functions
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Use RemoteAddr
	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

func generateRequestID() string {
	return fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), time.Now().Unix()%1000)
}

// Simple dashboard
func serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	dashboard := `<!DOCTYPE html>
<html>
<head>
    <title>JSON-RPC Gateway</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { color: #333; border-bottom: 3px solid #007cba; padding-bottom: 10px; }
        .endpoint { background: #f8f9fa; padding: 15px; margin: 10px 0; border-radius: 5px; border-left: 4px solid #007cba; }
        .method { font-weight: bold; color: #007cba; }
        pre { background: #2d3748; color: #e2e8f0; padding: 15px; border-radius: 5px; overflow-x: auto; }
        .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; margin: 20px 0; }
        .stat-card { background: #e7f3ff; padding: 20px; border-radius: 8px; text-align: center; }
        .stat-number { font-size: 2em; font-weight: bold; color: #007cba; }
        .button { display: inline-block; background: #007cba; color: white; padding: 10px 20px; text-decoration: none; border-radius: 5px; margin: 5px; }
        .button:hover { background: #005a8b; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 JSON-RPC Gateway</h1>
        
        <div class="stats">
            <div class="stat-card">
                <div class="stat-number" id="totalRequests">-</div>
                <div>Total Requests</div>
            </div>
            <div class="stat-card">
                <div class="stat-number" id="recentRequests">-</div>
                <div>Last Hour</div>
            </div>
        </div>

        <div style="margin: 20px 0;">
            <a href="/audit/logs" class="button">📋 View Logs</a>
            <a href="/audit/stats" class="button">📊 Statistics</a>
            <a href="/health" class="button">❤️ Health Check</a>
        </div>

        <h2>📡 API Endpoints</h2>
        
        <div class="endpoint">
            <span class="method">POST</span> <strong>/rpc</strong><br>
            Main JSON-RPC endpoint. Accepts any JSON-RPC 2.0 request and logs it.
        </div>

        <div class="endpoint">
            <span class="method">GET</span> <strong>/audit/logs</strong><br>
            Retrieve audit logs with pagination. Query params: limit, offset, method
        </div>

        <div class="endpoint">
            <span class="method">GET</span> <strong>/audit/stats</strong><br>
            Get statistics about requests and methods.
        </div>

        <h2>🧪 Test JSON-RPC Request</h2>
        <pre>curl -X POST http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "getUserInfo",
    "params": {"userId": 123},
    "id": 1
  }'</pre>

        <h2>📋 Example Response</h2>
        <pre>{
  "jsonrpc": "2.0",
  "result": {
    "message": "Mock response for method: getUserInfo",
    "timestamp": 1640995200,
    "echo_params": {"userId": 123}
  },
  "id": 1
}</pre>
    </div>

    <script>
        // Load stats
        fetch('/audit/stats')
            .then(r => r.json())
            .then(data => {
                document.getElementById('totalRequests').textContent = data.total_requests || 0;
                document.getElementById('recentRequests').textContent = data.requests_last_hour || 0;
            })
            .catch(() => {
                document.getElementById('totalRequests').textContent = '0';
                document.getElementById('recentRequests').textContent = '0';
            });
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboard))
}

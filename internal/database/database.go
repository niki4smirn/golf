package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/niki4smirn/golf/internal/types"
)

const createTableSQL = `
-- Requests table - stores every incoming request immediately
CREATE TABLE IF NOT EXISTS audit_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    method TEXT NOT NULL,
    request_id TEXT UNIQUE NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    request TEXT NOT NULL,
    headers TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Responses table - stores responses (success or failure)
CREATE TABLE IF NOT EXISTS audit_responses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    response TEXT,
    status_code INTEGER NOT NULL,
    process_time_ms INTEGER NOT NULL,
    error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (request_id) REFERENCES audit_requests(request_id)
);

-- Indexes for requests
CREATE INDEX IF NOT EXISTS idx_audit_requests_timestamp ON audit_requests(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_requests_method ON audit_requests(method);
CREATE INDEX IF NOT EXISTS idx_audit_requests_ip_address ON audit_requests(ip_address);
CREATE INDEX IF NOT EXISTS idx_audit_requests_request_id ON audit_requests(request_id);

-- Indexes for responses
CREATE INDEX IF NOT EXISTS idx_audit_responses_timestamp ON audit_responses(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_responses_request_id ON audit_responses(request_id);
CREATE INDEX IF NOT EXISTS idx_audit_responses_status_code ON audit_responses(status_code);

-- View for backward compatibility - combines requests and responses
CREATE VIEW IF NOT EXISTS audit_logs AS
SELECT 
    r.id,
    r.timestamp,
    r.method,
    r.request_id,
    r.ip_address,
    r.user_agent,
    r.request,
    r.headers,
    COALESCE(resp.response, '{}') as response,
    COALESCE(resp.status_code, 0) as status_code,
    COALESCE(resp.process_time_ms, 0) as process_time_ms,
    resp.error
FROM audit_requests r
LEFT JOIN audit_responses resp ON r.request_id = resp.request_id
ORDER BY r.timestamp DESC;
`

// Database wraps the SQLite database connection
type Database struct {
	db *sql.DB
}

// New creates a new database connection and initializes tables
func New(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Create tables and indexes
	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &Database{db: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// InsertAuditRequest inserts a new audit request entry immediately when request is received
func (d *Database) InsertAuditRequest(req *types.AuditRequest) error {
	query := `
		INSERT INTO audit_requests (
			timestamp, method, request_id, ip_address, user_agent, request, headers
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	requestJSON, err := json.Marshal(req.Request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	var headersJSON []byte
	if req.Headers != nil {
		headersJSON, err = json.Marshal(req.Headers)
		if err != nil {
			return fmt.Errorf("failed to marshal headers: %w", err)
		}
	}

	result, err := d.db.Exec(query,
		req.Timestamp,
		req.Method,
		req.RequestID,
		req.IPAddress,
		req.UserAgent,
		string(requestJSON),
		string(headersJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to insert audit request: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	req.ID = id
	return nil
}

// unwrapSSEResponse removes SSE wrapper from response data
func unwrapSSEResponse(data []byte) []byte {
	dataStr := string(data)

	// Check if it's SSE format (starts with "event:" or "data:")
	if !strings.HasPrefix(dataStr, "event:") && !strings.HasPrefix(dataStr, "data:") {
		// Not SSE format, return as-is
		return data
	}

	// Split by lines and extract JSON from data: lines
	lines := strings.Split(dataStr, "\n")
	var jsonData strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Extract data from "data: " lines
		if strings.HasPrefix(line, "data: ") {
			jsonContent := strings.TrimPrefix(line, "data: ")
			jsonData.WriteString(jsonContent)
		}
	}

	result := jsonData.String()

	// If no data was found, return original
	if result == "" {
		return data
	}

	return []byte(result)
}

// InsertAuditResponse inserts a response entry linked to a request
func (d *Database) InsertAuditResponse(resp *types.AuditResponse) error {
	query := `
		INSERT INTO audit_responses (
			request_id, timestamp, response, status_code, process_time_ms, error
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	var responseJSON []byte
	if resp.Response != nil {
		var err error
		withoutSSE := unwrapSSEResponse(resp.Response)
		responseJSON, err = json.Marshal(withoutSSE)
		if err != nil {
			return fmt.Errorf("failed to marshal response: %w (%s)", err, resp.Response)
		}
	}

	result, err := d.db.Exec(query,
		resp.RequestID,
		resp.Timestamp,
		string(responseJSON),
		resp.StatusCode,
		resp.ProcessTime,
		resp.Error,
	)
	if err != nil {
		return fmt.Errorf("failed to insert audit response: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	resp.ID = id
	return nil
}

// InsertAuditLog inserts a new audit log entry (legacy method for backward compatibility)
func (d *Database) InsertAuditLog(log *types.AuditLog) error {
	// Insert request first
	req := &types.AuditRequest{
		Timestamp: log.Timestamp,
		Method:    log.Method,
		RequestID: log.RequestID,
		IPAddress: log.IPAddress,
		UserAgent: log.UserAgent,
		Request:   log.Request,
		Headers:   log.Headers,
	}

	if err := d.InsertAuditRequest(req); err != nil {
		return err
	}

	// Insert response if we have status code or response data
	if log.StatusCode > 0 || log.Response != nil || log.Error != "" {
		resp := &types.AuditResponse{
			RequestID:   log.RequestID,
			Timestamp:   log.Timestamp,
			Response:    log.Response,
			StatusCode:  log.StatusCode,
			ProcessTime: log.ProcessTime,
			Error:       log.Error,
		}

		if err := d.InsertAuditResponse(resp); err != nil {
			return err
		}
	}

	return nil
}

// GetAuditRequests retrieves audit requests with pagination
func (d *Database) GetAuditRequests(limit, offset int) ([]types.AuditRequest, error) {
	query := `
		SELECT id, timestamp, method, request_id, ip_address, user_agent, request, headers
		FROM audit_requests
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit requests: %w", err)
	}
	defer rows.Close()

	var requests []types.AuditRequest
	for rows.Next() {
		var req types.AuditRequest
		var requestStr, headersStr sql.NullString

		err := rows.Scan(
			&req.ID,
			&req.Timestamp,
			&req.Method,
			&req.RequestID,
			&req.IPAddress,
			&req.UserAgent,
			&requestStr,
			&headersStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if requestStr.Valid {
			req.Request = json.RawMessage(requestStr.String)
		}

		if headersStr.Valid {
			req.Headers = json.RawMessage(headersStr.String)
		}

		requests = append(requests, req)
	}

	return requests, nil
}

// GetAuditResponses retrieves audit responses with pagination
func (d *Database) GetAuditResponses(limit, offset int) ([]types.AuditResponse, error) {
	query := `
		SELECT id, request_id, timestamp, response, status_code, process_time_ms, error
		FROM audit_responses
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit responses: %w", err)
	}
	defer rows.Close()

	var responses []types.AuditResponse
	for rows.Next() {
		var resp types.AuditResponse
		var responseStr, errorStr sql.NullString

		err := rows.Scan(
			&resp.ID,
			&resp.RequestID,
			&resp.Timestamp,
			&responseStr,
			&resp.StatusCode,
			&resp.ProcessTime,
			&errorStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if responseStr.Valid {
			resp.Response = json.RawMessage(responseStr.String)
		}

		if errorStr.Valid {
			resp.Error = errorStr.String
		}

		responses = append(responses, resp)
	}

	return responses, nil
}

// GetOrphanedRequests retrieves requests that have no corresponding response
func (d *Database) GetOrphanedRequests(limit, offset int) ([]types.AuditRequest, error) {
	query := `
		SELECT r.id, r.timestamp, r.method, r.request_id, r.ip_address, r.user_agent, r.request, r.headers
		FROM audit_requests r
		LEFT JOIN audit_responses resp ON r.request_id = resp.request_id
		WHERE resp.request_id IS NULL
		ORDER BY r.timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query orphaned requests: %w", err)
	}
	defer rows.Close()

	var requests []types.AuditRequest
	for rows.Next() {
		var req types.AuditRequest
		var requestStr, headersStr sql.NullString

		err := rows.Scan(
			&req.ID,
			&req.Timestamp,
			&req.Method,
			&req.RequestID,
			&req.IPAddress,
			&req.UserAgent,
			&requestStr,
			&headersStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if requestStr.Valid {
			req.Request = json.RawMessage(requestStr.String)
		}

		if headersStr.Valid {
			req.Headers = json.RawMessage(headersStr.String)
		}

		requests = append(requests, req)
	}

	return requests, nil
}

// GetAuditLogs retrieves audit logs with pagination (combined view for backward compatibility)
func (d *Database) GetAuditLogs(limit, offset int) ([]types.AuditLog, error) {
	query := `
		SELECT id, timestamp, method, request_id, ip_address, user_agent,
			   request, headers, response, status_code, process_time_ms, error
		FROM audit_logs
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []types.AuditLog
	for rows.Next() {
		var log types.AuditLog
		var requestStr, headersStr, responseStr, errorStr sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&log.Method,
			&log.RequestID,
			&log.IPAddress,
			&log.UserAgent,
			&requestStr,
			&headersStr,
			&responseStr,
			&log.StatusCode,
			&log.ProcessTime,
			&errorStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if requestStr.Valid {
			log.Request = json.RawMessage(requestStr.String)
		}

		if headersStr.Valid {
			log.Headers = json.RawMessage(headersStr.String)
		}

		if responseStr.Valid {
			log.Response = json.RawMessage(responseStr.String)
		}

		if errorStr.Valid {
			log.Error = errorStr.String
		}

		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return logs, nil
}

// GetAuditLogsByMethod retrieves audit logs filtered by method
func (d *Database) GetAuditLogsByMethod(method string, limit, offset int) ([]types.AuditLog, error) {
	query := `
		SELECT id, timestamp, method, request_id, ip_address, user_agent,
			   request, response, status_code, process_time_ms, error
		FROM audit_logs
		WHERE method = ?
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, method, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs by method: %w", err)
	}
	defer rows.Close()

	var logs []types.AuditLog
	for rows.Next() {
		var log types.AuditLog
		var requestStr, responseStr sql.NullString
		var errorStr sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&log.Method,
			&log.RequestID,
			&log.IPAddress,
			&log.UserAgent,
			&requestStr,
			&responseStr,
			&log.StatusCode,
			&log.ProcessTime,
			&errorStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if requestStr.Valid {
			log.Request = json.RawMessage(requestStr.String)
		}

		if responseStr.Valid {
			log.Response = json.RawMessage(responseStr.String)
		}

		if errorStr.Valid {
			log.Error = errorStr.String
		}

		logs = append(logs, log)
	}

	return logs, nil
}

// GetStats returns statistics about the audit logs
func (d *Database) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total request count
	var totalRequests int
	err := d.db.QueryRow("SELECT COUNT(*) FROM audit_requests").Scan(&totalRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to get total request count: %w", err)
	}
	stats["total_requests"] = totalRequests

	// Total response count
	var totalResponses int
	err = d.db.QueryRow("SELECT COUNT(*) FROM audit_responses").Scan(&totalResponses)
	if err != nil {
		return nil, fmt.Errorf("failed to get total response count: %w", err)
	}
	stats["total_responses"] = totalResponses

	// Orphaned requests (requests without responses)
	var orphanedCount int
	orphanedQuery := `
		SELECT COUNT(*) 
		FROM audit_requests r 
		LEFT JOIN audit_responses resp ON r.request_id = resp.request_id 
		WHERE resp.request_id IS NULL
	`
	err = d.db.QueryRow(orphanedQuery).Scan(&orphanedCount)
	if err != nil {
		log.Printf("Failed to get orphaned count: %v", err)
	} else {
		stats["orphaned_requests"] = orphanedCount
	}

	// Method distribution
	methodQuery := `
		SELECT method, COUNT(*) as count
		FROM audit_requests
		GROUP BY method
		ORDER BY count DESC
		LIMIT 10
	`
	rows, err := d.db.Query(methodQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query method stats: %w", err)
	}
	defer rows.Close()

	methodStats := make(map[string]int)
	for rows.Next() {
		var method string
		var count int
		if err := rows.Scan(&method, &count); err != nil {
			log.Printf("Failed to scan method stats: %v", err)
			continue
		}
		methodStats[method] = count
	}
	stats["methods"] = methodStats

	// Status code distribution
	statusQuery := `
		SELECT status_code, COUNT(*) as count
		FROM audit_responses
		GROUP BY status_code
		ORDER BY count DESC
		LIMIT 10
	`
	statusRows, err := d.db.Query(statusQuery)
	if err != nil {
		log.Printf("Failed to query status stats: %v", err)
	} else {
		defer statusRows.Close()
		statusStats := make(map[string]int)
		for statusRows.Next() {
			var statusCode int
			var count int
			if err := statusRows.Scan(&statusCode, &count); err != nil {
				log.Printf("Failed to scan status stats: %v", err)
				continue
			}
			statusStats[fmt.Sprintf("%d", statusCode)] = count
		}
		stats["status_codes"] = statusStats
	}

	// Recent activity (last hour)
	var recentRequests int
	recentQuery := "SELECT COUNT(*) FROM audit_requests WHERE timestamp > datetime('now', '-1 hour')"
	err = d.db.QueryRow(recentQuery).Scan(&recentRequests)
	if err != nil {
		log.Printf("Failed to get recent request count: %v", err)
	} else {
		stats["requests_last_hour"] = recentRequests
	}

	// Error rate (responses with errors)
	var errorCount int
	errorQuery := "SELECT COUNT(*) FROM audit_responses WHERE error IS NOT NULL AND error != ''"
	err = d.db.QueryRow(errorQuery).Scan(&errorCount)
	if err != nil {
		log.Printf("Failed to get error count: %v", err)
	} else {
		stats["error_count"] = errorCount
		if totalResponses > 0 {
			stats["error_rate"] = float64(errorCount) / float64(totalResponses) * 100
		} else {
			stats["error_rate"] = 0.0
		}
	}

	// Average response time (in milliseconds)
	var avgResponseTime sql.NullFloat64
	avgQuery := "SELECT AVG(process_time_ms) FROM audit_responses WHERE process_time_ms > 0"
	err = d.db.QueryRow(avgQuery).Scan(&avgResponseTime)
	if err != nil {
		log.Printf("Failed to get average response time: %v", err)
	} else if avgResponseTime.Valid {
		stats["avg_response_time_ms"] = avgResponseTime.Float64
	}

	return stats, nil
}

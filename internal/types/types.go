package types

import (
	"encoding/json"
	"time"
)

// JSONRPCRequest represents a standard JSON-RPC 2.0 request
type JSONRPCRequest struct {
	ID      interface{} `json:"id"`
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a standard JSON-RPC 2.0 response
type JSONRPCResponse struct {
	ID      interface{}   `json:"id"`
	JSONRPC string        `json:"jsonrpc"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// AuditRequest represents a logged request entry
type AuditRequest struct {
	ID        int64           `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Method    string          `json:"method"`
	RequestID string          `json:"request_id"`
	IPAddress string          `json:"ip_address"`
	UserAgent string          `json:"user_agent"`
	Request   json.RawMessage `json:"request"`
	Headers   json.RawMessage `json:"headers,omitempty"`
}

// AuditResponse represents a logged response entry
type AuditResponse struct {
	ID          int64           `json:"id"`
	RequestID   string          `json:"request_id"`
	Timestamp   time.Time       `json:"timestamp"`
	Response    json.RawMessage `json:"response,omitempty"`
	StatusCode  int             `json:"status_code"`
	ProcessTime int64           `json:"process_time_ms"` // in milliseconds
	Error       string          `json:"error,omitempty"`
}

// AuditLog represents a combined view of request and response for compatibility
type AuditLog struct {
	ID          int64           `json:"id"`
	Timestamp   time.Time       `json:"timestamp"`
	Method      string          `json:"method"`
	RequestID   string          `json:"request_id"`
	IPAddress   string          `json:"ip_address"`
	UserAgent   string          `json:"user_agent"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response,omitempty"`
	StatusCode  int             `json:"status_code"`
	ProcessTime int64           `json:"process_time_ms"` // in milliseconds
	Error       string          `json:"error,omitempty"`
	Headers     json.RawMessage `json:"headers,omitempty"`
}

// GatewayMetadata contains additional context for the audit log
type GatewayMetadata struct {
	ClientIP     string            `json:"client_ip"`
	UserAgent    string            `json:"user_agent"`
	Headers      map[string]string `json:"headers,omitempty"`
	RequestSize  int               `json:"request_size"`
	ResponseSize int               `json:"response_size"`
}

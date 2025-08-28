package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/niki4smirn/golf/internal/types"
)

// TinybirdDatabase handles audit logging to Tinybird Cloud
type TinybirdDatabase struct {
	token   string
	baseURL string
	client  *http.Client
}

// NewTinybirdDatabase creates a new Tinybird database instance
func NewTinybirdDatabase(token string) *TinybirdDatabase {
	return &TinybirdDatabase{
		token:   token,
		baseURL: "https://api.eu-central-1.aws.tinybird.co",
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// InsertAuditRequest sends request data to Tinybird
func (t *TinybirdDatabase) InsertAuditRequest(req *types.AuditRequest) error {
	event := map[string]interface{}{
		"id":         time.Now().UnixNano(),
		"timestamp":  req.Timestamp.Format("2006-01-02 15:04:05.000"),
		"method":     req.Method,
		"request_id": req.RequestID,
		"ip_address": req.IPAddress,
		"user_agent": req.UserAgent,
		"request":    string(req.Request),
		"headers":    string(req.Headers),
	}

	return t.sendEvent("audit_requests", event)
}

// InsertAuditResponse sends response data to Tinybird
func (t *TinybirdDatabase) InsertAuditResponse(resp *types.AuditResponse) error {
	event := map[string]interface{}{
		"id":              time.Now().UnixNano(),
		"request_id":      resp.RequestID,
		"timestamp":       resp.Timestamp.Format("2006-01-02 15:04:05.000"),
		"response":        string(resp.Response),
		"status_code":     resp.StatusCode,
		"process_time_ms": resp.ProcessTime,
		"error":           resp.Error,
	}

	return t.sendEvent("audit_responses", event)
}

// sendEvent sends an event to Tinybird Events API
func (t *TinybirdDatabase) sendEvent(datasource string, event map[string]interface{}) error {
	url := fmt.Sprintf("%s/events?name=%s", t.baseURL, datasource)

	jsonData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		// Read response body for better error details
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tinybird returned status: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Close is a no-op for Tinybird (HTTP-based)
func (t *TinybirdDatabase) Close() error {
	return nil
}

// Placeholder methods for interface compatibility
func (t *TinybirdDatabase) InsertAuditLog(log *types.AuditLog) error {
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

	if err := t.InsertAuditRequest(req); err != nil {
		return err
	}

	// Insert response if we have data
	if log.StatusCode > 0 || log.Response != nil || log.Error != "" {
		resp := &types.AuditResponse{
			RequestID:   log.RequestID,
			Timestamp:   log.Timestamp,
			Response:    log.Response,
			StatusCode:  log.StatusCode,
			ProcessTime: log.ProcessTime,
			Error:       log.Error,
		}

		return t.InsertAuditResponse(resp)
	}

	return nil
}

// Note: Query methods would need to be implemented using Tinybird's Query API
// For now, we'll keep SQLite for reads and use Tinybird for writes
func (t *TinybirdDatabase) GetAuditRequests(limit, offset int) ([]types.AuditRequest, error) {
	return nil, fmt.Errorf("read operations not implemented for Tinybird adapter")
}

func (t *TinybirdDatabase) GetAuditResponses(limit, offset int) ([]types.AuditResponse, error) {
	return nil, fmt.Errorf("read operations not implemented for Tinybird adapter")
}

func (t *TinybirdDatabase) GetOrphanedRequests(limit, offset int) ([]types.AuditRequest, error) {
	return nil, fmt.Errorf("read operations not implemented for Tinybird adapter")
}

func (t *TinybirdDatabase) GetAuditLogs(limit, offset int) ([]types.AuditLog, error) {
	return nil, fmt.Errorf("read operations not implemented for Tinybird adapter")
}

func (t *TinybirdDatabase) GetAuditLogsByMethod(method string, limit, offset int) ([]types.AuditLog, error) {
	return nil, fmt.Errorf("read operations not implemented for Tinybird adapter")
}

func (t *TinybirdDatabase) GetStats() (map[string]interface{}, error) {
	return nil, fmt.Errorf("read operations not implemented for Tinybird adapter")
}

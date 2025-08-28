package database

import "github.com/niki4smirn/golf/internal/types"

// AuditDatabase defines the interface for audit logging operations
type AuditDatabase interface {
	InsertAuditRequest(req *types.AuditRequest) error
	InsertAuditResponse(resp *types.AuditResponse) error
	GetAuditRequests(limit, offset int) ([]types.AuditRequest, error)
	GetAuditResponses(limit, offset int) ([]types.AuditResponse, error)
	GetOrphanedRequests(limit, offset int) ([]types.AuditRequest, error)
	GetAuditLogs(limit, offset int) ([]types.AuditLog, error)
	GetAuditLogsByMethod(method string, limit, offset int) ([]types.AuditLog, error)
	GetStats() (map[string]interface{}, error)
	Close() error
}

package database

import (
	"fmt"
	"log"

	"github.com/niki4smirn/golf/internal/types"
)

// DualDatabase writes to both SQLite and Tinybird
type DualDatabase struct {
	sqlite   *Database
	tinybird *TinybirdDatabase
}

// NewDualDatabase creates a database that writes to both SQLite and Tinybird
func NewDualDatabase(sqlitePath, tinybirdToken string) (*DualDatabase, error) {
	// Initialize SQLite
	sqlite, err := New(sqlitePath)
	if err != nil {
		return nil, err
	}

	// Initialize Tinybird
	tinybird := NewTinybirdDatabase(tinybirdToken)

	return &DualDatabase{
		sqlite:   sqlite,
		tinybird: tinybird,
	}, nil
}

// InsertAuditRequest writes to both databases
func (d *DualDatabase) InsertAuditRequest(req *types.AuditRequest) error {
	// Write to SQLite (primary - must succeed)
	if err := d.sqlite.InsertAuditRequest(req); err != nil {
		return err
	}

	// Write to Tinybird (best effort - log error but don't fail)
	if err := d.tinybird.InsertAuditRequest(req); err != nil {
		log.Printf("Failed to write request to Tinybird: %v", err)
	}

	return nil
}

// InsertAuditResponse writes to both databases
func (d *DualDatabase) InsertAuditResponse(resp *types.AuditResponse) error {
	// Write to SQLite (primary - must succeed)
	if err := d.sqlite.InsertAuditResponse(resp); err != nil {
		return err
	}

	// Write to Tinybird (best effort - log error but don't fail)
	if err := d.tinybird.InsertAuditResponse(resp); err != nil {
		log.Printf("Failed to write response to Tinybird: %v", err)
	}

	return nil
}

// Read operations use SQLite
func (d *DualDatabase) GetAuditRequests(limit, offset int) ([]types.AuditRequest, error) {
	return d.sqlite.GetAuditRequests(limit, offset)
}

func (d *DualDatabase) GetAuditResponses(limit, offset int) ([]types.AuditResponse, error) {
	return d.sqlite.GetAuditResponses(limit, offset)
}

func (d *DualDatabase) GetOrphanedRequests(limit, offset int) ([]types.AuditRequest, error) {
	return d.sqlite.GetOrphanedRequests(limit, offset)
}

func (d *DualDatabase) GetAuditLogs(limit, offset int) ([]types.AuditLog, error) {
	return d.sqlite.GetAuditLogs(limit, offset)
}

func (d *DualDatabase) GetAuditLogsByMethod(method string, limit, offset int) ([]types.AuditLog, error) {
	return d.sqlite.GetAuditLogsByMethod(method, limit, offset)
}

func (d *DualDatabase) GetStats() (map[string]interface{}, error) {
	return d.sqlite.GetStats()
}

func (d *DualDatabase) InsertAuditLog(log *types.AuditLog) error {
	// Write to SQLite (primary - must succeed)
	if err := d.sqlite.InsertAuditLog(log); err != nil {
		return err
	}

	// Write to Tinybird (best effort - log error but don't fail)
	if err := d.tinybird.InsertAuditLog(log); err != nil {
		fmt.Printf("Failed to write audit log to Tinybird: %v", err)
	}

	return nil
}

// Close both connections
func (d *DualDatabase) Close() error {
	d.tinybird.Close() // No-op for Tinybird
	return d.sqlite.Close()
}

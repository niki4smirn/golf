package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/niki4smirn/golf/internal/types"
)

// SimpleJSONRPCServer provides basic JSON-RPC responses for testing
type SimpleJSONRPCServer struct {
	methods map[string]func(params interface{}) (interface{}, error)
}

func NewSimpleJSONRPCServer() *SimpleJSONRPCServer {
	server := &SimpleJSONRPCServer{
		methods: make(map[string]func(params interface{}) (interface{}, error)),
	}

	// Register some example methods
	server.RegisterMethod("ping", server.handlePing)
	server.RegisterMethod("echo", server.handleEcho)
	server.RegisterMethod("getUserInfo", server.handleGetUserInfo)
	server.RegisterMethod("getTime", server.handleGetTime)
	server.RegisterMethod("calculate", server.handleCalculate)
	server.RegisterMethod("slowOperation", server.handleSlowOperation)
	server.RegisterMethod("errorTest", server.handleErrorTest)

	return server
}

func (s *SimpleJSONRPCServer) RegisterMethod(name string, handler func(params interface{}) (interface{}, error)) {
	s.methods[name] = handler
}

func (s *SimpleJSONRPCServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req types.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, nil, -32700, "Parse error", "Invalid JSON")
		return
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		s.sendError(w, req.ID, -32600, "Invalid Request", "Invalid JSON-RPC version")
		return
	}

	// Find method handler
	handler, exists := s.methods[req.Method]
	if !exists {
		s.sendError(w, req.ID, -32601, "Method not found", fmt.Sprintf("Method '%s' not found", req.Method))
		return
	}

	// Execute method
	result, err := handler(req.Params)
	if err != nil {
		s.sendError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	// Send success response
	resp := types.JSONRPCResponse{
		ID:      req.ID,
		JSONRPC: "2.0",
		Result:  result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *SimpleJSONRPCServer) sendError(w http.ResponseWriter, id interface{}, code int, message, data string) {
	resp := types.JSONRPCResponse{
		ID:      id,
		JSONRPC: "2.0",
		Error: &types.JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors are still HTTP 200
	json.NewEncoder(w).Encode(resp)
}

// Method handlers
func (s *SimpleJSONRPCServer) handlePing(params interface{}) (interface{}, error) {
	return map[string]interface{}{
		"message":   "pong",
		"timestamp": time.Now().Unix(),
		"server":    "simple-jsonrpc-server",
	}, nil
}

func (s *SimpleJSONRPCServer) handleEcho(params interface{}) (interface{}, error) {
	return map[string]interface{}{
		"echo":      params,
		"timestamp": time.Now().Unix(),
	}, nil
}

func (s *SimpleJSONRPCServer) handleGetUserInfo(params interface{}) (interface{}, error) {
	// Parse user ID from params
	var userID int
	if paramsMap, ok := params.(map[string]interface{}); ok {
		if id, ok := paramsMap["userId"].(float64); ok {
			userID = int(id)
		}
	}

	return map[string]interface{}{
		"userId":    userID,
		"username":  fmt.Sprintf("user%d", userID),
		"email":     fmt.Sprintf("user%d@example.com", userID),
		"active":    true,
		"createdAt": time.Now().Add(-time.Duration(userID*24) * time.Hour).Unix(),
	}, nil
}

func (s *SimpleJSONRPCServer) handleGetTime(params interface{}) (interface{}, error) {
	now := time.Now()
	return map[string]interface{}{
		"unix":      now.Unix(),
		"iso":       now.Format(time.RFC3339),
		"formatted": now.Format("2006-01-02 15:04:05"),
		"timezone":  now.Location().String(),
	}, nil
}

func (s *SimpleJSONRPCServer) handleCalculate(params interface{}) (interface{}, error) {
	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid parameters")
	}

	operation, ok := paramsMap["operation"].(string)
	if !ok {
		return nil, fmt.Errorf("missing operation")
	}

	a, ok1 := paramsMap["a"].(float64)
	b, ok2 := paramsMap["b"].(float64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("missing or invalid numbers")
	}

	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		result = a / b
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	return map[string]interface{}{
		"operation": operation,
		"a":         a,
		"b":         b,
		"result":    result,
	}, nil
}

func (s *SimpleJSONRPCServer) handleSlowOperation(params interface{}) (interface{}, error) {
	// Simulate a slow operation
	duration := 2 * time.Second
	if paramsMap, ok := params.(map[string]interface{}); ok {
		if d, ok := paramsMap["duration"].(float64); ok {
			duration = time.Duration(d) * time.Second
		}
	}

	time.Sleep(duration)

	return map[string]interface{}{
		"message":      "Slow operation completed",
		"duration":     duration.Seconds(),
		"completed_at": time.Now().Unix(),
	}, nil
}

func (s *SimpleJSONRPCServer) handleErrorTest(params interface{}) (interface{}, error) {
	return nil, fmt.Errorf("this is a test error for audit logging")
}

func main() {
	port := flag.String("port", "9000", "Port to run the JSON-RPC server on")
	flag.Parse()

	server := NewSimpleJSONRPCServer()

	httpServer := &http.Server{
		Addr:         ":" + *port,
		Handler:      server,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting JSON-RPC test server on port %s", *port)
		log.Printf("Available methods:")
		log.Printf("  - ping: Returns pong with timestamp")
		log.Printf("  - echo: Echoes back the parameters")
		log.Printf("  - getUserInfo: Returns user info (params: {userId: number})")
		log.Printf("  - getTime: Returns current time in various formats")
		log.Printf("  - calculate: Performs math operations (params: {operation: string, a: number, b: number})")
		log.Printf("  - slowOperation: Simulates slow operation (params: {duration: seconds})")
		log.Printf("  - errorTest: Always returns an error for testing")
		log.Printf("")
		log.Printf("Example usage:")
		log.Printf("curl -X POST http://localhost:%s/rpc \\", *port)
		log.Printf("  -H 'Content-Type: application/json' \\")
		log.Printf("  -d '{\"jsonrpc\":\"2.0\",\"method\":\"ping\",\"id\":1}'")

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	if err := httpServer.Close(); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}
	log.Println("Server stopped")
}

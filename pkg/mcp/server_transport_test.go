package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/mark3labs/mcp-go/server"
)

func TestHealthReadyHandlers(t *testing.T) {
	s := NewServer(ServerConfig{Config: config.DefaultConfig()})

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	s.handleHealth(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected health status 200, got %d", healthRec.Code)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	readyRec := httptest.NewRecorder()
	s.handleReady(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected ready status 200, got %d", readyRec.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(readyRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse ready response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected ready status 'ok', got %v", payload["status"])
	}
}

func TestSSEEndpoint(t *testing.T) {
	s := NewServer(ServerConfig{Config: config.DefaultConfig()})

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	sseHandler := server.NewSSEServer(s.mcp,
		server.WithBaseURL("http://127.0.0.1"),
		server.WithUseFullURLForMessageEndpoint(true),
		server.WithHTTPServer(srv),
	)
	mux.Handle("/sse", sseHandler.SSEHandler())
	mux.Handle("/message", sseHandler.MessageHandler())

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatalf("failed to call /sse: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /sse status 200, got %d", resp.StatusCode)
	}

	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("failed to read /sse response: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "event: endpoint") {
		t.Fatalf("expected /sse response to include endpoint event, got: %q", string(buf[:n]))
	}
}

func TestStreamableHTTPEndpoint(t *testing.T) {
	s := NewServer(ServerConfig{Config: config.DefaultConfig()})

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	streamable := server.NewStreamableHTTPServer(
		s.mcp,
		server.WithEndpointPath("/mcp"),
		server.WithStreamableHTTPServer(srv),
	)
	mux.Handle("/mcp", streamable)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("failed to call /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /mcp status 200, got %d", resp.StatusCode)
	}
}

func TestCombinedSSEAndStreamableHTTP(t *testing.T) {
	s := NewServer(ServerConfig{Config: config.DefaultConfig()})

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	sseHandler := server.NewSSEServer(s.mcp,
		server.WithBaseURL("http://127.0.0.1"),
		server.WithUseFullURLForMessageEndpoint(true),
		server.WithHTTPServer(srv),
	)
	streamable := server.NewStreamableHTTPServer(
		s.mcp,
		server.WithEndpointPath("/mcp"),
		server.WithStreamableHTTPServer(srv),
	)

	mux.Handle("/sse", sseHandler.SSEHandler())
	mux.Handle("/message", sseHandler.MessageHandler())
	mux.Handle("/mcp", streamable)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatalf("failed to call /sse: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /sse status 200, got %d", resp.StatusCode)
	}

	resp, err = client.Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("failed to call /mcp: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /mcp status 200, got %d", resp.StatusCode)
	}
}

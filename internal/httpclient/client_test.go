package httpclient

import (
	"testing"
	"time"
)

func TestGetSharedClient(t *testing.T) {
	// Test that timeout is consistently applied
	client1 := GetSharedClient(30 * time.Second)
	if client1.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client1.Timeout)
	}

	// Test that zero timeout (no timeout) is properly handled
	client2 := GetSharedClient(0)
	if client2.Timeout != 0 {
		t.Errorf("Expected timeout 0 (no timeout), got %v", client2.Timeout)
	}

	// Test that different timeouts work correctly
	client3 := GetSharedClient(120 * time.Second)
	if client3.Timeout != 120*time.Second {
		t.Errorf("Expected timeout 120s, got %v", client3.Timeout)
	}

	// Verify all clients share the same transport
	if client1.Transport != client2.Transport {
		t.Error("Expected all clients to share the same transport")
	}
	if client1.Transport != client3.Transport {
		t.Error("Expected all clients to share the same transport")
	}

	// Test concurrent access
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			client := GetSharedClient(time.Duration(i+1) * time.Second)
			if client.Transport == nil {
				t.Error("Expected non-nil transport")
			}
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

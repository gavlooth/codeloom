package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaStreamContextCancellation(t *testing.T) {
	// Create test server that streams responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Stream multiple responses (newline-delimited JSON)
		responses := []string{
			`{"model":"test","message":{"role":"assistant","content":"Hello "},"done":false}`,
			`{"model":"test","message":{"role":"assistant","content":"world "},"done":false}`,
			`{"model":"test","message":{"role":"assistant","content":"there "},"done":false}`,
			`{"model":"test","message":{"role":"assistant","content":"!"},"done":true}`,
		}

		// Send responses with delay to allow cancellation
		for i, resp := range responses {
			if i > 0 {
				time.Sleep(50 * time.Millisecond)
			}
			w.Write([]byte(resp + "\n"))
			flusher, _ := w.(http.Flusher)
			flusher.Flush()
		}
	}))
	defer server.Close()

	// Create Ollama provider
	provider := &OllamaProvider{
		baseURL: server.URL,
		model:   "test",
		client:  server.Client(),
	}

	t.Run("cancellation_during_stream", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start streaming
		ch, err := provider.Stream(ctx, []Message{
			{Role: RoleUser, Content: "test"},
		})
		if err != nil {
			t.Fatalf("Stream() failed: %v", err)
		}

		// Read first message
		select {
		case msg := <-ch:
			if msg == "" {
				t.Error("Expected non-empty message")
			}
			t.Logf("Received: %q", msg)
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for first message")
		}

		// Cancel context immediately after first message
		cancel()

		// Verify channel is closed quickly (within 500ms)
		select {
		case _, ok := <-ch:
			if ok {
				// Channel might have buffered data, wait for close
				select {
				case _, ok := <-ch:
					if !ok {
						t.Log("Channel closed after cancellation (expected)")
					}
				case <-time.After(500 * time.Millisecond):
					t.Error("Channel not closed in time after cancellation")
				}
			} else {
				t.Log("Channel closed immediately after cancellation (expected)")
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Channel should have been closed after cancellation")
		}
	})

	t.Run("cancellation_before_stream", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		// Start streaming after cancellation
		ch, err := provider.Stream(ctx, []Message{
			{Role: RoleUser, Content: "test"},
		})
		// When context is already cancelled, the HTTP request will fail immediately
		if err != nil {
			// This is expected behavior - the request should fail due to context cancellation
			t.Logf("Stream() failed as expected with cancelled context: %v", err)
			return
		}

		// Verify channel is closed quickly (if stream was created)
		select {
		case _, ok := <-ch:
			if ok {
				t.Error("Expected channel to be closed, but received data")
			}
			t.Log("Channel closed immediately (expected)")
		case <-time.After(200 * time.Millisecond):
			t.Error("Channel should have been closed immediately after pre-cancelled context")
		}
	})

	t.Run("normal_streaming", func(t *testing.T) {
		ctx := context.Background()

		// Start streaming without cancellation
		ch, err := provider.Stream(ctx, []Message{
			{Role: RoleUser, Content: "test"},
		})
		if err != nil {
			t.Fatalf("Stream() failed: %v", err)
		}

		// Read all messages
		var fullContent strings.Builder
		timeout := time.After(2 * time.Second)
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					// Channel closed, streaming complete
					goto done
				}
				fullContent.WriteString(msg)
			case <-timeout:
				t.Fatal("Timeout waiting for stream completion")
			}
		}
	done:

		expected := "Hello world there !"
		result := fullContent.String()
		if result != expected {
			t.Errorf("Expected content %q, got %q", expected, result)
		}
		t.Logf("Successfully received full content: %q", result)
	})
}

func TestOllamaStreamMalformedJSON(t *testing.T) {
	// Create test server that sends malformed JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Send valid response then malformed
		responses := []string{
			`{"model":"test","message":{"role":"assistant","content":"Good "},"done":false}`,
			`{"invalid json}`,
		}

		for _, resp := range responses {
			time.Sleep(20 * time.Millisecond)
			w.Write([]byte(resp + "\n"))
			flusher, _ := w.(http.Flusher)
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := &OllamaProvider{
		baseURL: server.URL,
		model:   "test",
		client:  server.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := provider.Stream(ctx, []Message{
		{Role: RoleUser, Content: "test"},
	})
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}

	// Should receive first message then handle error gracefully
	var messages []string
	timeout := time.After(1 * time.Second)
loop:
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				break loop
			}
			messages = append(messages, msg)
		case <-timeout:
			break loop
		}
	}

	if len(messages) == 0 {
		t.Error("Expected to receive at least one message before malformed JSON")
	}
	if len(messages) == 1 && messages[0] != "Good " {
		t.Errorf("Expected first message to be 'Good ', got %q", messages[0])
	}
	t.Logf("Received %d messages (malformed JSON handled gracefully)", len(messages))
}

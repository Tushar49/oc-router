package gateway

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadFirstEvent(t *testing.T) {
	t.Run("comments and data-less events do not terminate", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader(": keepalive\n\nevent: ping\n\ndata: first\n\nrest"))
		prefix, err := readFirstEvent(reader)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(prefix), ": keepalive\n\nevent: ping\n\ndata: first\n\n"; got != want {
			t.Fatalf("prefix: got %q, want %q", got, want)
		}
		rest, _ := io.ReadAll(reader)
		if string(rest) != "rest" {
			t.Fatalf("remaining: got %q", rest)
		}
	})

	t.Run("bare data line terminates", func(t *testing.T) {
		if prefix, err := readFirstEvent(bufio.NewReader(strings.NewReader("data\n\n"))); err != nil || string(prefix) != "data\n\n" {
			t.Fatalf("prefix=%q err=%v", prefix, err)
		}
	})

	t.Run("EOF before the first data event", func(t *testing.T) {
		_, err := readFirstEvent(bufio.NewReader(strings.NewReader(": keepalive\n\n")))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("err: got %v, want unexpected EOF", err)
		}
	})

	t.Run("oversized prelude", func(t *testing.T) {
		payload := strings.Repeat(": keepalive\n", (1<<20)/12+2)
		if _, err := readFirstEvent(bufio.NewReader(strings.NewReader(payload))); err == nil {
			t.Fatal("expected an error for an oversized prelude")
		}
	})
}

func TestDoInferenceAttemptFirstEvent(t *testing.T) {
	t.Run("times out before the first event", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		defer server.Close()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
		_, err := doInferenceAttempt(server.Client(), req, 20*time.Millisecond, 0)
		if !errors.Is(err, errFirstEventTimeout) {
			t.Fatalf("err: got %v, want first-event timeout", err)
		}
		var phased *phasedError
		if !errors.As(err, &phased) || phased.phase != "first_event" {
			t.Fatalf("phase: %#v", err)
		}
	})

	t.Run("replays the buffered prefix", func(t *testing.T) {
		body := ": keepalive\n\ndata: hello\n\n"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}))
		defer server.Close()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
		resp, err := doInferenceAttempt(server.Client(), req, time.Second, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, []byte(body)) {
			t.Fatalf("replayed body: got %q, want %q", got, body)
		}
	})

	t.Run("attempt timeout covers the header wait", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(200 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
			}
		}))
		defer server.Close()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
		_, err := doInferenceAttempt(server.Client(), req, 0, 20*time.Millisecond)
		if !errors.Is(err, errAttemptTimeout) {
			t.Fatalf("err: got %v, want attempt timeout", err)
		}
	})
}

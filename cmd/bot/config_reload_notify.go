package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

func startReloadNotifyServer(
	ctx context.Context,
	logger *log.Logger,
	addr string,
) <-chan reloadEvent {
	ch := make(chan reloadEvent, 1)
	addr = strings.TrimSpace(addr)
	if addr == "" {
		if logger != nil {
			logger.Printf("active reload notify endpoint disabled")
		}
		return ch
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if logger != nil {
			logger.Printf("active reload notify endpoint unavailable on %s: %v", addr, err)
		}
		return ch
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !isLoopbackRemote(r.RemoteAddr) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var req struct {
			Source       string   `json:"source"`
			Reason       string   `json:"reason"`
			ChangedPaths []string `json:"changed_paths"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		ev := reloadEvent{ChangedPaths: req.ChangedPaths}
		if len(ev.ChangedPaths) == 0 {
			ev.ChangedPaths = []string{"active_notify"}
		}
		select {
		case ch <- ev:
		default:
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	if logger != nil {
		logger.Printf("active reload notify endpoint: http://%s/reload", addr)
	}

	go func() {
		<-ctx.Done()
		closeCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		_ = srv.Shutdown(closeCtx)
		cancel()
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if logger != nil {
				logger.Printf("active reload notify server stopped: %v", err)
			}
		}
	}()

	return ch
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

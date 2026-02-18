package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"wxbot/internal/configui"
)

func ensureAndOpenConfigUIOnStartup(
	ctx context.Context,
	logger *log.Logger,
	baseDir, botConfigPath, configUIAddr, reloadListen string,
) {
	url := ensureConfigUIReachableOnStartup(ctx, logger, baseDir, botConfigPath, configUIAddr, reloadListen)
	openConfigUIInBrowser(logger, url)
}

func ensureConfigUIReachableOnStartup(
	ctx context.Context,
	logger *log.Logger,
	baseDir, botConfigPath, configUIAddr, reloadListen string,
) string {
	addr := strings.TrimSpace(configUIAddr)
	if addr == "" {
		addr = "127.0.0.1:19090"
	}
	url := configUIURL(addr)
	if !isHTTPReachable(url) {
		if err := startEmbeddedConfigUI(ctx, logger, baseDir, botConfigPath, addr, reloadListen); err != nil {
			if logger != nil {
				logger.Printf("startup ui: start embedded config ui failed: %v", err)
			}
		} else if logger != nil {
			logger.Printf("startup ui: embedded config ui started at %s", url)
		}
	}
	return url
}

func openConfigUIInBrowser(logger *log.Logger, rawURL string) {
	if err := openBrowser(rawURL); err != nil {
		if logger != nil {
			logger.Printf("startup ui: open browser failed: %v", err)
		}
		return
	}
	if logger != nil {
		logger.Printf("startup ui: browser opened: %s", rawURL)
	}
}

func startEmbeddedConfigUI(
	ctx context.Context,
	logger *log.Logger,
	baseDir, botConfigPath, addr, reloadListen string,
) error {
	notifyURL := ""
	rl := strings.TrimSpace(reloadListen)
	if rl != "" {
		if strings.Contains(rl, "://") {
			notifyURL = rl
		} else {
			notifyURL = "http://" + rl + "/reload"
		}
	}

	srv, err := configui.New(baseDir, botConfigPath, notifyURL, "", logger)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUseError(err) {
			return nil
		}
		return err
	}

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		<-ctx.Done()
		closeCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		_ = httpSrv.Shutdown(closeCtx)
		cancel()
	}()
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && logger != nil {
			logger.Printf("embedded config ui stopped: %v", err)
		}
	}()
	return nil
}

func isHTTPReachable(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get(rawURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func openBrowser(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("url is empty")
	}
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	if err := cmd.Start(); err == nil {
		return nil
	}
	cmd = exec.Command("cmd", "/c", "start", "", rawURL)
	return cmd.Start()
}

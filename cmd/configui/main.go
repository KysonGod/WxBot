package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"wxbot/internal/configui"
)

func main() {
	baseDir := flag.String("base-dir", ".", "base directory of WxBot")
	botConfig := flag.String("bot-config", "", "path to bot config json (default: <base-dir>/config.json)")
	addr := flag.String("addr", "127.0.0.1:19090", "http listen address")
	notifyURL := flag.String("notify-url", "http://127.0.0.1:19091/reload", "bot hot-reload notify endpoint (empty to disable)")
	authToken := flag.String("auth-token", "", "api auth token for config ui (recommended when addr is non-loopback)")
	authTokenFile := flag.String("auth-token-file", "", "path to file containing config ui auth token (default: disabled)")
	flag.Parse()

	logger := log.New(os.Stdout, "[WxBot][config_ui] ", log.LstdFlags|log.Lmsgprefix)
	resolvedBaseDir := resolveBaseDirForExe(*baseDir, *botConfig)
	resolvedToken, err := resolveAuthToken(*authToken, *authTokenFile, resolvedBaseDir)
	if err != nil {
		logger.Fatalf("resolve auth token failed: %v", err)
	}

	logger.Printf(
		"using base-dir=%s bot-config=%s notify-url=%s auth=%s",
		resolvedBaseDir,
		strings.TrimSpace(*botConfig),
		strings.TrimSpace(*notifyURL),
		yesNo(resolvedToken != ""),
	)

	srv, err := configui.New(resolvedBaseDir, *botConfig, *notifyURL, resolvedToken, logger)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "config.json") {
			logger.Printf("hint: run with -base-dir pointing to project root or place config.json under base-dir")
		}
		logger.Fatalf("init config ui failed: %v", err)
	}
	logger.Printf("open http://%s in your browser", *addr)
	if err := srv.Start(*addr); err != nil {
		logger.Fatalf("config ui exited: %v", err)
	}
}

func resolveBaseDirForExe(flagBaseDir, flagBotConfig string) string {
	if strings.TrimSpace(flagBotConfig) != "" {
		return flagBaseDir
	}
	trimmed := strings.TrimSpace(flagBaseDir)
	if trimmed != "" && trimmed != "." {
		return flagBaseDir
	}

	candidates := make([]string, 0, 4)
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, exeDir)
		candidates = append(candidates, filepath.Dir(exeDir))
	}

	for _, c := range candidates {
		if looksLikeProjectRoot(c) {
			return c
		}
	}
	return flagBaseDir
}

func looksLikeProjectRoot(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	cfg := filepath.Join(dir, "config.json")
	prompts := filepath.Join(dir, "prompts")
	if _, err := os.Stat(cfg); err != nil {
		return false
	}
	if st, err := os.Stat(prompts); err != nil || !st.IsDir() {
		return false
	}
	return true
}

func resolveAuthToken(tokenRaw, tokenFileRaw, baseDir string) (string, error) {
	token := strings.TrimSpace(tokenRaw)
	tokenFile := strings.TrimSpace(tokenFileRaw)
	if tokenFile == "" {
		return token, nil
	}
	if !filepath.IsAbs(tokenFile) {
		tokenFile = filepath.Join(baseDir, tokenFile)
	}
	b, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file failed: %w", err)
	}
	fileToken := strings.TrimSpace(string(b))
	if fileToken == "" {
		return "", fmt.Errorf("token file is empty: %s", tokenFile)
	}
	if token == "" {
		return fileToken, nil
	}
	return token, nil
}

func yesNo(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

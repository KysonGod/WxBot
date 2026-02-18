package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"wxbot/internal/bootstrap"
	"wxbot/internal/shared/config"
	applog "wxbot/internal/shared/log"
)

func main() {
	pythonExe := flag.String("python", "python", "python executable path")
	baseDir := flag.String("base-dir", ".", "base directory of standalone bot project")
	botConfigPath := flag.String("bot-config", "", "path to standalone bot config json (default: <base-dir>/config.json)")
	configMode := flag.String("config-mode", "auto", "model config source: auto|file|cli")
	setupModels := flag.Bool("setup-models", false, "run interactive model setup wizard and exit")
	reloadListen := flag.String("reload-listen", "127.0.0.1:19091", "local http listen address for active config reload notify (empty to disable)")
	stateDir := flag.String("state", "", "state directory")
	promptsDir := flag.String("prompts", "", "prompts directory")
	showWX := flag.Bool("show", true, "show wechat window")
	flag.Parse()

	logger := applog.New()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resolvedBaseDir := resolveBaseDirForExe(*baseDir, *botConfigPath)
	resolvedBotConfigPath, err := resolveBotConfigPath(resolvedBaseDir, *botConfigPath)
	if err != nil {
		logger.Printf("resolve bot config path failed: %v", err)
		return
	}

	interactive := isInteractiveTerminal()

	if *setupModels {
		if !interactive {
			logger.Printf("model setup wizard requires an interactive terminal")
			return
		}
		if err := runModelSetupWizard(resolvedBotConfigPath, os.Stdin, os.Stdout, logger); err != nil {
			logger.Printf("model setup failed: %v", err)
			return
		}
		logger.Printf("model setup completed, restart bot to apply")
		return
	}

	mode, err := chooseConfigSourceMode(*configMode, interactive, os.Stdin, os.Stdout)
	if err != nil {
		logger.Printf("invalid config mode: %v", err)
		return
	}

	if mode == configModeCLI {
		if !interactive {
			logger.Printf("config-mode=cli requires interactive terminal, fallback to file mode")
			mode = configModeFile
		} else {
			if err := runModelSetupWizard(resolvedBotConfigPath, os.Stdin, os.Stdout, logger); err != nil {
				logger.Printf("model setup failed: %v", err)
				return
			}
		}
	}
	logger.Printf("config mode selected: %s", mode)

	opts := bootstrap.Options{
		PythonExe:     *pythonExe,
		BaseDir:       resolvedBaseDir,
		BotConfigPath: resolvedBotConfigPath,
		StateDir:      *stateDir,
		PromptsDir:    *promptsDir,
		ShowWX:        *showWX,
	}
	pollReloadCh := startConfigReloadWatcher(ctx, logger, resolvedBaseDir, resolvedBotConfigPath, 1*time.Second)
	activeReloadCh := startReloadNotifyServer(ctx, logger, *reloadListen)
	reloadCh := mergeReloadChannels(ctx, pollReloadCh, activeReloadCh)
	logHotReloadWatchedPaths(logger, resolvedBaseDir, resolvedBotConfigPath)

	restartDelay := 2 * time.Second
	const maxRestartDelay = 20 * time.Second

	for {
		if ctx.Err() != nil {
			logger.Printf("bot runtime exited")
			return
		}

		app, err := bootstrap.Build(ctx, opts, logger)
		if err != nil {
			logger.Printf("build app failed: %v", err)
			if strings.Contains(err.Error(), "model config is incomplete") {
				logger.Printf("hint: fill model api_key in config.local.json, use env refs (e.g. env:DEEPSEEK_API_KEY), or run with -setup-models")
				logLocalConfigHint(logger, resolvedBaseDir, opts.BotConfigPath)
			}
			if strings.Contains(err.Error(), "bridge script not found") {
				logger.Printf("hint: run with -base-dir pointing to project root that contains python/wx_bridge.py")
			}
			if strings.Contains(err.Error(), "load bot config failed") {
				logger.Printf("startup aborted due to configuration error")
				if keepRunning, reloadNow := waitRestartSignal(ctx, reloadCh, restartDelay); !keepRunning {
					logger.Printf("bot runtime exited")
					return
				} else if reloadNow {
					logger.Printf("config change detected, retry build now")
					drainReloadEvents(reloadCh)
					restartDelay = 2 * time.Second
					continue
				}
				restartDelay = nextDelay(restartDelay, maxRestartDelay)
				continue
			}
			if keepRunning, reloadNow := waitRestartSignal(ctx, reloadCh, restartDelay); !keepRunning {
				logger.Printf("bot runtime exited")
				return
			} else if reloadNow {
				logger.Printf("config change detected, retry build now")
				drainReloadEvents(reloadCh)
				restartDelay = 2 * time.Second
				continue
			}
			restartDelay = nextDelay(restartDelay, maxRestartDelay)
			continue
		}

		logger.Printf("bot runtime started, listen users=%d", len(app.Config.ListenList))
		restartDelay = 2 * time.Second

		runCtx, cancelRun := context.WithCancel(ctx)
		runDone := make(chan error, 1)
		go func() {
			runDone <- app.Run(runCtx)
		}()

		var (
			runErr           error
			reloadTriggered  bool
			reloadEventPaths string
		)
		select {
		case <-ctx.Done():
			cancelRun()
			runErr = <-runDone
		case ev := <-reloadCh:
			reloadTriggered = true
			reloadEventPaths = strings.Join(ev.ChangedPaths, ", ")
			cancelRun()
			runErr = <-runDone
		case runErr = <-runDone:
		}

		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		app.Close(closeCtx)
		cancel()
		cancelRun()

		if errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
			logger.Printf("bot runtime exited")
			return
		}
		if reloadTriggered {
			if strings.TrimSpace(reloadEventPaths) == "" {
				logger.Printf("config change detected, reloading bot runtime")
			} else {
				logger.Printf("config change detected (%s), reloading bot runtime", reloadEventPaths)
			}
			drainReloadEvents(reloadCh)
			restartDelay = 2 * time.Second
			continue
		}

		logger.Printf("bot runtime stopped with error: %v", runErr)
		if keepRunning, reloadNow := waitRestartSignal(ctx, reloadCh, restartDelay); !keepRunning {
			logger.Printf("bot runtime exited")
			return
		} else if reloadNow {
			logger.Printf("config change detected, retry build now")
			drainReloadEvents(reloadCh)
			restartDelay = 2 * time.Second
			continue
		}
		restartDelay = nextDelay(restartDelay, maxRestartDelay)
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextDelay(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	if next <= 0 {
		return current
	}
	return next
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
	bridge := filepath.Join(dir, "python", "wx_bridge.py")
	if _, err := os.Stat(cfg); err != nil {
		return false
	}
	if _, err := os.Stat(bridge); err != nil {
		return false
	}
	return true
}

func logLocalConfigHint(logger interface{ Printf(string, ...any) }, baseDir, botConfigPath string) {
	cfgPath := strings.TrimSpace(botConfigPath)
	if cfgPath == "" {
		cfgPath = filepath.Join(baseDir, "config.json")
	} else if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(baseDir, cfgPath)
	}
	cfgPathAbs, err := filepath.Abs(cfgPath)
	if err != nil {
		return
	}
	localPath, err := config.ResolveLocalOverridePath(cfgPathAbs)
	if err != nil || strings.TrimSpace(localPath) == "" {
		return
	}
	if _, statErr := os.Stat(localPath); statErr == nil {
		logger.Printf("hint: local override exists at %s (check llm.api_key/base_url/model)", localPath)
		return
	} else if os.IsNotExist(statErr) {
		logger.Printf("hint: local override not found, create %s (copy from config.local.json.example)", localPath)
	}
}

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wxbot/internal/shared/config"
)

type reloadEvent struct {
	ChangedPaths []string
}

type fileSnapshot struct {
	Exists  bool
	Size    int64
	ModUnix int64
}

func startConfigReloadWatcher(
	ctx context.Context,
	logger interface{ Printf(string, ...any) },
	baseDir string,
	botConfigPath string,
	pollInterval time.Duration,
) <-chan reloadEvent {
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}

	eventCh := make(chan reloadEvent, 1)
	paths := collectReloadWatchPaths(logger, botConfigPath)
	if len(paths) == 0 {
		if logger != nil {
			logger.Printf("config hot-reload disabled: no valid config path")
		}
		return eventCh
	}

	snapshots := make(map[string]fileSnapshot, len(paths))
	for _, p := range paths {
		snapshots[p] = statSnapshot(p)
	}

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				changed := make([]string, 0, len(paths))
				for _, p := range paths {
					next := statSnapshot(p)
					prev := snapshots[p]
					if next != prev {
						changed = append(changed, shortPath(baseDir, p))
						snapshots[p] = next
					}
				}
				if len(changed) == 0 {
					continue
				}
				ev := reloadEvent{ChangedPaths: changed}
				select {
				case eventCh <- ev:
				default:
				}
			}
		}
	}()

	return eventCh
}

func collectReloadWatchPaths(logger interface{ Printf(string, ...any) }, botConfigPath string) []string {
	paths := make([]string, 0, 2)
	cfg := strings.TrimSpace(botConfigPath)
	if cfg != "" {
		if abs, err := filepath.Abs(cfg); err == nil {
			paths = append(paths, abs)
		}
	}
	if localPath, err := config.ResolveLocalOverridePath(botConfigPath); err == nil {
		localPath = strings.TrimSpace(localPath)
		if localPath != "" {
			if abs, err := filepath.Abs(localPath); err == nil {
				paths = append(paths, abs)
			}
		}
	} else if logger != nil {
		logger.Printf("resolve local config path failed for hot-reload: %v", err)
	}
	return uniqueStrings(paths)
}

func statSnapshot(path string) fileSnapshot {
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{Exists: false}
	}
	return fileSnapshot{
		Exists:  true,
		Size:    info.Size(),
		ModUnix: info.ModTime().UnixNano(),
	}
}

func shortPath(baseDir, p string) string {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return p
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return p
	}
	pathAbs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil {
		return p
	}
	if strings.HasPrefix(rel, "..") {
		return pathAbs
	}
	return rel
}

func uniqueStrings(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func waitRestartSignal(
	ctx context.Context,
	reloadCh <-chan reloadEvent,
	delay time.Duration,
) (continueRun bool, reloadNow bool) {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false, false
		case <-reloadCh:
			return true, true
		default:
			return true, false
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, false
	case <-reloadCh:
		return true, true
	case <-timer.C:
		return true, false
	}
}

func drainReloadEvents(ch <-chan reloadEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func logHotReloadWatchedPaths(logger *log.Logger, baseDir, botConfigPath string) {
	if logger == nil {
		return
	}
	paths := collectReloadWatchPaths(logger, botConfigPath)
	if len(paths) == 0 {
		return
	}
	pretty := make([]string, 0, len(paths))
	for _, p := range paths {
		pretty = append(pretty, shortPath(baseDir, p))
	}
	logger.Printf("config hot-reload enabled: %s", strings.Join(pretty, ", "))
}

func mergeReloadChannels(ctx context.Context, sources ...<-chan reloadEvent) <-chan reloadEvent {
	out := make(chan reloadEvent, 1)
	for _, src := range sources {
		if src == nil {
			continue
		}
		go func(ch <-chan reloadEvent) {
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-ch:
					if !ok {
						return
					}
					select {
					case out <- ev:
					default:
					}
				}
			}
		}(src)
	}
	return out
}

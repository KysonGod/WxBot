package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	chatapp "wxbot/internal/contexts/chat/application"
	chatinfra "wxbot/internal/contexts/chat/infrastructure"
	cmdapp "wxbot/internal/contexts/command/application"
	"wxbot/internal/integration/llm"
	"wxbot/internal/integration/wxbridge"
	"wxbot/internal/shared/clock"
	"wxbot/internal/shared/config"
)

type Options struct {
	PythonExe     string
	BaseDir       string
	BotConfigPath string
	ShowWX        bool
	StateDir      string
	PromptsDir    string
}

type App struct {
	Bridge   *wxbridge.Client
	Chat     *chatapp.Service
	Config   config.AppConfig
	Logger   *log.Logger
	Robot    string
	stateDir string
}

func Build(ctx context.Context, opts Options, logger *log.Logger) (*App, error) {
	if logger == nil {
		logger = log.Default()
	}

	baseDir, botConfigPath, err := resolveBaseDirAndConfig(opts.BaseDir, opts.BotConfigPath)
	if err != nil {
		return nil, err
	}
	stateDir := opts.StateDir
	if strings.TrimSpace(stateDir) == "" {
		stateDir = filepath.Join(baseDir, "state")
	}
	if !filepath.IsAbs(stateDir) {
		stateDir = filepath.Join(baseDir, stateDir)
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve state dir failed: %w", err)
	}

	promptsDir := opts.PromptsDir
	if strings.TrimSpace(promptsDir) == "" {
		promptsDir = filepath.Join(baseDir, "prompts")
	}
	if !filepath.IsAbs(promptsDir) {
		promptsDir = filepath.Join(baseDir, promptsDir)
	}
	promptsDir, err = filepath.Abs(promptsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve prompts dir failed: %w", err)
	}

	bridgeScript := filepath.Join(baseDir, "python", "wx_bridge.py")
	if _, err := os.Stat(bridgeScript); err != nil {
		return nil, fmt.Errorf("bridge script not found: %s", bridgeScript)
	}

	pythonExe := opts.PythonExe
	if strings.TrimSpace(pythonExe) == "" {
		pythonExe = "python"
	}

	cfg, err := config.LoadStandaloneConfig(botConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load bot config failed: %w", err)
	}
	logger.Printf("loaded bot config from %s", botConfigPath)

	if strings.TrimSpace(cfg.Emoji.Dir) != "" {
		if !filepath.IsAbs(cfg.Emoji.Dir) {
			cfg.Emoji.Dir = filepath.Join(baseDir, cfg.Emoji.Dir)
		}
		cfg.Emoji.Dir, err = filepath.Abs(cfg.Emoji.Dir)
		if err != nil {
			return nil, fmt.Errorf("resolve emoji dir failed: %w", err)
		}
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir failed: %w", err)
	}
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create prompts dir failed: %w", err)
	}
	if cfg.Emoji.Enabled && strings.TrimSpace(cfg.Emoji.Dir) != "" {
		if err := os.MkdirAll(cfg.Emoji.Dir, 0o755); err != nil {
			return nil, fmt.Errorf("create emoji dir failed: %w", err)
		}
	}

	bridgeCtx, cancelBridge := context.WithCancel(ctx)
	_ = cancelBridge
	bc, err := wxbridge.Start(bridgeCtx, logger, pythonExe, bridgeScript)
	if err != nil {
		return nil, fmt.Errorf("start wx bridge failed: %w", err)
	}

	initCtx, cancelInit := context.WithTimeout(ctx, 120*time.Second)
	initResult, err := bc.Init(initCtx, opts.ShowWX)
	cancelInit()
	if err != nil {
		_ = bc.Close()
		return nil, fmt.Errorf("wx init failed: %w", err)
	}
	logger.Printf("wx initialized, nickname=%s", initResult.Nickname)

	mapping := make(map[string]string, len(cfg.ListenList))
	for _, row := range cfg.ListenList {
		mapping[row.Nickname] = row.Prompt
		promptFile := filepath.Join(promptsDir, row.Prompt+".md")
		if _, err := os.Stat(promptFile); err != nil {
			_ = bc.Close()
			return nil, fmt.Errorf("prompt file not found for %s: %s", row.Nickname, promptFile)
		}
	}

	contextRepo, err := chatinfra.NewJSONContextRepository(filepath.Join(stateDir, "chat_contexts.json"))
	if err != nil {
		_ = bc.Close()
		return nil, fmt.Errorf("create context repo failed: %w", err)
	}
	promptRepo := chatinfra.NewFSPromptRepository(promptsDir, mapping)
	mainLLMClient := llm.NewOpenAICompatClient(cfg.MainModel)
	assistantLLMClient := llm.NewOpenAICompatClient(cfg.AssistantModel)
	onlineLLMClient := llm.NewOpenAICompatClient(cfg.OnlineModel)
	var visionLLMClient chatapp.VisionPort
	if cfg.Vision.Enabled {
		visionLLMClient = llm.NewVisionClient(cfg.VisionModel)
	}

	commandHandler := &cmdapp.Handler{Cleaner: contextRepo}
	chatSvc := chatapp.NewService(
		cfg,
		initResult.Nickname,
		&wechatAdapter{client: bc},
		mainLLMClient,
		assistantLLMClient,
		onlineLLMClient,
		visionLLMClient,
		contextRepo,
		promptRepo,
		commandHandler,
		clock.RealClock{},
		logger,
	)

	return &App{
		Bridge:   bc,
		Chat:     chatSvc,
		Config:   cfg,
		Logger:   logger,
		Robot:    initResult.Nickname,
		stateDir: stateDir,
	}, nil
}

func resolveBaseDirAndConfig(baseDir, botConfigPath string) (string, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(baseDir) == "" {
		if strings.TrimSpace(botConfigPath) != "" {
			if !filepath.IsAbs(botConfigPath) {
				botConfigPath = filepath.Join(wd, botConfigPath)
			}
			botConfigPath, err = filepath.Abs(botConfigPath)
			if err != nil {
				return "", "", err
			}
			return filepath.Dir(botConfigPath), botConfigPath, nil
		}
		baseDir = wd
	}
	if !filepath.IsAbs(baseDir) {
		baseDir = filepath.Join(wd, baseDir)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(botConfigPath) == "" {
		botConfigPath = filepath.Join(baseDir, "config.json")
	} else if !filepath.IsAbs(botConfigPath) {
		botConfigPath = filepath.Join(baseDir, botConfigPath)
	}
	botConfigPath, err = filepath.Abs(botConfigPath)
	if err != nil {
		return "", "", err
	}
	return baseDir, botConfigPath, nil
}

type wechatAdapter struct {
	client *wxbridge.Client
}

func (a *wechatAdapter) SendText(ctx context.Context, who, text string) error {
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return a.client.SendMsg(callCtx, who, text)
}

func (a *wechatAdapter) SendFile(ctx context.Context, who, path string) error {
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return a.client.SendFiles(callCtx, who, path)
}

func (a *wechatAdapter) MessageDownload(ctx context.Context, eventID string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	return a.client.MessageDownload(callCtx, eventID)
}

func (a *wechatAdapter) MessageCapture(ctx context.Context, eventID string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	return a.client.MessageCapture(callCtx, eventID)
}

func (a *wechatAdapter) MessageToText(ctx context.Context, eventID string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return a.client.MessageToText(callCtx, eventID)
}

func (a *wechatAdapter) MessageGetURL(ctx context.Context, eventID string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return a.client.MessageGetURL(callCtx, eventID)
}

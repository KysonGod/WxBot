package configui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wxbot/internal/shared/config"
)

//go:embed web/index.html
var webFS embed.FS

type Server struct {
	baseDir       string
	botConfigPath string
	localCfgPath  string
	promptsDir    string
	notifyURL     string
	authToken     string
	httpClient    *http.Client
	logger        *log.Logger
	mux           *http.ServeMux
}

type modelUI struct {
	Provider       string  `json:"provider"`
	BaseURL        string  `json:"base_url"`
	APIKey         string  `json:"api_key"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	MaxTokens      int     `json:"max_tokens"`
	TimeoutSeconds int     `json:"timeout_seconds"`
}

type groupPolicyUI struct {
	AcceptAll           bool     `json:"accept_all"`
	EnableAtReply       bool     `json:"enable_at_reply"`
	EnableKeywordReply  bool     `json:"enable_keyword_reply"`
	KeywordList         []string `json:"keyword_list"`
	ResponseProbability int      `json:"response_probability"`
}

type emojiUI struct {
	Enabled         bool   `json:"enabled"`
	Dir             string `json:"dir"`
	SendProbability int    `json:"send_probability"`
}

type visionUI struct {
	Enabled     bool   `json:"enabled"`
	ImagePrompt string `json:"image_prompt"`
	EmojiPrompt string `json:"emoji_prompt"`
}

type onlineUI struct {
	Enabled         bool   `json:"enabled"`
	DetectionPrompt string `json:"detection_prompt"`
	FixedPrompt     string `json:"fixed_prompt"`
}

type runtimeConfigUI struct {
	Port                   int                  `json:"port"`
	QueueWaitingTime       int                  `json:"queue_waiting_time"`
	QueueMaxMessages       int                  `json:"queue_max_messages_per_user"`
	QueueWorkerConcurrency int                  `json:"queue_worker_concurrency"`
	MaxGroups              int                  `json:"max_groups"`
	EnableTextCommands     bool                 `json:"enable_text_commands"`
	ListenList             []config.ListenEntry `json:"listen_list"`
	GroupPolicy            groupPolicyUI        `json:"group_policy"`
	LLM                    modelUI              `json:"llm"`
	AssistantLLM           modelUI              `json:"assistant_llm"`
	VisionLLM              modelUI              `json:"vision_llm"`
	OnlineLLM              modelUI              `json:"online_llm"`
	Emoji                  emojiUI              `json:"emoji"`
	Vision                 visionUI             `json:"vision"`
	Online                 onlineUI             `json:"online"`
}

type stateResponse struct {
	BotConfigPath   string          `json:"bot_config_path"`
	LocalConfigPath string          `json:"local_config_path"`
	PromptsDir      string          `json:"prompts_dir"`
	PromptFiles     []string        `json:"prompt_files"`
	Config          runtimeConfigUI `json:"config"`
}

func New(baseDir, botConfigPath, notifyURL, authToken string, logger *log.Logger) (*Server, error) {
	if logger == nil {
		logger = log.Default()
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(baseDir) == "" {
		baseDir = wd
	}
	if !filepath.IsAbs(baseDir) {
		baseDir = filepath.Join(wd, baseDir)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(botConfigPath) == "" {
		botConfigPath = filepath.Join(baseDir, "config.json")
	} else if !filepath.IsAbs(botConfigPath) {
		botConfigPath = filepath.Join(baseDir, botConfigPath)
	}
	botConfigPath, err = filepath.Abs(botConfigPath)
	if err != nil {
		return nil, err
	}

	localCfgPath, err := config.ResolveLocalOverridePath(botConfigPath)
	if err != nil {
		return nil, err
	}
	promptsDir := filepath.Join(baseDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create prompts dir failed: %w", err)
	}

	s := &Server{
		baseDir:       baseDir,
		botConfigPath: botConfigPath,
		localCfgPath:  localCfgPath,
		promptsDir:    promptsDir,
		notifyURL:     normalizeNotifyURL(notifyURL),
		authToken:     normalizeAuthToken(authToken),
		httpClient:    &http.Client{Timeout: 3 * time.Second},
		logger:        logger,
		mux:           http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/state", s.withAuth(s.handleState))
	s.mux.HandleFunc("/api/config", s.withAuth(s.handleConfig))
	s.mux.HandleFunc("/api/prompts", s.withAuth(s.handlePrompts))
	s.mux.HandleFunc("/api/prompts/{name}", s.withAuth(s.handlePromptByName))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "read web template failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg, err := s.loadRuntimeUIConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	prompts, err := s.listPromptFiles()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := stateResponse{
		BotConfigPath:   s.botConfigPath,
		LocalConfigPath: s.localCfgPath,
		PromptsDir:      s.promptsDir,
		PromptFiles:     prompts,
		Config:          cfg,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.loadRuntimeUIConfig()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var payload runtimeConfigUI
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid json payload")
			return
		}
		if err := s.validateRuntimeUIConfig(payload); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.saveRuntimeUIConfig(payload); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp := map[string]any{
			"ok":      true,
			"message": "配置已保存",
		}
		notifyErr := s.notifyBotReload(r.Context(), []string{"config.json", "config.local.json"})
		if notifyErr != nil {
			resp["notify_ok"] = false
			resp["notify_error"] = notifyErr.Error()
			resp["message"] = "配置已保存，但未通知到 bot，请确认 bot 正在运行（轮询兜底仍会触发）"
		} else if strings.TrimSpace(s.notifyURL) != "" {
			resp["notify_ok"] = true
			resp["message"] = "配置已保存，已通知 bot 热重载"
		}
		s.writeJSON(w, http.StatusOK, resp)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePrompts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		files, err := s.listPromptFiles()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{
			"files": files,
		})
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Template string `json:"template"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid json payload")
			return
		}
		content, err := s.templateContent(req.Template)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		path, err := s.promptPath(req.Name)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := os.Stat(path); err == nil {
			s.writeError(w, http.StatusConflict, "prompt file already exists")
			return
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			s.writeError(w, http.StatusInternalServerError, "write prompt failed")
			return
		}
		s.writeJSON(w, http.StatusCreated, map[string]any{
			"ok":      true,
			"name":    strings.TrimSuffix(filepath.Base(path), ".md"),
			"message": "Prompt已创建",
		})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePromptByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	path, err := s.promptPath(name)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				s.writeError(w, http.StatusNotFound, "prompt not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, "read prompt failed")
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{
			"name":    strings.TrimSuffix(filepath.Base(path), ".md"),
			"content": string(stripUTF8BOM(b)),
		})
	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid json payload")
			return
		}
		content := strings.ReplaceAll(req.Content, "\r\n", "\n")
		if strings.TrimSpace(content) == "" {
			s.writeError(w, http.StatusBadRequest, "prompt content is empty")
			return
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			s.writeError(w, http.StatusInternalServerError, "write prompt failed")
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Prompt已保存",
		})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) loadRuntimeUIConfig() (runtimeConfigUI, error) {
	baseCfg, err := config.LoadLocalRuntimeConfig(s.botConfigPath)
	if err != nil {
		return runtimeConfigUI{}, fmt.Errorf("load base config failed: %w", err)
	}
	localCfg := config.LocalRuntimeConfig{}
	if _, err := os.Stat(s.localCfgPath); err == nil {
		localCfg, err = config.LoadLocalRuntimeConfig(s.localCfgPath)
		if err != nil {
			return runtimeConfigUI{}, fmt.Errorf("load local config failed: %w", err)
		}
	}
	return mergeUIConfig(baseCfg, localCfg), nil
}

func mergeUIConfig(base, local config.LocalRuntimeConfig) runtimeConfigUI {
	def := config.DefaultAppConfig()
	out := runtimeConfigUI{
		Port:                   choosePositive(local.Port, choosePositive(base.Port, def.Port)),
		QueueWaitingTime:       choosePositive(local.QueueWaitingTime, choosePositive(base.QueueWaitingTime, def.QueueWaitingTime)),
		QueueMaxMessages:       choosePositive(local.QueueMaxMessages, choosePositive(base.QueueMaxMessages, def.QueueMaxMessages)),
		QueueWorkerConcurrency: choosePositive(local.QueueWorkers, choosePositive(base.QueueWorkers, def.QueueWorkers)),
		MaxGroups:              choosePositive(local.MaxGroups, choosePositive(base.MaxGroups, def.MaxGroups)),
		EnableTextCommands:     chooseBool(coalesceBoolPtr(local.EnableTextCommands, base.EnableTextCommands), boolPtr(def.EnableTextCommands)),
		ListenList:             chooseListenList(local.ListenList, chooseListenList(base.ListenList, nil)),
		GroupPolicy: groupPolicyUI{
			AcceptAll:           chooseBool(coalesceBoolPtr(local.GroupPolicy.AcceptAll, base.GroupPolicy.AcceptAll), boolPtr(def.GroupPolicy.AcceptAll)),
			EnableAtReply:       chooseBool(coalesceBoolPtr(local.GroupPolicy.EnableAtReply, base.GroupPolicy.EnableAtReply), boolPtr(def.GroupPolicy.EnableAtReply)),
			EnableKeywordReply:  chooseBool(coalesceBoolPtr(local.GroupPolicy.EnableKeywordReply, base.GroupPolicy.EnableKeywordReply), boolPtr(def.GroupPolicy.EnableKeywordReply)),
			KeywordList:         chooseKeywords(local.GroupPolicy.KeywordList, chooseKeywords(base.GroupPolicy.KeywordList, def.GroupPolicy.KeywordList)),
			ResponseProbability: choosePositive(local.GroupPolicy.ResponseProbability, choosePositive(base.GroupPolicy.ResponseProbability, def.GroupPolicy.ResponseProbability)),
		},
		LLM:          mergeModelUI(base.LLM, local.LLM, modelUI{Temperature: 1.0, MaxTokens: 2000, TimeoutSeconds: 45}),
		AssistantLLM: mergeModelUI(base.AssistantLLM, local.AssistantLLM, modelUI{Temperature: 0.7, MaxTokens: 1200, TimeoutSeconds: 45}),
		VisionLLM:    mergeModelUI(base.VisionLLM, local.VisionLLM, modelUI{Temperature: 0.2, MaxTokens: 500, TimeoutSeconds: 45}),
		OnlineLLM:    mergeModelUI(base.OnlineLLM, local.OnlineLLM, modelUI{Temperature: 0.7, MaxTokens: 1500, TimeoutSeconds: 45}),
		Emoji: emojiUI{
			Enabled:         chooseBool(coalesceBoolPtr(local.Emoji.Enabled, base.Emoji.Enabled), boolPtr(def.Emoji.Enabled)),
			Dir:             chooseText(local.Emoji.Dir, chooseText(base.Emoji.Dir, def.Emoji.Dir)),
			SendProbability: choosePositive(local.Emoji.SendProbability, choosePositive(base.Emoji.SendProbability, def.Emoji.SendProbability)),
		},
		Vision: visionUI{
			Enabled:     chooseBool(coalesceBoolPtr(local.Vision.Enabled, base.Vision.Enabled), boolPtr(def.Vision.Enabled)),
			ImagePrompt: chooseText(local.Vision.ImagePrompt, chooseText(base.Vision.ImagePrompt, def.Vision.ImagePrompt)),
			EmojiPrompt: chooseText(local.Vision.EmojiPrompt, chooseText(base.Vision.EmojiPrompt, def.Vision.EmojiPrompt)),
		},
		Online: onlineUI{
			Enabled:         chooseBool(coalesceBoolPtr(local.Online.Enabled, base.Online.Enabled), boolPtr(def.Online.Enabled)),
			DetectionPrompt: chooseText(local.Online.DetectionPrompt, chooseText(base.Online.DetectionPrompt, def.Online.DetectionPrompt)),
			FixedPrompt:     chooseText(local.Online.FixedPrompt, chooseText(base.Online.FixedPrompt, def.Online.FixedPrompt)),
		},
	}
	if out.Port <= 0 {
		out.Port = 5000
	}
	return out
}

func mergeModelUI(base, local config.LLMOverride, defaults modelUI) modelUI {
	out := modelUI{
		Provider:       chooseText(local.Provider, chooseText(base.Provider, defaults.Provider)),
		BaseURL:        chooseText(local.BaseURL, chooseText(base.BaseURL, defaults.BaseURL)),
		APIKey:         chooseText(local.APIKey, chooseText(base.APIKey, defaults.APIKey)),
		Model:          chooseText(local.Model, chooseText(base.Model, defaults.Model)),
		Temperature:    chooseFloat(local.Temperature, chooseFloat(base.Temperature, defaults.Temperature)),
		MaxTokens:      choosePositive(local.MaxTokens, choosePositive(base.MaxTokens, defaults.MaxTokens)),
		TimeoutSeconds: choosePositive(local.TimeoutSeconds, choosePositive(base.TimeoutSeconds, defaults.TimeoutSeconds)),
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = 45
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = 2000
	}
	if out.Temperature == 0 {
		out.Temperature = 1.0
	}
	return out
}

func (s *Server) validateRuntimeUIConfig(cfg runtimeConfigUI) error {
	if len(cfg.ListenList) == 0 {
		return errors.New("listen_list 不能为空，请至少配置一个聊天对象")
	}
	for _, row := range cfg.ListenList {
		n := strings.TrimSpace(row.Nickname)
		p := strings.TrimSpace(row.Prompt)
		if n == "" || p == "" {
			return errors.New("listen_list 中存在空的 nickname 或 prompt")
		}
		if _, err := s.promptPath(p); err != nil {
			return fmt.Errorf("prompt 名称非法: %s", p)
		}
	}

	if !isModelComplete(cfg.LLM) {
		return errors.New("主模型配置不完整（llm.base_url/api_key/model）")
	}
	if cfg.Vision.Enabled && !isModelComplete(cfg.VisionLLM) {
		return errors.New("视觉功能已启用，但视觉模型配置不完整（vision_llm.base_url/api_key/model）")
	}
	if cfg.Online.Enabled && !isModelComplete(cfg.OnlineLLM) {
		return errors.New("联网功能已启用，但联网模型配置不完整（online_llm.base_url/api_key/model）")
	}
	if strings.TrimSpace(cfg.Emoji.Dir) == "" {
		return errors.New("emoji.dir 不能为空")
	}

	return nil
}

func isModelComplete(m modelUI) bool {
	return strings.TrimSpace(m.BaseURL) != "" &&
		strings.TrimSpace(m.APIKey) != "" &&
		strings.TrimSpace(m.Model) != ""
}

func (s *Server) saveRuntimeUIConfig(cfg runtimeConfigUI) error {
	baseRaw, err := readJSONMap(s.botConfigPath)
	if err != nil {
		return fmt.Errorf("load base config map failed: %w", err)
	}
	localRaw, err := readJSONMap(s.localCfgPath)
	if err != nil {
		return fmt.Errorf("load local config map failed: %w", err)
	}

	setNumber(baseRaw, "port", cfg.Port)
	setNumber(baseRaw, "queue_waiting_time", cfg.QueueWaitingTime)
	setNumber(baseRaw, "queue_max_messages_per_user", cfg.QueueMaxMessages)
	setNumber(baseRaw, "queue_worker_concurrency", cfg.QueueWorkerConcurrency)
	setNumber(baseRaw, "max_groups", cfg.MaxGroups)
	setBool(baseRaw, "enable_text_commands", cfg.EnableTextCommands)
	setListenList(baseRaw, "listen_list", cfg.ListenList)

	setGroupPolicy(baseRaw, cfg.GroupPolicy)
	setModel(baseRaw, "llm", cfg.LLM, false)
	setModel(baseRaw, "assistant_llm", cfg.AssistantLLM, false)
	setModel(baseRaw, "vision_llm", cfg.VisionLLM, false)
	setModel(baseRaw, "online_llm", cfg.OnlineLLM, false)
	setEmoji(baseRaw, cfg.Emoji)
	setVision(baseRaw, cfg.Vision)
	setOnline(baseRaw, cfg.Online)

	setModel(localRaw, "llm", modelUI{APIKey: cfg.LLM.APIKey}, true)
	setModel(localRaw, "assistant_llm", modelUI{APIKey: cfg.AssistantLLM.APIKey}, true)
	setModel(localRaw, "vision_llm", modelUI{APIKey: cfg.VisionLLM.APIKey}, true)
	setModel(localRaw, "online_llm", modelUI{APIKey: cfg.OnlineLLM.APIKey}, true)

	originBase, _ := os.ReadFile(s.botConfigPath)
	originLocal, _ := os.ReadFile(s.localCfgPath)

	if err := writeJSONMap(s.botConfigPath, baseRaw, 0o644); err != nil {
		return err
	}
	if err := writeJSONMap(s.localCfgPath, localRaw, 0o600); err != nil {
		_ = os.WriteFile(s.botConfigPath, originBase, 0o644)
		return err
	}
	if _, err := config.LoadStandaloneConfig(s.botConfigPath); err != nil {
		_ = os.WriteFile(s.botConfigPath, originBase, 0o644)
		if len(originLocal) > 0 {
			_ = os.WriteFile(s.localCfgPath, originLocal, 0o600)
		}
		return fmt.Errorf("配置验证失败，已回滚: %w", err)
	}
	return nil
}

func (s *Server) notifyBotReload(ctx context.Context, changedPaths []string) error {
	if strings.TrimSpace(s.notifyURL) == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"source":        "configui",
		"reason":        "config_saved",
		"changed_paths": changedPaths,
		"at":            time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.notifyURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("notify status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func setGroupPolicy(root map[string]any, v groupPolicyUI) {
	m := ensureSection(root, "group_policy")
	m["accept_all"] = v.AcceptAll
	m["enable_at_reply"] = v.EnableAtReply
	m["enable_keyword_reply"] = v.EnableKeywordReply
	m["keyword_list"] = v.KeywordList
	m["response_probability"] = v.ResponseProbability
}

func setEmoji(root map[string]any, v emojiUI) {
	m := ensureSection(root, "emoji")
	m["enabled"] = v.Enabled
	m["dir"] = strings.TrimSpace(v.Dir)
	m["send_probability"] = v.SendProbability
}

func setVision(root map[string]any, v visionUI) {
	m := ensureSection(root, "vision")
	m["enabled"] = v.Enabled
	m["image_prompt"] = strings.TrimSpace(v.ImagePrompt)
	m["emoji_prompt"] = strings.TrimSpace(v.EmojiPrompt)
}

func setOnline(root map[string]any, v onlineUI) {
	m := ensureSection(root, "online")
	m["enabled"] = v.Enabled
	m["detection_prompt"] = strings.TrimSpace(v.DetectionPrompt)
	m["fixed_prompt"] = strings.TrimSpace(v.FixedPrompt)
}

func setModel(root map[string]any, key string, m modelUI, keyOnly bool) {
	section := ensureSection(root, key)
	if keyOnly {
		if strings.TrimSpace(m.APIKey) == "" {
			delete(section, "api_key")
		} else {
			section["api_key"] = strings.TrimSpace(m.APIKey)
		}
		return
	}
	section["provider"] = strings.TrimSpace(m.Provider)
	section["base_url"] = strings.TrimSpace(m.BaseURL)
	section["api_key"] = ""
	section["model"] = strings.TrimSpace(m.Model)
	section["temperature"] = m.Temperature
	section["max_tokens"] = m.MaxTokens
	section["timeout_seconds"] = m.TimeoutSeconds
}

func setNumber(root map[string]any, key string, value int) {
	root[key] = value
}

func setBool(root map[string]any, key string, value bool) {
	root[key] = value
}

func setListenList(root map[string]any, key string, list []config.ListenEntry) {
	out := make([]map[string]any, 0, len(list))
	for _, row := range list {
		nick := strings.TrimSpace(row.Nickname)
		prompt := strings.TrimSpace(row.Prompt)
		if nick == "" || prompt == "" {
			continue
		}
		out = append(out, map[string]any{
			"nickname": nick,
			"prompt":   prompt,
		})
	}
	root[key] = out
}

func ensureSection(root map[string]any, key string) map[string]any {
	if raw, ok := root[key]; ok {
		if m, ok := raw.(map[string]any); ok {
			return m
		}
	}
	m := make(map[string]any)
	root[key] = m
	return m
}

func (s *Server) listPromptFiles() ([]string, error) {
	entries, err := os.ReadDir(s.promptsDir)
	if err != nil {
		return nil, fmt.Errorf("read prompts dir failed: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			out = append(out, strings.TrimSuffix(name, filepath.Ext(name)))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Server) templateContent(name string) (string, error) {
	template := strings.TrimSpace(name)
	if template == "" || strings.EqualFold(template, "blank") {
		return "# 新提示词\n\n", nil
	}
	path, err := s.promptPath(template)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("template not found: %s", template)
	}
	content := string(stripUTF8BOM(b))
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content, nil
}

func (s *Server) promptPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".md")
	if name == "" {
		return "", errors.New("prompt name is empty")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", errors.New("invalid prompt name")
	}
	for _, r := range name {
		if r < 32 {
			return "", errors.New("invalid prompt name")
		}
	}
	target := filepath.Join(s.promptsDir, name+".md")
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	baseAbs, err := filepath.Abs(s.promptsDir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, baseAbs+string(os.PathSeparator)) && abs != baseAbs {
		return "", errors.New("invalid prompt path")
	}
	return abs, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": message,
	})
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthorized(r) {
			s.writeError(w, http.StatusUnauthorized, "未授权：缺少或错误的访问令牌")
			return
		}
		next(w, r)
	}
}

func (s *Server) isAuthorized(r *http.Request) bool {
	required := strings.TrimSpace(s.authToken)
	if required == "" {
		return true
	}
	token := strings.TrimSpace(r.Header.Get("X-Bot-Token"))
	if token == "" {
		token = extractBearerToken(r.Header.Get("Authorization"))
	}
	return token == required
}

func extractBearerToken(header string) string {
	h := strings.TrimSpace(header)
	if len(h) < len("Bearer ")+1 {
		return ""
	}
	if !strings.EqualFold(h[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func readJSONMap(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read file failed: %w", err)
	}
	b = stripUTF8BOM(b)
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode json failed: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func writeJSONMap(path string, data map[string]any, perm os.FileMode) error {
	if data == nil {
		data = map[string]any{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir failed: %w", err)
	}
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json failed: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, perm); err != nil {
		return fmt.Errorf("write file failed: %w", err)
	}
	return nil
}

func stripUTF8BOM(b []byte) []byte {
	utf8BOM := []byte{0xEF, 0xBB, 0xBF}
	if len(b) >= 3 && bytes.Equal(b[:3], utf8BOM) {
		return b[3:]
	}
	return b
}

func normalizeNotifyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.TrimSpace(u.Path) == "" || u.Path == "/" {
		u.Path = "/reload"
	}
	return u.String()
}

func normalizeAuthToken(raw string) string {
	return strings.TrimSpace(raw)
}

func boolPtr(v bool) *bool {
	return &v
}

func coalesceBoolPtr(a, b *bool) *bool {
	if a != nil {
		return a
	}
	return b
}

func choosePositive(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func chooseBool(v, fallback *bool) bool {
	if v != nil {
		return *v
	}
	if fallback != nil {
		return *fallback
	}
	return false
}

func chooseFloat(v, fallback float64) float64 {
	if v != 0 {
		return v
	}
	return fallback
}

func chooseText(v, fallback string) string {
	t := strings.TrimSpace(v)
	if t != "" {
		return t
	}
	return strings.TrimSpace(fallback)
}

func chooseListenList(v, fallback []config.ListenEntry) []config.ListenEntry {
	if len(v) > 0 {
		out := make([]config.ListenEntry, 0, len(v))
		for _, row := range v {
			nick := strings.TrimSpace(row.Nickname)
			prompt := strings.TrimSpace(row.Prompt)
			if nick == "" || prompt == "" {
				continue
			}
			out = append(out, config.ListenEntry{Nickname: nick, Prompt: prompt})
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]config.ListenEntry, 0, len(fallback))
	for _, row := range fallback {
		nick := strings.TrimSpace(row.Nickname)
		prompt := strings.TrimSpace(row.Prompt)
		if nick == "" || prompt == "" {
			continue
		}
		out = append(out, config.ListenEntry{Nickname: nick, Prompt: prompt})
	}
	return out
}

func chooseKeywords(v, fallback []string) []string {
	if len(v) > 0 {
		out := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]string, 0, len(fallback))
	for _, item := range fallback {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Start(addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:19090"
	}
	if !isLoopbackListenAddr(addr) && strings.TrimSpace(s.authToken) == "" {
		return fmt.Errorf("refuse to listen on non-loopback addr without auth token: %s", addr)
	}
	if strings.TrimSpace(s.authToken) != "" {
		s.logger.Printf("config api auth enabled (use header X-Bot-Token)")
	}
	s.logger.Printf("config ui listening on http://%s", addr)
	return http.ListenAndServe(addr, s)
}

func readBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 2*1024*1024))
}

func isLoopbackListenAddr(addr string) bool {
	a := strings.TrimSpace(addr)
	if a == "" {
		return true
	}
	host := ""
	if strings.HasPrefix(a, ":") {
		return false
	}
	h, _, err := net.SplitHostPort(a)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(h)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"wxbot/internal/configui"
	"wxbot/internal/shared/config"
)

type startupSetupOptions struct {
	BaseDir       string
	BotConfigPath string
	ConfigMode    string
	ConfigUIAddr  string
	Interactive   bool
	In            io.Reader
	Out           io.Writer
	Logger        *log.Logger
}

func ensureStartupConfigured(ctx context.Context, opts startupSetupOptions) error {
	mode := strings.ToLower(strings.TrimSpace(opts.ConfigMode))
	if mode == "" {
		mode = configModeFile
	}

	switch mode {
	case configModeFile:
		if err := validateStandaloneRuntimeConfig(opts.BotConfigPath); err != nil {
			return fmt.Errorf("load bot config failed: %w", err)
		}
	case configModeCLI:
		if !opts.Interactive {
			return errors.New("config-mode=cli requires interactive terminal")
		}
		if err := runModelSetupWizard(opts.BotConfigPath, opts.In, opts.Out, opts.Logger); err != nil {
			return err
		}
		if err := validateStandaloneRuntimeConfig(opts.BotConfigPath); err != nil {
			return fmt.Errorf("config check failed after cli setup: %w", err)
		}
	case configModeUI:
		if !opts.Interactive {
			return errors.New("config-mode=ui requires interactive terminal")
		}
		reader := bufio.NewReader(opts.In)
		if err := runUIConfigCheckLoop(ctx, reader, opts.Out, opts.BaseDir, opts.BotConfigPath, opts.ConfigUIAddr, opts.Logger); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported config mode: %s", mode)
	}

	printConfigEntryGuide(opts.Out, opts.Interactive, opts.Logger, opts.ConfigUIAddr)
	return nil
}

func runUIConfigCheckLoop(
	ctx context.Context,
	reader *bufio.Reader,
	out io.Writer,
	baseDir, botConfigPath, addr string,
	logger *log.Logger,
) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "127.0.0.1:19090"
	}

	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintln(out, "[配置方式] UI")
	_, _ = fmt.Fprintf(out, "配置页面地址: %s\n", configUIURL(addr))

	srv, err := configui.New(baseDir, botConfigPath, "", "", logger)
	if err != nil {
		return fmt.Errorf("init config ui failed: %w", err)
	}

	var stopUI func()
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		httpSrv := &http.Server{
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 3 * time.Second,
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			serveErr := httpSrv.Serve(ln)
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
				logger.Printf("config ui server stopped: %v", serveErr)
			}
		}()
		stopUI = func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			_ = httpSrv.Shutdown(shutdownCtx)
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		}
		_, _ = fmt.Fprintln(out, "配置UI已启动。完成保存后回到此窗口执行校验。")
	} else if isAddrInUseError(err) {
		_, _ = fmt.Fprintln(out, "检测到配置UI地址已被占用，将复用已运行的配置页面。")
	} else {
		return fmt.Errorf("start config ui failed: %w", err)
	}

	if stopUI != nil {
		defer stopUI()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return waitForConfigValidation(reader, out, botConfigPath)
}

func waitForConfigValidation(reader *bufio.Reader, out io.Writer, botConfigPath string) error {
	for {
		line, err := promptLine(reader, out, "编辑完成后按回车进行校验: ")
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(line), "q") {
			return errors.New("ui config canceled")
		}
		if err := validateStandaloneRuntimeConfig(botConfigPath); err != nil {
			_, _ = fmt.Fprintf(out, "配置校验失败: %v\n", err)
			continue
		}
		return nil
	}
}

func validateStandaloneRuntimeConfig(botConfigPath string) error {
	_, err := config.LoadStandaloneConfig(botConfigPath)
	return err
}

func configUIURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "127.0.0.1:19090"
	}
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func printConfigEntryGuide(out io.Writer, interactive bool, logger *log.Logger, configUIAddr string) {
	url := configUIURL(configUIAddr)
	if logger != nil {
		logger.Printf("config ready. ui entries: WxBot.exe -open-config-ui | WxBot.exe -config-mode ui | %s", url)
	}
	if out == nil {
		return
	}
	header := "[配置入口] 后续修改可通过以下任一方式"
	line1 := "1) 运行 WxBot.exe -open-config-ui（自动打开浏览器配置页）"
	line2 := "2) 运行 WxBot.exe -config-mode ui（当前程序进入UI配置）"
	line3 := fmt.Sprintf("3) 浏览器地址: %s", url)
	if interactive {
		_, _ = fmt.Fprintln(out, "")
		_, _ = fmt.Fprintf(out, "\x1b[1;33m%s\x1b[0m\n", header)
		_, _ = fmt.Fprintf(out, "\x1b[1;36m%s\x1b[0m\n", line1)
		_, _ = fmt.Fprintf(out, "\x1b[1;36m%s\x1b[0m\n", line2)
		_, _ = fmt.Fprintf(out, "\x1b[1;36m%s\x1b[0m\n", line3)
		_, _ = fmt.Fprintln(out, "")
		return
	}
	_, _ = fmt.Fprintln(out, header)
	_, _ = fmt.Fprintln(out, line1)
	_, _ = fmt.Fprintln(out, line2)
	_, _ = fmt.Fprintln(out, line3)
}

func isAddrInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

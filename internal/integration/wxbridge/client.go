package wxbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu sync.Mutex
	pendMu  sync.Mutex
	pending map[string]chan responseEnvelope
	events  chan EventEnvelope
	done    chan struct{}

	idSeq  atomic.Uint64
	logger *log.Logger
}

func Start(ctx context.Context, logger *log.Logger, pythonExe, scriptPath string) (*Client, error) {
	if strings.TrimSpace(pythonExe) == "" {
		pythonExe = "python"
	}
	if strings.TrimSpace(scriptPath) == "" {
		return nil, errors.New("script path is required")
	}

	cmd := exec.CommandContext(ctx, pythonExe, "-u", scriptPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start python bridge: %w", err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: map[string]chan responseEnvelope{},
		events:  make(chan EventEnvelope, 4096),
		done:    make(chan struct{}),
		logger:  logger,
	}
	go c.readStdout()
	go c.readStderr()
	go c.waitProcess()

	var pong map[string]any
	if err := c.Call(ctx, "ping", nil, &pong); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("bridge handshake failed: %w", err)
	}
	return c, nil
}

func (c *Client) waitProcess() {
	_ = c.cmd.Wait()
	close(c.done)

	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
}

func (c *Client) readStderr() {
	scanner := bufio.NewScanner(c.stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if c.logger != nil {
			c.logger.Printf("[py-bridge] %s", line)
		}
	}
	if err := scanner.Err(); err != nil && c.logger != nil {
		c.logger.Printf("py bridge stderr scanner error: %v", err)
	}
}

func (c *Client) readStdout() {
	defer close(c.events)
	scanner := bufio.NewScanner(c.stdout)
	buf := make([]byte, 0, 128*1024)
	scanner.Buffer(buf, 8*1024*1024)

	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		// Some wx backends print plain text banners to stdout.
		// They are not part of the JSON protocol and should be ignored.
		if !(strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[")) {
			if c.logger != nil {
				c.logger.Printf("[py-bridge-stdout] %s", raw)
			}
			continue
		}
		line := []byte(raw)

		var typ struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &typ); err != nil {
			if c.logger != nil {
				c.logger.Printf("invalid bridge json: %v", err)
			}
			continue
		}

		switch typ.Type {
		case "response":
			var resp responseEnvelope
			if err := json.Unmarshal(line, &resp); err != nil {
				if c.logger != nil {
					c.logger.Printf("invalid response json: %v", err)
				}
				continue
			}
			c.pendMu.Lock()
			ch := c.pending[resp.ID]
			if ch != nil {
				delete(c.pending, resp.ID)
			}
			c.pendMu.Unlock()
			if ch != nil {
				ch <- resp
				close(ch)
			}
		case "event":
			var evt EventEnvelope
			if err := json.Unmarshal(line, &evt); err != nil {
				if c.logger != nil {
					c.logger.Printf("invalid event json: %v", err)
				}
				continue
			}
			select {
			case c.events <- evt:
			default:
				if c.logger != nil {
					c.logger.Printf("event channel full, dropping %s", evt.Event)
				}
			}
		default:
			if c.logger != nil {
				c.logger.Printf("unknown message type from bridge: %s", typ.Type)
			}
		}
	}
	if err := scanner.Err(); err != nil && c.logger != nil {
		c.logger.Printf("py bridge stdout scanner error: %v", err)
	}
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := fmt.Sprintf("%d", c.idSeq.Add(1))
	respCh := make(chan responseEnvelope, 1)

	c.pendMu.Lock()
	c.pending[id] = respCh
	c.pendMu.Unlock()

	req := requestEnvelope{ID: id, Method: method, Params: params}
	payload, err := json.Marshal(req)
	if err != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return fmt.Errorf("marshal request: %w", err)
	}

	c.writeMu.Lock()
	_, err = c.stdin.Write(append(payload, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return fmt.Errorf("write request: %w", err)
	}

	select {
	case <-ctx.Done():
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return ctx.Err()
	case <-c.done:
		return errors.New("python bridge exited")
	case resp, ok := <-respCh:
		if !ok {
			return errors.New("bridge response channel closed")
		}
		if !resp.OK {
			if strings.TrimSpace(resp.Error) == "" {
				resp.Error = "unknown bridge error"
			}
			return errors.New(resp.Error)
		}
		if out != nil && len(resp.Result) > 0 && string(resp.Result) != "null" {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
		}
		return nil
	}
}

func (c *Client) Events() <-chan EventEnvelope {
	return c.events
}

func (c *Client) Init(ctx context.Context, show bool) (InitResult, error) {
	var out InitResult
	err := c.Call(ctx, "init", map[string]any{"show": show}, &out)
	return out, err
}

func (c *Client) AddListenChat(ctx context.Context, nickname string) error {
	return c.Call(ctx, "add_listen_chat", map[string]any{"nickname": nickname}, nil)
}

func (c *Client) SendMsg(ctx context.Context, who, msg string) error {
	return c.Call(ctx, "send_msg", map[string]any{"who": who, "msg": msg}, nil)
}

func (c *Client) SendFiles(ctx context.Context, who, filepath string) error {
	return c.Call(ctx, "send_files", map[string]any{"who": who, "filepath": filepath}, nil)
}

func (c *Client) VoiceCall(ctx context.Context, who string) error {
	return c.Call(ctx, "voice_call", map[string]any{"who": who}, nil)
}

func (c *Client) MessageDownload(ctx context.Context, eventID string) (string, error) {
	var out string
	err := c.messageAction(ctx, eventID, "download", nil, &out)
	return out, err
}

func (c *Client) MessageCapture(ctx context.Context, eventID string) (string, error) {
	var out string
	err := c.messageAction(ctx, eventID, "capture", nil, &out)
	return out, err
}

func (c *Client) MessageToText(ctx context.Context, eventID string) (string, error) {
	var out string
	err := c.messageAction(ctx, eventID, "to_text", nil, &out)
	return out, err
}

func (c *Client) MessageGetURL(ctx context.Context, eventID string) (string, error) {
	var out string
	err := c.messageAction(ctx, eventID, "get_url", nil, &out)
	return out, err
}

func (c *Client) messageAction(ctx context.Context, eventID, action string, option any, out any) error {
	params := map[string]any{
		"event_id": eventID,
		"action":   action,
	}
	if option != nil {
		params["option"] = option
	}
	return c.Call(ctx, "message_action", params, out)
}

func (c *Client) Shutdown(ctx context.Context) {
	_ = c.Call(ctx, "shutdown", nil, nil)
}

func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return nil
}

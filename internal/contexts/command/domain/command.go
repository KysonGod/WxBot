package domain

import "strings"

const (
	CmdPing  = "/ping"
	CmdHelp  = "/help"
	CmdClear = "/clear"
)

func Normalize(raw string) string {
	cmd := strings.TrimSpace(raw)
	cmd = strings.ReplaceAll(cmd, "：", ":")
	return strings.ToLower(cmd)
}

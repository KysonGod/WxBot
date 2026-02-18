package main

import (
	"fmt"
	"io"
	"strings"
)

func printGreenNotice(out io.Writer, interactive bool, message string) {
	if out == nil {
		return
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	if interactive {
		_, _ = fmt.Fprintf(out, "\x1b[1;32m%s\x1b[0m\n", msg)
		return
	}
	_, _ = fmt.Fprintln(out, msg)
}

func formatConfigSetupNotice(interactive bool, url string) string {
	trimmedURL := strings.TrimSpace(url)
	if trimmedURL == "" {
		trimmedURL = "http://127.0.0.1:19090"
	}
	targetFiles := "config.json/config.local.json"
	if interactive {
		return fmt.Sprintf(
			"你可以通过 \x1b[1;31m%s\x1b[0m 或编辑 \x1b[1;31m%s\x1b[0m 进行配置",
			trimmedURL,
			targetFiles,
		)
	}
	return fmt.Sprintf("你可以通过 %s 或编辑 %s 进行配置", trimmedURL, targetFiles)
}

func printConfigSetupNotice(out io.Writer, interactive bool, url string) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintln(out, formatConfigSetupNotice(interactive, url))
}

//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

func promptAnyKeyToExit(interactive bool, out io.Writer, message string) {
	if !interactive || out == nil {
		return
	}
	msg := message
	if msg == "" {
		msg = "按回车退出..."
	}
	_, _ = fmt.Fprintf(out, "\n%s", msg)
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	_, _ = fmt.Fprintln(out)
}

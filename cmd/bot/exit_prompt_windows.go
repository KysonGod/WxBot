//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

const (
	enableEchoInput uint32 = 0x0004
	enableLineInput uint32 = 0x0002
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func promptAnyKeyToExit(interactive bool, out io.Writer, message string) {
	if !interactive || out == nil {
		return
	}
	msg := message
	if msg == "" {
		msg = "按任意键退出..."
	}
	_, _ = fmt.Fprintf(out, "\n%s", msg)
	if oldMode, err := enableRawConsoleInput(); err == nil {
		defer restoreConsoleInputMode(oldMode)
		buf := make([]byte, 1)
		_, _ = os.Stdin.Read(buf)
		_, _ = fmt.Fprintln(out)
		return
	}
	buf := make([]byte, 1)
	_, _ = os.Stdin.Read(buf)
	_, _ = fmt.Fprintln(out)
}

func enableRawConsoleInput() (uint32, error) {
	h := syscall.Handle(os.Stdin.Fd())
	mode, err := getConsoleMode(h)
	if err != nil {
		return 0, err
	}
	rawMode := mode &^ (enableEchoInput | enableLineInput)
	if err := setConsoleMode(h, rawMode); err != nil {
		return 0, err
	}
	return mode, nil
}

func restoreConsoleInputMode(mode uint32) {
	h := syscall.Handle(os.Stdin.Fd())
	_ = setConsoleMode(h, mode)
}

func getConsoleMode(handle syscall.Handle) (uint32, error) {
	var mode uint32
	r1, _, e1 := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r1 == 0 {
		if e1 != nil && e1 != syscall.Errno(0) {
			return 0, error(e1)
		}
		return 0, errors.New("GetConsoleMode failed")
	}
	return mode, nil
}

func setConsoleMode(handle syscall.Handle, mode uint32) error {
	r1, _, e1 := procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
	if r1 == 0 {
		if e1 != nil && e1 != syscall.Errno(0) {
			return error(e1)
		}
		return errors.New("SetConsoleMode failed")
	}
	return nil
}

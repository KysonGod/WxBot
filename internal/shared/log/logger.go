package logger

import (
	"log"
	"os"
)

func New() *log.Logger {
	return log.New(os.Stdout, "[WxBot] ", log.LstdFlags|log.Lmsgprefix)
}

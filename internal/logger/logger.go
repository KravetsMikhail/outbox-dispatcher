package logger

import (
	"log"
	"os"
)

var L = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)

func Init(prefix string) {
	L.SetPrefix(prefix + " ")
}

package logger

import (
	"log"
	"os"
)

var L = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)

// Init configures the default logger. In production, flags are minimal to limit Docker log volume.
func Init(prefix string, production bool) {
	flags := log.LstdFlags | log.Lmicroseconds | log.Lshortfile
	if production {
		flags = log.LstdFlags
	}
	L = log.New(os.Stdout, prefix+" ", flags)
}

package appstatus

import (
	"sync/atomic"
)

var processErr atomic.Value // string

// SetProcessError stores the last error returned by ProcessPending (empty clears).
func SetProcessError(err error) {
	if err == nil {
		processErr.Store("")
		return
	}
	processErr.Store(err.Error())
}

// ProcessError returns the last ProcessPending error message, or empty if none.
func ProcessError() string {
	v := processErr.Load()
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

package webui

import "strings"

func quoteIdent(name string) string {
	if name == "" {
		return `"outbox_messages"`
	}
	s := strings.ReplaceAll(name, `"`, `""`)
	return `"` + s + `"`
}

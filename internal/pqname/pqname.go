package pqname

import "strings"

// QuoteIdent returns a double-quoted PostgreSQL identifier.
func QuoteIdent(ident string) string {
	s := strings.TrimSpace(ident)
	if s == "" {
		return `""`
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QualifiedTable returns "schema"."table" for use in SQL (default schema public, default table outbox_messages if empty).
func QualifiedTable(schema, table string) string {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "public"
	}
	table = strings.TrimSpace(table)
	if table == "" {
		table = "outbox_messages"
	}
	return QuoteIdent(schema) + "." + QuoteIdent(table)
}

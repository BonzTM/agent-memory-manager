package postgres

import "strings"

func sanitizeTSQuery(query string) string {
	return strings.TrimSpace(query)
}

func tsQueryLanguage() string {
	return "simple"
}

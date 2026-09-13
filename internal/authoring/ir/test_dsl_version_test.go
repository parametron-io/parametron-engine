package ir

import "strings"

func withDSLVersionHeader(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "dsl v1.0") || strings.HasPrefix(trimmed, "dsl 1.0") {
		return content
	}
	return "dsl v1.0\n" + content
}

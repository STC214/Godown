package httpdownload

import (
	"fmt"
	"strings"
	"unicode"
)

func ParseHeaders(text string) (map[string]string, error) {
	headers := make(map[string]string)
	for lineNumber, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid header line %d: missing ':'", lineNumber+1)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("invalid header line %d: empty name", lineNumber+1)
		}
		headers[name] = value
	}
	return headers, nil
}

func NormalizeCookies(text string) string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ';' || r == '\r' || r == '\n'
	})
	cookies := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" || hasControl(value) {
			continue
		}
		cookies = append(cookies, name+"="+value)
	}
	return strings.Join(cookies, "; ")
}

func MergeCookies(headers map[string]string, cookiesText string) map[string]string {
	normalized := NormalizeCookies(cookiesText)
	if normalized == "" {
		return headers
	}
	if headers == nil {
		headers = make(map[string]string)
	}
	for key, value := range headers {
		if strings.EqualFold(key, "Cookie") {
			if strings.TrimSpace(value) == "" {
				headers[key] = normalized
			} else {
				headers[key] = strings.TrimRight(strings.TrimSpace(value), ";") + "; " + normalized
			}
			return headers
		}
	}
	headers["Cookie"] = normalized
	return headers
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

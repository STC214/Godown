package btdownload

import (
	"net/url"
	"strings"
)

var trackerSchemes = map[string]struct{}{
	"http": {}, "https": {}, "udp": {}, "ws": {}, "wss": {},
}

// ParseTrackers validates and de-duplicates tracker URLs while retaining order.
func ParseTrackers(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, candidate := range strings.Fields(value) {
			candidate = strings.TrimSpace(candidate)
			parsed, err := url.Parse(candidate)
			if err != nil || parsed.Host == "" {
				continue
			}
			if _, ok := trackerSchemes[strings.ToLower(parsed.Scheme)]; !ok {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	return result
}

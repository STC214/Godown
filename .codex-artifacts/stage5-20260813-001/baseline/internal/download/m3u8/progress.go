package m3u8download

import (
	"regexp"
	"strconv"
	"strings"
)

var vodProgressPattern = regexp.MustCompile(`(\d+)/(\d+)\s+(\d+(?:\.\d+)?)%\s+(\d+(?:\.\d+)?)(KB|MB|GB|B)/(\d+(?:\.\d+)?)(KB|MB|GB|B)\s+(\d+(?:\.\d+)?)(GBps|MBps|KBps|Bps)\s+(.+)`)
var liveProgressPattern = regexp.MustCompile(`(\d{2}m\d{2}s)/(\d{2}m\d{2}s)\s+\d+/\d+\s+(Recording|Waiting)\s+(\d+)%\s+(-|(\d+(?:\.\d+)?)(GBps|MBps|KBps|Bps))`)

type Progress struct {
	Matched     bool
	Progress    float64
	Received    int64
	Total       int64
	Speed       int64
	Message     string
	LiveStatus  string
	LiveElapsed string
	LiveTotal   string
}

func ParseProgressLine(line string) Progress {
	text := strings.TrimSpace(line)
	if text == "" {
		return Progress{}
	}
	if len(text) > 1000 {
		text = text[:1000]
	}
	if match := vodProgressPattern.FindStringSubmatch(text); match != nil {
		progress, _ := strconv.ParseFloat(match[3], 64)
		return Progress{
			Matched:  true,
			Progress: progress,
			Received: toBytes(match[4], match[5]),
			Total:    toBytes(match[6], match[7]),
			Speed:    toBytes(match[8], strings.TrimSuffix(match[9], "ps")),
			Message:  text,
		}
	}
	if match := liveProgressPattern.FindStringSubmatch(text); match != nil {
		progress, _ := strconv.ParseFloat(match[4], 64)
		speed := int64(0)
		if match[5] != "-" {
			speed = toBytes(match[6], strings.TrimSuffix(match[7], "ps"))
		}
		return Progress{
			Matched:     true,
			Progress:    progress,
			Speed:       speed,
			Message:     text,
			LiveStatus:  match[3],
			LiveElapsed: match[1],
			LiveTotal:   match[2],
		}
	}
	return Progress{Message: text}
}

func toBytes(valueText, unit string) int64 {
	value, err := strconv.ParseFloat(valueText, 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(unit) {
	case "KB":
		value *= 1024
	case "MB":
		value *= 1024 * 1024
	case "GB":
		value *= 1024 * 1024 * 1024
	}
	return int64(value)
}

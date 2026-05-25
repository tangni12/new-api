package tencentvod

import "strings"

func ptrValue[T any](value *T) T {
	var zero T

	if value == nil {
		return zero
	}

	return *value
}

func normalizeResolution(resolution string) string {
	value := strings.ToUpper(strings.TrimSpace(resolution))
	value = strings.ReplaceAll(value, " ", "")
	switch value {
	case "480", "480P", "540", "540P":
		return "480P"
	case "", "720", "720P", "768", "768P":
		return "720P"
	case "1080", "1080P":
		return "1080P"
	case "2K", "2048", "2048P":
		return "2K"
	case "4K", "4096", "4096P":
		return "4K"
	default:
		return ""
	}
}

func unitPriceByResolution(p720, p1080, p2k, p4k float64, resolution string) float64 {
	switch normalizeResolution(resolution) {
	case "480P":
		if p720 > 0 {
			return p720
		}
	case "720P":
		if p720 > 0 {
			return p720
		}

		if p720 == 0 {
			return p1080
		}
	case "1080P":
		if p1080 > 0 {
			return p1080
		}
	case "2K":
		if p2k > 0 {
			return p2k
		}
	case "4K":
		if p4k > 0 {
			return p4k
		}
	}

	return p720
}

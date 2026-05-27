package shubiaoveo

import (
	"fmt"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// buildBillingRatios returns multipliers relative to the model price configured
// in NewAPI. Configure the frontend model price as the 720p no-audio base
// price, then this adapter applies explicit model-specific multipliers:
//   - veo-3.1-generate-001: $0.20/s base
//   - veo-3.1-fast-generate-001: $0.08/s base
//
// The ratios below are model-specific on purpose; do not infer future model
// pricing from name fragments such as "fast".
func buildBillingRatios(req relaycommon.TaskSubmitReq, modelName string) map[string]float64 {
	seconds := resolveDuration(req)
	specRatio := resolveSpecRatio(req, modelName)

	ratios := map[string]float64{
		"seconds": float64(seconds),
	}
	if specRatio != 1 {
		ratios["spec"] = specRatio
	}
	return ratios
}

func resolveDuration(req relaycommon.TaskSubmitReq) int {
	if v, ok := metadataField(req.Metadata, "durationSeconds"); ok {
		if duration := toPositiveInt(v); duration > 0 {
			return duration
		}
	}
	if req.Duration > 0 {
		return req.Duration
	}
	if duration, err := strconv.Atoi(strings.TrimSpace(req.Seconds)); err == nil && duration > 0 {
		return duration
	}
	return defaultDurationSec
}

type modelBillingRatioMatrix struct {
	ratios map[bool]map[string]float64
}

var modelBillingMatrices = map[string]modelBillingRatioMatrix{
	"veo-3.1-generate-001": {
		ratios: map[bool]map[string]float64{
			false: {
				"720p":  1,
				"1080p": 1,
				"4k":    2,
			},
			true: {
				"720p":  2,
				"1080p": 2,
				"4k":    3,
			},
		},
	},
	"veo-3.1-fast-generate-001": {
		ratios: map[bool]map[string]float64{
			false: {
				"720p":  1,
				"1080p": 1.25,
				"4k":    3.125,
			},
			true: {
				"720p":  1.25,
				"1080p": 1.5,
				"4k":    3.75,
			},
		},
	},
}

func resolveSpecRatio(req relaycommon.TaskSubmitReq, modelName string) float64 {
	matrix, ok := modelBillingMatrices[strings.TrimSpace(modelName)]
	if !ok {
		return 1
	}

	resolution := resolveResolution(req)
	hasAudio := resolveGenerateAudio(req)
	ratioByResolution := matrix.ratios[hasAudio]
	ratio, ok := ratioByResolution[resolution]
	if !ok {
		// If a caller passes a future/unsupported resolution, let
		// upstream decide validity, but pre-charge with the highest known ratio
		// for this audio mode to avoid undercharging if upstream accepts it.
		ratio = highestRatio(ratioByResolution)
	}
	if ratio <= 0 {
		return 1
	}
	return ratio
}

func resolveResolution(req relaycommon.TaskSubmitReq) string {
	if v, ok := metadataField(req.Metadata, "resolution"); ok {
		if resolution := normalizeResolution(fmt.Sprint(v)); resolution != "" {
			return resolution
		}
	}
	if req.Size != "" {
		return normalizeResolution(req.Size)
	}
	return defaultResolution
}

func normalizeResolution(resolution string) string {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	resolution = strings.ReplaceAll(resolution, " ", "")
	switch resolution {
	case "720", "720p", "1280x720", "720x1280":
		return "720p"
	case "1080", "1080p", "1920x1080", "1080x1920":
		return "1080p"
	case "4k", "2160p", "3840x2160", "2160x3840":
		return "4k"
	default:
		return defaultResolution
	}
}

func resolveGenerateAudio(req relaycommon.TaskSubmitReq) bool {
	if v, ok := metadataField(req.Metadata, "generateAudio"); ok {
		return toBool(v)
	}
	// The request builder sends generateAudio=true when omitted, so billing
	// must use the audio-enabled price by default as well.
	return true
}

func metadataField(metadata map[string]any, key string) (any, bool) {
	if metadata == nil {
		return nil, false
	}
	if params, ok := mapFromAny(metadata["parameters"]); ok {
		if v, exists := params[key]; exists {
			return v, true
		}
	}
	v, ok := metadata[key]
	return v, ok
}

func mapFromAny(value any) (map[string]any, bool) {
	switch m := value.(type) {
	case map[string]any:
		return m, true
	default:
		return nil, false
	}
}

func toPositiveInt(value any) int {
	switch n := value.(type) {
	case int:
		if n > 0 {
			return n
		}
	case int64:
		if n > 0 {
			return int(n)
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	case string:
		if v, err := strconv.Atoi(strings.TrimSpace(n)); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

func toBool(value any) bool {
	switch b := value.(type) {
	case bool:
		return b
	case string:
		text := strings.ToLower(strings.TrimSpace(b))
		return text == "true" || text == "1" || text == "enabled"
	case float64:
		return b != 0
	case int:
		return b != 0
	default:
		return false
	}
}

func highestRatio(ratios map[string]float64) float64 {
	var max float64
	for _, ratio := range ratios {
		if ratio > max {
			max = ratio
		}
	}
	return max
}

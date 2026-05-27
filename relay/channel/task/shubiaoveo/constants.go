package shubiaoveo

const (
	defaultBaseURL     = "https://api2img.shubiaobiao.com"
	defaultDurationSec = 8
	defaultResolution  = "720p"
	defaultSampleCount = 1
	maxVeoImageSize    = 20 * 1024 * 1024
)

var supportedModels = []string{
	"veo-3.1-generate-001",
	"veo-3.1-fast-generate-001",
}

package tencentkling

import (
	"strings"

	"github.com/shopspring/decimal"
	vod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vod/v20180717"
)

type ModelBilling struct {
	// 视频时长
	Duration float64
	// 分辨率
	Resolution string
	// 是否有声
	HasAudio bool
	// 是否有参考视频
	HasReferenceVideo bool
	// 是否有音色
	HasVoice bool

	req *vod.CreateAigcVideoTaskRequest
}

func NewModelBilling(req *vod.CreateAigcVideoTaskRequest) *ModelBilling {
	p := &ModelBilling{
		req: req,
	}

	if req.OutputConfig != nil {
		if req.OutputConfig.Duration != nil && *req.OutputConfig.Duration > 0 {
			p.Duration = *req.OutputConfig.Duration
		}
		p.Resolution = normalizeResolution(ptrValue(req.OutputConfig.Resolution))
		p.HasAudio = strings.EqualFold(strings.TrimSpace(ptrValue(req.OutputConfig.AudioGeneration)), "Enabled")
	}

	for _, fileInfo := range req.FileInfos {
		if fileInfo == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ptrValue(fileInfo.Category)), "Video") {
			p.HasReferenceVideo = true
		}
		if strings.TrimSpace(ptrValue(fileInfo.VoiceId)) != "" {
			p.HasVoice = true
		}
	}

	return p
}

// CalculatePrice 计算基于基础价格倍率
func (p *ModelBilling) CalculatePrice() map[string]float64 {
	req := p.req

	switch ptrValue(req.ModelName) {
	case string(ModelKling):
		return p.klingPrice(req)
	}

	return nil
}

func (p *ModelBilling) klingPrice(req *vod.CreateAigcVideoTaskRequest) map[string]float64 {
	// 视频时长
	duration := 5.0
	// 分辨率
	resolution := p.Resolution
	// 是否有声
	hasAudio := p.HasAudio
	// 是否有参考视频
	hasReferenceVideo := p.HasReferenceVideo
	// 是否有音色
	hasVoice := p.HasVoice

	if p.Duration > 0 {
		duration = p.Duration
	}

	modelVersion := strings.TrimSpace(ptrValue(req.ModelVersion))
	sceneType := strings.ToLower(strings.TrimSpace(ptrValue(req.SceneType)))

	var basePrice, unitPrice float64

	switch sceneType {
	case "lip_sync": // 对口型
		basePrice = 0.1
		unitPrice = 0.1
		if duration < 5 {
			duration = 5
		}
	case "avatar_i2v": // 数字人
		basePrice = 0.4
		unitPrice = unitPriceByResolution(0.4, 0.8, 1.2, 1.8, resolution)
	default:
		switch strings.ToLower(modelVersion) {
		case "1.6", "2.0", "2.1":
			basePrice = 0.4
			unitPrice = unitPriceByResolution(0.4, 0.7, 1.0, 1.5, resolution)
		case "2.5", "2.5-pro":
			basePrice = 0.3
			unitPrice = unitPriceByResolution(0.3, 0.5, 0.75, 1.12, resolution)
		case "2.6":
			basePrice = 0.3

			if sceneType == "motion_control" { // 动作控制
				unitPrice = unitPriceByResolution(0.5, 0.8, 1.2, 1.8, resolution)
			} else {
				if hasAudio {
					unitPrice = unitPriceByResolution(0, 1.0, 1.5, 2.25, resolution)
				} else {
					unitPrice = unitPriceByResolution(0.3, 0.5, 0.75, 1.12, resolution)
				}
			}
		case "o1":
			basePrice = 0.6
			if hasReferenceVideo {
				unitPrice = unitPriceByResolution(0.9, 1.2, 1.8, 2.7, resolution)
			} else {
				unitPrice = unitPriceByResolution(0.6, 0.8, 1.2, 1.8, resolution)
			}
		case "3.0":
			basePrice = 0.6

			if sceneType == "motion_control" { // 动作控制
				unitPrice = unitPriceByResolution(0.9, 1.2, 1.8, 2.7, resolution)
			} else {
				if hasAudio {
					if hasVoice {
						unitPrice = unitPriceByResolution(1.1, 1.4, 1.8, 2.4, resolution)
					} else {
						unitPrice = unitPriceByResolution(0.9, 1.2, 1.5, 3.0, resolution)
					}
				} else {
					unitPrice = unitPriceByResolution(0.6, 0.8, 1.0, 3.0, resolution)
				}
			}
		case "3.0-omni":
			basePrice = 0.6
			if hasReferenceVideo {
				if hasAudio {
					unitPrice = unitPriceByResolution(1.1, 1.4, 1.8, 2.4, resolution)
				} else {
					unitPrice = unitPriceByResolution(0.9, 1.2, 1.5, 2.0, resolution)
				}
			} else {
				if hasAudio {
					unitPrice = unitPriceByResolution(0.8, 1.0, 1.2, 3.0, resolution)
				} else {
					unitPrice = unitPriceByResolution(0.6, 0.8, 1.0, 3.0, resolution)
				}
			}
		}
	}

	ratios := map[string]float64{
		"seconds": duration,
	}

	unitPriceDecimal := decimal.NewFromFloat(unitPrice)
	basePriceDecimal := decimal.NewFromFloat(basePrice)

	if basePrice > 0 && unitPrice > 0 {
		ratios["spec"] = unitPriceDecimal.Div(basePriceDecimal).InexactFloat64()
	}

	return ratios
}

package alividu

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
)

// ChannelName 渠道内部名称。
const ChannelName = "alividu"

// baseResolution 计费基准分辨率档位。
// 前端为官方 vidu 模型配置的「固定价格」即代表该档位每秒的价格，
// 其余分辨率通过 price[分辨率] / price[基准] 的倍率换算。
const baseResolution = "720P"

// ModelList 对外暴露的官方 Vidu 模型名称。
//
// 平台对外只暴露官方 Vidu 的模型名与调用方式，作为针对 vidu 模型的唯一调用方式；
// 后端再根据动作（文生 / 图生 / 首尾帧）映射到阿里百炼的具体模型。
//
// 官方模型在不同动作下的可用性（依据阿里百炼控制台 Vidu 模型定价）：
//   - text2video      : viduq3-pro, viduq3-turbo, viduq2
//   - img2video       : viduq3-pro, viduq3-turbo, viduq2-pro, viduq2-turbo, viduq2-pro-fast
//   - start-end2video : viduq3-pro, viduq3-turbo, viduq2-pro, viduq2-turbo
//
// 这里取并集对外暴露；实际某个模型在某个动作下是否可用，由 aliViduPrices 中是否存在
// 对应的 vidu/{model}_{action} 条目决定。
var ModelList = []string{
	"viduq3-pro",
	"viduq3-turbo",
	"viduq2",
	"viduq2-pro",
	"viduq2-turbo",
	"viduq2-pro-fast",
}

// aliViduPrices 阿里百炼 vidu 各模型在不同分辨率档位下的输出单价（元/秒）。
//
// 数据来源：阿里云百炼控制台 Vidu 模型定价页面（文生 / 图生 / 首尾帧）。
// key   : 阿里百炼模型名 vidu/{model}_{action}
// value : 分辨率档位 -> 元/秒 单价
//
// 计费时不直接使用绝对单价，而是用「分辨率档位单价 / 基准档位(720P)单价」得到相对倍率，
// 与前端为官方模型配置的基础价格（即 720P 每秒价）相乘，再乘以视频时长（秒）。
// 这样既保证倍率精确（运行时 float64 除法，无手工舍入误差），也便于阿里调价时维护。
var aliViduPrices = map[string]map[string]float64{
	// ---- 文生视频 text2video ----
	"vidu/viduq3-pro_text2video": {
		"540P":  0.3125,
		"720P":  0.78125,
		"1080P": 0.9375,
	},
	"vidu/viduq3-turbo_text2video": {
		"540P":  0.25,
		"720P":  0.375,
		"1080P": 0.4375,
	},
	"vidu/viduq2_text2video": {
		"540P":  0.1125,
		"720P":  0.21875,
		"1080P": 0.375,
	},

	// ---- 图生视频 img2video ----
	"vidu/viduq3-pro_img2video": {
		"540P":  0.3125,
		"720P":  0.78125,
		"1080P": 0.9375,
	},
	"vidu/viduq3-turbo_img2video": {
		"540P":  0.25,
		"720P":  0.375,
		"1080P": 0.4375,
	},
	"vidu/viduq2-pro_img2video": {
		"540P":  0.15625,
		"720P":  0.34375,
		"1080P": 0.71875,
	},
	"vidu/viduq2-turbo_img2video": {
		"540P":  0.0875,
		"720P":  0.25,
		"1080P": 0.46875,
	},
	"vidu/viduq2-pro-fast_img2video": {
		"720P":  0.1,
		"1080P": 0.2,
	},

	// ---- 首尾帧生视频 start-end2video ----
	"vidu/viduq3-pro_start-end2video": {
		"540P":  0.3125,
		"720P":  0.78125,
		"1080P": 0.9375,
	},
	"vidu/viduq3-turbo_start-end2video": {
		"540P":  0.25,
		"720P":  0.375,
		"1080P": 0.4375,
	},
	"vidu/viduq2-pro_start-end2video": {
		"540P":  0.15625,
		"720P":  0.34375,
		"1080P": 0.71875,
	},
	"vidu/viduq2-turbo_start-end2video": {
		"540P":  0.0875,
		"720P":  0.25,
		"1080P": 0.46875,
	},
}

// isSupportedAliModel 判断某个阿里 vidu 模型（含动作后缀）是否受支持。
func isSupportedAliModel(aliModel string) bool {
	_, ok := aliViduPrices[aliModel]
	return ok
}

// lookupResolutionRatio 计算某个阿里 vidu 模型在指定分辨率档位相对基准档位(720P)的价格倍率。
//
// 倍率 = price[resolution] / price[720P]，运行时精确计算，无预先舍入。
// 前端配置的基础价格代表 720P 每秒价，乘以本倍率即得到目标分辨率的每秒价。
func lookupResolutionRatio(aliModel, resolution string) (float64, bool) {
	prices, ok := aliViduPrices[aliModel]
	if !ok {
		return 0, false
	}
	base, ok := prices[baseResolution]
	if !ok || base == 0 {
		return 0, false
	}
	price, ok := prices[resolution]
	if !ok {
		return 0, false
	}
	return price / base, true
}

// ---- 像素尺寸 -> 分辨率档位映射（与 ali 适配器保持一致）----

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
	size540p = []string{
		"1024*576",
		"576*1024",
		"1024*1024",
		"1024*768",
		"768*1024",
	}
)

// sizeToResolution 将像素尺寸（宽*高）映射到分辨率档位。
func sizeToResolution(size string) (string, error) {
	switch {
	case lo.Contains(size480p, size):
		return "480P", nil
	case lo.Contains(size720p, size):
		return "720P", nil
	case lo.Contains(size1080p, size):
		return "1080P", nil
	case lo.Contains(size540p, size):
		return "540P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

// sizeToResolutionOrDefault 将像素尺寸映射到分辨率档位，无法识别时回退为 720P。
// 主要用于像素尺寸下的计费档位估算。
func sizeToResolutionOrDefault(size string) string {
	if resolution, err := sizeToResolution(size); err == nil {
		return resolution
	}
	return baseResolution
}

// normalizeResolution 归一化分辨率档位字符串（如 "720p" -> "720P"），空值回退为 720P。
func normalizeResolution(resolution string) string {
	value := strings.ToUpper(strings.TrimSpace(resolution))
	if value == "" {
		return baseResolution
	}
	if !strings.HasSuffix(value, "P") && !strings.HasSuffix(value, "K") {
		value += "P"
	}
	return value
}

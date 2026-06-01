// Package viducommon 定义 Vidu 官方调用方式的统一入参结构。
//
// 平台对外只暴露 Vidu 官方的模型名称与调用方式，作为针对 vidu 模型的唯一调用入口；
// 各个承载 vidu 模型的渠道适配器（当前为 alividu，未来可扩展官方直连等）都先把
// 平台请求归一化为这里的 RequestPayload，再各自映射为上游渠道的具体请求格式。
//
// 这与 kling 渠道的 klingcommon.RequestPayload 作用一致：把“对外入参”与
// “各渠道上游格式”解耦，结构更清晰、便于复用。
//
// 注意：当前字段仅覆盖已适配的阿里百炼（DashScope）vidu 渠道所支持的参数，
// 其余 Vidu 官方参数（bgm / movement_amplitude / off_peak / style 等）暂不支持。
package viducommon

// RequestPayload Vidu 官方调用方式的统一入参。
//
// 字段来源：
//   - 通用参数（prompt / model / images / size→resolution / duration）来自平台
//     TaskSubmitReq 的顶层字段；
//   - 其余官方参数（seed / audio / watermark 等）通过 metadata 透传后合并到这里。
type RequestPayload struct {
	// Model Vidu 官方模型名，例如 viduq3-pro / viduq3-turbo / viduq2 等。
	Model string `json:"model,omitempty"`

	// Prompt 文本提示词。
	Prompt string `json:"prompt,omitempty"`

	// Images 输入图片列表：
	//   - 图生视频：传 1 张作为首帧；
	//   - 首尾帧生视频：传 2 张（第一张首帧、第二张尾帧）。
	// 支持图片 URL 或 base64。
	Images []string `json:"images,omitempty"`

	// Duration 视频时长，单位秒。默认 5。
	Duration int `json:"duration,omitempty"`

	// Resolution 分辨率档位：540P / 720P / 1080P。默认 720P。
	// 平台通用参数 size（如 "720p"）会归一化后写入此字段。
	Resolution string `json:"resolution,omitempty"`

	// Size 像素尺寸（宽*高，如 "1024*576"）。仅文生视频支持，
	// 当平台 size 传入像素尺寸时使用。
	Size string `json:"size,omitempty"`

	// Seed 随机种子。不传或为 0 时上游使用随机数。
	Seed int `json:"seed,omitempty"`

	// Audio 是否使用音视频直出能力（输出带声音的视频）。
	// 指针类型：为 nil 时不下发该参数，交由上游使用其默认值。
	Audio *bool `json:"audio,omitempty"`

	// Watermark 是否为生成的视频添加水印。
	// 指针类型：为 nil 时不下发该参数，交由上游使用其默认值。
	Watermark *bool `json:"watermark,omitempty"`
}

package klingcommon

type TextToVideo struct {
}
type ImageToVideo struct {
	// 参考图像
	Image string `json:"image"`
	// 参考图像 - 尾帧控制
	ImageTail string `json:"image_tail"`
}

type RequestPayload struct {
	//// 模型名称，例如：kling/kling-v1
	//ModelName string `json:"model_name"`
	// 提示词
	Prompt string `json:"prompt,omitempty"`
	// 分辨率模式：720P、1080P、2K、4K 默认 720P
	Mode string `json:"mode,omitempty"`
	// 视频生成时长，单位：秒
	Duration int `json:"duration,omitempty"`

	// 类型 text_to_video | image_to_video 默认:text_to_video
	KlingType string `json:"kling_type,omitempty"`

	// 反向提示词
	NegativePrompt string `json:"negative_prompt,omitempty"`
	// 视频宽高比，可选值：16:9、9:16、1:1 默认16:9
	AspectRatio string `json:"aspect_ratio,omitempty"`
	// 是否生成声音，可选值：true|false 默认false
	SoundEnable bool `json:"sound_enable,omitempty"`

	// 文生视频相关参数
	TextToVideo TextToVideo `json:"text_to_video,omitempty"`
	// 图生视频相关参数
	ImageToVideo ImageToVideo `json:"image_to_video,omitempty"`
}

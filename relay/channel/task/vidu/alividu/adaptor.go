// Package alividu adapts Alibaba Bailian (DashScope) hosted Vidu video models
// to the unified, official-Vidu calling convention exposed by this platform.
//
// 设计目标：
//   - 对外暴露 Vidu 官方的模型名称（viduq3-pro / viduq3-turbo / viduq2 ...）与官方调用方式
//     （通用参数 prompt / model / image / images / size / duration + metadata 透传）。
//   - 对内将官方调用归一化为 viducommon.RequestPayload，再映射为阿里百炼（DashScope）的
//     vidu/{model}_{text2video|img2video|start-end2video} 模型名以及 DashScope 的请求结构。
//   - 价格按阿里渠道的分辨率倍率适配：前端只需为某个官方 vidu 模型配置一个基础价格，
//     后端再根据时长（秒）与分辨率档位进行倍率换算。
//
// 该适配器不改动原有 vidu（官方直连）适配器，行为完全独立。
package alividu

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/relay/channel/task/vidu/viducommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// DashScope (阿里百炼) 请求 / 响应结构
// ============================

// AliVideoRequest 阿里百炼视频生成请求体。
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

type AliVideoMedia struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// AliVideoInput DashScope 视频生成的 input 对象。
type AliVideoInput struct {
	Prompt string          `json:"prompt,omitempty"`
	Media  []AliVideoMedia `json:"media,omitempty"`
}

// AliVideoParameters DashScope 视频生成的 parameters 对象。
type AliVideoParameters struct {
	Resolution string `json:"resolution,omitempty"` // 分辨率档位: 540P/720P/1080P
	Size       string `json:"size,omitempty"`       // 像素尺寸, 如 "1024*576"（仅文生视频）
	Duration   int    `json:"duration,omitempty"`   // 时长(秒)
	Seed       int    `json:"seed,omitempty"`       // 随机种子
	Audio      *bool  `json:"audio,omitempty"`      // 是否音视频直出（带声音）
	Watermark  *bool  `json:"watermark,omitempty"`  // 是否添加水印
}

// AliVideoResponse 阿里百炼响应体。
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

type AliVideoOutput struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	SubmitTime    string `json:"submit_time,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	OrigPrompt    string `json:"orig_prompt,omitempty"`
	ActualPrompt  string `json:"actual_prompt,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

type AliUsage struct {
	Duration   dto.IntValue `json:"duration,omitempty"`
	VideoCount dto.IntValue `json:"video_count,omitempty"`
	SR         dto.IntValue `json:"SR,omitempty"`
}

// ============================
// Adaptor 实现
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction 解析请求体并根据图片数量确定视频生成类型。
//   - 无图片        -> 文生视频   (text2video)
//   - 1 张图片      -> 图生视频   (img2video)
//   - 2 张图片      -> 首尾帧生视频 (start-end2video)
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); err != nil {
		return err
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}

	switch len(collectImageInputs(req)) {
	case 0:
		info.Action = constant.TaskActionTextGenerate
	case 1:
		info.Action = constant.TaskActionGenerate
	case 2:
		info.Action = constant.TaskActionFirstTailGenerate
	default:
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("alividu supports at most two images (image-to-video or start-end-to-video)"),
			"invalid_images", http.StatusBadRequest)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	payload, err := a.convertToViduPayload(&req, info)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_vidu_payload_failed")
	}

	aliReq, err := a.convertToAliRequest(payload, info)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_alividu_request_failed")
	}
	logger.LogJson(c, "alividu video request body", aliReq)

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_alividu_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}
	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.Model = info.OriginModelName
	ov.Status = convertAliStatus(aliResp.Output.TaskStatus)
	ov.CreatedAt = common.GetTimestamp()
	c.JSON(http.StatusOK, ov)

	return aliResp.Output.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// EstimateBilling 根据用户请求参数计算 OtherRatios（时长、分辨率倍率等）。
// 在 ValidateRequestAndSetAction 之后、价格计算之前调用。
// 前端只需为官方 vidu 模型配置一个基础价格（对应 720P 每秒价格），
// 这里根据时长与分辨率档位换算最终倍率。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	payload, err := a.convertToViduPayload(&req, info)
	if err != nil {
		return nil
	}
	aliModel := a.resolveAliModel(info, payload)
	resolution := payload.Resolution

	otherRatios := map[string]float64{
		"seconds": float64(payload.Duration),
	}
	if ratio, ok := lookupResolutionRatio(aliModel, resolution); ok {
		otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
	}
	return otherRatios
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{Code: 0}
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = aliResp.Output.VideoURL
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}
	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal alividu response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	if aliResp.Output.VideoURL != "" {
		openAIResp.SetMetadata("url", aliResp.Output.VideoURL)
	}

	if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{Code: aliResp.Code, Message: aliResp.Message}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{Code: aliResp.Output.Code, Message: aliResp.Output.Message}
	}

	return common.Marshal(openAIResp)
}

// ============================
// helpers
// ============================

// convertToViduPayload 将平台 TaskSubmitReq 归一化为 Vidu 官方统一入参。
// 通用参数取自顶层字段，其余官方参数（seed/audio/watermark）从 metadata 透传合并。
func (a *TaskAdaptor) convertToViduPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*viducommon.RequestPayload, error) {
	officialModel := info.UpstreamModelName
	if officialModel == "" {
		officialModel = req.Model
	}

	payload := &viducommon.RequestPayload{
		Model:    officialModel,
		Prompt:   req.Prompt,
		Images:   collectImageInputs(*req),
		Duration: resolveDuration(req),
	}

	// 通用参数 size：像素尺寸（含 "*"）保留为 Size，否则归一化为分辨率档位。
	size := strings.TrimSpace(req.Size)
	if strings.Contains(size, "*") {
		payload.Size = size
		payload.Resolution = sizeToResolutionOrDefault(size)
	} else {
		payload.Resolution = normalizeResolution(size)
	}

	// metadata 透传：合并官方非通用参数（seed / audio / watermark）。
	if len(req.Metadata) > 0 {
		if err := taskcommon.UnmarshalMetadata(req.Metadata, payload); err != nil {
			return nil, errors.Wrap(err, "unmarshal metadata failed")
		}
	}
	return payload, nil
}

// convertToAliRequest 将 Vidu 官方统一入参映射为阿里百炼请求结构。
func (a *TaskAdaptor) convertToAliRequest(payload *viducommon.RequestPayload, info *relaycommon.RelayInfo) (*AliVideoRequest, error) {
	aliModel := a.resolveAliModel(info, payload)
	if !isSupportedAliModel(aliModel) {
		return nil, fmt.Errorf("unsupported alividu model/action combination: %s", aliModel)
	}

	aliReq := &AliVideoRequest{
		Model: aliModel,
		Input: AliVideoInput{
			Prompt: payload.Prompt,
		},
		Parameters: &AliVideoParameters{
			Duration:  payload.Duration,
			Seed:      payload.Seed,
			Audio:     payload.Audio,
			Watermark: payload.Watermark,
		},
	}

	// 图片输入（图生视频 / 首尾帧生视频）
	if info.Action == constant.TaskActionGenerate || info.Action == constant.TaskActionFirstTailGenerate {
		if len(payload.Images) == 0 {
			return nil, fmt.Errorf("alividu image video model requires image, images, or input_reference")
		}
		if info.Action == constant.TaskActionFirstTailGenerate && len(payload.Images) != 2 {
			return nil, fmt.Errorf("alividu start-end video model requires exactly two images")
		}
		aliReq.Input.Media = make([]AliVideoMedia, 0, len(payload.Images))
		for _, image := range payload.Images {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "image", URL: image})
		}
	}

	// 分辨率 / 尺寸映射：
	//   - 文生视频: 阿里同时支持 size(像素) 与 resolution(档位)，用户传像素则透传 size。
	//   - 图生 / 首尾帧生视频: 阿里仅支持 resolution 档位（模型按输入图自动缩放比例）。
	if info.Action == constant.TaskActionTextGenerate && payload.Size != "" {
		aliReq.Parameters.Size = payload.Size
	} else {
		aliReq.Parameters.Resolution = payload.Resolution
	}

	return aliReq, nil
}

// resolveAliModel 根据官方模型名 + 动作，解析出阿里百炼的 vidu 模型名。
//   - 官方名 "viduq3-pro" + 图生视频 -> "vidu/viduq3-pro_img2video"
//   - 若用户直接传入阿里格式（vidu/xxx_yyy2video），则原样使用。
func (a *TaskAdaptor) resolveAliModel(info *relaycommon.RelayInfo, payload *viducommon.RequestPayload) string {
	officialModel := strings.TrimSpace(payload.Model)
	if officialModel == "" {
		officialModel = strings.TrimSpace(info.UpstreamModelName)
	}

	// 已经是阿里格式：直接透传
	if strings.HasPrefix(officialModel, "vidu/") && strings.Contains(officialModel, "2video") {
		return officialModel
	}
	officialModel = strings.TrimPrefix(officialModel, "vidu/")
	return fmt.Sprintf("vidu/%s_%s", officialModel, actionToAliSuffix(info.Action))
}

// actionToAliSuffix 将平台动作映射为阿里 vidu 模型名后缀。
func actionToAliSuffix(action string) string {
	switch action {
	case constant.TaskActionGenerate:
		return "img2video"
	case constant.TaskActionFirstTailGenerate:
		return "start-end2video"
	default:
		return "text2video"
	}
}

// resolveDuration 解析视频时长（秒），默认 5 秒。
func resolveDuration(req *relaycommon.TaskSubmitReq) int {
	if req.Duration > 0 {
		return req.Duration
	}
	if req.Seconds != "" {
		if seconds, err := strconv.Atoi(req.Seconds); err == nil && seconds > 0 {
			return seconds
		}
	}
	return 5
}

// collectImageInputs 收集去重后的图片输入（images / image / input_reference）。
func collectImageInputs(req relaycommon.TaskSubmitReq) []string {
	seen := make(map[string]bool)
	images := make([]string, 0, len(req.Images)+2)
	add := func(url string) {
		url = strings.TrimSpace(url)
		if url == "" || seen[url] {
			return
		}
		seen[url] = true
		images = append(images, url)
	}
	for _, image := range req.Images {
		add(image)
	}
	add(req.Image)
	add(req.InputReference)
	return images
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}

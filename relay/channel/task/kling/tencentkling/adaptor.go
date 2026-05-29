package tencentkling

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/kling/klingcommon"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	vod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vod/v20180717"
)

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	baseURL     string

	SecretId  string
	SecretKey string
	SubAppId  *uint64
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl

	// 处理秘钥
	parts := strings.Split(info.ApiKey, "|")
	if len(parts) == 3 {
		if subAppId, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64); err == nil {
			a.SubAppId = &subAppId
			a.SecretId = strings.TrimSpace(parts[1])
			a.SecretKey = strings.TrimSpace(parts[2])
		}
	}
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// Use the standard validation method for TaskSubmitReq
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); err != nil {
		return err
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return &dto.TaskError{
			Code:       "invalid_request",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
			LocalError: true,
			Error:      err,
		}
	}

	sdkRequest, err := a.buildCreateAigcVideoTaskRequest(&req, info)
	if err != nil {
		return &dto.TaskError{
			Code:       "invalid_request",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
			LocalError: true,
			Error:      err,
		}
	}

	// 参数写入
	c.Set("vod_request", sdkRequest)

	return nil
}

func (a *TaskAdaptor) buildCreateAigcVideoTaskRequest(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*vod.CreateAigcVideoTaskRequest, error) {
	request, err := a.convertToRequestPayload(req)
	if err != nil {
		return nil, err
	}

	// 解析和版本
	if modelVersion, ok := ModelVersionMap[info.UpstreamModelName]; ok {
		request.ModelVersion = tccommon.StringPtr(modelVersion)
	} else {
		return nil, errors.New("不支持的模型版本")
	}

	// 验证时长
	if !slices.Contains([]float64{5, 10}, ptrValue(request.OutputConfig.Duration)) {
		return nil, errors.New("kling时长只支持5秒和10秒")
	}
	// 验证分辨率
	size := ptrValue(request.OutputConfig.Resolution)
	if size != "" {
		size = strings.ToUpper(size)

		if !slices.Contains(resolutionMap[ModelKling], size) {
			return nil, errors.New("size格式不正确, 可选值为 720P、1080P、2K、4K")
		}
		request.OutputConfig.Resolution = tccommon.StringPtr(size)
	} else {
		request.OutputConfig.Resolution = tccommon.StringPtr("720P")
	}

	request.SubAppId = a.SubAppId
	// 模型名称
	request.ModelName = tccommon.StringPtr(string(ModelKling))

	return request, nil
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*vod.CreateAigcVideoTaskRequest, error) {
	payload := &klingcommon.RequestPayload{
		//ModelName:      req.Model,
		Prompt:         req.Prompt,
		Mode:           taskcommon.DefaultString(req.Size, "720P"),
		Duration:       req.Duration,
		KlingType:      "text_to_video",
		NegativePrompt: "",
		AspectRatio:    "",
		SoundEnable:    false,
		TextToVideo:    klingcommon.TextToVideo{},
		ImageToVideo: klingcommon.ImageToVideo{
			Image:     req.Image,
			ImageTail: "",
		},
	}

	if err := taskcommon.UnmarshalMetadata(req.Metadata, payload); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	r := vod.NewCreateAigcVideoTaskRequest()
	r.Prompt = tccommon.StringPtr(payload.Prompt)

	switch payload.KlingType {
	case "text_to_video":

	case "image_to_video":
		r.FileInfos = append(r.FileInfos, &vod.AigcVideoTaskInputFileInfo{
			Type:     tccommon.StringPtr("Url"),
			Category: tccommon.StringPtr("Image"),
			Url:      tccommon.StringPtr(payload.ImageToVideo.Image),
			Usage:    tccommon.StringPtr("FirstFrame"),
		})

		// 首尾帧
		if payload.ImageToVideo.ImageTail != "" {
			r.LastFrameUrl = tccommon.StringPtr(payload.ImageToVideo.ImageTail)
		}
	}

	if payload.NegativePrompt != "" {
		r.NegativePrompt = tccommon.StringPtr(payload.NegativePrompt)
	}

	duration := float64(payload.Duration)

	AudioGeneration := "Disabled"
	if payload.SoundEnable {
		AudioGeneration = "Enabled"
	}

	r.OutputConfig = &vod.AigcVideoOutputConfig{
		StorageMode: tccommon.StringPtr("Temporary"),
		Duration:    &duration,

		Resolution:      tccommon.StringPtr(payload.Mode),    // 分辨率
		AudioGeneration: tccommon.StringPtr(AudioGeneration), // 是否开启音频
	}

	if payload.AspectRatio != "" {
		r.OutputConfig.AspectRatio = tccommon.StringPtr(payload.AspectRatio) // 视频宽高比
	}

	return r, nil
}

func getVodRequest(c *gin.Context) (*vod.CreateAigcVideoTaskRequest, error) {
	v, exists := c.Get("vod_request")
	if !exists {
		return nil, fmt.Errorf("vod request not found in context")
	}
	req, ok := v.(*vod.CreateAigcVideoTaskRequest)
	if !ok {
		return nil, fmt.Errorf("invalid vod request type")
	}
	return req, nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return "https://vod.tencentcloudapi.com", nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, _ *http.Request, _ *relaycommon.RelayInfo) error {
	return nil
}

// BuildRequestBody 转换为腾讯vod参数
func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, _ *relaycommon.RelayInfo) (io.Reader, error) {
	return nil, nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
	sdkRequest, err := getVodRequest(c)
	if err != nil {
		return nil, err
	}

	// 判断视频生成类型
	info.Action = getTaskAction(sdkRequest)

	client, err := a.newTencentVodClient(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, err
	}

	response, err := client.CreateAigcVideoTask(sdkRequest)
	if err != nil {
		var sdkErr *tcerrors.TencentCloudSDKError
		if errors.As(err, &sdkErr) {
			return nil, fmt.Errorf("create aigc video task failed: code=%s, message=%s, request_id=%s", sdkErr.Code, sdkErr.Message, sdkErr.RequestId)
		}
		return nil, fmt.Errorf("create aigc video task failed: %w", err)
	}
	if response == nil || response.Response == nil {
		return nil, errors.New("create aigc video task returned empty response")
	}

	return buildTencentVodHTTPResponse(http.StatusOK, response)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var kResp vod.CreateAigcVideoTaskResponse
	err = common.Unmarshal(responseBody, &kResp)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	return *kResp.Response.TaskId, responseBody, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	sdkRequest, err := getVodRequest(c)
	if err != nil || sdkRequest == nil {
		return nil
	}
	sessionId := strings.TrimSpace(info.PublicTaskID)
	if sessionId != "" {
		sdkRequest.SessionId = &sessionId
	}

	modelPrice := NewModelBilling(sdkRequest)

	return modelPrice.CalculatePrice()
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	client, err := a.newTencentVodClient(proxy)
	if err != nil {
		return nil, err
	}

	request := vod.NewDescribeTaskDetailRequest()
	request.TaskId = &taskID
	request.SubAppId = a.SubAppId

	response, err := client.DescribeTaskDetail(request)
	if err != nil {
		if sdkErr, ok := err.(*tcerrors.TencentCloudSDKError); ok {
			return nil, fmt.Errorf("tencent vod describe task detail failed: code=%s, message=%s, request_id=%s", sdkErr.Code, sdkErr.Message, sdkErr.RequestId)
		}
		return nil, fmt.Errorf("tencent vod describe task detail failed: %w", err)
	}
	if response == nil || response.Response == nil {
		return nil, errors.New("tencent vod describe task detail returned empty response")
	}

	return buildTencentVodHTTPResponse(http.StatusOK, response)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{""}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "TencentVod"
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	taskInfo := &relaycommon.TaskInfo{}

	var resPayload vod.DescribeTaskDetailResponse
	err := common.Unmarshal(respBody, &resPayload)
	if err != nil {
		return nil, err
	}

	aigcVideoTask := resPayload.Response.AigcVideoTask

	taskInfo.Code = int(*aigcVideoTask.ErrCode)

	taskInfo.TaskID = *aigcVideoTask.TaskId
	taskInfo.Reason = *aigcVideoTask.Message

	status := *resPayload.Response.Status
	switch status {
	case "WAITING":
		taskInfo.Status = model.TaskStatusQueued
	case "PROCESSING":
		taskInfo.Status = model.TaskStatusInProgress
	case "FINISH":
		if ptrValue(aigcVideoTask.ErrCode) != 0 {
			taskInfo.Status = model.TaskStatusFailure
		} else {
			taskInfo.Status = model.TaskStatusSuccess
			if videos := aigcVideoTask.Output.FileInfos; len(videos) > 0 {
				video := videos[0]
				taskInfo.Url = *video.FileUrl
			}
		}
	case "ABORTED":
		taskInfo.Status = model.TaskStatusFailure
	default:
		return nil, fmt.Errorf("unknown task status: %s", status)
	}

	return taskInfo, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var taskDetail vod.DescribeTaskDetailResponse
	if err := common.Unmarshal(originTask.Data, &taskDetail); err != nil {
		return nil, errors.Wrap(err, "unmarshal task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = parseTencentVodTime(taskDetail.Response.CreateTime)
	openAIVideo.CompletedAt = parseTencentVodTime(taskDetail.Response.FinishTime)

	aigcVideoTask := taskDetail.Response.AigcVideoTask

	if len(aigcVideoTask.Output.FileInfos) > 0 {
		video := aigcVideoTask.Output.FileInfos[0]
		if *video.FileUrl != "" {
			openAIVideo.SetMetadata("url", *video.FileUrl)
		}
		duration := strconv.FormatFloat(*video.MetaData.Duration, 'f', -1, 64)

		openAIVideo.Seconds = duration
	}

	// https://app.klingai.com/cn/dev/document-api/apiReference/model/textToVideo
	if *aigcVideoTask.ErrCode != 0 {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: *aigcVideoTask.Message,
		}
	}
	return common.Marshal(openAIVideo)
}

func (a *TaskAdaptor) newTencentVodClient(proxyURL string) (*vod.Client, error) {
	credential := tccommon.NewCredential(a.SecretId, a.SecretKey)

	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile.Endpoint = "vod.tencentcloudapi.com"
	if proxyURL != "" {
		clientProfile.HttpProfile.Proxy = proxyURL
	}
	if common.RelayTimeout > 0 {
		clientProfile.HttpProfile.ReqTimeout = common.RelayTimeout
	}

	client, err := vod.NewClient(credential, "", clientProfile)
	if err != nil {
		return nil, fmt.Errorf("create tencent vod client failed: %w", err)
	}
	return client, nil
}

//func buildTencentVodFileInfo(source string, usage string) (*vod.AigcVideoTaskInputFileInfo, error) {
//	value := strings.TrimSpace(source)
//	if value == "" {
//		return nil, errors.New("tencent vod input file is empty")
//	}
//
//	fileInfo := &vod.AigcVideoTaskInputFileInfo{
//		Category: stringPtr("Image"),
//		Usage:    &usage,
//	}
//	if isHTTPURL(value) {
//		fileInfo.Type = stringPtr("Url")
//		fileInfo.Url = &value
//		return fileInfo, nil
//	}
//
//	fileInfo.Type = stringPtr("File")
//	fileInfo.FileId = &value
//	return fileInfo, nil
//}

func buildTencentVodHTTPResponse(statusCode int, payload any) (*http.Response, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tencent vod response failed: %w", err)
	}

	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}, nil
}

func normalizeTencentVodModel(raw string) (string, string) {
	result := strings.SplitN(raw, "-", 2)
	if len(result) != 2 {
		return result[0], ""
	}

	return result[0], result[1]
}

func parseTencentVodTime(value *string) int64 {
	text := strings.TrimSpace(ptrValue(value))
	if text == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}

func pickFirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func getTaskAction(req *vod.CreateAigcVideoTaskRequest) string {
	if len(req.FileInfos) > 0 {
		fileInfo := req.FileInfos[0]

		if ptrValue(fileInfo.Usage) == "Reference" {
			return constant.TaskActionReferenceGenerate
		}

		if ptrValue(req.LastFrameUrl) != "" || ptrValue(req.LastFrameFileId) != "" {
			return constant.TaskActionFirstTailGenerate
		}

		if ptrValue(fileInfo.FileId) == "" && ptrValue(fileInfo.Url) == "" {
			return constant.TaskActionTextGenerate
		}

		return constant.TaskActionGenerate
	}

	return constant.TaskActionTextGenerate
}

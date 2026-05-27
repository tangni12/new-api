package shubiaoveo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.apiKey = strings.TrimSpace(info.ApiKey)
	a.baseURL = strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	if a.baseURL == "" {
		a.baseURL = defaultBaseURL
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(info.OriginModelName)
	}
	if !isSupportedModel(modelName) {
		return "", fmt.Errorf("unsupported shubiao veo model: %s", modelName)
	}
	return fmt.Sprintf("%s/vertex/v1/publishers/google/models/%s:predictLongRunning", a.baseURL, modelName), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	instances, err := buildInstances(c, info, req)
	if err != nil {
		return nil, err
	}
	params, err := buildParameters(req)
	if err != nil {
		return nil, err
	}

	payload := veoRequestPayload{
		Instances:  instances,
		Parameters: params,
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var s submitResponse
	if err := common.Unmarshal(responseBody, &s); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
	}
	taskID = extractTaskID(firstNonEmpty(s.TaskID, s.ID, s.Name))
	if taskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("missing task id"), "invalid_response", http.StatusInternalServerError)
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = info.PublicTaskID
	openAIVideo.TaskID = info.PublicTaskID
	openAIVideo.CreatedAt = time.Now().Unix()
	openAIVideo.Model = info.OriginModelName
	c.JSON(http.StatusOK, openAIVideo)

	return taskID, responseBody, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	return buildBillingRatios(req, firstNonEmpty(info.UpstreamModelName, info.OriginModelName))
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	baseUrl = strings.TrimRight(strings.TrimSpace(baseUrl), "/")
	if baseUrl == "" {
		baseUrl = defaultBaseURL
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/tasks/%s", baseUrl, taskID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", strings.TrimSpace(key))

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var task taskQueryResponse
	if err := common.Unmarshal(respBody, &task); err != nil {
		return nil, fmt.Errorf("unmarshal shubiao task response failed: %w", err)
	}

	info := &relaycommon.TaskInfo{
		TaskID:   firstNonEmpty(task.TaskID, task.ID),
		Reason:   firstNonEmpty(task.FailReason, taskErrorMessage(task.Error)),
		Url:      task.ResultURL,
		Progress: task.Progress,
	}
	if info.Progress == "" {
		info.Progress = "50%"
	}

	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "completed", "success", "succeeded", "done":
		info.Status = model.TaskStatusSuccess
		info.Progress = "100%"
	case "failed", "failure", "error":
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		if info.Reason == "" {
			info.Reason = "task failed"
		}
	case "submitted", "created":
		info.Status = model.TaskStatusSubmitted
	case "queued", "pending":
		info.Status = model.TaskStatusQueued
	case "processing", "running", "in_progress", "":
		info.Status = model.TaskStatusInProgress
	default:
		return nil, fmt.Errorf("unknown shubiao task status: %s", task.Status)
	}
	return info, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return supportedModels
}

func (a *TaskAdaptor) GetChannelName() string {
	return "ShubiaoVeo"
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := dto.NewOpenAIVideo()
	video.ID = task.TaskID
	video.Model = firstNonEmpty(task.Properties.OriginModelName, task.Properties.UpstreamModelName)
	video.Status = task.Status.ToVideoStatus()
	video.SetProgressStr(task.Progress)
	video.CreatedAt = task.CreatedAt
	if task.FinishTime > 0 {
		video.CompletedAt = task.FinishTime
	} else {
		video.CompletedAt = task.UpdatedAt
	}
	if resultURL := task.GetResultURL(); resultURL != "" && task.Status == model.TaskStatusSuccess {
		video.SetMetadata("url", resultURL)
	}
	if task.Status == model.TaskStatusFailure && strings.TrimSpace(task.FailReason) != "" {
		video.Error = &dto.OpenAIVideoError{Message: task.FailReason}
	}
	return common.Marshal(video)
}

func isSupportedModel(modelName string) bool {
	for _, item := range supportedModels {
		if item == modelName {
			return true
		}
	}
	return false
}

func extractTaskID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func taskErrorMessage(errInfo *taskErrorInfo) string {
	if errInfo == nil {
		return ""
	}
	return strings.TrimSpace(errInfo.Message)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildInstances(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (any, error) {
	commonInstance, err := buildCommonInstance(c, info, req)
	if err != nil {
		return nil, err
	}
	if rawInstances, ok := req.Metadata["instances"]; ok {
		instances, err := mergeMetadataInstances(rawInstances, commonInstance)
		if err != nil {
			return nil, err
		}
		setActionFromInstance(instances[0], info)
		return instances, nil
	}
	return []map[string]any{commonInstance}, nil
}

func buildCommonInstance(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (map[string]any, error) {
	instance := map[string]any{
		"prompt": req.Prompt,
	}
	if images := extractMultipartImages(c); len(images) > 0 {
		applyVeoImages(instance, images, info)
		return instance, nil
	}
	if len(req.Images) > 0 {
		images := parseImageInputs(req.Images)
		if len(images) == 0 {
			return nil, fmt.Errorf("invalid image input: only supports data URI base64 or raw base64 in image/images")
		}
		applyVeoImages(instance, images, info)
	}
	return instance, nil
}

func mergeMetadataInstances(rawInstances any, commonInstance map[string]any) ([]map[string]any, error) {
	var instances []map[string]any
	if err := unmarshalAny(rawInstances, &instances); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata instances failed")
	}
	if len(instances) == 0 {
		instances = []map[string]any{{}}
	}
	mergeCommonInstanceDefaults(instances[0], commonInstance)
	return instances, nil
}

func mergeCommonInstanceDefaults(instance map[string]any, commonInstance map[string]any) {
	for _, key := range []string{"prompt", "image", "lastFrame"} {
		if isEmptyInstanceField(instance[key]) {
			if value, ok := commonInstance[key]; ok && !isEmptyInstanceField(value) {
				instance[key] = value
			}
		}
	}
}

func isEmptyInstanceField(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func applyVeoImages(instance map[string]any, images []*veoImageInput, info *relaycommon.RelayInfo) {
	instance["image"] = images[0]
	info.Action = constant.TaskActionGenerate
	if len(images) > 1 {
		instance["lastFrame"] = images[1]
		info.Action = constant.TaskActionFirstTailGenerate
	}
}

func setActionFromInstance(instance map[string]any, info *relaycommon.RelayInfo) {
	if !isEmptyInstanceField(instance["referenceImages"]) {
		info.Action = constant.TaskActionReferenceGenerate
		return
	}
	if !isEmptyInstanceField(instance["image"]) && !isEmptyInstanceField(instance["lastFrame"]) {
		info.Action = constant.TaskActionFirstTailGenerate
		return
	}
	if !isEmptyInstanceField(instance["image"]) {
		info.Action = constant.TaskActionGenerate
	}
}

func extractMultipartImages(c *gin.Context) []*veoImageInput {
	mf, err := c.MultipartForm()
	if err != nil {
		return nil
	}
	files, exists := mf.File["input_reference"]
	if !exists || len(files) == 0 {
		return nil
	}
	images := make([]*veoImageInput, 0, 2)
	for _, fh := range files {
		if fh.Size > maxVeoImageSize {
			continue
		}
		file, err := fh.Open()
		if err != nil {
			continue
		}
		fileBytes, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil {
			continue
		}
		mimeType := fh.Header.Get("Content-Type")
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = http.DetectContentType(fileBytes)
		}
		images = append(images, &veoImageInput{
			BytesBase64Encoded: base64.StdEncoding.EncodeToString(fileBytes),
			MimeType:           mimeType,
		})
		if len(images) == 2 {
			break
		}
	}
	return images
}

func parseImageInputs(imageStrs []string) []*veoImageInput {
	images := make([]*veoImageInput, 0, 2)
	for _, imageStr := range imageStrs {
		if parsed := parseImageInput(imageStr); parsed != nil {
			images = append(images, parsed)
		}
		if len(images) == 2 {
			break
		}
	}
	return images
}

func parseImageInput(imageStr string) *veoImageInput {
	imageStr = strings.TrimSpace(imageStr)
	if imageStr == "" {
		return nil
	}

	if strings.HasPrefix(imageStr, "data:") {
		return parseDataURI(imageStr)
	}

	raw, err := base64.StdEncoding.DecodeString(imageStr)
	if err != nil {
		return nil
	}
	return &veoImageInput{
		BytesBase64Encoded: imageStr,
		MimeType:           http.DetectContentType(raw),
	}
}

func parseDataURI(uri string) *veoImageInput {
	rest := uri[len("data:"):]
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return nil
	}
	meta := rest[:idx]
	b64 := rest[idx+1:]
	if b64 == "" {
		return nil
	}

	mimeType := "application/octet-stream"
	parts := strings.SplitN(meta, ";", 2)
	if len(parts) >= 1 && parts[0] != "" {
		mimeType = parts[0]
	}

	return &veoImageInput{
		BytesBase64Encoded: b64,
		MimeType:           mimeType,
	}
}

func buildParameters(req relaycommon.TaskSubmitReq) (*shubiaoVeoParameters, error) {
	params := &shubiaoVeoParameters{
		DurationSeconds: defaultDurationSec,
		Resolution:      defaultResolution,
		SampleCount:     defaultSampleCount,
	}
	generateAudio := true
	params.GenerateAudio = &generateAudio

	if req.Duration > 0 {
		params.DurationSeconds = req.Duration
	} else if strings.TrimSpace(req.Seconds) != "" {
		params.DurationSeconds = resolveDuration(req)
	}
	if req.Size != "" {
		params.Resolution = normalizeResolution(req.Size)
		params.AspectRatio = sizeToVeoAspectRatio(req.Size)
	}

	if err := unmarshalMetadataWithoutNested(req.Metadata, params); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}
	if rawParams, ok := req.Metadata["parameters"]; ok {
		if err := unmarshalAny(rawParams, params); err != nil {
			return nil, errors.Wrap(err, "unmarshal metadata parameters failed")
		}
	}
	if params.DurationSeconds == 0 {
		params.DurationSeconds = defaultDurationSec
	}
	if params.Resolution == "" {
		params.Resolution = defaultResolution
	}
	params.Resolution = strings.ToLower(strings.TrimSpace(params.Resolution))
	// ShubiaoVeo 当前即使传 sampleCount>1 也只返回一条视频，先固定为 1，
	// 避免请求参数和预扣费产生不一致。
	params.SampleCount = defaultSampleCount
	if params.GenerateAudio == nil {
		generateAudio := true
		params.GenerateAudio = &generateAudio
	}
	return params, nil
}

func sizeToVeoAspectRatio(size string) string {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(size)), "x", 2)
	if len(parts) != 2 {
		return "16:9"
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w <= 0 || h <= 0 {
		return "16:9"
	}
	if h > w {
		return "9:16"
	}
	return "16:9"
}

func unmarshalMetadataWithoutNested(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	copyMap := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if k == "model" || k == "instances" || k == "parameters" {
			continue
		}
		copyMap[k] = v
	}
	return unmarshalAny(copyMap, target)
}

func unmarshalAny(value any, target any) error {
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	return common.Unmarshal(data, target)
}

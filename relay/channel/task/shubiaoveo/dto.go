package shubiaoveo

type submitResponse struct {
	Name   string `json:"name"`
	TaskID string `json:"task_id"`
	ID     string `json:"id"`
}

type veoRequestPayload struct {
	Instances  any                   `json:"instances"`
	Parameters *shubiaoVeoParameters `json:"parameters,omitempty"`
}

type veoImageInput struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	MimeType           string `json:"mimeType"`
}

type shubiaoVeoParameters struct {
	SampleCount        int    `json:"sampleCount"`
	DurationSeconds    int    `json:"durationSeconds,omitempty"`
	AspectRatio        string `json:"aspectRatio,omitempty"`
	Resolution         string `json:"resolution,omitempty"`
	NegativePrompt     string `json:"negativePrompt,omitempty"`
	PersonGeneration   string `json:"personGeneration,omitempty"`
	StorageUri         string `json:"storageUri,omitempty"`
	CompressionQuality string `json:"compressionQuality,omitempty"`
	ResizeMode         string `json:"resizeMode,omitempty"`
	Seed               *int   `json:"seed,omitempty"`
	GenerateAudio      *bool  `json:"generateAudio,omitempty"`
}

type taskQueryResponse struct {
	Action     string         `json:"action"`
	Created    int64          `json:"created"`
	Error      *taskErrorInfo `json:"error"`
	FailReason string         `json:"fail_reason"`
	FinishTime int64          `json:"finish_time"`
	ID         string         `json:"id"`
	Object     string         `json:"object"`
	Platform   string         `json:"platform"`
	Progress   string         `json:"progress"`
	Result     map[string]any `json:"result"`
	ResultURL  string         `json:"result_url"`
	StartTime  int64          `json:"start_time"`
	Status     string         `json:"status"`
	SubmitTime int64          `json:"submit_time"`
	TaskID     string         `json:"task_id"`
	TaskType   string         `json:"task_type"`
	UserID     int            `json:"user_id"`
}

type taskErrorInfo struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

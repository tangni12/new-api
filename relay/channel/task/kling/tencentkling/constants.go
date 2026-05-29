package tencentkling

type ModelName string

const (
	ModelKling ModelName = "Kling"
)

var (
	resolutionMap = map[ModelName][]string{
		ModelKling: {
			"720P",
			"1080P",
			"2K",
			"4K",
		},
	}

	// {
	//		"1.6",
	//		"2.0",
	//		"2.1",
	//		"2.5",
	//		"2.6",
	//		"O1",
	//		"3.0",
	//		"3.0-Omni",
	//	},
	ModelVersionMap = map[string]string{
		"kling-v1-6": "1.6",
		//"kling-v2-master":   "2.0",
		"kling-v2-1": "2.1",
		//"kling-v2-1-master": "2.1",
		"kling-v2-5-turbo": "2.5",
		"kling-v2-6":       "2.6",
		"kling-v3":         "3.0",
	}
)

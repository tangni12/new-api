package tencentvod

type ModelName string

const (
	ModelKling ModelName = "Kling"
)

var resolutionMap = map[ModelName][]string{
	ModelKling: {
		"720P",
		"1080P",
		"2K",
		"4K",
	},
}

var ModelVersionMap = map[ModelName][]string{
	ModelKling: {
		"1.6",
		"2.0",
		"2.1",
		"2.5",
		"2.6",
		"O1",
		"3.0",
		"3.0-Omni",
	},
}

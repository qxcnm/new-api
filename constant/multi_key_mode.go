package constant

type MultiKeyMode string

const (
	MultiKeyModeRandom     MultiKeyMode = "random"     // 随机
	MultiKeyModePolling    MultiKeyMode = "polling"    // 轮询
	MultiKeyModeSequential MultiKeyMode = "sequential" // 顺序：优先使用列表前面的可用 Key
)

func IsValidMultiKeyMode(mode MultiKeyMode) bool {
	return mode == MultiKeyModeRandom || mode == MultiKeyModePolling || mode == MultiKeyModeSequential
}

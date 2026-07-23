package models

// SystemMetrics は、サーバー（fly.ioインスタンス）のハードウェアリソース使用状況を表します。
type SystemMetrics struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	DiskSpace   float64 `json:"diskSpace"`
}

package models

// - エンドポイント別のレイテンシ集計データモデル
// - path+methodをキーに、avg/p95/p99/リクエスト数/エラー率を保持
type EndpointLatency struct {
	Method   string  `json:"method" bson:"method"`
	Path     string  `json:"path" bson:"path"`
	AvgMs    float64 `json:"avg_ms"`
	P95Ms    float64 `json:"p95_ms"`
	P99Ms    float64 `json:"p99_ms"`
	Count    int64   `json:"count"`
	ErrorPct float64 `json:"error_pct"`
}

// - Response Timeサマリーカード用データモデル
// - 全エンドポイントの合算平均/パーセンタイル/エラー率を格納
type ResponseTimeSummary struct {
	AvgMs        float64 `json:"avg_ms"`
	P95Ms        float64 `json:"p95_ms"`
	P99Ms        float64 `json:"p99_ms"`
	ErrorRatePct float64 `json:"error_rate_pct"`
}

// - Response Timeダッシュボード全体のレスポンスデータモデル
// - サマリーと上位10件の遅いエンドポイントを含む
type ResponseTimeMetrics struct {
	Summary   ResponseTimeSummary `json:"summary"`
	Endpoints []EndpointLatency   `json:"endpoints"`
}

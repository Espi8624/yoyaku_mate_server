package models

// ChartData はグラフ1本分のデータポイント（時間帯別 or 曜日別）を表す
type ChartData struct {
	Label     string `json:"label" bson:"label"` // X軸ラベル (例: "13時", "月")
	Value     int    `json:"value" bson:"value"`
	PrevValue int    `json:"prev_value" bson:"prev_value"` // 比較対象期間（昨日 or 先週同曜日）のデータ
}

// StatisticsResponse は統計API のレスポンス全体
// period="auto" の場合は「今日」を時間帯別(0〜23時)に、
// period="weekly" の場合は「今週(日〜土、常に直近の週固定)」を曜日別に集計する。
// 過去の期間へのナビゲーションは提供しない（機能を今日/今週に絞ったため）。
type StatisticsResponse struct {
	Period            string      `json:"period"`
	VisitorTotal      int         `json:"visitor_total"`
	VisitorGrowthRate float64     `json:"visitor_growth_rate"` // autoなら前日比、weeklyなら前週比
	CancelledTotal    int         `json:"cancelled_total"`
	NoShowTotal       int         `json:"no_show_total"`
	NoShowRate        float64     `json:"no_show_rate"`
	AverageWaitTime   string      `json:"average_wait_time"` // 例: "15分"
	WaitTimeSeconds   int         `json:"wait_time_seconds"`
	VisitorChart      []ChartData `json:"visitor_chart"`
	CancelledChart    []ChartData `json:"cancelled_chart"`
	NoShowChart       []ChartData `json:"no_show_chart"`
}

package models

type HourlyData struct {
	Hour  int `json:"hour" bson:"hour"`
	Count int `json:"count" bson:"count"`
}

type VisitorStats struct {
	Today           int     `json:"today"`
	Yesterday       int     `json:"yesterday"`
	LastWeekSameDay int     `json:"last_week_same_day"`
	WowGrowthRate   float64 `json:"wow_growth_rate"` // 前週比成長率
	DodGrowthRate   float64 `json:"dod_growth_rate"` // 前日比成長率
}

type ChartData struct {
	Label     string `json:"label" bson:"label"` // X軸ラベル (例: "Mon", "1日", "1月")
	Value     int    `json:"value" bson:"value"`
	PrevValue int    `json:"prev_value" bson:"prev_value"` // 前期間のデータ
}

type StatisticsResponse struct {
	VisitorStats       VisitorStats `json:"visitor_stats"`
	HourlyCongestion   []HourlyData `json:"hourly_congestion"`
	ChartData          []ChartData  `json:"chart_data"`        // 選択された期間のチャートデータ
	AverageWaitTime    string       `json:"average_wait_time"` // 例: "15分"
	WaitTimeSeconds    int          `json:"wait_time_seconds"`
	NoShowRate         float64      `json:"no_show_rate"`
	NoShowChartData    []ChartData  `json:"no_show_chart_data"`   // 期間ごとのNo-Show数
	CancelledChartData []ChartData  `json:"cancelled_chart_data"` // 期間ごとのキャンセル数
}

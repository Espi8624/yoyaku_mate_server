package handlers

import (
	"context"
	"net/http"
	"time"

	"yoyaku_mate_server/db"
)

// HealthHandler は外部の死活監視サービス(UptimeRobot等)がpingするための無認証エンドポイント。
// - MongoDBへの疎通を確認し、DBが応答しない場合は503を返す
// - fly.ioのauto_stop_machines設定でマシンが停止していた場合はauto_start_machinesにより
//   このリクエスト自体がマシンを起こすため、コールドスタート分のレイテンシが乗ることがある
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	client := db.Client()
	if client == nil {
		respondWithError(w, "Database client is not initialized", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx, nil); err != nil {
		respondWithError(w, "Database ping failed", http.StatusServiceUnavailable)
		return
	}

	respondWithJSON(w, map[string]string{"status": "ok"}, http.StatusOK)
}

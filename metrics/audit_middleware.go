package metrics

import (
	"context"
	"net/http"
	"time"
	"yoyaku_mate_server/models"
)

// - Contextキー型（他パッケージとのキー衝突防止）
type auditContextKeyType struct{}

var auditContextKey = auditContextKeyType{}

// - ハンドラーとミドルウェア間で監査イベントを共有するポインター構造体
// - ハンドラーで値を設定すると、ミドルウェアが同一ポインターを介して読み取る
type AuditEvent struct {
	Action  string
	Target  string
	Details string
	filled  bool
}

// - リクエスト開始時にミドルウェアが AuditEvent ポインターをContextに挿入
func withAuditEvent(r *http.Request) (*http.Request, *AuditEvent) {
	event := &AuditEvent{}
	return r.WithContext(context.WithValue(r.Context(), auditContextKey, event)), event
}

// - ハンドラーから呼び出し、監査イベント情報をContextに設定
// - 単一管理者ローカル環境のため、実行者(actor)情報は記録しない
func SetAuditContext(r *http.Request, action, target, details string) {
	event, ok := r.Context().Value(auditContextKey).(*AuditEvent)
	if !ok || event == nil {
		return
	}
	event.Action = action
	event.Target = target
	event.Details = details
	event.filled = true
}

// - Admin subrouter 専用監査ログミドルウェア
// - 既存の MetricsMiddleware の responseWriterWrapper パターンを再利用して StatusCode を検知
// - ハンドラー終了後に AuditEvent ポインターから監査情報を抽出し AuditTracker に非同期記録
func AuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// - AuditEvent ポインターをContextに注入しハンドラーと共有
		r, event := withAuditEvent(r)

		rw := &responseWriterWrapper{w, http.StatusOK}
		next.ServeHTTP(rw, r)

		// - ハンドラーが SetAuditContext を呼び出していない場合は記録をスキップ（GETリクエスト等）
		if !event.filled {
			return
		}

		// - HTTPステータスコードに基づいて処理結果を判定
		status := "SUCCESS"
		if rw.statusCode >= 400 {
			status = "FAILED"
		}

		GetAuditTracker().RecordAudit(models.AuditLog{
			Timestamp: time.Now().UTC(),
			Action:    event.Action,
			Target:    event.Target,
			Status:    status,
			Details:   event.Details,
		})
	})
}

package db

import (
	"strings"
	"testing"
)

// TestMaskMongoURI は接続文字列がログへ出る際に認証情報が残らないことを検証する。
// このマスキングが外れるとfly.ioのログにDBのパスワードが平文で蓄積される
func TestMaskMongoURI(t *testing.T) {
	const password = "s3cr3tP4ssw0rd"

	tests := []struct {
		name string
		uri  string
		// 出力に必ず含まれるべき文字列 (接続先の特定に必要な情報)
		wantContains []string
	}{
		{
			name:         "mongodb+srv (Atlas)",
			uri:          "mongodb+srv://appuser:" + password + "@cluster0.example.mongodb.net/project_rusui?retryWrites=true&w=majority",
			wantContains: []string{"cluster0.example.mongodb.net", "project_rusui"},
		},
		{
			name:         "標準スキーム",
			uri:          "mongodb://appuser:" + password + "@localhost:27017/project_rusui",
			wantContains: []string{"localhost:27017", "project_rusui"},
		},
		{
			name:         "認証情報なし",
			uri:          "mongodb://localhost:27017/project_rusui",
			wantContains: []string{"localhost:27017", "project_rusui"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskMongoURI(tt.uri)

			if strings.Contains(got, password) {
				t.Fatalf("パスワードが出力に残っている: %q", got)
			}
			if strings.Contains(got, "appuser") {
				t.Fatalf("ユーザー名が出力に残っている: %q", got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("接続先の特定に必要な %q が欠けている: %q", want, got)
				}
			}
		})
	}
}

// TestMaskMongoURIUnparsable は解析できないURIで原文へフォールバックしないことを検証する。
// フォールバックすると「伏せ字にしたつもりが出ている」最悪の状態になる
func TestMaskMongoURIUnparsable(t *testing.T) {
	const broken = "mongodb://user:pass@%%%invalid host/db"

	got := maskMongoURI(broken)

	if strings.Contains(got, "pass") || strings.Contains(got, "invalid host") {
		t.Fatalf("解析失敗時に原文が漏れている: %q", got)
	}
}

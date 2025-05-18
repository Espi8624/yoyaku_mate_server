package handlers

import (
	"net/http"
)

// 基本ハンドラー
func CustomerHomeHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, This is Yoyaku Mate Server."))
}

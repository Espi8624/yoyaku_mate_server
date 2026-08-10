package handlers

import (
	"net/http"
	"strings"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// StoreListRepository ユーザーの店舗リスト取得を抽象化するインターフェース
type StoreListRepository interface {
	GetStoresByFirebaseUID(firebaseUID string) ([]data.StoreWithStatus, error)
}

// StoreListHandler 店舗リスト取得関連のHTTPリクエストを処理するハンドラ
type StoreListHandler struct {
	storeRepo StoreListRepository
	authSvc   AuthService
}

func NewStoreListHandler(storeRepo StoreListRepository, authSvc AuthService) *StoreListHandler {
	return &StoreListHandler{
		storeRepo: storeRepo,
		authSvc:   authSvc,
	}
}

// GetMyStoresHandler 保有全店舗リスト取得
func (h *StoreListHandler) GetMyStoresHandler(w http.ResponseWriter, r *http.Request) {
	// Authorizationヘッダーからトークンを抽出
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	if idToken == authHeader { // "Bearer "が含まれていない場合
		utils.RespondWithError(w, "Invalid Authorization header format", http.StatusUnauthorized)
		return
	}

	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	stores, err := h.storeRepo.GetStoresByFirebaseUID(firebaseUID)
	if err != nil {
		utils.RespondWithError(w, "Failed to retrieve stores", http.StatusInternalServerError)
		return
	}

	// データ形式: {"data": [...]}
	utils.RespondWithJSON(w, map[string]interface{}{"data": stores}, http.StatusOK)
}

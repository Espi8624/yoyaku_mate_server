package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"yoyaku_mate_server/metrics"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/safego"
	"yoyaku_mate_server/storage"

	"github.com/gorilla/mux"
)

// UploadUserRepository ユーザー画像更新を抽象化するインターフェース
type UploadUserRepository interface {
	UpdateUserImageURL(firebaseUID string, imageURL string) (*models.User, error)
}

// UploadStoreRepository 店舗画像更新を抽象化するインターフェース
type UploadStoreRepository interface {
	UpdateStoreImageURL(storeID string, imageURL string) (*models.Store, error)
	UpdateLicenseInfoAfterUpload(storeID string, imageURL string) (*models.StoreLicense, error)
}

// UploadMenuRepository メニュー画像更新を抽象化するインターフェース
type UploadMenuRepository interface {
	UpdateMenuImageURL(menuID string, imageURL string) (*models.MenuList, error)
}

// UploadHandler はストレージクライアント(R2)を依存関係として持つ
type UploadHandler struct {
	Storage            *storage.Client
	MenuRepo           UploadMenuRepository
	UserRepo           UploadUserRepository
	StoreRepo          UploadStoreRepository
	AuthSvc            AuthService
	AssetsBucketName   string
	AssetsPublicDomain string
	BizBucketName      string
	// - 営業許可証の新規申請をSlackへ通知するためのWebhook URL。空の場合は通知しない
	SlackWebhookURL string
}

// ハンドラ初期化
func NewUploadHandler(storageClient *storage.Client, menuRepo UploadMenuRepository, userRepo UploadUserRepository, storeRepo UploadStoreRepository, authSvc AuthService, assetsBucket, assetsPublicDomain, bizBucket, slackWebhookURL string) *UploadHandler {
	return &UploadHandler{
		Storage:            storageClient,
		MenuRepo:           menuRepo,
		UserRepo:           userRepo,
		StoreRepo:          storeRepo,
		AuthSvc:            authSvc,
		AssetsBucketName:   assetsBucket,
		AssetsPublicDomain: assetsPublicDomain,
		BizBucketName:      bizBucket,
		SlackWebhookURL:    slackWebhookURL,
	}
}

// 営業許可証のアップロードリクエストを処理
func (h *UploadHandler) UploadLicense(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Storage == nil {
		respondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodPost {
		respondWithError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// formData をパース (最大 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	// formData から 'storeId' を取得
	storeID := r.FormValue("storeId")
	if storeID == "" {
		respondWithError(w, "Invalid storeId", http.StatusBadRequest)
		return
	}

	// formData から 'licenseImage' ファイルを取得
	file, header, err := r.FormFile("licenseImage")
	if err != nil {
		respondWithError(w, "Could not get uploaded file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileKey, err := h.Storage.UploadFile(h.BizBucketName, "", file, header)
	if err != nil {
		log.Printf("Error uploading file: %v", err)
		respondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DB にファイルの URL とステータスを更新し、更新後のライセンスドキュメントを取得
	updatedLicense, err := h.StoreRepo.UpdateLicenseInfoAfterUpload(storeID, fileKey)
	if err != nil {
		log.Printf("Error updating database: %v", err)
		respondWithError(w, "Could not update store information", http.StatusInternalServerError)
		return
	}

	// - 審査待ちの新規申請が発生したことをSlackへ通知する(応答を遅らせないよう非同期で送信)
	slackMessage := fmt.Sprintf(
		":clipboard: 新しい営業許可証の承認リクエストが届きました\n店舗ID: %s\n管理画面のLicense Approvalから確認してください。",
		storeID,
	)
	safego.Go("slack_license_request_notify", func() {
		metrics.SendSlackMessage(h.SlackWebhookURL, slackMessage)
	})

	// REST 標準: POST レスポンスに更新後のリソースを返却 (200 OK)
	respondWithJSON(w, updatedLicense, http.StatusOK)
}

func (h *UploadHandler) UploadMenuImage(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Storage == nil {
		respondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	// menuId取得
	vars := mux.Vars(r)
	menuId := vars["menuId"]

	// 'menuImage'ファイルをマルチパートフォームから取得
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("menuImage")
	if err != nil {
		respondWithError(w, "Could not get uploaded file named 'menuImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Storage.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading file to storage: %v", err)
		respondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBのメニュー情報をアップデート (DI利用)
	updatedMenu, err := h.MenuRepo.UpdateMenuImageURL(menuId, fileURL)
	if err != nil {
		log.Printf("Error updating menu image URL in database: %v", err)
		respondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, updatedMenu, http.StatusOK)
}

func (h *UploadHandler) UploadUserImage(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Storage == nil {
		respondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	// 認証情報取得と検証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		respondWithError(w, "Unauthorized: Authorization header not found", http.StatusUnauthorized)
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	if idToken == authHeader {
		respondWithError(w, "Unauthorized: Invalid token format", http.StatusUnauthorized)
		return
	}

	firebaseUID, err := h.AuthSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		respondWithError(w, "Unauthorized: Invalid ID token", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("userImage")
	if err != nil {
		respondWithError(w, "Could not get uploaded file named 'userImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Storage.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading user to storage: %v", err)
		respondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBアップデート
	updatedUser, err := h.UserRepo.UpdateUserImageURL(firebaseUID, fileURL)
	if err != nil {
		log.Printf("Error updating user image URL in DB: %v", err)
		respondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, updatedUser, http.StatusOK)
}

func (h *UploadHandler) UploadStoreImage(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Storage == nil {
		respondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	// storeId取得
	vars := mux.Vars(r)
	storeId := vars["storeId"]

	// 'storeImage'ファイルをマルチパートフォームから取得
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("storeImage")
	if err != nil {
		respondWithError(w, "Could not get uploaded file named 'storeImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Storage.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading store image to storage: %v", err)
		respondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBの店舗情報をアップデート
	updatedStore, err := h.StoreRepo.UpdateStoreImageURL(storeId, fileURL)
	if err != nil {
		log.Printf("Error updating store image URL in DB: %v", err)
		respondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, updatedStore, http.StatusOK)
}

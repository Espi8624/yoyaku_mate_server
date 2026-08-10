package handlers

import (
	"log"
	"net/http"
	"strings"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

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

// UploadHandler は Minio クライアントを依存関係として持つ
type UploadHandler struct {
	Minio              *data.MinioClient
	MenuRepo           data.MenuRepository
	UserRepo           UploadUserRepository
	StoreRepo          UploadStoreRepository
	AuthSvc            AuthService
	AssetsBucketName   string
	AssetsPublicDomain string
	BizBucketName      string
}

// ハンドラ初期化
func NewUploadHandler(minio *data.MinioClient, menuRepo data.MenuRepository, userRepo UploadUserRepository, storeRepo UploadStoreRepository, authSvc AuthService, assetsBucket, assetsPublicDomain, bizBucket string) *UploadHandler {
	return &UploadHandler{
		Minio:              minio,
		MenuRepo:           menuRepo,
		UserRepo:           userRepo,
		StoreRepo:          storeRepo,
		AuthSvc:            authSvc,
		AssetsBucketName:   assetsBucket,
		AssetsPublicDomain: assetsPublicDomain,
		BizBucketName:      bizBucket,
	}
}

// 営業許可証のアップロードリクエストを処理
func (h *UploadHandler) UploadLicense(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Minio == nil {
		utils.RespondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// formData をパース (最大 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	// formData から 'storeId' を取得
	storeID := r.FormValue("storeId")
	if storeID == "" {
		utils.RespondWithError(w, "Invalid storeId", http.StatusBadRequest)
		return
	}

	// formData から 'licenseImage' ファイルを取得
	file, header, err := r.FormFile("licenseImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileKey, err := h.Minio.UploadFile(h.BizBucketName, "", file, header)
	if err != nil {
		log.Printf("Error uploading file: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DB にファイルの URL とステータスを更新し、更新後のライセンスドキュメントを取得
	updatedLicense, err := h.StoreRepo.UpdateLicenseInfoAfterUpload(storeID, fileKey)
	if err != nil {
		log.Printf("Error updating database: %v", err)
		utils.RespondWithError(w, "Could not update store information", http.StatusInternalServerError)
		return
	}

	// REST 標準: POST レスポンスに更新後のリソースを返却 (200 OK)
	utils.RespondWithJSON(w, updatedLicense, http.StatusOK)
}

func (h *UploadHandler) UploadMenuImage(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Minio == nil {
		utils.RespondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	// menuId取得
	vars := mux.Vars(r)
	menuId := vars["menuId"]

	// 'menuImage'ファイルをマルチパートフォームから取得
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("menuImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file named 'menuImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Minio.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading file to Minio: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBのメニュー情報をアップデート (DI利用)
	updatedMenu, err := h.MenuRepo.UpdateMenuImageURL(menuId, fileURL)
	if err != nil {
		log.Printf("Error updating menu image URL in database: %v", err)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, updatedMenu, http.StatusOK)
}

func (h *UploadHandler) UploadUserImage(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Minio == nil {
		utils.RespondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	// 認証情報取得と検証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Unauthorized: Authorization header not found", http.StatusUnauthorized)
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	if idToken == authHeader {
		utils.RespondWithError(w, "Unauthorized: Invalid token format", http.StatusUnauthorized)
		return
	}

	firebaseUID, err := h.AuthSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Unauthorized: Invalid ID token", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("userImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file named 'userImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Minio.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading user to Minio: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBアップデート
	updatedUser, err := h.UserRepo.UpdateUserImageURL(firebaseUID, fileURL)
	if err != nil {
		log.Printf("Error updating user image URL in DB: %v", err)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, updatedUser, http.StatusOK)
}

func (h *UploadHandler) UploadStoreImage(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Minio == nil {
		utils.RespondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	// storeId取得
	vars := mux.Vars(r)
	storeId := vars["storeId"]

	// 'storeImage'ファイルをマルチパートフォームから取得
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("storeImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file named 'storeImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Minio.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading store image to Minio: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBの店舗情報をアップデート
	updatedStore, err := h.StoreRepo.UpdateStoreImageURL(storeId, fileURL)
	if err != nil {
		log.Printf("Error updating store image URL in DB: %v", err)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, updatedStore, http.StatusOK)
}

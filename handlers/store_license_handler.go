package handlers

import (
	"log"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// UploadHandler は Minio クライアントを依存関係として持つ
type UploadHandler struct {
	Minio              *data.MinioClient
	AssetsBucketName   string
	AssetsPublicDomain string
	BizBucketName      string
}

// ハンドラ初期化
func NewUploadHandler(minio *data.MinioClient, assetsBucket, assetsPublicDomain, bizBucket string) *UploadHandler {
	return &UploadHandler{
		Minio:              minio,
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
	updatedLicense, err := data.UpdateLicenseInfoAfterUpload(storeID, fileKey)
	if err != nil {
		log.Printf("Error updating database: %v", err)
		utils.RespondWithError(w, "Could not update store information", http.StatusInternalServerError)
		return
	}

	// REST 標準: POST レスポンスに更新後のリソースを返却 (200 OK)
	utils.RespondWithJSON(w, updatedLicense, http.StatusOK)
}

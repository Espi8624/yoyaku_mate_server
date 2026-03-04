package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"yoyaku_mate_server/data"
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
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// formData をパース (最大 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	// formData から 'storeId' を取得
	storeID := r.FormValue("storeId")
	if storeID == "" {
		http.Error(w, "Invalid storeId", http.StatusBadRequest)
		return
	}

	// formData から 'licenseImage' ファイルを取得
	file, header, err := r.FormFile("licenseImage")
	if err != nil {
		http.Error(w, "Could not get uploaded file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// assetsPublicDomain := os.Getenv("R2_ASSETS_PUBLIC_DOMAIN")

	// MinIOにアップロード
	fileKey, err := h.Minio.UploadFile(h.BizBucketName, "", file, header)
	if err != nil {
		log.Printf("Error uploading file: %v", err)
		http.Error(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DB にファイルの URL とステータスを更新
	if err := data.UpdateLicenseInfoAfterUpload(storeID, fileKey); err != nil {
		log.Printf("Error updating database: %v", err)
		http.Error(w, "Could not update store information", http.StatusInternalServerError)
		return
	}

	// 成功応答返却
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "File uploaded successfully. Awaiting verification.",
		"fileURL": fileKey,
	})
}

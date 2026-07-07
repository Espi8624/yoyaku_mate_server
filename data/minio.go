package data

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"yoyaku_mate_server/config"

	"github.com/google/uuid"
)

func (c *MinioClient) UploadFile(bucketName, publicDomain string, file multipart.File, header *multipart.FileHeader) (string, error) {
	// ファイル名衝突を回避するため、ユニークなファイル名を生成
	uniqueFileName := uuid.New().String() + filepath.Ext(header.Filename)

	/*
	// --- オンライン移行時に使用 (Cloudflare R2 / S3 アップロード) ---
	_, err := c.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(uniqueFileName),
		Body:        file,
		ContentType: aws.String(header.Header.Get("Content-Type")),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to minio bucket %s: %w", bucketName, err)
	}

	if publicDomain != "" {
		// publicDomainが提供された場合(例: メニュー, プロフィールイメージ)
		// 完全な公開URLを生成し返却
		fileURL := fmt.Sprintf("https://%s/%s", publicDomain, uniqueFileName)
		log.Printf("Successfully uploaded file to public bucket. URL: %s", fileURL)
		return fileURL, nil
	} else {
		// publicDomainが提供されなかった場合（例：営業許可証）
		// ファイルのキーのみ返却
		log.Printf("Successfully uploaded file to private bucket. Key: %s", uniqueFileName)
		return uniqueFileName, nil
	}
	// -------------------------------------------------------------
	*/

	// --- ローカル開発用のファイル保存処理 ---
	uploadDir := "./uploads"
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create upload directory: %w", err)
	}

	dstPath := filepath.Join(uploadDir, uniqueFileName)
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("failed to create local file: %w", err)
	}
	defer dst.Close()

	if _, err = io.Copy(dst, file); err != nil {
		return "", fmt.Errorf("failed to save file locally: %w", err)
	}

	log.Printf("Successfully saved file locally: %s", dstPath)

	// configパッケージからサーバーのベースURLを取得
	serverURL := config.Get().Server.URL
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	fileURL := fmt.Sprintf("%s/uploads/%s", serverURL, uniqueFileName)

	if publicDomain != "" {
		return fileURL, nil
	} else {
		return uniqueFileName, nil
	}
}

// 非公開バケット内のオブジェクトに対する一時的なアクセスURLを生成
func (c *MinioClient) GetPresignedURL(bucketName, objectKey string) (string, error) {
	/*
	// --- オンライン移行時に使用 (S3 Presign) ---
	presignClient := s3.NewPresignClient(c.S3Client)

	presignedUrl, err := presignClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	}, func(opts *s3.PresignOptions) {
		// URLの有効期限を設定
		opts.Expires = 15 * time.Minute
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL for key %s: %w", objectKey, err)
	}

	return presignedUrl.URL, nil
	// -------------------------------------------
	*/

	// --- ローカル開発用のURL返却処理 ---
	serverURL := config.Get().Server.URL
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	localURL := fmt.Sprintf("%s/uploads/%s", serverURL, objectKey)
	return localURL, nil
}

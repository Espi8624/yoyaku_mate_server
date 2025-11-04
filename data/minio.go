package data

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func (c *MinioClient) UploadFile(bucketName, publicDomain string, file multipart.File, header *multipart.FileHeader) (string, error) {
	// file名生成
	uniqueFileName := uuid.New().String() + filepath.Ext(header.Filename)

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
}

// 非公開バケット内のオブジェクトに対する一時的なアクセスURLを生成
func (c *MinioClient) GetPresignedURL(bucketName, objectKey string) (string, error) {
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
}

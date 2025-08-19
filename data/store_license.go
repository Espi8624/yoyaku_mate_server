// data/minio.go
package data

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"path/filepath"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

type MinioClient struct {
	S3Client *s3.Client
	Bucket   string
	Endpoint string
}

// MinIO クライアントを作成して初期化
func NewMinioClient(endpoint, accessKey, secretKey, bucket string) (*MinioClient, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:           endpoint,
			SigningRegion: "us-east-1", // MinIO uses a single region
			PartitionID:   "aws",
			Source:        aws.EndpointSourceCustom,
		}, nil
	})

	creds := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithEndpointResolverWithOptions(resolver),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load s3 config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // MinIO uses path-style requests
	})

	return &MinioClient{
		S3Client: s3Client,
		Bucket:   bucket,
		Endpoint: endpoint,
	}, nil
}

// ファイルを MinIO バケットにアップロード
func (c *MinioClient) UploadFile(file multipart.File, header *multipart.FileHeader) (string, error) {
	// ファイル名衝突を回避するため、ユニークなファイル名を生成
	uniqueFileName := uuid.New().String() + filepath.Ext(header.Filename)

	_, err := c.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(c.Bucket),
		Key:         aws.String(uniqueFileName),
		Body:        file,
		ContentType: aws.String(header.Header.Get("Content-Type")),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to minio: %w", err)
	}

	// ファイルの URL を生成
	// 実際には Endpoint を設定ファイルから読み込む必要がある
	fileURL := fmt.Sprintf("%s/%s/%s", c.Endpoint, c.Bucket, uniqueFileName)
	log.Printf("Successfully uploaded file: %s", fileURL)

	return fileURL, nil
}

// MongoDB　の 'stores' コレクションのライセンス情報を更新
func UpdateStoreLicenseInfo(storeID string, imageURL string) error {
	// MongoDB コレクションを取得
	collection := db.GetCollection(DatabaseName, CollectionStoreLicense)

	// 文字列形式の storeID をそのまま使用し、フィルター作成
	filter := bson.M{"store_id": storeID}

	// 更新内容を定義
	update := bson.M{
		"$set": bson.M{
			"license_image_url":   imageURL,
			"verification_status": models.StatusPending,
			"updated_at":          time.Now(),
		},
	}

	// コンテキストを作成
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// DB 更新を実行
	// Filter: 'store_id'が渡された storeID(string) と一致するドキュメントを検索
	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Error: Failed to update store document in MongoDB. storeID: %s, err: %v", storeID, err)
		return err
	}

	// 結果確認
	if result.MatchedCount == 0 {
		log.Printf("Warning: No store document found with the given ID. storeID: %s", storeID)
		return nil
	}

	log.Printf("DATABASE: Successfully updated license info for storeID: %s. Documents matched: %d, Documents modified: %d", storeID, result.MatchedCount, result.ModifiedCount)
	return nil
}

// store_license コレクションのドキュメントを更新
func UpdateLicenseInfoAfterUpload(storeID string, imageURL string) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreLicense)

	filter := bson.M{"store_id": storeID}

	update := bson.M{
		"$set": bson.M{
			"license_image_url":   imageURL,
			"verification_status": models.StatusPending, // ステータスを "PENDING" (審査中)に変更
			"updated_at":          time.Now(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Error: Failed to update license document in MongoDB. storeID: %s, err: %v", storeID, err)
		return err
	}

	if result.MatchedCount == 0 {
		log.Printf("Warning: No license document found for the given storeID. storeID: %s", storeID)
		return nil
	}

	log.Printf("DATABASE: Successfully updated license info for storeID: %s.", storeID)
	return nil
}

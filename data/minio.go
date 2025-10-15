package data

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func (c *MinioClient) UploadFile(bucketName string, file multipart.File, header *multipart.FileHeader) (string, error) {
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

	fileURL := fmt.Sprintf("%s/%s/%s", c.Endpoint, bucketName, uniqueFileName)
	log.Printf("Successfully uploaded file: %s", fileURL)

	return fileURL, nil
}

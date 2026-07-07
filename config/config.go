package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	MongoDB struct {
		URI      string `json:"uri"`
		Database string `json:"database"`
		Timeout  int    `json:"timeout"` // seconds
	} `json:"mongodb"`
	Server struct {
		Port         string   `json:"port"`
		AllowOrigins []string `json:"allowOrigins"`
		URL          string   `json:"url"`
	} `json:"server"`
	R2         R2Config `json:"r2"`
	HMACSecret string   `json:"hmacSecret"`
}

type R2Config struct {
	AccountID          string `json:"accountId"`
	AccessKey          string `json:"accessKey"`
	SecretKey          string `json:"secretKey"`
	AssetsBucketName   string // R2_ASSETS_BUCKET_NAME: 公開バケット名 (例: saboten-assets-prod)
	AssetsPublicDomain string // R2_ASSETS_PUBLIC_DOMAIN: 公開バケットのパブリックドメイン
	BizBucketName      string // R2_BIZ_BUCKET_NAME: 非公開バケット名 (例: saboten-biz-prod)
}

var cfg Config

func Load() Config {
	env := os.Getenv("GO_ENV")
	if env == "" {
		// デフォルトで'development'を使用
		env = "development"
	}
	log.Printf("Loading configuration for environment: %s", env)

	fileName := env + ".json"

	// ローカル起動時は相対パス "config/xxx.json" を先に確認
	configPath := filepath.Join("config", fileName)
	f, err := os.Open(configPath)
	if err != nil {
		// ローカルの相対パスで見つからない場合、コンテナ環境の "/config/xxx.json" を試行
		fallbackPath := filepath.Join("/", "config", fileName)
		log.Printf("Warning: Could not open config file at local relative path '%s'. Trying fallback '%s'...", configPath, fallbackPath)

		var openErr error
		f, openErr = os.Open(fallbackPath)
		if openErr != nil {
			log.Printf("Warning: Could not open config file at fallback path '%s', using default config. Error: %v", fallbackPath, openErr)
			return getDefaultConfig()
		}
		configPath = fallbackPath
	}
	defer f.Close()

	log.Printf("Using config file: %s", configPath)

	var cfg Config
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		log.Printf("Warning: Could not decode config file, using defaults. Error: %v", err)
		return getDefaultConfig()
	}

	// Environment variables override
	if mongoURI := os.Getenv("MONGODB_URI"); mongoURI != "" {
		cfg.MongoDB.URI = mongoURI
		log.Println("Using MONGODB_URI from environment variable")
	}
	if mongoDB := os.Getenv("MONGODB_DATABASE"); mongoDB != "" {
		cfg.MongoDB.Database = mongoDB
		log.Println("Using MONGODB_DATABASE from environment variable")
	}
	if serverPort := os.Getenv("SERVER_PORT"); serverPort != "" {
		cfg.Server.Port = serverPort
		log.Println("Using SERVER_PORT from environment variable")
	}
	if serverURL := os.Getenv("SERVER_URL"); serverURL != "" {
		cfg.Server.URL = serverURL
		log.Println("Using SERVER_URL from environment variable")
	}
	if hmacSecret := os.Getenv("HMAC_SECRET"); hmacSecret != "" {
		cfg.HMACSecret = hmacSecret
		log.Println("Using HMAC_SECRET from environment variable")
	}

	cfg.R2 = R2Config{
		AccountID:          os.Getenv("R2_ACCOUNT_ID"),
		AccessKey:          os.Getenv("R2_ACCESS_KEY"),
		SecretKey:          os.Getenv("R2_SECRET_KEY"),
		AssetsBucketName:   os.Getenv("R2_ASSETS_BUCKET_NAME"),
		AssetsPublicDomain: os.Getenv("R2_ASSETS_PUBLIC_DOMAIN"),
		BizBucketName:      os.Getenv("R2_BIZ_BUCKET_NAME"),
	}

	// Override AllowedOrigins from environment variable (comma separated)
	if allowedOrigins := os.Getenv("ALLOWED_ORIGINS"); allowedOrigins != "" {
		origins := strings.Split(allowedOrigins, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		cfg.Server.AllowOrigins = origins
		log.Printf("Overriding AllowedOrigins from env: %v", cfg.Server.AllowOrigins)
	} else {
		// If Env is not set, ensure we have at least "*" or default from file
		// If file didn't specify and env didn't specify, default to "*" for backward compatibility if needed?
		// But best practice is strictly what's in config.
		// For now, if empty, let's look at main.go (it was hardcoded to *).
		// Let's ensure if cfg.Server.AllowOrigins is empty, we default to "*" to match previous behavior if config file is missing it.
		if len(cfg.Server.AllowOrigins) == 0 {
			cfg.Server.AllowOrigins = []string{"*"}
		}
	}

	return cfg
}

func getDefaultConfig() Config {
	return Config{
		MongoDB: struct {
			URI      string `json:"uri"`
			Database string `json:"database"`
			Timeout  int    `json:"timeout"`
		}{
			URI:      "mongodb://localhost:27017",
			Database: "project_rusui",
			Timeout:  30,
		},
		Server: struct {
			Port         string   `json:"port"`
			AllowOrigins []string `json:"allowOrigins"`
			URL          string   `json:"url"`
		}{
			Port:         ":8080",
			AllowOrigins: []string{"http://localhost:3000", "https://localhost:3000"},
			URL:          "http://localhost:8080",
		},
		R2: R2Config{
			AccountID:          os.Getenv("R2_ACCOUNT_ID"),
			AccessKey:          os.Getenv("R2_ACCESS_KEY"),
			SecretKey:          os.Getenv("R2_SECRET_KEY"),
			AssetsBucketName:   os.Getenv("R2_ASSETS_BUCKET_NAME"),
			AssetsPublicDomain: os.Getenv("R2_ASSETS_PUBLIC_DOMAIN"),
			BizBucketName:      os.Getenv("R2_BIZ_BUCKET_NAME"),
		},
		HMACSecret: "local-dev-only-do-not-use-in-production",
	}
}

func Get() Config {
	return cfg
}

func GetMongoTimeout() time.Duration {
	return time.Duration(cfg.MongoDB.Timeout) * time.Second
}

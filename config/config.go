package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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
	} `json:"server"`
}

var cfg Config

func Load(env string) Config {
	if env == "" {
		env = "development"
	}

	// 設定ファイル Path リスト
	configPaths := []string{
		filepath.Join(".", "config."+env+".json"),
		filepath.Join(".", "config", "config."+env+".json"),
		filepath.Join("..", "config."+env+".json"),
	}

	var f *os.File
	var err error
	var configPath string

	// パスをループし、最初に存在する設定ファイルを検索
	for _, path := range configPaths {
		if f, err = os.Open(path); err == nil {
			configPath = path
			break
		}
	}

	if err != nil {
		log.Printf("Warning: No config file found in paths %v, using defaults", configPaths)
		return getDefaultConfig()
	}
	defer f.Close()

	log.Printf("Using config file: %s", configPath)
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		log.Printf("Warning: Could not decode config file, using defaults: %v", err)
		return getDefaultConfig()
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
			Database: "yoyaku_mate_provider_db",
			Timeout:  30,
		},
		Server: struct {
			Port         string   `json:"port"`
			AllowOrigins []string `json:"allowOrigins"`
		}{
			Port:         ":8080",
			AllowOrigins: []string{"http://localhost:3000", "https://localhost:3000"},
		},
	}
}

func Get() Config {
	return cfg
}

func GetMongoTimeout() time.Duration {
	return time.Duration(cfg.MongoDB.Timeout) * time.Second
}

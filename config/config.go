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
	R2 R2Config `json:"r2"`
}

type R2Config struct {
	AccountID    string `json:"accountId"`
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey"`
	PublicDomain string `json:"publicDomain"`
}

var cfg Config

func Load() Config {
	env := os.Getenv("GO_ENV")
	if env == "" {
		log.Println("Warning: GO_ENV is not set. Defaulting to 'development'.")
		env = "development"
	}
	log.Printf("Loading configuration for environment: %s", env)

	fileName := env + ".json"
	configPath := filepath.Join("/", "config", fileName)

	f, err := os.Open(configPath)
	if err != nil {
		log.Printf("Warning: Could not open config file at '%s', using default config. Error: %v", configPath, err)
		return getDefaultConfig()
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

	cfg.R2 = R2Config{
		AccountID:    os.Getenv("R2_ACCOUNT_ID"),
		AccessKey:    os.Getenv("R2_ACCESS_KEY"),
		SecretKey:    os.Getenv("R2_SECRET_KEY"),
		PublicDomain: os.Getenv("R2_PUBLIC_DOMAIN"),
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
			Database: "saboten_provider",
			Timeout:  30,
		},
		Server: struct {
			Port         string   `json:"port"`
			AllowOrigins []string `json:"allowOrigins"`
		}{
			Port:         ":8080",
			AllowOrigins: []string{"http://localhost:3000", "https://localhost:3000"},
		},
		R2: R2Config{
			AccountID:    os.Getenv("R2_ACCOUNT_ID"),
			AccessKey:    os.Getenv("R2_ACCESS_KEY"),
			SecretKey:    os.Getenv("R2_SECRET_KEY"),
			PublicDomain: os.Getenv("R2_PUBLIC_DOMAIN"),
		},
	}
}

func Get() Config {
	return cfg
}

func GetMongoTimeout() time.Duration {
	return time.Duration(cfg.MongoDB.Timeout) * time.Second
}

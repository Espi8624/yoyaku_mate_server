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

	// Environment variables override
	if mongoURI := os.Getenv("MONGODB_URI"); mongoURI != "" {
		cfg.MongoDB.URI = mongoURI
		log.Println("Using MONGODB_URI from environment variable")
	}

	cfg.R2 = R2Config{
		AccountID:    os.Getenv("R2_ACCOUNT_ID"),
		AccessKey:    os.Getenv("R2_ACCESS_KEY"),
		SecretKey:    os.Getenv("R2_SECRET_KEY"),
		PublicDomain: os.Getenv("R2_PUBLIC_DOMAIN"),
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

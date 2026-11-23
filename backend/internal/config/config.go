// Package config reads the backend configuration from environment variables.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL      string
	DataDir          string
	OpenAIKey        string
	OpenAIBaseURL    string
	OpenAIChatModel  string
	OpenAIEmbedModel string
	HTTPAddr         string
	MaxUploadMB      int64
	WebDir           string
	Seed             bool
}

func Load() Config {
	return Config{
		DatabaseURL:      env("DATABASE_URL", "postgres://rag:rag@localhost:5433/rag?sslmode=disable"),
		DataDir:          env("DATA_DIR", "./data"),
		OpenAIKey:        os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:    env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIChatModel:  env("OPENAI_CHAT_MODEL", "gpt-4.1-mini"),
		OpenAIEmbedModel: env("OPENAI_EMBED_MODEL", "text-embedding-3-small"),
		HTTPAddr:         env("HTTP_ADDR", ":8080"),
		MaxUploadMB:      int64(envInt("MAX_UPLOAD_MB", 50)),
		WebDir:           env("WEB_DIR", "../web/dist"),
		Seed:             env("SEED", "true") == "true",
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

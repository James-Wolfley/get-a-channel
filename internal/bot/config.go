package bot

import (
	"log"
	"os"
	"strconv"
	"strings"
)

const (
	defaultDatabasePath        = "/data/bot.db"
	defaultCleanupIntervalSecs = 300
)

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Printf("invalid positive integer %s=%q, using %d", key, value, fallback)
		return fallback
	}
	return parsed
}

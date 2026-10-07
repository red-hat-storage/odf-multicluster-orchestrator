package utils

import (
	"os"

	"github.com/joho/godotenv"
)

func GetEnv(key string, filenames ...string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	if len(filenames) == 0 {
		return ""
	}

	envMap, err := godotenv.Read(filenames...)
	if err != nil {
		return ""
	}

	if value, ok := envMap[key]; ok {
		return value
	}

	return ""
}

func GetEnvOrDefault(key, defaultValue string, filenames ...string) string {
	if value := GetEnv(key, filenames...); value != "" {
		return value
	}
	return defaultValue
}

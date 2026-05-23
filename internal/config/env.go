package config

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

// Config holds all environmental configurations for the application
type Config struct {
	Port     string
	MongoURI string
	DBName   string
}

// LoadConfig loads variables from .env and maps them to the Config struct
func LoadConfig() *Config {
	// Attempt to load .env file. We don't fail if it's missing, as environment
	// variables can be set directly in the environment (e.g., in production)
	if err := godotenv.Load(); err != nil {
		log.Warn().Msg("No .env file found or unable to load. Proceeding with system environment variables.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "pulse"
	}

	return &Config{
		Port:     port,
		MongoURI: mongoURI,
		DBName:   dbName,
	}
}

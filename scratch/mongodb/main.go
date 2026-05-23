package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Load env file
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	mongoURI := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME")

	if mongoURI == "" {
		fmt.Println("❌ Error: MONGO_URI is not set in .env")
		return
	}
	if dbName == "" {
		dbName = "pulse" // fallback
	}

	fmt.Printf("📡 Attempting connection to MongoDB (with insecure TLS skip)...\n")
	fmt.Printf("📦 Database Target: %s\n", dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Configure TLS config to bypass verification for diagnostics
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	clientOptions := options.Client().
		ApplyURI(mongoURI).
		SetTLSConfig(tlsConfig)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		fmt.Printf("❌ Failed to initiate connection client: %v\n", err)
		return
	}
	defer func() {
		disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer disconnectCancel()
		_ = client.Disconnect(disconnectCtx)
	}()

	// 1. Ping test
	err = client.Ping(ctx, nil)
	if err != nil {
		fmt.Printf("❌ Failed to ping MongoDB. Please verify your connection URI, network, or IP white-lists.\nError: %v\n", err)
		return
	}
	fmt.Println("✅ Ping Test: Success!")

	// 2. Query collections test to verify authorization
	db := client.Database(dbName)
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		fmt.Printf("❌ Failed to fetch collection names (Auth error?): %v\n", err)
		return
	}

	fmt.Println("✅ DB Query Test: Success!")
	fmt.Println("=========================================")
	fmt.Println("Existing collections in database:")
	if len(collections) == 0 {
		fmt.Println("  (No collections found yet. Ready to ingest telemetry data!)")
	} else {
		for _, name := range collections {
			fmt.Printf("  - %s\n", name)
		}
	}
	fmt.Println("=========================================")
	fmt.Println("🎉 MongoDB Connection is fully operational!")
}

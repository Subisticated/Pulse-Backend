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
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	mongoURI := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "pulse"
	}

	tlsConf := &tls.Config{InsecureSkipVerify: true}
	client, _ := mongo.Connect(context.Background(),
		options.Client().ApplyURI(mongoURI).SetTLSConfig(tlsConf).
			SetServerSelectionTimeout(30*time.Second))
	defer client.Disconnect(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	count, err := client.Database(dbName).Collection("logs").CountDocuments(ctx, bson.M{})
	if err != nil {
		fmt.Printf("❌ Count error: %v\n", err)
		return
	}
	fmt.Printf("💾 Documents in logs collection: %d\n", count)
}

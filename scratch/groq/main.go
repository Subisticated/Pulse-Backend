package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func main() {
	// Load .env from current directory, parent directory or grandparent directory
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("groq_API_KEY")
	}

	if apiKey == "" {
		fmt.Println("❌ Error: groq_API_KEY or GROQ_API_KEY is not set in .env file or system environment")
		return
	}

	// Safe print (mask middle characters)
	var maskedKey string
	if len(apiKey) > 10 {
		maskedKey = fmt.Sprintf("%s...%s", apiKey[:7], apiKey[len(apiKey)-5:])
	} else {
		maskedKey = apiKey
	}

	fmt.Printf("🔑 Loaded Groq API Key: %s\n", maskedKey)
	fmt.Println("📡 Connecting to Groq API (llama-3.1-8b-instant)...")

	reqBody := groqRequest{
		Model: "llama-3.1-8b-instant",
		Messages: []groqMessage{
			{Role: "user", Content: "Hello! Respond with 'Groq API connection is working!' and a 1-sentence SRE tip."},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Printf("❌ Failed to build request: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("❌ Connection error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Failed to read response body: %v\n", err)
		return
	}

	fmt.Printf("📡 HTTP Status Code: %s\n", resp.Status)

	var grqResp groqResponse
	if err := json.Unmarshal(respBytes, &grqResp); err != nil {
		fmt.Printf("❌ Failed to decode response JSON: %v\n", err)
		fmt.Printf("Raw Response Data:\n%s\n", string(respBytes))
		return
	}

	if grqResp.Error != nil {
		fmt.Printf("❌ Groq Error: %s\n", grqResp.Error.Message)
		return
	}

	if len(grqResp.Choices) > 0 {
		fmt.Println("✅ Groq API Test Successful!")
		fmt.Println("=========================================")
		fmt.Printf("Model Response: %s\n", grqResp.Choices[0].Message.Content)
		fmt.Println("=========================================")
	} else {
		fmt.Println("⚠️ Request completed but no choices were returned. Raw response:")
		fmt.Println(string(respBytes))
	}
}

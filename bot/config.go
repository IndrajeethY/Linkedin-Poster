package bot

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken       string
	OwnerID        int64
	GeminiAPIKey   string
	LinkedInToken  string
	LinkedInAuthor string
}

var config *Config

func LoadConfig() error {
	_ = godotenv.Load()

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		return fmt.Errorf("BOT_TOKEN is required")
	}

	ownerID, err := strconv.ParseInt(os.Getenv("OWNER_ID"), 10, 64)
	if err != nil {
		return fmt.Errorf("OWNER_ID must be a valid integer: %w", err)
	}

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY is required")
	}

	config = &Config{
		BotToken:       botToken,
		OwnerID:        ownerID,
		GeminiAPIKey:   geminiKey,
		LinkedInToken:  os.Getenv("LINKEDIN_TOKEN"),
		LinkedInAuthor: os.Getenv("AUTHOR_ID"),
	}

	return nil
}

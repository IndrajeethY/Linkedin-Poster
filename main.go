package main

import (
	"Linkedin-Poster/bot"
	"log"
)

func main() {
	if err := bot.LoadConfig(); err != nil {
		log.Fatalf("config: %v", err)
	}
	bot.InitTgBot()
}

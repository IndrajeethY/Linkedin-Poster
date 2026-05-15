package bot

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/google/generative-ai-go/genai"
)

type PostData struct {
	Prompt    string
	Text      string
	PhotoID   string
	ImagePath string
}

var postStore sync.Map
var chatSessions sync.Map // chatID -> []*genai.Content

func InitTgBot() {
	b, err := gotgbot.NewBot(config.BotToken, nil)
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Printf("handler error: %v", err)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})
	updater := ext.NewUpdater(dispatcher, nil)

	dispatcher.AddHandler(handlers.NewCommand("start", startCmd))
	dispatcher.AddHandler(handlers.NewCommand("help", helpCmd))
	dispatcher.AddHandler(handlers.NewCommand("id", idCmd))
	dispatcher.AddHandler(handlers.NewCommand("ai", aiCmd))
	dispatcher.AddHandler(handlers.NewCommand("chat", chatCmd))
	dispatcher.AddHandler(handlers.NewCommand("clear", clearCmd))
	dispatcher.AddHandler(handlers.NewCommand("genpost", genpostCmd))
	dispatcher.AddHandler(handlers.NewCommand("topic", topicCmd))
	dispatcher.AddHandler(handlers.NewCommand("post", postCmd))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("regen."), regenCallback))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("post."), postCallback))

	err = updater.StartPolling(b, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 10 * time.Second,
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to start polling: %v", err)
	}
	log.Printf("%s started", b.User.Username)
	updater.Idle()
}

func isOwner(ctx *ext.Context) bool {
	return ctx.EffectiveSender.Id() == config.OwnerID
}

func startCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	text := fmt.Sprintf("Hey! I'm %s — I generate and post LinkedIn content using AI.\n\nUse /help to see available commands.", b.User.Username)
	_, err := ctx.EffectiveMessage.Reply(b, text, nil)
	return err
}

func helpCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	text := `<b>Commands</b>

<b>AI Chat (with tools)</b>
/chat <code>&lt;message&gt;</code> — Chat with AI assistant that can fetch repos, read URLs, and post to LinkedIn
/clear — Reset chat history

<b>Quick Post Generation</b>
/genpost <code>&lt;github_repo_url&gt;</code> — Generate a LinkedIn post from a GitHub repo
/topic <code>&lt;topic&gt;</code> — Generate a LinkedIn post about any topic

<b>Direct Actions</b>
/post <code>&lt;text&gt;</code> — Post text directly to LinkedIn (reply to a photo to include it)
/ai <code>&lt;prompt&gt;</code> — Ask Gemini AI anything (no tools)
/id — Show your Telegram user ID`
	_, err := ctx.EffectiveMessage.Reply(b, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func idCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Your Telegram ID: <code>%d</code>", ctx.EffectiveSender.Id()), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func aiCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if !isOwner(ctx) {
		_, err := msg.Reply(b, "Not authorized.", nil)
		return err
	}

	query := strings.Join(ctx.Args()[1:], " ")
	if query == "" {
		_, err := msg.Reply(b, "Usage: /ai &lt;prompt&gt;", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	response, err := ProcessGemini(query)
	if err != nil {
		_, err := msg.Reply(b, fmt.Sprintf("Gemini error: %v", err), nil)
		return err
	}

	if len(response) > 4096 {
		response = response[:4080] + "\n\n... (truncated)"
	}
	_, err = msg.Reply(b, response, nil)
	return err
}

func chatCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if !isOwner(ctx) {
		_, err := msg.Reply(b, "Not authorized.", nil)
		return err
	}

	query := strings.Join(ctx.Args()[1:], " ")
	if query == "" {
		_, err := msg.Reply(b, "Usage: /chat &lt;message&gt;\n\nExamples:\n• /chat write a post about my WiseHosting project on github.com/Wisehosting1/WiseHosting\n• /chat what repos does IndrajeethY have on GitHub?\n• /chat write a post about microservices vs monoliths", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	chatID := fmt.Sprintf("%d", msg.Chat.Id)
	var history []*genai.Content

	if raw, ok := chatSessions.Load(chatID); ok {
		history = raw.([]*genai.Content)
	}

	history = append(history, &genai.Content{
		Role:  "user",
		Parts: []genai.Part{genai.Text(query)},
	})

	waiting, _ := msg.Reply(b, "Thinking...", nil)

	response, newHistory, err := ProcessGeminiWithTools(history)
	if err != nil {
		waiting.EditText(b, fmt.Sprintf("Error: %v", err), nil)
		return nil
	}

	chatSessions.Store(chatID, newHistory)

	if len(response) > 4096 {
		response = response[:4080] + "\n\n... (truncated)"
	}
	waiting.EditText(b, response, nil)
	return nil
}

func clearCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	chatID := fmt.Sprintf("%d", ctx.EffectiveMessage.Chat.Id)
	chatSessions.Delete(chatID)
	_, err := ctx.EffectiveMessage.Reply(b, "Chat history cleared.", nil)
	return err
}

func genpostCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if !isOwner(ctx) {
		_, err := msg.Reply(b, "Not authorized.", nil)
		return err
	}

	repoURL := strings.Join(ctx.Args()[1:], " ")
	if repoURL == "" {
		_, err := msg.Reply(b, "Usage: /genpost &lt;github_repo_url&gt;", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	waiting, _ := msg.Reply(b, "Fetching repo and generating post...", nil)

	prompt, err := GenRepoPrompt(repoURL)
	if err != nil {
		waiting.EditText(b, fmt.Sprintf("Failed to fetch repo: %v", err), nil)
		return nil
	}

	var photoID, imagePath string
	if msg.ReplyToMessage != nil && len(msg.ReplyToMessage.Photo) > 0 {
		photoID = msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1].FileId
		file, ferr := b.GetFile(photoID, nil)
		if ferr == nil {
			imagePath, _ = DownloadFile(file.URL(b, nil))
		}
	}

	text, err := ProcessGemini(prompt)
	if err != nil {
		waiting.EditText(b, fmt.Sprintf("Gemini error: %v", err), nil)
		return nil
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	postStore.Store(id, PostData{
		Prompt:    prompt,
		Text:      text,
		PhotoID:   photoID,
		ImagePath: imagePath,
	})

	if len(text) > 4096 {
		text = text[:4080] + "\n\n... (truncated)"
	}
	waiting.EditText(b, text, &gotgbot.EditMessageTextOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "Regenerate", CallbackData: "regen." + id},
					{Text: "Post to LinkedIn", CallbackData: "post." + id},
				},
			},
		},
	})
	return nil
}

func topicCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if !isOwner(ctx) {
		_, err := msg.Reply(b, "Not authorized.", nil)
		return err
	}

	topic := strings.Join(ctx.Args()[1:], " ")
	if topic == "" {
		_, err := msg.Reply(b, "Usage: /topic &lt;topic&gt;", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	waiting, _ := msg.Reply(b, "Generating post...", nil)

	var photoID, imagePath string
	if msg.ReplyToMessage != nil && len(msg.ReplyToMessage.Photo) > 0 {
		photoID = msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1].FileId
		file, ferr := b.GetFile(photoID, nil)
		if ferr == nil {
			imagePath, _ = DownloadFile(file.URL(b, nil))
		}
	}

	prompt := GenTopicPrompt(topic)
	text, err := ProcessGemini(prompt)
	if err != nil {
		waiting.EditText(b, fmt.Sprintf("Gemini error: %v", err), nil)
		return nil
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	postStore.Store(id, PostData{
		Prompt:    prompt,
		Text:      text,
		PhotoID:   photoID,
		ImagePath: imagePath,
	})

	if len(text) > 4096 {
		text = text[:4080] + "\n\n... (truncated)"
	}
	waiting.EditText(b, text, &gotgbot.EditMessageTextOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "Regenerate", CallbackData: "regen." + id},
					{Text: "Post to LinkedIn", CallbackData: "post." + id},
				},
			},
		},
	})
	return nil
}

func postCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if !isOwner(ctx) {
		_, err := msg.Reply(b, "Not authorized.", nil)
		return err
	}

	text := strings.Join(ctx.Args()[1:], " ")

	if text == "" && msg.ReplyToMessage != nil && msg.ReplyToMessage.Text != "" {
		text = msg.ReplyToMessage.Text
	}
	if text == "" {
		_, err := msg.Reply(b, "Usage: /post &lt;text&gt; or reply to a message with /post", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	var imagePath string
	if msg.ReplyToMessage != nil && len(msg.ReplyToMessage.Photo) > 0 {
		photoID := msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1].FileId
		file, ferr := b.GetFile(photoID, nil)
		if ferr == nil {
			imagePath, _ = DownloadFile(file.URL(b, nil))
		}
	}

	var link string
	var postErr error

	if imagePath != "" {
		defer os.Remove(imagePath)
		uploadURL, imageURN, err := InitializeImageUpload()
		if err != nil {
			_, err := msg.Reply(b, fmt.Sprintf("Image upload init failed: %v", err), nil)
			return err
		}
		if err := UploadImage(uploadURL, imagePath); err != nil {
			_, err := msg.Reply(b, fmt.Sprintf("Image upload failed: %v", err), nil)
			return err
		}
		link, postErr = PostToLinkedInWithImage(text, imageURN)
	} else {
		link, postErr = PostToLinkedIn(text)
	}

	if postErr != nil {
		_, err := msg.Reply(b, fmt.Sprintf("Failed to post: %v", postErr), nil)
		return err
	}

	_, err := msg.Reply(b, fmt.Sprintf("Posted to LinkedIn!\n%s", link), nil)
	return err
}

func regenCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.Update.CallbackQuery
	id := strings.TrimPrefix(query.Data, "regen.")

	rawData, ok := postStore.Load(id)
	if !ok {
		query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Session expired"})
		return nil
	}

	query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Regenerating..."})

	data := rawData.(PostData)
	text, err := ProcessGemini(data.Prompt)
	if err != nil {
		query.Message.EditText(b, fmt.Sprintf("Gemini error: %v", err), nil)
		return nil
	}

	data.Text = text
	postStore.Store(id, data)

	if len(text) > 4096 {
		text = text[:4080] + "\n\n... (truncated)"
	}
	query.Message.EditText(b, text, &gotgbot.EditMessageTextOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "Regenerate", CallbackData: "regen." + id},
					{Text: "Post to LinkedIn", CallbackData: "post." + id},
				},
			},
		},
	})
	return nil
}

func postCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.Update.CallbackQuery
	id := strings.TrimPrefix(query.Data, "post.")

	rawData, ok := postStore.LoadAndDelete(id)
	if !ok {
		query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Session expired"})
		return nil
	}

	data := rawData.(PostData)
	var link string
	var postErr error

	if data.ImagePath != "" {
		defer os.Remove(data.ImagePath)
		uploadURL, imageURN, err := InitializeImageUpload()
		if err != nil {
			query.Message.EditText(b, fmt.Sprintf("Image upload failed: %v", err), nil)
			return nil
		}
		if err := UploadImage(uploadURL, data.ImagePath); err != nil {
			query.Message.EditText(b, fmt.Sprintf("Image upload failed: %v", err), nil)
			return nil
		}
		link, postErr = PostToLinkedInWithImage(data.Text, imageURN)
	} else {
		link, postErr = PostToLinkedIn(data.Text)
	}

	if postErr != nil {
		query.Message.EditText(b, fmt.Sprintf("Failed to post: %v", postErr), nil)
		return nil
	}

	query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Posted!", ShowAlert: true})
	query.Message.EditText(b, data.Text, &gotgbot.EditMessageTextOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{{Text: "View Post", Url: link}},
			},
		},
	})
	return nil
}

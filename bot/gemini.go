package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func ProcessGemini(text string) (string, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(config.GeminiAPIKey))
	if err != nil {
		return "", fmt.Errorf("create gemini client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-3.1-flash-lite")
	resp, err := model.GenerateContent(ctx, genai.Text(text))
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}

	return fmt.Sprintf("%s", resp.Candidates[0].Content.Parts[0]), nil
}

const agentSystemPrompt = `You are a LinkedIn content assistant for a software developer. You help create, refine, and publish LinkedIn posts.

You have tools available:
- fetch_github_readme: Read a GitHub repo's README to understand a project
- fetch_url_content: Read any webpage for research
- get_github_repos: List a user's GitHub repos
- post_to_linkedin: Publish a post to LinkedIn (ONLY when user explicitly confirms)

Workflow:
1. When asked to write a post about a project, use fetch_github_readme first to understand it
2. Write the post following LinkedIn best practices (no links in body, hook in first line, 1000-1300 chars)
3. Show the draft and wait for approval before posting
4. NEVER call post_to_linkedin unless the user explicitly says to post/publish/send it

LinkedIn post rules:
- No external links in the post body (kills reach). Say "Link in comments" instead
- First 210 characters must hook the reader
- Use line breaks generously
- End with a question to drive comments
- 3-5 hashtags on their own line
- No markdown formatting
- No clichés: "excited to share", "thrilled", "game-changer"
- 2-3 emojis max
- First person, conversational tone`

func ProcessGeminiWithTools(history []*genai.Content) (string, []*genai.Content, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(config.GeminiAPIKey))
	if err != nil {
		return "", history, fmt.Errorf("create gemini client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-3.1-flash-lite")
	model.Tools = LinkedInTools()
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(agentSystemPrompt)},
	}

	chat := model.StartChat()
	chat.History = history

	lastContent := history[len(history)-1]
	resp, err := model.GenerateContent(ctx, lastContent.Parts...)
	if err != nil {
		return "", history, fmt.Errorf("generate content: %w", err)
	}

	for {
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return "", history, fmt.Errorf("empty response from Gemini")
		}

		candidate := resp.Candidates[0].Content
		history = append(history, candidate)

		var functionCalls []*genai.FunctionCall
		var textParts []string
		for _, part := range candidate.Parts {
			switch v := part.(type) {
			case genai.FunctionCall:
				functionCalls = append(functionCalls, &v)
			case genai.Text:
				textParts = append(textParts, string(v))
			}
		}

		if len(functionCalls) == 0 {
			return strings.Join(textParts, "\n"), history, nil
		}

		var responseParts []genai.Part
		for _, fc := range functionCalls {
			result, err := ExecuteTool(fc.Name, fc.Args)
			if err != nil {
				responseParts = append(responseParts, genai.FunctionResponse{
					Name:     fc.Name,
					Response: map[string]interface{}{"error": err.Error()},
				})
			} else {
				responseParts = append(responseParts, genai.FunctionResponse{
					Name:     fc.Name,
					Response: map[string]interface{}{"result": result},
				})
			}
		}

		toolResultContent := &genai.Content{
			Role:  "user",
			Parts: responseParts,
		}
		history = append(history, toolResultContent)

		resp, err = model.GenerateContent(ctx, responseParts...)
		if err != nil {
			return "", history, fmt.Errorf("generate content after tool call: %w", err)
		}
	}
}

func GenRepoPrompt(repoURL string) (string, error) {
	readme, err := fetchReadme(repoURL)
	if err != nil {
		return "", err
	}

	parts := strings.Split(strings.TrimSuffix(repoURL, "/"), "/")
	projectName := parts[len(parts)-1]

	description := "A software project"
	for _, line := range strings.Split(readme, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "![") || strings.HasPrefix(trimmed, "<") {
			continue
		}
		description = trimmed
		break
	}

	if len(readme) > 3000 {
		readme = readme[:3000] + "\n... (truncated)"
	}

	return fmt.Sprintf(`You are a LinkedIn ghostwriter for a software developer. Your goal is maximum organic reach.

LinkedIn algorithm facts you MUST use:
- Posts with NO external links in the body get 3x more reach. Put the repo link in a COMMENT, not the post. Instead write "Link in comments" or "Drop a comment and I'll share the repo".
- First 210 characters appear above the fold — the hook MUST land here.
- Line breaks and whitespace boost dwell time. One thought per line. Use blank lines between sections.
- Posts between 1000-1300 characters perform best. Aim for ~1200.
- Questions at the end drive comments, which 10x distribution.
- Carousel/image posts get 3x reach over text-only. Suggest the reader check the image if one is attached.

Structure:
1. HOOK (first line, punchy — a hot take, surprising stat, or contrarian opinion about the tech domain)
2. CONTEXT (2-3 short lines: what is this, what problem does it solve)
3. KEY DETAILS (3-4 short lines about the interesting technical decisions — not a feature list)
4. PERSONAL ANGLE (1-2 lines: what you learned building it, what surprised you)
5. CTA (question that invites discussion + "Link in comments")
6. HASHTAGS (3-5, on their own line, relevant to the tech stack)

Hard rules:
- First person, conversational tone
- No markdown (no **, ##, backticks) — LinkedIn renders plain text only
- No "excited to share", "thrilled", "game-changer", "proud to announce", "just launched"
- 2-3 emojis max, only at section transitions
- Do NOT put the repo URL in the post body

Project: %s
Description: %s
Repository (for your context only, do NOT include in post): %s

README:
%s`, projectName, description, repoURL, readme), nil
}

func GenTopicPrompt(topic string) string {
	return fmt.Sprintf(`You are a LinkedIn ghostwriter for a software developer. Your goal is maximum organic reach and engagement.

LinkedIn algorithm facts you MUST use:
- First 210 characters appear above the fold — the hook MUST land here.
- Posts with NO external links get 3x more reach.
- Line breaks and whitespace boost dwell time. One thought per line. Blank lines between sections.
- Posts between 1000-1300 characters perform best. Aim for ~1200.
- Questions at the end drive comments, which 10x distribution.
- Contrarian takes and personal stories outperform generic advice.

Structure:
1. HOOK (first line — a hot take, surprising stat, personal failure, or contrarian opinion. Make people stop scrolling.)
2. STORY/CONTEXT (3-5 short lines: set up the insight with a real-world scenario or personal experience)
3. INSIGHT (3-4 lines: the actual value — what most people get wrong, what you learned, a framework or mental model)
4. CTA (end with a specific question that invites discussion, not just "thoughts?")
5. HASHTAGS (3-5, on their own line)

Hard rules:
- First person, conversational — write like you're telling a friend over coffee
- No markdown (no **, ##, backticks) — LinkedIn renders plain text only
- No "excited to share", "thrilled", "game-changer", "proud", "in today's fast-paced world"
- No generic advice everyone already knows
- 2-3 emojis max, only at section transitions
- No external links in the post body

Topic: %s`, topic)
}

func fetchReadme(repoURL string) (string, error) {
	raw := strings.TrimPrefix(repoURL, "https://github.com/")
	raw = strings.TrimSuffix(raw, "/")

	for _, branch := range []string{"main", "master"} {
		url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/README.md", raw, branch)
		body, err := httpGet(url)
		if err == nil {
			return body, nil
		}
	}

	return "", fmt.Errorf("could not fetch README from %s (tried main and master branches)", repoURL)
}

func httpGet(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

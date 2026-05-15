package bot

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/generative-ai-go/genai"
)

func LinkedInTools() []*genai.Tool {
	return []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "fetch_github_readme",
					Description: "Fetch the README.md content from a GitHub repository. Use this when you need to understand a project before writing a post about it.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"repo_url": {
								Type:        genai.TypeString,
								Description: "GitHub repository URL (e.g. https://github.com/user/repo)",
							},
						},
						Required: []string{"repo_url"},
					},
				},
				{
					Name:        "fetch_url_content",
					Description: "Fetch and extract text content from any URL. Use this to read articles, blog posts, or documentation that could inform a LinkedIn post.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"url": {
								Type:        genai.TypeString,
								Description: "The URL to fetch content from",
							},
						},
						Required: []string{"url"},
					},
				},
				{
					Name:        "post_to_linkedin",
					Description: "Publish a text post to LinkedIn. Only call this when the user has explicitly approved the post content. Never call this without user confirmation.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"text": {
								Type:        genai.TypeString,
								Description: "The post content to publish on LinkedIn",
							},
						},
						Required: []string{"text"},
					},
				},
				{
					Name:        "get_github_repos",
					Description: "List public repositories for a GitHub user. Use this to find interesting projects to post about.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"username": {
								Type:        genai.TypeString,
								Description: "GitHub username",
							},
						},
						Required: []string{"username"},
					},
				},
			},
		},
	}
}

func ExecuteTool(name string, args map[string]interface{}) (string, error) {
	switch name {
	case "fetch_github_readme":
		repoURL, _ := args["repo_url"].(string)
		return toolFetchGitHubReadme(repoURL)
	case "fetch_url_content":
		url, _ := args["url"].(string)
		return toolFetchURL(url)
	case "post_to_linkedin":
		text, _ := args["text"].(string)
		return toolPostToLinkedIn(text)
	case "get_github_repos":
		username, _ := args["username"].(string)
		return toolGetGitHubRepos(username)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func toolFetchGitHubReadme(repoURL string) (string, error) {
	readme, err := fetchReadme(repoURL)
	if err != nil {
		return "", err
	}
	if len(readme) > 4000 {
		readme = readme[:4000] + "\n... (truncated)"
	}
	return readme, nil
}

func toolFetchURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	text := stripHTML(string(body))
	if len(text) > 4000 {
		text = text[:4000] + "\n... (truncated)"
	}
	return text, nil
}

func toolPostToLinkedIn(text string) (string, error) {
	link, err := PostToLinkedIn(text)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully posted to LinkedIn: %s", link), nil
}

func toolGetGitHubRepos(username string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/repos?sort=updated&per_page=15&type=owner", username)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("github API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, body)
	}

	return string(body), nil
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var multiSpaceRe = regexp.MustCompile(`\s{3,}`)

func stripHTML(s string) string {
	s = regexp.MustCompile(`<script[^>]*>[\s\S]*?</script>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`<style[^>]*>[\s\S]*?</style>`).ReplaceAllString(s, "")
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = multiSpaceRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

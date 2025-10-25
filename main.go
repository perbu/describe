package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

//go:embed .version
var embeddedVersion string

type config struct {
	apiKey string
	model  string
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	err := run(ctx, os.Stdout, os.Args[1:], os.Environ())
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, output io.Writer, argv []string, env []string) error {
	_, _ = fmt.Fprintf(output, "describe %s\n", strings.TrimSpace(embeddedVersion))

	runConfig, showHelp, err := getConfig(argv, env)
	if err != nil {
		return fmt.Errorf("getConfig: %w", err)
	}
	if showHelp {
		return nil
	}

	repo, err := git.PlainOpen(".")
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	changes, err := getStagedChanges(repo)
	if err != nil {
		return fmt.Errorf("getStagedChanges: %w", err)
	}

	if changes == "" {
		_, _ = fmt.Fprintf(output, "No staged changes found.\n")
		return nil
	}

	description, err := describeChanges(ctx, runConfig, changes)
	if err != nil {
		return fmt.Errorf("describeChanges: %w", err)
	}

	_, _ = fmt.Fprintf(output, "\n%s\n", description)
	return nil
}

func getConfig(args []string, env []string) (config, bool, error) {
	var cfg config
	var showhelp bool

	flagSet := flag.NewFlagSet("describe", flag.ContinueOnError)
	flagSet.StringVar(&cfg.model, "model", "anthropic/claude-4.5-sonnet", "Model to use for description.")
	flagSet.BoolVar(&showhelp, "help", false, "Show help message.")

	err := flagSet.Parse(args)
	if err != nil {
		return config{}, false, fmt.Errorf("failed to parse flags: %w", err)
	}
	if showhelp {
		flagSet.Usage()
		return config{}, true, nil
	}

	// check if there are any arguments left
	if flagSet.NArg() > 0 {
		return config{}, false, fmt.Errorf("unexpected arguments: %s", flagSet.Args())
	}

	// Get API key from environment
	cfg.apiKey = getEnv(env, "OPENROUTER_API_KEY")
	if cfg.apiKey == "" {
		return config{}, false, fmt.Errorf("OPENROUTER_API_KEY environment variable not set")
	}

	return cfg, false, nil
}

func getEnv(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

func getStagedChanges(repo *git.Repository) (string, error) {
	w, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("repo.Worktree: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return "", fmt.Errorf("worktree.Status: %w", err)
	}

	// Try to get HEAD, but handle the case where there are no commits yet
	head, err := repo.Head()
	var headTree *object.Tree
	if err == nil {
		headCommit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return "", fmt.Errorf("failed to get HEAD commit: %w", err)
		}
		headTree, err = headCommit.Tree()
		if err != nil {
			return "", fmt.Errorf("failed to get HEAD tree: %w", err)
		}
	}
	// If HEAD doesn't exist (no commits yet), headTree will be nil and we'll treat all files as new

	var changes strings.Builder

	for path, fileStatus := range status {
		// Only process staged files
		if fileStatus.Staging == git.Unmodified {
			continue
		}

		changes.WriteString(fmt.Sprintf("\n=== %s ===\n", path))
		changes.WriteString(fmt.Sprintf("Status: %s\n\n", stagingStatusString(fileStatus.Staging)))

		// Get the staged content
		stagedEntry, err := w.Filesystem.Stat(path)
		if err == nil && !stagedEntry.IsDir() {
			stagedFile, err := w.Filesystem.Open(path)
			if err == nil {
				stagedContent, _ := io.ReadAll(stagedFile)
				stagedFile.Close()

				// Get the HEAD version for comparison (if HEAD exists)
				var headContent string
				if headTree != nil {
					headEntry, err := headTree.File(path)
					if err == nil {
						headContent, _ = headEntry.Contents()
					}
				}

				// Show a simple diff representation
				if headContent == "" && len(stagedContent) > 0 {
					changes.WriteString("New file:\n")
					changes.WriteString(string(stagedContent))
				} else if len(stagedContent) == 0 {
					changes.WriteString("Deleted file\n")
				} else {
					changes.WriteString("Modified file:\n")
					changes.WriteString(string(stagedContent))
				}
				changes.WriteString("\n")
			}
		}
	}

	return changes.String(), nil
}

func stagingStatusString(status git.StatusCode) string {
	switch status {
	case git.Added:
		return "Added"
	case git.Modified:
		return "Modified"
	case git.Deleted:
		return "Deleted"
	case git.Renamed:
		return "Renamed"
	case git.Copied:
		return "Copied"
	default:
		return "Unknown"
	}
}

func describeChanges(ctx context.Context, cfg config, changes string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}

	prompt := fmt.Sprintf(`You are a helpful assistant that writes git commit messages.
Based on the following staged changes, generate a properly formatted git commit message.

Format requirements:
- First line: Short summary (50-72 chars) describing WHAT changed and WHY
- Second line: Blank line
- Following lines: More detailed explanation of the changes, their purpose and impact

Staged changes:
%s

Generate the commit message:`, changes)

	reqBody := request{
		Model: cfg.model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

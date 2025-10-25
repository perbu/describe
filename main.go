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
	debug  bool
}

var debugLog = func(format string, args ...interface{}) {
	// no-op by default
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	err := run(ctx, os.Stdout, os.Args[1:])
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, output io.Writer, argv []string) error {
	runConfig, showHelp, err := getConfig(argv)
	if err != nil {
		return fmt.Errorf("getConfig: %w", err)
	}
	if showHelp {
		return nil
	}

	// Enable debug logging if requested
	if runConfig.debug {
		debugLog = func(format string, args ...interface{}) {
			fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
		}
		debugLog("Debug mode enabled")
		debugLog("Version: %s", strings.TrimSpace(embeddedVersion))
		debugLog("Model: %s", runConfig.model)
	}

	debugLog("Opening git repository")
	repo, err := git.PlainOpen(".")
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	debugLog("Getting staged changes")
	changes, err := getStagedChanges(repo)
	if err != nil {
		return fmt.Errorf("getStagedChanges: %w", err)
	}

	if changes == "" {
		debugLog("No staged changes found")
		_, _ = fmt.Fprintf(output, "No staged changes found.\n")
		return nil
	}

	debugLog("Found staged changes (%d bytes)", len(changes))
	debugLog("Calling OpenRouter API")
	description, err := describeChanges(ctx, runConfig, changes)
	if err != nil {
		return fmt.Errorf("describeChanges: %w", err)
	}

	debugLog("Received description from API (%d bytes)", len(description))
	_, _ = fmt.Fprintf(output, "%s\n", description)
	return nil
}

func getConfig(args []string) (config, bool, error) {
	var cfg config
	var showhelp bool

	flagSet := flag.NewFlagSet("describe", flag.ContinueOnError)
	flagSet.StringVar(&cfg.model, "model", "anthropic/claude-4.5-sonnet", "Model to use for description.")
	flagSet.BoolVar(&cfg.debug, "debug", false, "Enable debug logging.")
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
	cfg.apiKey = os.Getenv("OPENROUTER_API_KEY")
	if cfg.apiKey == "" {
		return config{}, false, fmt.Errorf("OPENROUTER_API_KEY environment variable not set")
	}

	return cfg, false, nil
}

func getStagedChanges(repo *git.Repository) (string, error) {
	debugLog("Getting worktree")
	w, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("repo.Worktree: %w", err)
	}

	debugLog("Getting status")
	status, err := w.Status()
	if err != nil {
		return "", fmt.Errorf("worktree.Status: %w", err)
	}

	// Try to get HEAD, but handle the case where there are no commits yet
	debugLog("Getting HEAD")
	head, err := repo.Head()
	var headTree *object.Tree
	if err == nil {
		debugLog("HEAD found, getting commit")
		headCommit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return "", fmt.Errorf("failed to get HEAD commit: %w", err)
		}
		headTree, err = headCommit.Tree()
		if err != nil {
			return "", fmt.Errorf("failed to get HEAD tree: %w", err)
		}
	} else {
		debugLog("No HEAD found (new repository)")
	}
	// If HEAD doesn't exist (no commits yet), headTree will be nil and we'll treat all files as new

	var changes strings.Builder
	stagedFileCount := 0

	for path, fileStatus := range status {
		// Only process files that are actually staged (not unmodified, untracked, or unknown)
		if fileStatus.Staging == git.Unmodified || fileStatus.Staging == git.Untracked {
			continue
		}
		stagedFileCount++
		debugLog("Processing staged file: %s (status: %s)", path, stagingStatusString(fileStatus.Staging))

		changes.WriteString(fmt.Sprintf("\n=== %s ===\n", path))
		changes.WriteString(fmt.Sprintf("Status: %s\n\n", stagingStatusString(fileStatus.Staging)))

		// Get the staged content from the index
		var stagedContent string
		if fileStatus.Staging == git.Deleted {
			stagedContent = ""
		} else {
			// Read from worktree index (staged content)
			file, err := w.Filesystem.Open(path)
			if err == nil {
				content, _ := io.ReadAll(file)
				file.Close()
				stagedContent = string(content)
			}
		}

		// Get the HEAD version for comparison (if HEAD exists)
		var headContent string
		if headTree != nil {
			headEntry, err := headTree.File(path)
			if err == nil {
				headContent, _ = headEntry.Contents()
			}
		}

		// Show a simple diff representation
		if headContent == "" && stagedContent != "" {
			changes.WriteString("New file:\n")
			changes.WriteString(stagedContent)
		} else if stagedContent == "" {
			changes.WriteString("Deleted file\n")
		} else {
			changes.WriteString("Modified file:\n")
			changes.WriteString(stagedContent)
		}
		changes.WriteString("\n")
	}

	debugLog("Processed %d staged files", stagedFileCount)
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

	debugLog("Sending request to OpenRouter API (payload size: %d bytes)", len(jsonBody))
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

	debugLog("Received response with status: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debugLog("API error response: %s", string(body))
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
		debugLog("API returned empty choices array")
		return "", fmt.Errorf("no response from API")
	}

	debugLog("Successfully decoded API response")
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

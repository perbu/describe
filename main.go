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
	"path/filepath"
	"strings"
	"syscall"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"
)

//go:embed .version
var embeddedVersion string

// fileConfig represents the YAML config file structure
type fileConfig struct {
	Provider    string `yaml:"provider"`     // "openrouter" or "ollama"
	APIKey      string `yaml:"api_key"`      // For OpenRouter
	APIEndpoint string `yaml:"api_endpoint"` // Custom endpoint (optional)
	Model       string `yaml:"model"`
	Debug       bool   `yaml:"debug"`
	MaxLines    int    `yaml:"max_lines"`
}

// config represents the runtime configuration
type config struct {
	provider    string
	apiKey      string
	apiEndpoint string
	model       string
	debug       bool
	maxLines    int
}

var debugLog = func(format string, args ...interface{}) {
	// no-op by default
}

// ignoredDirs contains directory names that should be skipped
var ignoredDirs = []string{
	"vendor",
	"node_modules",
	".git",
	"dist",
	"build",
	"target",
	".next",
	".nuxt",
	"__pycache__",
	".pytest_cache",
	".tox",
	"venv",
	".venv",
}

// shouldIgnorePath checks if a path should be ignored based on directory patterns
func shouldIgnorePath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		for _, ignored := range ignoredDirs {
			if part == ignored {
				return true
			}
		}
	}
	return false
}

// isBinary checks if a file appears to be binary by examining its contents
func isBinary(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Read a sample — 8KB is enough to classify most files.
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	buf = buf[:n]

	// Empty files are not binary
	if n == 0 {
		return false, nil
	}

	// Heuristic: if any NUL (0x00) bytes exist, assume binary.
	if bytes.IndexByte(buf, 0x00) != -1 {
		return true, nil
	}

	// Heuristic: count printable ASCII and UTF-8 valid characters.
	printable := 0
	for _, b := range buf {
		if b == 9 || b == 10 || b == 13 || (b >= 32 && b <= 126) {
			printable++
		}
	}
	ratio := float64(printable) / float64(len(buf))
	return ratio < 0.95, nil // mostly printable = text
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
	changes, err := getStagedChanges(repo, runConfig)
	if err != nil {
		return fmt.Errorf("getStagedChanges: %w", err)
	}

	if changes == "" {
		debugLog("No staged changes found")
		_, _ = fmt.Fprintf(output, "No staged changes found.\n")
		return nil
	}

	debugLog("Found staged changes (%d bytes)", len(changes))
	debugLog("Calling %s API", runConfig.provider)
	description, err := describeChanges(ctx, runConfig, changes)
	if err != nil {
		return fmt.Errorf("describeChanges: %w", err)
	}

	debugLog("Received description from API (%d bytes)", len(description))
	_, _ = fmt.Fprintf(output, "%s\n", description)
	return nil
}

// loadConfigFile loads configuration from the YAML file
func loadConfigFile() (fileConfig, error) {
	// Get config directory using stdlib
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fileConfig{}, fmt.Errorf("failed to get config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "describe", "config.yaml")

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		debugLog("No config file found at %s, using defaults", configPath)
		return fileConfig{
			Provider:    "ollama",
			APIEndpoint: "http://localhost:11434",
			Model:       "llama3.2",
			MaxLines:    10000,
		}, nil
	}

	debugLog("Loading config from %s", configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fileConfig{}, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg fileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults if not specified in config file
	if cfg.Provider == "" {
		cfg.Provider = "ollama"
	}
	if cfg.APIEndpoint == "" {
		if cfg.Provider == "ollama" {
			cfg.APIEndpoint = "http://localhost:11434"
		} else if cfg.Provider == "openrouter" {
			cfg.APIEndpoint = "https://openrouter.ai/api/v1"
		}
	}
	if cfg.Model == "" {
		if cfg.Provider == "ollama" {
			cfg.Model = "llama3.2"
		} else {
			cfg.Model = "anthropic/claude-4.5-sonnet"
		}
	}
	if cfg.MaxLines == 0 {
		cfg.MaxLines = 10000
	}

	return cfg, nil
}

func getConfig(args []string) (config, bool, error) {
	// Load config from file first
	fileCfg, err := loadConfigFile()
	if err != nil {
		return config{}, false, fmt.Errorf("loadConfigFile: %w", err)
	}

	// Initialize runtime config with file config values
	var cfg config
	cfg.provider = fileCfg.Provider
	cfg.apiKey = fileCfg.APIKey
	cfg.apiEndpoint = fileCfg.APIEndpoint
	cfg.model = fileCfg.Model
	cfg.debug = fileCfg.Debug
	cfg.maxLines = fileCfg.MaxLines

	var showhelp bool
	var modelFlag, providerFlag, endpointFlag string

	flagSet := flag.NewFlagSet("describe", flag.ContinueOnError)
	flagSet.StringVar(&providerFlag, "provider", "", "API provider (openrouter or ollama)")
	flagSet.StringVar(&modelFlag, "model", "", "Model to use for description")
	flagSet.StringVar(&endpointFlag, "endpoint", "", "API endpoint URL")
	flagSet.BoolVar(&cfg.debug, "debug", cfg.debug, "Enable debug logging")
	flagSet.IntVar(&cfg.maxLines, "max-lines", cfg.maxLines, "Maximum number of lines to process")
	flagSet.BoolVar(&showhelp, "help", false, "Show help message")

	err = flagSet.Parse(args)
	if err != nil {
		return config{}, false, fmt.Errorf("failed to parse flags: %w", err)
	}
	if showhelp {
		flagSet.Usage()
		return config{}, true, nil
	}

	// CLI flags override config file
	if providerFlag != "" {
		cfg.provider = providerFlag
		// If provider changed and model/endpoint weren't explicitly set, use provider's defaults
		if modelFlag == "" {
			if cfg.provider == "ollama" {
				cfg.model = "llama3.2"
			} else if cfg.provider == "openrouter" {
				cfg.model = "anthropic/claude-4.5-sonnet"
			}
		}
		if endpointFlag == "" {
			if cfg.provider == "ollama" {
				cfg.apiEndpoint = "http://localhost:11434"
			} else if cfg.provider == "openrouter" {
				cfg.apiEndpoint = "https://openrouter.ai/api/v1"
			}
		}
	}
	if modelFlag != "" {
		cfg.model = modelFlag
	}
	if endpointFlag != "" {
		cfg.apiEndpoint = endpointFlag
	}

	// check if there are any arguments left
	if flagSet.NArg() > 0 {
		return config{}, false, fmt.Errorf("unexpected arguments: %s", flagSet.Args())
	}

	// Get API key from environment if not in config file (for OpenRouter)
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("OPENROUTER_API_KEY")
	}

	// Validate provider
	if cfg.provider != "openrouter" && cfg.provider != "ollama" {
		return config{}, false, fmt.Errorf("invalid provider: %s (must be 'openrouter' or 'ollama')", cfg.provider)
	}

	// Check API key for OpenRouter
	if cfg.provider == "openrouter" && cfg.apiKey == "" {
		return config{}, false, fmt.Errorf("OPENROUTER_API_KEY environment variable or api_key in config file required for OpenRouter provider")
	}

	return cfg, false, nil
}

func getStagedChanges(repo *git.Repository, cfg config) (string, error) {
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

	// Try to get HEAD tree, handle case where there are no commits yet
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
		// For new repository, use empty tree
		headTree = &object.Tree{}
	}

	// Filter out binary files and ignored paths before generating diff
	var filesToInclude []string
	stagedFileCount := 0
	for path, fileStatus := range status {
		// Only process files that are actually staged
		if fileStatus.Staging == git.Unmodified || fileStatus.Staging == git.Untracked {
			continue
		}

		// Skip ignored directories
		if shouldIgnorePath(path) {
			debugLog("Skipping ignored path: %s", path)
			continue
		}

		// Skip binary files (unless deleted)
		if fileStatus.Staging != git.Deleted {
			binary, err := isBinary(path)
			if err != nil {
				debugLog("Error checking if file is binary: %s: %v", path, err)
			} else if binary {
				debugLog("Skipping binary file: %s", path)
				continue
			}
		}

		stagedFileCount++
		debugLog("Processing staged file: %s (status: %s)", path, stagingStatusString(fileStatus.Staging))
		filesToInclude = append(filesToInclude, path)
	}

	if stagedFileCount == 0 {
		return "", nil
	}

	// Get the index to access staged file hashes
	debugLog("Getting index")
	idx, err := repo.Storer.Index()
	if err != nil {
		return "", fmt.Errorf("failed to get index: %w", err)
	}

	// Create a map of paths to hashes from the index
	indexMap := make(map[string]plumbing.Hash)
	for _, entry := range idx.Entries {
		indexMap[entry.Name] = entry.Hash
	}

	// Manually generate diffs by fetching blob contents
	debugLog("Generating diffs for staged files")
	var patchBuf strings.Builder

	for _, path := range filesToInclude {
		fileStatus := status[path]

		// Get HEAD content
		var headContent string
		var headHash plumbing.Hash
		if fileStatus.Staging != git.Added && headTree != nil {
			headFile, err := headTree.File(path)
			if err == nil {
				headContent, _ = headFile.Contents()
				headHash = headFile.Hash
			}
		}

		// Get staged content from index
		var stagedContent string
		var stagedHash plumbing.Hash
		if fileStatus.Staging != git.Deleted {
			if hash, ok := indexMap[path]; ok {
				stagedHash = hash
				// Fetch the blob object
				blob, err := repo.BlobObject(hash)
				if err == nil {
					reader, _ := blob.Reader()
					content, _ := io.ReadAll(reader)
					reader.Close()
					stagedContent = string(content)
				}
			}
		}

		// Generate diff header
		if fileStatus.Staging == git.Added {
			patchBuf.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))
			patchBuf.WriteString("new file mode 100644\n")
			patchBuf.WriteString(fmt.Sprintf("index 0000000..%s\n", stagedHash.String()[:7]))
			patchBuf.WriteString("--- /dev/null\n")
			patchBuf.WriteString(fmt.Sprintf("+++ b/%s\n", path))
		} else if fileStatus.Staging == git.Deleted {
			patchBuf.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))
			patchBuf.WriteString("deleted file mode 100644\n")
			patchBuf.WriteString(fmt.Sprintf("index %s..0000000\n", headHash.String()[:7]))
			patchBuf.WriteString(fmt.Sprintf("--- a/%s\n", path))
			patchBuf.WriteString("+++ /dev/null\n")
		} else {
			patchBuf.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))
			patchBuf.WriteString(fmt.Sprintf("index %s..%s 100644\n", headHash.String()[:7], stagedHash.String()[:7]))
			patchBuf.WriteString(fmt.Sprintf("--- a/%s\n", path))
			patchBuf.WriteString(fmt.Sprintf("+++ b/%s\n", path))
		}

		// Generate unified diff content
		diffContent := generateUnifiedDiffContent(headContent, stagedContent)
		patchBuf.WriteString(diffContent)
	}

	patchStr := patchBuf.String()
	lineCount := strings.Count(patchStr, "\n")

	// Check if we've exceeded the limit
	if cfg.maxLines > 0 && lineCount > cfg.maxLines {
		return "", fmt.Errorf("staged changes exceed maximum line limit of %d (currently at %d lines). Consider staging fewer files or using -max-lines flag to increase the limit", cfg.maxLines, lineCount)
	}

	debugLog("Processed %d staged files (%d total lines)", stagedFileCount, lineCount)
	return patchStr, nil
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

// generateUnifiedDiffContent creates a unified diff from two strings
func generateUnifiedDiffContent(oldContent, newContent string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Handle empty content
	if oldContent == "" {
		oldLines = []string{}
	}
	if newContent == "" {
		newLines = []string{}
	}

	// Simple line-by-line diff (not optimal but works for our purpose)
	var result strings.Builder

	// For simplicity, we'll use a basic diff strategy
	// Find common prefix and suffix
	commonPrefix := 0
	minLen := len(oldLines)
	if len(newLines) < minLen {
		minLen = len(newLines)
	}

	for commonPrefix < minLen && oldLines[commonPrefix] == newLines[commonPrefix] {
		commonPrefix++
	}

	commonSuffix := 0
	oldEnd := len(oldLines)
	newEnd := len(newLines)
	for commonSuffix < (minLen-commonPrefix) &&
		oldLines[oldEnd-1-commonSuffix] == newLines[newEnd-1-commonSuffix] {
		commonSuffix++
	}

	// Calculate hunk ranges
	oldStart := commonPrefix
	oldCount := len(oldLines) - commonPrefix - commonSuffix
	newStart := commonPrefix
	newCount := len(newLines) - commonPrefix - commonSuffix

	// If there are no changes, return empty
	if oldCount == 0 && newCount == 0 {
		return ""
	}

	// Add context lines (3 before and after)
	contextLines := 3
	oldStart = oldStart - contextLines
	if oldStart < 0 {
		oldStart = 0
	}
	newStart = newStart - contextLines
	if newStart < 0 {
		newStart = 0
	}

	oldEnd = commonPrefix + oldCount + contextLines
	if oldEnd > len(oldLines) {
		oldEnd = len(oldLines)
	}
	newEnd = commonPrefix + newCount + contextLines
	if newEnd > len(newLines) {
		newEnd = len(newLines)
	}

	oldCount = oldEnd - oldStart
	newCount = newEnd - newStart

	// Write hunk header
	result.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
		oldStart+1, oldCount, newStart+1, newCount))

	// Write context before changes
	for i := oldStart; i < commonPrefix && i < oldEnd; i++ {
		result.WriteString(" " + oldLines[i] + "\n")
	}

	// Write removed lines
	for i := commonPrefix; i < commonPrefix+oldCount-contextLines && i < len(oldLines)-commonSuffix; i++ {
		if i < len(oldLines) {
			result.WriteString("-" + oldLines[i] + "\n")
		}
	}

	// Write added lines
	for i := commonPrefix; i < commonPrefix+newCount-contextLines && i < len(newLines)-commonSuffix; i++ {
		if i < len(newLines) {
			result.WriteString("+" + newLines[i] + "\n")
		}
	}

	// Write context after changes
	startSuffix := len(oldLines) - commonSuffix
	for i := startSuffix; i < oldEnd && i < len(oldLines); i++ {
		result.WriteString(" " + oldLines[i] + "\n")
	}

	return result.String()
}

func describeChanges(ctx context.Context, cfg config, changes string) (string, error) {
	if cfg.provider == "ollama" {
		return describeChangesOllama(ctx, cfg, changes)
	}
	return describeChangesOpenRouter(ctx, cfg, changes)
}

func describeChangesOllama(ctx context.Context, cfg config, changes string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
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
		Stream: false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	debugLog("Sending request to Ollama API (payload size: %d bytes)", len(jsonBody))
	if cfg.debug {
		fmt.Fprintln(os.Stderr, "[DEBUG] === Full prompt being sent to LLM ===")
		fmt.Fprintln(os.Stderr, prompt)
		fmt.Fprintln(os.Stderr, "[DEBUG] === End of prompt ===")
	}

	endpoint := cfg.apiEndpoint + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Message.Content == "" {
		debugLog("API returned empty message content")
		return "", fmt.Errorf("no response from API")
	}

	debugLog("Successfully decoded API response")
	return strings.TrimSpace(result.Message.Content), nil
}

func describeChangesOpenRouter(ctx context.Context, cfg config, changes string) (string, error) {
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
	if cfg.debug {
		fmt.Fprintln(os.Stderr, "[DEBUG] === Full prompt being sent to LLM ===")
		fmt.Fprintln(os.Stderr, prompt)
		fmt.Fprintln(os.Stderr, "[DEBUG] === End of prompt ===")
	}

	endpoint := cfg.apiEndpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
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

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based command-line tool called "describe" that uses AI to generate descriptions of staged git changes. It supports both Ollama (local) and OpenRouter (cloud) API providers and returns a natural language summary.

## Key Architecture

- **Single-file application**: The entire application is contained in `main.go` (~750 lines)
- **Git integration**: Uses `go-git/go-git/v5` library for reading staged changes
- **AI integration**: Supports two providers:
  - **Ollama** (default): Local AI models via Ollama API
  - **OpenRouter**: Cloud-based AI models via OpenRouter API
- **Configuration**: YAML-based config file with command-line overrides
- **Config location**: Uses `os.UserConfigDir()` to find the config directory
- **Embedded version**: The tool's own version is embedded from `.version` file using `//go:embed`

## Core Workflow

1. Loads configuration from YAML file (or uses defaults)
2. Opens the git repository in the current directory
3. Reads all staged changes (files in the staging area)
4. Formats the changes with file paths and content
5. Sends changes to the configured AI provider (Ollama or OpenRouter)
6. Returns AI-generated description of what the changes do

## Development Commands

```bash
# Build and test the application
go build -o /dev/null .

# Run tests
go test -v

# Run the application (using default Ollama provider)
go run main.go

# Run with OpenRouter provider
export OPENROUTER_API_KEY="your-api-key-here"
go run main.go -provider openrouter

# Install from source
go install github.com/perbu/describe@latest
```

## Configuration

### Config File

Config file location: `$XDG_CONFIG_HOME/describe/config.yaml` (or platform equivalent via `os.UserConfigDir()`)

Example config file:
```yaml
provider: ollama          # or "openrouter"
api_endpoint: http://localhost:11434  # optional, defaults based on provider
model: llama3.2          # model name
api_key: ""              # only needed for openrouter
debug: false
max_lines: 10000
```

### Command Line Interface

- `-provider string`: API provider (ollama or openrouter)
- `-model string`: Model to use for description
- `-endpoint string`: Custom API endpoint URL
- `-debug`: Enable debug logging
- `-max-lines int`: Maximum number of lines to process (default: 10000)
- `-help`: Show usage information

Command-line flags override config file values.

## Important Constraints

- Must be run from within a git repository
- Only describes staged changes (files added to staging area with `git add`)
- **For Ollama**: Requires Ollama to be running locally (default: http://localhost:11434)
- **For OpenRouter**: Requires `OPENROUTER_API_KEY` environment variable or config file setting

## API Integration

### Ollama Provider (Default)
- Default endpoint: `http://localhost:11434/api/chat`
- Default model: `llama3.2`
- No authentication required
- Requires Ollama to be installed and running locally

### OpenRouter Provider
- Default endpoint: `https://openrouter.ai/api/v1/chat/completions`
- Default model: `anthropic/claude-4.5-sonnet`
- Authentication: Bearer token via `OPENROUTER_API_KEY` environment variable or config file
- Requires internet connection

## Key Functions

- `loadConfigFile()`: Loads YAML configuration from user config directory (main.go:177)
- `getConfig()`: Merges config file with CLI flags (main.go:234)
- `getStagedChanges()`: Reads staged files from git worktree (main.go:304)
- `describeChanges()`: Routes to appropriate API provider (main.go:576)
- `describeChangesOllama()`: Calls Ollama API (main.go:583)
- `describeChangesOpenRouter()`: Calls OpenRouter API (main.go:669)

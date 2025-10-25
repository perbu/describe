# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based command-line tool called "describe" that uses AI to generate descriptions of staged git changes. It sends your staged changes to OpenRouter's API and returns a natural language summary.

## Key Architecture

- **Single-file application**: The entire application is contained in `main.go` (~270 lines)
- **Git integration**: Uses `go-git/go-git/v5` library for reading staged changes
- **AI integration**: Uses OpenRouter API (OpenAI-compatible) for generating descriptions
- **API Key**: Reads from `OPENROUTER_API_KEY` environment variable
- **Embedded version**: The tool's own version is embedded from `.version` file using `//go:embed`

## Core Workflow

1. Opens the git repository in the current directory
2. Reads all staged changes (files in the staging area)
3. Formats the changes with file paths and content
4. Sends changes to OpenRouter API with a prompt
5. Returns AI-generated description of what the changes do

## Development Commands

```bash
# Build and test the application
go build -o /dev/null .

# Run tests
go test -v

# Run the application (requires OPENROUTER_API_KEY)
export OPENROUTER_API_KEY="your-api-key-here"
go run main.go

# Install from source
go install github.com/perbu/describe@latest
```

## Command Line Interface

- `-model string`: Specify AI model to use (default: "anthropic/claude-3.5-sonnet")
- `-help`: Show usage information

## Important Constraints

- Requires `OPENROUTER_API_KEY` environment variable to be set
- Must be run from within a git repository
- Only describes staged changes (files added to staging area with `git add`)
- Requires internet connection to reach OpenRouter API

## API Integration

The tool uses OpenRouter's OpenAI-compatible API:
- Endpoint: `https://openrouter.ai/api/v1/chat/completions`
- Authentication: Bearer token via `OPENROUTER_API_KEY`
- Default model: `anthropic/claude-3.5-sonnet`

Key functions:
- `getStagedChanges()`: Reads staged files from git worktree (main.go:115)
- `describeChanges()`: Calls OpenRouter API with changes (main.go:202)

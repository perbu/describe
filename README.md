# describe

A command-line tool that generates git commit messages from staged changes using AI.

## How it works

The tool reads your staged git changes, sends them to OpenRouter's API, and returns a formatted commit message with a summary line and detailed description.

## Installation

```bash
go install github.com/perbu/describe@latest
```

## Configuration

Set the `OPENROUTER_API_KEY` environment variable:

```bash
export OPENROUTER_API_KEY="your-api-key"
```

## Usage

Stage your changes and run:

```bash
git add .
describe
```

The default model is `anthropic/claude-4.5-sonnet`. To use a different model:

```bash
describe -model openai/gpt-4
```

## Requirements

- Go 1.24 or later
- OpenRouter API key
- Git repository with staged changes

## License

BSD 3-Clause

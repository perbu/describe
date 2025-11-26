# describe

A command-line tool that generates git commit messages from staged changes using AI.

## How it works

The tool reads your staged git changes, sends them to an AI provider (Ollama or OpenRouter), and returns a formatted commit message with a summary line and detailed description.

## Installation

```bash
go install github.com/perbu/describe@latest
```

## Configuration

### Quick Start with Ollama (Default)

1. Install and run [Ollama](https://ollama.ai)
2. Pull a model: `ollama pull llama3.2`
3. Run `describe` - it works out of the box!

### Using OpenRouter

Set the `OPENROUTER_API_KEY` environment variable:

```bash
export OPENROUTER_API_KEY="your-api-key"
describe -provider openrouter
```

### Using vLLM

You can use vLLM's OpenAI-compatible API by using the `openrouter` provider:

```bash
describe -provider openrouter -endpoint http://your-vllm-host:8000/v1 -model your-model-name
```

Or via config file (see below).

### Config File (Optional)

Create a config file at:
- **Linux**: `~/.config/describe/config.yaml`
- **macOS**: `~/Library/Application Support/describe/config.yaml`

**Ollama example:**
```yaml
provider: ollama
model: llama3.2
api_endpoint: http://localhost:11434
debug: false
max_lines: 10000
```

**OpenRouter example:**
```yaml
provider: openrouter
model: anthropic/claude-4.5-sonnet
api_endpoint: https://openrouter.ai/api/v1
api_key: "your-api-key-here"
debug: false
max_lines: 10000
```

**vLLM example:**
```yaml
provider: openrouter  # Use openrouter provider for vLLM's OpenAI-compatible API
model: Qwen/Qwen2.5-Coder-32B-Instruct-AWQ  # Your vLLM model name
api_endpoint: http://your-vllm-host:8000/v1
api_key: "not_needed"  # vLLM doesn't require auth by default
debug: false
max_lines: 10000
```

See `config.yaml.example` for a complete example.

## Usage

Stage your changes and run:

```bash
git add .
describe
```

### Command-line Options

```bash
# Use a different model
describe -model codellama

# Use OpenRouter instead of Ollama
describe -provider openrouter -model anthropic/claude-4.5-sonnet

# Use a custom endpoint
describe -endpoint http://localhost:8080

# Enable debug logging
describe -debug

# Adjust maximum lines to process
describe -max-lines 5000
```

## Requirements

- Go 1.24 or later
- Git repository with staged changes
- **One of the following AI providers**:
  - [Ollama](https://ollama.ai) installed and running locally (default)
  - OpenRouter API key for cloud-based models
  - [vLLM](https://docs.vllm.ai/) server with OpenAI-compatible API

## License

BSD 3-Clause

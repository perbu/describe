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

### Config File (Optional)

Create a config file at `~/.config/describe/config.yaml` (Linux/macOS).

```yaml
provider: ollama          # or "openrouter"
model: llama3.2          # or "anthropic/claude-4.5-sonnet" for openrouter
api_endpoint: http://localhost:11434  # optional
api_key: ""              # only needed for openrouter
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
- **Either**:
  - [Ollama](https://ollama.ai) installed and running locally (default), **or**
  - OpenRouter API key for cloud-based models

## License

BSD 3-Clause

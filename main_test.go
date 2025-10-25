package main

import (
	"os"
	"testing"

	"github.com/go-git/go-git/v5"
)

func TestShouldIgnorePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"vendor directory", "vendor/package.go", true},
		{"node_modules directory", "node_modules/package.js", true},
		{"nested vendor", "src/vendor/lib.go", true},
		{"nested node_modules", "app/node_modules/react.js", true},
		{"normal file", "main.go", false},
		{"src directory", "src/main.go", false},
		{".git directory", ".git/config", true},
		{"dist directory", "dist/bundle.js", true},
		{"build directory", "build/output.o", true},
		{"target directory", "target/release/binary", true},
		{".next directory", ".next/static/page.js", true},
		{".nuxt directory", ".nuxt/components.js", true},
		{"__pycache__ directory", "__pycache__/module.pyc", true},
		{".pytest_cache directory", ".pytest_cache/test.py", true},
		{".tox directory", ".tox/py38/lib.py", true},
		{"venv directory", "venv/lib/python3.9/site.py", true},
		{".venv directory", ".venv/bin/activate", true},
		{"multiple nested ignored", "src/vendor/node_modules/package.json", true},
		{"similar but not exact", "vendor_backup/file.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldIgnorePath(tt.path)
			if result != tt.expected {
				t.Errorf("shouldIgnorePath(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
		wantErr  bool
	}{
		{"text file", "testdata/text.txt", false, false},
		{"binary file", "testdata/binary.bin", true, false},
		{"empty file", "testdata/empty.txt", false, false},
		{"non-existent file", "testdata/nonexistent.txt", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := isBinary(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("isBinary(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("isBinary(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestStagingStatusString(t *testing.T) {
	tests := []struct {
		name     string
		status   git.StatusCode
		expected string
	}{
		{"Added", git.Added, "Added"},
		{"Modified", git.Modified, "Modified"},
		{"Deleted", git.Deleted, "Deleted"},
		{"Renamed", git.Renamed, "Renamed"},
		{"Copied", git.Copied, "Copied"},
		{"Unknown", git.Unmodified, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stagingStatusString(tt.status)
			if result != tt.expected {
				t.Errorf("stagingStatusString(%v) = %q, expected %q", tt.status, result, tt.expected)
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	// Save original env var and restore after test
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	tests := []struct {
		name           string
		args           []string
		envKey         string
		expectError    bool
		expectHelp     bool
		expectedModel  string
		expectedDebug  bool
		expectedMaxLines int
	}{
		{
			name:           "default values",
			args:           []string{},
			envKey:         "test-key",
			expectError:    false,
			expectHelp:     false,
			expectedModel:  "anthropic/claude-4.5-sonnet",
			expectedDebug:  false,
			expectedMaxLines: 10000,
		},
		{
			name:           "custom max-lines",
			args:           []string{"-max-lines", "5000"},
			envKey:         "test-key",
			expectError:    false,
			expectHelp:     false,
			expectedModel:  "anthropic/claude-4.5-sonnet",
			expectedDebug:  false,
			expectedMaxLines: 5000,
		},
		{
			name:           "custom model and debug",
			args:           []string{"-model", "gpt-4", "-debug"},
			envKey:         "test-key",
			expectError:    false,
			expectHelp:     false,
			expectedModel:  "gpt-4",
			expectedDebug:  true,
			expectedMaxLines: 10000,
		},
		{
			name:        "help flag",
			args:        []string{"-help"},
			envKey:      "test-key",
			expectError: false,
			expectHelp:  true,
		},
		{
			name:        "missing API key",
			args:        []string{},
			envKey:      "",
			expectError: true,
			expectHelp:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envKey != "" {
				os.Setenv("OPENROUTER_API_KEY", tt.envKey)
			} else {
				os.Unsetenv("OPENROUTER_API_KEY")
			}

			cfg, help, err := getConfig(tt.args)

			if (err != nil) != tt.expectError {
				t.Errorf("getConfig() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if help != tt.expectHelp {
				t.Errorf("getConfig() help = %v, expectHelp %v", help, tt.expectHelp)
				return
			}

			if !tt.expectError && !tt.expectHelp {
				if cfg.model != tt.expectedModel {
					t.Errorf("getConfig() model = %q, expected %q", cfg.model, tt.expectedModel)
				}
				if cfg.debug != tt.expectedDebug {
					t.Errorf("getConfig() debug = %v, expected %v", cfg.debug, tt.expectedDebug)
				}
				if cfg.maxLines != tt.expectedMaxLines {
					t.Errorf("getConfig() maxLines = %d, expected %d", cfg.maxLines, tt.expectedMaxLines)
				}
				if cfg.apiKey != tt.envKey {
					t.Errorf("getConfig() apiKey = %q, expected %q", cfg.apiKey, tt.envKey)
				}
			}
		})
	}
}

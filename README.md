# speedrun

> Swiss Army knife for on-call engineers

[![Go Report Card](https://goreportcard.com/badge/github.com/kennyp/speedrun)](https://goreportcard.com/report/github.com/kennyp/speedrun)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Latest Release](https://img.shields.io/github/v/release/kennyp/speedrun)](https://github.com/kennyp/speedrun/releases/latest)

Speedrun is a terminal-based productivity dashboard designed for on-call engineers who need to efficiently manage code reviews, pull requests, and repository workflows. With advanced filtering, intelligent automation, and optional AI assistance, speedrun helps you stay on top of your responsibilities without context switching between multiple tools and browser tabs.

## ✨ Features

- **🔍 Advanced Filtering**: Multi-criteria filtering by review status, PR type, repository, and more
- **🤖 AI-Powered Analysis**: Optional intelligent code review assistance with configurable models
- **⚡ Smart Automation**: Auto-merge capabilities, smart refresh, and workflow acceleration
- **💾 Intelligent Caching**: Persistent data storage with configurable retention policies
- **🔐 Secure Integration**: 1Password integration for credential management
- **⌨️ Keyboard-Driven**: Efficient TUI navigation designed for speed and productivity
- **🎯 Context-Aware**: Smart PR type detection and status-aware operations
- **📊 Rich Display**: Color-coded status indicators, diff stats, and check summaries

## 🚀 Installation

### Download Binary

Download the latest release for your platform from the [releases page](https://github.com/kennyp/speedrun/releases/latest):

```bash
# Linux
curl -L https://github.com/kennyp/speedrun/releases/latest/download/speedrun-linux-amd64 -o speedrun
chmod +x speedrun

# macOS
curl -L https://github.com/kennyp/speedrun/releases/latest/download/speedrun-darwin-amd64 -o speedrun
chmod +x speedrun

# Windows
# Download manually from releases page
```

### Install with Go

```bash
go install github.com/kennyp/speedrun/cmd/speedrun@latest
```

### Build from Source

```bash
git clone https://github.com/kennyp/speedrun.git
cd speedrun
just build  # or: go build -o bin/speedrun ./cmd/speedrun
```

## ⚡ Quick Start

1. **Initialize configuration**:
   ```bash
   speedrun init --edit
   ```

2. **Configure your credentials** in the generated config file:
   ```toml
   [github]
   token = "ghp_your_token_here"  # or "op://vault/GitHub/token"
   search_query = "is:open is:pr org:yourcompany"
   
   [ai]
   enabled = true
   api_key = "sk_your_key_here"  # or "op://vault/OpenAI/api-key"
   model = "gpt-4"
   ```

3. **Launch speedrun**:
   ```bash
   speedrun
   ```

## ⚙️ Configuration

Speedrun uses TOML configuration with support for environment variables and 1Password references. Run `speedrun init` to create a default config file.

### Core Settings

```toml
[github]
# Personal access token for GitHub API
token = "ghp_..." # or "op://vault/GitHub/token"
# Search query for finding PRs
search_query = "is:open is:pr org:yourcompany label:on-call"
# Auto-merge behavior: "true", "false", or "ask"
auto_merge_on_approval = "ask"

[ai]
# Enable AI-powered PR analysis
enabled = true
# API base URL (supports OpenAI, Azure, or custom endpoints)
base_url = "https://api.openai.com/v1"
# API key for AI service
api_key = "sk_..." # or "op://vault/OpenAI/api-key"
# Model to use for analysis
model = "gpt-4"
# Timeout for AI analysis
analysis_timeout = "2m"

[cache]
# Enable persistent caching
enabled = true
# Maximum age of cache entries
max_age = "7d"

[log]
# Log level: debug, info, warn, error
level = "info"
# Log output: file path, "stderr", or "default"
path = "default"
```

### Advanced Configuration

- **Check Filtering**: Configure which CI checks to ignore or require
- **Backoff Policies**: Customize retry behavior for GitHub and AI APIs
- **Client Timeouts**: Set request timeouts for different services
- **1Password Integration**: Use `op://vault/item/field` references for secure credential storage

See the generated config file for complete documentation of all options.

## 🎮 Usage

### Navigation

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate PR list |
| `Enter` | View PR details/diff |
| `a` | Approve PR |
| `v` | Enable auto-merge |
| `m` | Merge PR directly |
| `o` | Open PR in browser |
| `r` | Refresh PR list |
| `R` | Smart refresh (fetch latest) |

### Filtering

| Key | Action |
|-----|--------|
| `f` | Quick filter toggle |
| `F` | Advanced filter dialog |
| `Esc` | Clear filters |

#### Advanced Filtering Options

- **Review Status**: Not reviewed, approved, changes requested, commented
- **PR Type**: Code changes, documentation, dependencies, mixed
- **Repository**: Filter by specific repositories
- **Combinations**: Mix and match multiple criteria

### AI Analysis

When enabled, speedrun provides intelligent PR analysis including:
- **Risk Assessment**: High/Medium/Low risk categorization
- **Review Recommendations**: Approve, review carefully, or request changes
- **Key Insights**: Summary of important changes and potential issues
- **Tool Integration**: Automatic use of GitHub API and diff analysis tools

## 🛠️ Development

### Prerequisites

- Go 1.24+
- [just](https://github.com/casey/just) (optional, for build scripts)

### Building

```bash
# Build binary
just build

# Run tests
just test

# Run linting
just test-lint

# Update dependencies
just vendor
```

### Project Structure

```
speedrun/
├── cmd/speedrun/          # Main application entry point
├── internal/ui/           # Terminal UI components
├── pkg/
│   ├── agent/            # AI analysis integration
│   ├── cache/            # Caching implementation
│   ├── config/           # Configuration management
│   ├── github/           # GitHub API client
│   └── version/          # Version detection
└── vendor/               # Vendored dependencies
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Run `just test` to ensure everything passes
5. Submit a pull request

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

Built with:
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [OpenAI Go](https://github.com/openai/openai-go) - AI integration

---

*Accelerate your on-call workflows. Stay focused. Ship faster.*
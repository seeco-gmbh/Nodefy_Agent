# Nodefy Agent

A lightweight file watcher that enables real-time file synchronization between your local machine and the Nodefy web application.

## Features

- **Real-time File Watching**: Monitors files and folders for changes using native OS events
- **Automatic Sync**: Sends file changes to the Nodefy Orchestrator via WebSocket
- **Cross-Platform**: Works on Windows, macOS, and Linux
- **Lightweight**: Single binary, ~10MB
- **Auto-Reconnect**: Automatically reconnects if connection is lost
- **Configurable**: Watch specific file types and folders

## Installation

### Download Binary

Download the latest release for your platform from the [Releases](https://github.com/nodefy/nodefy-agent/releases) page.

### Build from Source

```bash
# Clone the repository
git clone https://github.com/nodefy/Nodefy_Agent.git
cd Nodefy_Agent

# Build
make build

# Or install to /usr/local/bin
make install
```

## Usage

### Quick Start

```bash
# Create a config file
nodefy-agent --init

# Edit the config file (~/.nodefy/agent.json)
# Set your session_key and watch_paths

# Run the agent
nodefy-agent
```

### Command Line Options

```bash
nodefy-agent [options] [paths...]

Options:
  -config string    Path to config file (default: ~/.nodefy/agent.json)
  -url string       Orchestrator WebSocket URL
  -session string   Session key (from browser localStorage)
  -debug            Enable debug logging
  -version          Show version
  -init             Create default config file

Examples:
  # Watch a folder with config file
  nodefy-agent

  # Watch specific paths
  nodefy-agent -session "your-session-key" /path/to/data /path/to/files

  # Use custom orchestrator URL
  nodefy-agent -url "wss://api.nodefy.app/ws/agent" -session "key" /path
```

## Configuration

The config file is stored at `~/.nodefy/agent.json`:

```json
{
  "orchestrator_url": "wss://api.nodefy.app/ws/agent",
  "session_key": "your-session-key-from-browser",
  "watch_paths": [
    "/Users/you/Documents/Data",
    "/Users/you/Projects/data-files"
  ],
  "file_types": [".csv", ".xlsx", ".xls", ".json", ".xml", ".parquet"],
  "recursive": true,
  "reconnect_delay_seconds": 5
}
```

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `orchestrator_url` | WebSocket URL of the Nodefy Orchestrator | `ws://localhost:9080/ws/agent` |
| `session_key` | Your session key (from browser localStorage) | Required |
| `watch_paths` | List of paths to watch | Required |
| `file_types` | File extensions to watch (empty = all) | Common data formats |
| `recursive` | Watch subdirectories | `true` |
| `reconnect_delay_seconds` | Delay before reconnect attempts | `5` |

### Getting Your Session Key

1. Open Nodefy in your browser
2. Open Developer Tools (F12)
3. Go to Application > Local Storage
4. Copy the value of `nodefy_session_key`

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                  │
│   Your Computer                 Cloud (VPS)                      │
│   ┌─────────────────┐          ┌─────────────────────────┐      │
│   │  Nodefy Agent   │ WebSocket│    Orchestrator         │      │
│   │                 │─────────►│                         │      │
│   │  • Watch files  │          │    ┌─────────────┐      │      │
│   │  • Send changes │          │    │   Browser   │      │      │
│   │  • Auto-reconnect          │    │  (Nodefy)   │      │      │
│   └────────┬────────┘          │    └─────────────┘      │      │
│            │                   │                         │      │
│            ▼                   └─────────────────────────┘      │
│   📁 Your Local Files                                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Development

```bash
# Run in development mode
make run ARGS="--debug /path/to/watch"

# Run tests
make test

# Cross-compile for all platforms
make cross

# Build for specific platform
make darwin   # macOS
make linux    # Linux
make windows  # Windows
```

## License

MIT

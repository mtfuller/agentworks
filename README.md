# AgentWorks

A local-first, vendor-agnostic tool for building agent contexts, skills, tools, hooks,
and workflows — author them once as plain files, then export target-specific artifacts
for the AI harnesses you actually use (Claude Code, ChatGPT, GitHub Copilot,
Microsoft 365 Copilot, and others).

## Why

The industry has started standardizing pieces of the agent-tooling stack (e.g.
`AGENTS.md`), but most of it — skills, tools, hooks, agent definitions, workflows — is
still vendor-specific, and that's likely to stay true for a while. AgentWorks doesn't
bet on any one vendor winning; it lets you build against a vendor-agnostic core model
and generate whatever vendor-specific format you need from it, so switching or
supporting multiple harnesses doesn't mean maintaining N copies by hand.

## What it's for

- **Skills** — e.g. a data-processing skill (script + tests + sample inputs) exported
  as a ChatGPT skill upload and a Microsoft 365 Copilot package from one source.
- **Tools** — e.g. a Jira-fetching tool with real tests and a simulated API, exported to
  both Claude Code and GitHub Copilot.
- **Agents** — e.g. a domain-specific research agent with its own guidance/resources,
  exported to ChatGPT, Claude, and others as drag-and-drop artifacts.
- **Workflows/plugins** — multiple agents, tools, and MCP servers composed into a
  pipeline ("software factory"), exported as one or more vendor plugins.
- **Scaffolding & discovery** — guided boilerplate generation for any of the above, and
  an early, explicit view of which capabilities are portable across every target versus
  specific to one.

See [AGENTS.md](AGENTS.md) for the guiding scenarios and how the project is organized
for agentic development.

## Status

Early scaffold. The CLI currently provides the base command surface (help, version,
logging, colored output) that the artifact/skill/agent/export functionality described
above will be built on top of — that functionality doesn't exist yet.

## Quick Start

### Prerequisites

- Go 1.21 or higher
- [Task](https://taskfile.dev) (optional, for build automation)

### Installation

1. Clone the repository:
```bash
git clone git@github.com:mtfuller/agentworks.git
cd agentworks
```

2. Build the application:
```bash
task build
```

3. Run the application:
```bash
./agentworks --help
```

## Usage

### Available Commands

#### Version Command
Display version information:
```bash
./agentworks version
```

Short version output:
```bash
./agentworks version --short
```

### Global Flags

- `-v, --verbose`: Enable verbose output (debug level logging)
- `-l, --log-level`: Set log level (debug, info, warn, error)
- `-h, --help`: Display help information

## Development

### Running Tests

Run all tests:
```bash
task test
```

Run only unit tests:
```bash
task test-unit
```

Run only integration tests:
```bash
task test-integration
```

Generate coverage report:
```bash
task coverage
```

### Building

Build the binary:
```bash
task build
```

Install to GOPATH/bin:
```bash
task install
```

### Project Structure

```
.
├── cmd/                    # Command definitions
│   ├── root.go            # Root command
│   └── version.go         # Version command
├── internal/              # Internal packages
│   ├── color/             # Colored text utilities
│   ├── logger/            # Structured logging
│   ├── spinner/           # Spinner animations
│   └── version/           # Version management
├── pkg/                   # Public packages (reusable, no CLI dependency)
├── tests/                 # Integration tests
├── main.go               # Application entry point
├── Taskfile.yml          # Build and test automation
└── README.md             # This file
```

## Adding New Commands

To add a new command, create a new file in the `cmd/` directory (or use the
`add-command` skill in `.claude/skills/`):

```go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/mtfuller/agentworks/internal/color"
)

var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "Description of my command",
    Run: func(cmd *cobra.Command, args []string) {
        color.Success("My command executed!")
    },
}

func init() {
    rootCmd.AddCommand(myCmd)
}
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

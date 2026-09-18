You are a knowledgeable software engineer familiar with best practices for building production-ready Go CLI applications using Cobra. Use the information below to assist users in understanding the structure, conventions, and best practices of this Go CLI project.

## Project summary
- AgentWorks: a local-first, vendor-agnostic tool for building agent contexts, skills,
  MCP servers, and hooks — author once, export to the harnesses you actually use (Claude Code,
  ChatGPT, GitHub Copilot, Cursor, Gemini CLI).
- A CLI (Cobra-based) for authoring, testing, validating, and exporting agent artifacts
  as plain files in a project directory, versioned and diffable, no vendor lock-in.
- Distinguishes core capabilities (portable across every target) from vendor-specific
  ones, and surfaces that distinction early so users know what will and won't travel.

## Layout (key parts)
- main.go: entrypoint that calls cmd.Execute()
- cmd/: Cobra commands (root.go, version.go, and future artifact/export commands)
- internal/: CLI-specific logic (logger, color, spinner, version)
- pkg/: reusable libraries (artifact model, target transforms, validation — as they land)
- tests/: integration tests
- Taskfile.yml: build automation

## Tech stack
- Cobra (CLI framework), custom logger, Task, ANSI colors

## Principles
- Clean layers: cmd (commands), internal (CLI-specific logic), pkg (reusable)
- Commands follow Cobra patterns with Use, Short, Long, Run/RunE functions
- Structured logging with levels (DEBUG, INFO, WARN, ERROR)
- Colored terminal output for better UX
- Commands support flags and arguments via Cobra's flag system
- Vendor-agnostic-first: model artifacts (agents, skills, MCP servers, hooks) independent of
  any single vendor's format, then transform to target-specific output on export

## Development conventions
- New command: create cmd/<command>.go, add cobra.Command, register in init() with rootCmd.AddCommand()
- New flag: add to specific command's init() function with cmd.Flags() or cmd.PersistentFlags()
- New internal package: create internal/<package>/<package>.go with unit tests
- Generic libraries go to pkg/, CLI-specific logic to internal/
- Always add help text (Short and Long descriptions) to commands

## Quality & testing
- Unit tests for all internal/ and pkg/ packages
- Integration tests for full CLI command execution
- Table-driven tests, aim >80% coverage

## Code style & error handling
- gofmt, clear names, small functions, comments for exported items
- Always check and return errors with context
- Use logger for error output with appropriate levels
- Use color package for user-facing output (color.Success, color.Error, color.Info, color.Warn)
- Prefer RunE + wrapped errors over os.Exit inside command logic

## Logging
- Custom logger in internal/logger with DEBUG, INFO, WARN, ERROR levels
- Set via --verbose flag or --log-level flag
- Use logger.Debug, logger.Info, logger.Warn, logger.Error with formatted strings

## Common tasks
- Build: task build
- Run: task run
- Tests: task test / task test-unit / task test-integration / task coverage
- Clean: task clean

## Packaging & Distribution
- Build binary with task build
- Cross-compile for different platforms
- Version info embedded via ldflags (see version package)

## Important files to edit first
- cmd/root.go (root command, global flags, persistent config)
- cmd/<command>.go (individual commands)
- internal/logger/logger.go (logging configuration)
- internal/color/color.go (terminal output styling)
- main.go (application entry point)
- Taskfile.yml (build and task automation)

## Do not change without strong reason
- Project structure, error-handling patterns, logging format, color output patterns, command registration flow.

# Runner Orchestrator CLI

A Go-based CLI application that acts as middleware between the Runner UI frontend and the Runner CLI tool.

## Overview

Runner Orchestrator serves as a bridge between the web-based Runner UI and the underlying Runner CLI tool. It handles:

- Starting and managing Runner CLI processes 
- Providing API endpoints for the UI to interact with Runner
- Running an HTTP server on port 2435 to facilitate communication between components

The HTTP server is the primary communication channel between the Runner UI and Runner Orchestrator CLI. It exposes RESTful endpoints that allow the UI to:

- Start a new Runner CLI
- Get information about the ongoing runners
- Stop running runners

## Building

```bash
# Build the project
./scripts/build.sh

# The binary will be created in the bin directory
./bin/spm_runner_orchestrator
```

## Running

### Flags

- `--port` (default: 2435): Port to run the server on
- `--path` (default: "spm_runner"): Path to runner executable
- `--help`: Show usage information

### Example

```bash
# Run on default port with custom runner path
spm_runner_orchestrator --path /usr/local/bin/spm_runner

# Run on custom port
spm_runner_orchestrator --port 8080

# Show help
spm_runner_orchestrator --help
```

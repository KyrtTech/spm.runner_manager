# Runner Manager CLI

A Go-based CLI application that acts as middleware between the Runner UI frontend and the Runner CLI tool.

## Overview

Runner Manager serves as a bridge between the web-based Runner UI and the underlying Runner CLI tool. It handles:

- Starting and managing Runner CLI processes 
- Providing API endpoints for the UI to interact with Runner
- Running an HTTP server on port 8080 to facilitate communication between components

The HTTP server is the primary communication channel between the Runner UI and Runner Manager CLI. It exposes RESTful endpoints that allow the UI to:

- Start a new Runner CLI
- Get information about the ongoing runners
- Stop running runners

## Installation

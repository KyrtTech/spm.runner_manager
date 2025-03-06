package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// RunnerProcess stores information about a running process
type RunnerProcess struct {
	ID      string
	Process *exec.Cmd
	Port    int
	Status  string
}

type RunnerResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Port   int    `json:"port"`
}

type RunnerArgs struct {
	Infra   string `json:"infra"`
	Service string `json:"service"`
}

var (
	// Store running processes with mutex for concurrent access
	runnerProcesses = make(map[string]*RunnerProcess)
	processesMutex  sync.RWMutex
	runnerPath      = ""
)

func getRoot(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got / request\n")
	io.WriteString(w, "Runner Manager API - Bridge between Runner UI and Runner CLI")
}

type ProcessInput struct {
	Infra   string `json:"infra"`
	Service string `json:"service"`
}

type StartRunnerBody struct {
	Infra string `json:"infra" binding:"required"`
	App   string `json:"app" binding:"required"`
}

func startRunner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input StartRunnerBody

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if input.Infra == "" {
		http.Error(w, "infra is required", http.StatusBadRequest)
		return
	}

	if input.App == "" {
		http.Error(w, "app is required", http.StatusBadRequest)
		return
	}

	port := rand.IntN(65535-1024) + 1024

	// Create new process
	args := []string{"install", "--interactive", "--port", strconv.Itoa(port), "--infra", input.Infra, "--application", input.App}
	cmd := exec.Command(runnerPath, args...)

	fmt.Printf("path %s\n", runnerPath)

	// Generate UUID for the process
	processID := uuid.New().String()

	// Start the process
	err := cmd.Start()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start runner: %v", err), http.StatusInternalServerError)
		return
	}

	// Start a goroutine to wait for the process
	go func() {
		err := cmd.Wait()
		if err != nil {
			fmt.Printf("Process %s exited with error: %v\n", processID, err)
		}
		// Clean up process from map when it exits
		processesMutex.Lock()
		runnerProcesses[processID].Status = "errored"
		processesMutex.Unlock()
	}()

	// Store process info
	processesMutex.Lock()
	runnerProcesses[processID] = &RunnerProcess{
		ID:      processID,
		Process: cmd,
		Port:    port,
		Status:  "running",
	}
	processesMutex.Unlock()

	// Return process ID to client
	response := map[string]any{"process_id": processID, "port": port}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func listRunners(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	keys := slices.Collect(maps.Keys(runnerProcesses))
	if keys == nil {
		keys = []string{}
	}

	json.NewEncoder(w).Encode(keys)
}

func runnerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get process ID from URL path
	processId := strings.TrimPrefix(r.URL.Path, "/runner/")
	if processId == "" {
		http.Error(w, "Process ID is required", http.StatusBadRequest)
		return
	}

	// Get process info from map
	processesMutex.Lock()
	process, exists := runnerProcesses[processId]
	processesMutex.Unlock()

	if !exists {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	response := RunnerResponse{
		ID:     process.ID,
		Status: process.Status,
		Port:   process.Port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	port := flag.Int("port", 2435, "Port to run the server on")
	path := flag.String("path", "spm_runner", "Path to runner executable")
	help := flag.Bool("help", false, "Show usage information")
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	runnerPath = *path

	mux := http.NewServeMux()

	mux.HandleFunc("/", getRoot)
	mux.HandleFunc("/start", startRunner)
	mux.HandleFunc("/list", listRunners)
	mux.HandleFunc("/runner/", runnerInfo)

	io.WriteString(os.Stdout, fmt.Sprintf("Runner Manager started on port: %d\n", *port))
	io.WriteString(os.Stdout, fmt.Sprintf("Using path: %s for runner\n", *path))

	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux)

	if errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("server closed\n")
	} else if err != nil {
		fmt.Printf("error starting server: %s\n", err)
		os.Exit(1)
	}
}

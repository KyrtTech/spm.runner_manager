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
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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
	ID       string `json:"id"`
	Status   string `json:"status"`
	Port     int    `json:"port"`
	Endpoint string `json:"endpoint"`
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

// enableCORS sets CORS headers to allow all origins
func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// corsMiddleware wraps an http.HandlerFunc with CORS support
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)

		// Handle preflight OPTIONS requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

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
			processesMutex.Lock()
			runnerProcesses[processID].Status = "errored"
			processesMutex.Unlock()
		} else {
			processesMutex.Lock()
			runnerProcesses[processID].Status = "stopped"
			processesMutex.Unlock()
		}
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

	// Wait for the endpoint to be ready
	endpoint := fmt.Sprintf("http://localhost:%d", port)
	maxRetries := 30
	retryDelay := time.Second
	ready := false

	for i := 0; i < maxRetries; i++ {
		resp, err := http.Get(endpoint)
		if err == nil && resp.StatusCode < 500 {
			resp.Body.Close()
			ready = true
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(retryDelay)
	}

	// Return process ID to client
	status := "running"
	if !ready {
		status = "starting"
	}

	response := map[string]any{
		"process_id": processID,
		"port":       port,
		"endpoint":   endpoint,
		"status":     status,
	}
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

func runnerInfo(w http.ResponseWriter, process *RunnerProcess) {
	response := RunnerResponse{
		ID:       process.ID,
		Status:   process.Status,
		Port:     process.Port,
		Endpoint: fmt.Sprintf("http://localhost:%d", process.Port),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func runnerStop(w http.ResponseWriter, process *RunnerProcess) {
	// Kill the process
	if process.Process != nil && process.Process.Process != nil {
		if err := process.Process.Process.Kill(); err != nil {
			http.Error(w, fmt.Sprintf("Failed to stop runner: %v", err), http.StatusInternalServerError)
			// Update the process status in the map
			processesMutex.Lock()
			process.Status = "errored"
			process.Process = nil
			processesMutex.Unlock()
			return
		}
	}

	// Update the process status in the map
	processesMutex.Lock()
	process.Status = "stopped"
	process.Process = nil
	processesMutex.Unlock()

	// Return success response
	response := map[string]string{"status": "stopped", "id": process.ID}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func runners(w http.ResponseWriter, r *http.Request) {
	// Get process ID from URL path
	processId := strings.TrimPrefix(r.URL.Path, "/runner/")
	if processId == "" {
		http.Error(w, "Process ID is required", http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	switch r.Method {
	case http.MethodGet:
		runnerInfo(w, process)
	case http.MethodDelete:
		runnerStop(w, process)
	}
}

// stopAllProcesses terminates all running processes
func stopAllProcesses() {
	fmt.Println("Stopping all runner processes...")

	processesMutex.Lock()
	defer processesMutex.Unlock()

	for id, process := range runnerProcesses {
		if process.Process != nil && process.Process.Process != nil {
			fmt.Printf("Stopping process %s\n", id)
			if err := process.Process.Process.Kill(); err != nil {
				fmt.Printf("Error stopping process %s: %v\n", id, err)
			}
			process.Status = "stopped"
			process.Process = nil
		}
	}

	fmt.Println("All processes stopped")
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

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Handle signals in a separate goroutine
	go func() {
		sig := <-sigChan
		fmt.Printf("Received signal: %v\n", sig)
		stopAllProcesses()
		os.Exit(0)
	}()

	// Ensure cleanup happens even on normal exit
	defer stopAllProcesses()

	mux := http.NewServeMux()

	mux.HandleFunc("/", corsMiddleware(getRoot))
	mux.HandleFunc("/start", corsMiddleware(startRunner))
	mux.HandleFunc("/list", corsMiddleware(listRunners))
	mux.HandleFunc("/runner/", corsMiddleware(runners))

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

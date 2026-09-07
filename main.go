package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/iloever-pix/r6checker/checker"
	"github.com/iloever-pix/r6checker/ui"
)

func main() {
	// Create application directories if they don't exist
	dataDir := getDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory %s: %v", dataDir, err)
	}

	// Initialize the account checker
	checkerInstance := checker.NewChecker(dataDir)

	// Start the UI
	app := ui.NewApp(checkerInstance)
	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}

// getDataDir returns the application data directory
func getDataDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home dir can't be determined
		return filepath.Join(".", "r6checker-data")
	}

	// Create platform-specific data directory using runtime.GOOS
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(homeDir, "AppData", "Local", "R6Checker")
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "R6Checker")
	default: // Linux and others
		return filepath.Join(homeDir, ".r6checker")
	}
}
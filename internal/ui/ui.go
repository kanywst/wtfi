// Package ui provides terminal user interface logic for network diagnostics.
package ui

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kanywst/wtfi/internal/diagnostic"

	"github.com/fatih/color"
)

// PrintHeader prints the diagnostic start header.
func PrintHeader() {
	if _, err := color.New(color.Bold, color.FgCyan).Println("🚀 wtfi: Starting Network Diagnostics..."); err != nil {
		log.Printf("UI Error: %v", err)
	}
	fmt.Println(strings.Repeat("-", 50))
}

// PrintFooter prints the diagnostic end footer.
func PrintFooter() {
	fmt.Println(strings.Repeat("-", 50))
}

// ClearScreen clears the terminal screen using ANSI escape codes.
func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

// PrintResult displays the diagnostic outcome of a single step.
func PrintResult(r diagnostic.Result, verbose bool) {
	c := color.New(color.FgGreen)
	switch r.Status {
	case diagnostic.StatusWarning:
		c = color.New(color.FgYellow)
	case diagnostic.StatusError:
		c = color.New(color.FgRed)
	case diagnostic.StatusOk:
		// Default green
	}

	fmt.Printf("%s %-25s", r.Emoji, r.Name)
	if r.Status != diagnostic.StatusError {
		latencyStr := ""
		if r.Latency > 0 {
			latencyStr = r.Latency.Round(time.Millisecond).String()
		} else {
			latencyStr = "OK"
		}
		if _, err := c.Printf("%22s\n", latencyStr); err != nil {
			log.Printf("UI Error: %v", err)
		}
	} else {
		if _, err := c.Printf("%22s\n", "ERROR"); err != nil {
			log.Printf("UI Error: %v", err)
		}
	}

	if r.Message != "" {
		msgColor := color.New(color.FgWhite).Add(color.Faint)
		if _, err := msgColor.Printf("   ├─ Info: %s\n", r.Message); err != nil {
			log.Printf("UI Error: %v", err)
		}
	}

	if verbose && len(r.Details) > 0 {
		for _, detail := range r.Details {
			if _, err := color.New(color.FgHiBlack).Printf("   │  %s\n", detail); err != nil {
				log.Printf("UI Error: %v", err)
			}
		}
	}

	if r.Status != diagnostic.StatusOk && r.Fix != "" {
		if _, err := color.New(color.FgHiBlue).Printf("   └─ Fix:  %s\n", r.Fix); err != nil {
			log.Printf("UI Error: %v", err)
		}
	}
}

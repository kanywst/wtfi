package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/kanywst/wtfi/internal/diagnostic"

	"github.com/fatih/color"
)

func PrintHeader() {
	color.New(color.Bold, color.FgCyan).Println("🚀 wtfi: Starting Network Diagnostics...")
	fmt.Println(strings.Repeat("-", 50))
}

func PrintFooter() {
	fmt.Println(strings.Repeat("-", 50))
}

func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

func PrintResult(r diagnostic.Result, verbose bool) {
	c := color.New(color.FgGreen)
	if r.Status == diagnostic.StatusWarning {
		c = color.New(color.FgYellow)
	} else if r.Status == diagnostic.StatusError {
		c = color.New(color.FgRed)
	}

	fmt.Printf("%s %-25s", r.Emoji, r.Name)
	if r.Status != diagnostic.StatusError {
		latencyStr := ""
		if r.Latency > 0 {
			latencyStr = r.Latency.Round(time.Millisecond).String()
		} else {
			latencyStr = "OK"
		}
		c.Printf("%22s\n", latencyStr)
	} else {
		c.Printf("%22s\n", "ERROR")
	}

	if r.Message != "" {
		msgColor := color.New(color.FgWhite).Add(color.Faint)
		msgColor.Printf("   ├─ Info: %s\n", r.Message)
	}

	if verbose && len(r.Details) > 0 {
		for _, detail := range r.Details {
			color.New(color.FgHiBlack).Printf("   │  %s\n", detail)
		}
	}

	if r.Status != diagnostic.StatusOk && r.Fix != "" {
		color.New(color.FgHiBlue).Printf("   └─ Fix:  %s\n", r.Fix)
	}
}

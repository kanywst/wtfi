// main is the entry point for the wtfi network diagnostic tool.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kanywst/wtfi/internal/diagnostic"
	"github.com/kanywst/wtfi/internal/ui"
)

// Version of the application.
const Version = "1.0.0"

func main() {
	verbose := flag.Bool("v", false, "Enable verbose output with protocol details")
	watch := flag.Bool("w", false, "Enable watch mode (real-time updates)")
	version := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *version {
		fmt.Printf("wtfi version %s\n", Version)
		os.Exit(0)
	}

	for {
		if *watch {
			ui.ClearScreen()
		}

		ui.PrintHeader()

		// Refactor: Use closures only when necessary.
		steps := []func() diagnostic.Result{
			func() diagnostic.Result { return diagnostic.CheckL2WiFi(*verbose) },
			func() diagnostic.Result { return diagnostic.CheckL3Gateway(*verbose) },
			diagnostic.CheckL3WAN,
			diagnostic.CheckDNSBenchmark,
			func() diagnostic.Result { return diagnostic.CheckPrivateRelay(*verbose) },
			func() diagnostic.Result { return diagnostic.FastTraceroute(*verbose) },
			func() diagnostic.Result { return diagnostic.CheckCaptivePortal(*verbose) },
		}

		for _, step := range steps {
			ui.PrintResult(step(), *verbose)
		}

		ui.PrintFooter()

		if !*watch {
			break
		}
		time.Sleep(2 * time.Second)
	}
}

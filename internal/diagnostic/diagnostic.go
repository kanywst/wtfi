// Package diagnostic provides network diagnostic logic for macOS.
package diagnostic

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status represents the health status of a diagnostic step.
type Status int

const (
	// StatusOk indicates a healthy state.
	StatusOk Status = iota
	// StatusWarning indicates a slow or weak state.
	StatusWarning
	// StatusError indicates a failure state.
	StatusError
)

// Result holds the outcome of a diagnostic check.
type Result struct {
	Name    string
	Latency time.Duration
	Status  Status
	Message string
	Fix     string
	Emoji   string
	Details []string
}

// CheckL2WiFi performs Layer 2 (Wi-Fi) diagnostics.
func CheckL2WiFi(verbose bool) Result {
	iface, err := getPrimaryInterface()
	if err != nil {
		return Result{Name: "Connectivity", Emoji: "📡", Status: StatusError, Message: "No default route found", Fix: "Check your network hardware."}
	}

	cmd := exec.Command("system_profiler", "SPAirPortDataType")
	out, err := cmd.Output()

	if err != nil {
		return Result{Name: "Wi-Fi", Emoji: "📡", Status: StatusError, Message: "Failed to retrieve Wi-Fi telemetry"}
	}

	return parseWiFiInfo(string(out), iface, verbose)
}

func parseWiFiInfo(output string, iface string, verbose bool) Result {
	res := Result{Name: "Wi-Fi", Emoji: "📡", Status: StatusOk}
	ssid, rssi := "", 0
	var details []string

	lines := strings.Split(output, "\n")
	isCurrent := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "Current Network Information") {
			isCurrent = true
			continue
		}
		if isCurrent {
			if strings.HasSuffix(trimmed, ":") && ssid == "" {
				ssid = strings.TrimSuffix(trimmed, ":")
				res.Name = fmt.Sprintf("Wi-Fi (%s)", ssid)
			}
			if strings.Contains(line, "Signal / Noise") {
				re := regexp.MustCompile(`(-?\d+) dBm / (-?\d+) dBm`)
				m := re.FindStringSubmatch(line)
				if len(m) > 1 {
					rssi, _ = strconv.Atoi(m[1])
				}
			}
			if verbose && strings.Contains(line, ":") {
				details = append(details, trimmed)
			}
			if strings.Contains(line, "Other Local Wi-Fi Networks") {
				break
			}
		}
	}

	res.Message = fmt.Sprintf("Interface: %s, Signal: %d dBm", iface, rssi)
	res.Details = details
	if rssi < -80 && rssi != 0 {
		res.Status = StatusWarning
		res.Fix = "Weak signal. Move closer to the Access Point."
	}
	return res
}

// CheckL3Gateway performs Layer 3 diagnostics for the local gateway.
func CheckL3Gateway(verbose bool) Result {
	gw, err := getGatewayIP()
	if err != nil {
		return Result{Name: "Gateway", Emoji: "🏠", Status: StatusError, Message: "Gateway IP discovery failed"}
	}

	lat, err := ping(gw)
	res := Result{Name: "Gateway (" + gw + ")", Emoji: "🏠", Latency: lat, Status: StatusOk}
	if err != nil {
		res.Status = StatusError
		res.Message = "Unreachable"
		res.Fix = "Check local cables or restart your router."
		return res
	}

	if verbose {
		out, _ := exec.Command("arp", "-n", gw).Output()
		res.Details = append(res.Details, "--- ARP Entry ---")
		res.Details = append(res.Details, strings.TrimSpace(string(out)))
		iface, _ := getPrimaryInterface()
		outIf, _ := exec.Command("ifconfig", iface).Output()
		res.Details = append(res.Details, "--- Interface Details ---")
		lines := strings.Split(string(outIf), "\n")
		for _, l := range lines {
			if strings.Contains(l, "inet ") {
				res.Details = append(res.Details, strings.TrimSpace(l))
			}
		}
	}
	return res
}

// CheckDNSBenchmark compares performance across multiple DNS resolvers.
func CheckDNSBenchmark() Result {
	resolvers := map[string]string{
		"System":     "",
		"Google":     "8.8.8.8:53",
		"Cloudflare": "1.1.1.1:53",
	}

	res := Result{Name: "DNS Benchmark", Emoji: "🚦", Status: StatusOk}
	var details []string

	for name, addr := range resolvers {
		start := time.Now()
		var err error
		if addr == "" {
			_, err = net.LookupIP("google.com")
		} else {
			r := &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, _, address string) (net.Conn, error) {
					d := net.Dialer{Timeout: 2 * time.Second}
					return d.DialContext(ctx, "udp", address)
				},
			}
			_, err = r.LookupIP(context.Background(), "ip", "google.com")
		}
		dur := time.Since(start)

		status := "OK"
		if err != nil {
			status = "FAIL"
		}
		details = append(details, fmt.Sprintf("%-10s: %s (%s)", name, dur.Round(time.Microsecond), status))

		if name == "System" {
			res.Latency = dur
		}
	}

	res.Details = details
	if res.Latency > 300*time.Millisecond {
		res.Status = StatusWarning
		res.Message = "High DNS latency detected"
		res.Fix = "Switch to a faster DNS provider like Cloudflare (1.1.1.1)."
	}
	return res
}

// CheckPrivateRelay detects the state of Apple's iCloud Private Relay.
func CheckPrivateRelay(verbose bool) Result {
	start := time.Now()
	ips, err := net.LookupIP("mask.icloud.com")
	dur := time.Since(start)

	res := Result{Name: "iCloud Private Relay", Emoji: "🛡️", Latency: dur, Status: StatusOk}
	if err == nil && len(ips) > 0 {
		res.Message = "Active (Apple Proxy Node detected)"
		if verbose {
			for _, ip := range ips {
				res.Details = append(res.Details, "Proxy Node: "+ip.String())
			}
		}
	} else {
		res.Message = "Inactive or Bypass mode"
	}
	return res
}

// FastTraceroute performs a concurrent traceroute to visualize the network path.
func FastTraceroute(verbose bool) Result {
	target := "1.1.1.1"
	res := Result{Name: "Fast Trace", Emoji: "📍", Status: StatusOk}
	if !verbose {
		res.Message = "Use -v flag to view the network path"
		return res
	}

	var wg sync.WaitGroup
	hops := make([]string, 11)
	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(ttl int) {
			defer wg.Done()
			out, err := exec.Command("ping", "-c", "1", "-t", strconv.Itoa(ttl), "-o", target).Output()
			if err == nil {
				re := regexp.MustCompile(`from ([\d\.]+):`)
				m := re.FindStringSubmatch(string(out))
				if len(m) > 1 {
					hops[ttl] = fmt.Sprintf("Hop %2d: %s", ttl, m[1])
				}
			} else {
				hops[ttl] = fmt.Sprintf("Hop %2d: * (Request timed out)", ttl)
			}
		}(i)
	}
	wg.Wait()

	for _, h := range hops {
		if h != "" {
			res.Details = append(res.Details, h)
		}
	}
	return res
}

// CheckCaptivePortal verifies if the user is behind a captive portal.
func CheckCaptivePortal(verbose bool) Result {
	start := time.Now()
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://captive.apple.com/hotspot-detect.html")
	if err != nil {
		return Result{Name: "Captive Portal", Emoji: "🍎", Status: StatusError, Message: "HTTP health check failed"}
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Printf("Network Error: Failed to close response body: %v", errClose)
		}
	}()
	dur := time.Since(start)

	res := Result{Name: "Captive Portal", Emoji: "🍎", Latency: dur, Status: StatusOk}
	if verbose {
		res.Details = append(res.Details, "Response Status: "+resp.Status)
		for k, v := range resp.Header {
			res.Details = append(res.Details, k+": "+strings.Join(v, ", "))
		}
	}

	lr := io.LimitReader(resp.Body, 1024)
	body, _ := io.ReadAll(lr)
	if !strings.Contains(string(body), "Success") {
		res.Status = StatusWarning
		res.Message = "Login Required (Captive Portal detected)"
		res.Fix = "Open your browser to sign in to the network."
	}
	return res
}

func getPrimaryInterface() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "", err
	}
	return parseInterface(string(out))
}

func parseInterface(output string) (string, error) {
	re := regexp.MustCompile(`interface: (\w+)`)
	m := re.FindStringSubmatch(output)
	if len(m) > 1 {
		return m[1], nil
	}
	return "", fmt.Errorf("no primary interface found")
}

func getGatewayIP() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "", err
	}
	return parseGateway(string(out))
}

func parseGateway(output string) (string, error) {
	re := regexp.MustCompile(`gateway: (\d+\.\d+\.\d+\.\d+)`)
	m := re.FindStringSubmatch(output)
	if len(m) > 1 {
		return m[1], nil
	}
	return "", fmt.Errorf("no gateway ip found")
}

func ping(ip string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-t", "1", ip)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return parsePing(string(out))
}

func parsePing(output string) (time.Duration, error) {
	re := regexp.MustCompile(`min/avg/max/stddev = [\d\.]+/([\d\.]+)`)
	m := re.FindStringSubmatch(output)
	if len(m) > 1 {
		avg, _ := strconv.ParseFloat(m[1], 64)
		return time.Duration(avg * float64(time.Millisecond)), nil
	}
	return 0, fmt.Errorf("failed to parse ping metrics")
}

// CheckL3WAN verifies WAN backbone reachability.
func CheckL3WAN() Result {
	target := "1.1.1.1"
	lat, err := ping(target)
	if err != nil {
		return Result{Name: "WAN Reachability", Emoji: "🌐", Status: StatusError, Message: "Internet backbone unreachable"}
	}
	res := Result{Name: "Internet (" + target + ")", Emoji: "🌐", Latency: lat, Status: StatusOk}
	if lat > 150*time.Millisecond {
		res.Status = StatusWarning
		res.Message = "High WAN latency"
	}
	return res
}

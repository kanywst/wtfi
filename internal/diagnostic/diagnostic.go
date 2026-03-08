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

var (
	reSignalNoise = regexp.MustCompile(`(-?\d+) dBm / (-?\d+) dBm`)
	reMTU         = regexp.MustCompile(`mtu (\d+)`)
	reIfaceFlags  = regexp.MustCompile(`^([a-z0-9]+): flags=`)
	reInet        = regexp.MustCompile(`\s+inet (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	rePingStat    = regexp.MustCompile(`min/avg/max/std-?dev = \d+(?:\.\d*)?/(\d+(?:\.\d*)?)`)
	rePingRoute   = regexp.MustCompile(`from (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):`)
	reRouteIface  = regexp.MustCompile(`interface: (\w+)`)
	reRouteGw     = regexp.MustCompile(`gateway: (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	reLoss        = regexp.MustCompile(`(\d+\.?\d*)% packet loss`)
	reJitter      = regexp.MustCompile(`min/avg/max/std-?dev = \d+(?:\.\d*)?/\d+(?:\.\d*)?/\d+(?:\.\d*)?/(\d+(?:\.\d*)?)`)
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
	mtu := ""
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
				m := reSignalNoise.FindStringSubmatch(line)
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

	if rssi == 0 {
		res.Message = "Wired connection (or Wi-Fi disabled)"
	} else {
		res.Message = fmt.Sprintf("Interface: %s, Signal: %d dBm", iface, rssi)
	}

	// Unify details for consistent prefixing
	var allDetails []string

	// Extract MTU size
	outIf, err := exec.Command("ifconfig", iface).Output()
	if err == nil {
		if m := reMTU.FindStringSubmatch(string(outIf)); len(m) > 1 {
			mtu = m[1]
			allDetails = append(allDetails, fmt.Sprintf("MTU: %s (Standard is 1500)", mtu))
		}
	}

	allDetails = append(allDetails, details...)

	for i, detail := range allDetails {
		prefix := "├─"
		if i == len(allDetails)-1 {
			prefix = "└─"
		}
		res.Details = append(res.Details, fmt.Sprintf("%s %s", prefix, detail))
	}
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
	res := Result{Name: "Gateway (" + gw + ")", Emoji: "🏠", Latency: lat, Status: StatusOk, Message: "Reachable"}
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

// CheckRoutingTable checks active network routing and Virtual Networks (VPNs/Docker).
func CheckRoutingTable(verbose bool) Result {
	res := Result{Name: "Routing Table & VPNs", Emoji: "🛣️", Status: StatusOk}

	// Get default route
	iface, err := getPrimaryInterface()
	if err != nil {
		res.Status = StatusError
		res.Message = "No Default Route"
		return res
	}

	gw, err := getGatewayIP()
	gwStr := "Unknown"
	if err == nil {
		gwStr = gw
	}

	// Get active VPNs and Bridges
	var virtuals []string
	out, errCmd := exec.Command("ifconfig").Output()
	if errCmd != nil {
		virtuals = append(virtuals, "Info: Virtual interface check failed")
	} else {
		lines := strings.Split(string(out), "\n")

		var currentIface string
		for _, line := range lines {
			if m := reIfaceFlags.FindStringSubmatch(line); len(m) > 1 {
				currentIface = m[1]
			} else if m := reInet.FindStringSubmatch(line); len(m) > 1 && currentIface != "" {
				if strings.HasPrefix(currentIface, "utun") {
					virtuals = append(virtuals, fmt.Sprintf("VPN/Tailscale (%s): Active (%s)", currentIface, m[1]))
				} else if strings.HasPrefix(currentIface, "bridge") {
					virtuals = append(virtuals, fmt.Sprintf("Bridge/Docker (%s): Active (%s)", currentIface, m[1]))
				}
			}
		}
	}

	routePrefix := "├─"
	if len(virtuals) == 0 {
		routePrefix = "└─"
	}
	res.Details = append(res.Details, fmt.Sprintf("%s Default Route: %s (Gateway: %s)", routePrefix, iface, gwStr))

	for i, v := range virtuals {
		prefix := "├─"
		if i == len(virtuals)-1 {
			prefix = "└─"
		}
		res.Details = append(res.Details, fmt.Sprintf("%s %s", prefix, v))
	}

	if len(res.Details) > 1 {
		res.Message = "Virtual interfaces detected"
	} else {
		res.Message = "Standard physical routing"
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
	if res.Latency > 200*time.Millisecond {
		res.Status = StatusWarning
		res.Message = "High DNS latency detected"
		res.Fix = "Switch to a faster DNS provider like Cloudflare (1.1.1.1)."
	} else {
		res.Message = "Fast and healthy"
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
				m := rePingRoute.FindStringSubmatch(string(out))
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
	m := reRouteIface.FindStringSubmatch(output)
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
	m := reRouteGw.FindStringSubmatch(output)
	if len(m) > 1 {
		return m[1], nil
	}
	return "", fmt.Errorf("no gateway ip found")
}

func ping(ip string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", ip)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return parsePing(string(out))
}

func parsePing(output string) (time.Duration, error) {
	m := rePingStat.FindStringSubmatch(output)
	if len(m) > 1 {
		avg, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse avg latency from '%s': %w", m[1], err)
		}
		return time.Duration(avg * float64(time.Millisecond)), nil
	}
	return 0, fmt.Errorf("failed to parse ping metrics")
}

// ping6 executes an IPv6 ping command.
func ping6(ip string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping6", "-c", "1", ip)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return parsePing(string(out))
}

// tcpPing attempts to establish a TCP connection to the specified address.
func tcpPing(address string) (time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

// MeasureLossAndJitter performs a 5-packet ping with 0.2s interval to calculate loss and jitter.
func MeasureLossAndJitter(ip string) (float64, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ping", "-c", "5", "-i", "0.2", ip)
	out, err := cmd.Output()
	// Ignore errors like exit status 68 if some packets drop, we still parse the output
	if err != nil && len(out) == 0 {
		return 0, 0, err
	}

	output := string(out)

	lossStr := "0"
	if m := reLoss.FindStringSubmatch(output); len(m) > 1 {
		lossStr = m[1]
	}

	jitterStr := "0.0"
	if m := reJitter.FindStringSubmatch(output); len(m) > 1 {
		jitterStr = m[1]
	}

	loss, err := strconv.ParseFloat(lossStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse loss: %w", err)
	}
	jitter, err := strconv.ParseFloat(jitterStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse jitter: %w", err)
	}

	return loss, jitter, nil
}

// CheckL3WAN verifies WAN backbone reachability across IPv4, IPv6, and TCP.
func CheckL3WAN() Result {
	targetIPv4 := "1.1.1.1"
	targetIPv6 := "2606:4700:4700::1111"
	targetTCP := "1.1.1.1:443"

	var wg sync.WaitGroup
	var latIPv4, latIPv6, latTCP time.Duration
	var errIPv4, errIPv6, errTCP error
	var loss, jitter float64
	var errQoS error

	wg.Add(4)
	go func() { defer wg.Done(); latIPv4, errIPv4 = ping(targetIPv4) }()
	go func() { defer wg.Done(); latIPv6, errIPv6 = ping6(targetIPv6) }()
	go func() { defer wg.Done(); latTCP, errTCP = tcpPing(targetTCP) }()
	go func() { defer wg.Done(); loss, jitter, errQoS = MeasureLossAndJitter(targetIPv4) }()
	wg.Wait()

	res := Result{Name: "Internet Reachability", Emoji: "🌐", Status: StatusOk}

	// Overall Status Determination
	if errIPv4 != nil && errTCP != nil {
		res.Status = StatusError
		res.Message = "Offline (Both ICMP and TCP failed)"
	} else if errIPv4 != nil && errTCP == nil {
		res.Message = "Firewalled ICMP detected"
		res.Latency = latTCP
	} else {
		res.Message = "Routing operational"
		res.Latency = latIPv4
	}

	if res.Latency > 150*time.Millisecond {
		res.Status = StatusWarning
		res.Message = "High WAN latency"
	}

	// Format Details
	var ipv4Status string
	if errIPv4 == nil {
		ipv4Status = fmt.Sprintf("%v (Reachable)", latIPv4.Round(time.Millisecond))
	} else if errTCP == nil {
		ipv4Status = "TIMEOUT (Dropped)"
	} else {
		ipv4Status = "TIMEOUT (Unreachable)"
	}
	res.Details = append(res.Details, fmt.Sprintf("├─ IPv4 (%s): %s", targetIPv4, ipv4Status))

	ipv6Status := "TIMEOUT (Unreachable)"
	if errIPv6 == nil {
		ipv6Status = fmt.Sprintf("%v (Reachable)", latIPv6.Round(time.Millisecond))
	}
	res.Details = append(res.Details, fmt.Sprintf("├─ IPv6 (%s): %s", targetIPv6, ipv6Status))

	var tcpStatus string
	if errTCP == nil {
		tcpStatus = fmt.Sprintf("%v (Connected)", latTCP.Round(time.Millisecond))
	} else {
		tcpStatus = "TIMEOUT (Failed)"
	}
	res.Details = append(res.Details, fmt.Sprintf("├─ TCP 443 (%s): %s", targetIPv4, tcpStatus))

	if errQoS == nil {
		res.Details = append(res.Details, fmt.Sprintf("└─ Quality: Loss: %.1f%%, Jitter: %.2fms", loss, jitter))
	} else {
		res.Details = append(res.Details, "└─ Quality: Measurement failed or timed out")
	}

	return res
}

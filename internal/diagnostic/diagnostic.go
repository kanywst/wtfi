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
	reSignalNoise  = regexp.MustCompile(`(-?\d+) dBm / (-?\d+) dBm`)
	reMTU          = regexp.MustCompile(`mtu (\d+)`)
	rePingStat     = regexp.MustCompile(`min/avg/max/std-?dev = \d+(?:\.\d*)?/(\d+(?:\.\d*)?)`)
	rePingRoute    = regexp.MustCompile(`from (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):`)
	reRouteIface   = regexp.MustCompile(`interface: (\w+)`)
	reRouteGw      = regexp.MustCompile(`gateway: (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	reLoss         = regexp.MustCompile(`(\d+\.?\d*)% packet loss`)
	reJitter       = regexp.MustCompile(`min/avg/max/std-?dev = \d+(?:\.\d*)?/\d+(?:\.\d*)?/\d+(?:\.\d*)?/(\d+(?:\.\d*)?)`)
	reSanitizeHTTP = regexp.MustCompile(`[\x00-\x1F\x7F-\x9F]`)
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

const (
	wanTargetIPv4 = "1.1.1.1"
	wanTargetIPv6 = "2606:4700:4700::1111"
	wanTargetTCP  = "1.1.1.1:443"
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
			allDetails = append(allDetails, fmt.Sprintf("MTU: %s (Standard is 1500)", m[1]))
		}
	}

	allDetails = append(allDetails, details...)

	res.Details = append(res.Details, formatDetailsWithPrefixes(allDetails)...)
	if rssi < -80 && rssi != 0 {
		res.Status = StatusWarning
		res.Fix = "Weak signal. Move closer to the Access Point."
	}
	return res
}

// formatDetailsWithPrefixes applies the correct UI tree prefixes to a slice of strings.
func formatDetailsWithPrefixes(details []string) []string {
	if len(details) == 0 {
		return nil
	}
	formatted := make([]string, len(details))
	for i, detail := range details {
		prefix := "├─"
		if i == len(details)-1 {
			prefix = "└─"
		}
		formatted[i] = fmt.Sprintf("%s %s", prefix, detail)
	}
	return formatted
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
		var details []string
		out, errArp := exec.Command("arp", "-n", gw).Output()
		details = append(details, "--- ARP Entry ---")
		if errArp != nil {
			details = append(details, fmt.Sprintf("Failed: %v", errArp))
		} else {
			details = append(details, strings.TrimSpace(string(out)))
		}

		iface, errIface := getPrimaryInterface()
		details = append(details, "--- Interface Details ---")
		if errIface != nil {
			details = append(details, fmt.Sprintf("Failed to get interface: %v", errIface))
		} else {
			outIf, errIf := exec.Command("ifconfig", iface).Output()
			if errIf != nil {
				details = append(details, fmt.Sprintf("Failed ifconfig: %v", errIf))
			} else {
				lines := strings.Split(string(outIf), "\n")
				for _, l := range lines {
					if strings.Contains(l, "inet ") {
						details = append(details, strings.TrimSpace(l))
					}
				}
			}
		}
		res.Details = formatDetailsWithPrefixes(details)
	}
	return res
}

// CheckRoutingTable checks active network routing and Virtual Networks (VPNs/Docker).
func CheckRoutingTable() Result {
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
	interfaces, errNet := net.Interfaces()
	if errNet != nil {
		res.Status = StatusWarning
		log.Printf("diagnostic: could not get interfaces: %v", errNet)
	} else {
		for _, ifaceObj := range interfaces {
			if strings.HasPrefix(ifaceObj.Name, "utun") || strings.HasPrefix(ifaceObj.Name, "bridge") {
				// Only show if it's "up"
				if (ifaceObj.Flags & net.FlagUp) != 0 {
					addrs, errAddrs := ifaceObj.Addrs()
					if errAddrs != nil {
						log.Printf("diagnostic: could not get addresses for interface %s: %v", ifaceObj.Name, errAddrs)
						continue
					}
					for _, addr := range addrs {
						if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
							if ipnet.IP.To4() != nil {
								var kind string
								if strings.HasPrefix(ifaceObj.Name, "utun") {
									kind = "VPN/Tailscale"
								} else {
									kind = "Bridge/Docker"
								}
								virtuals = append(virtuals, fmt.Sprintf("%s (%s): Active (%s)", kind, ifaceObj.Name, ipnet.IP.String()))
								break // Only show first IPv4 for brevity
							}
						}
					}
				}
			}
		}
	}

	routePrefix := "├─"
	if len(virtuals) == 0 {
		routePrefix = "└─"
	}
	res.Details = append(res.Details, fmt.Sprintf("%s Default Route: %s (Gateway: %s)", routePrefix, iface, gwStr))

	res.Details = append(res.Details, formatDetailsWithPrefixes(virtuals)...)

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

	res.Details = formatDetailsWithPrefixes(details)
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
			var details []string
			for _, ip := range ips {
				details = append(details, "Proxy Node: "+ip.String())
			}
			res.Details = formatDetailsWithPrefixes(details)
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

	var details []string
	for _, h := range hops {
		if h != "" {
			details = append(details, h)
		}
	}
	res.Details = formatDetailsWithPrefixes(details)
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
		var details []string
		safeStatus := reSanitizeHTTP.ReplaceAllString(resp.Status, "")
		details = append(details, "Response Status: "+safeStatus)
		for k, v := range resp.Header {
			safeK := reSanitizeHTTP.ReplaceAllString(k, "")
			safeV := reSanitizeHTTP.ReplaceAllString(strings.Join(v, ", "), "")
			details = append(details, safeK+": "+safeV)
		}
		res.Details = formatDetailsWithPrefixes(details)
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
	if errClose := conn.Close(); errClose != nil {
		log.Printf("diagnostic: could not close tcpPing connection: %v", errClose)
	}
	return time.Since(start), nil
}

// MeasureLossAndJitter performs a 5-packet ping with 0.2s interval to calculate loss and jitter.
func MeasureLossAndJitter(ip string) (float64, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	var wg sync.WaitGroup
	var latIPv4, latIPv6, latTCP time.Duration
	var errIPv4, errIPv6, errTCP error
	var loss, jitter float64
	var errQoS error

	wg.Add(4)
	go func() { defer wg.Done(); latIPv4, errIPv4 = ping(wanTargetIPv4) }()
	go func() { defer wg.Done(); latIPv6, errIPv6 = ping6(wanTargetIPv6) }()
	go func() { defer wg.Done(); latTCP, errTCP = tcpPing(wanTargetTCP) }()
	go func() { defer wg.Done(); loss, jitter, errQoS = MeasureLossAndJitter(wanTargetIPv4) }()
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
	var details []string
	var ipv4Status string
	if errIPv4 == nil {
		ipv4Status = fmt.Sprintf("%v (Reachable)", latIPv4.Round(time.Millisecond))
	} else if errTCP == nil {
		ipv4Status = "TIMEOUT (Dropped)"
	} else {
		ipv4Status = "TIMEOUT (Unreachable)"
	}
	details = append(details, fmt.Sprintf("IPv4 (%s): %s", wanTargetIPv4, ipv4Status))

	ipv6Status := "TIMEOUT (Unreachable)"
	if errIPv6 == nil {
		ipv6Status = fmt.Sprintf("%v (Reachable)", latIPv6.Round(time.Millisecond))
	}
	details = append(details, fmt.Sprintf("IPv6 (%s): %s", wanTargetIPv6, ipv6Status))

	var tcpStatus string
	if errTCP == nil {
		tcpStatus = fmt.Sprintf("%v (Connected)", latTCP.Round(time.Millisecond))
	} else {
		tcpStatus = "TIMEOUT (Failed)"
	}
	details = append(details, fmt.Sprintf("TCP 443 (%s): %s", wanTargetIPv4, tcpStatus))

	if errQoS == nil {
		details = append(details, fmt.Sprintf("Quality: Loss: %.1f%%, Jitter: %.2fms", loss, jitter))
	} else {
		details = append(details, "Quality: Measurement failed or timed out")
	}

	res.Details = formatDetailsWithPrefixes(details)

	return res
}

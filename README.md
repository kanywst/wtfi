<div align="center">

# wtfi
**What The F*ck Internet**

[![Go Report Card](https://goreportcard.com/badge/github.com/kanywst/wtfi)](https://goreportcard.com/report/github.com/kanywst/wtfi)
[![CI Status](https://github.com/kanywst/wtfi/actions/workflows/ci.yml/badge.svg)](https://github.com/kanywst/wtfi/actions)
[![Go Version](https://img.shields.io/github/go-mod/go-version/kanywst/wtfi)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> *The Wi-Fi icon shows full bars. The browser says "Offline". You are losing your mind.* <br>
> **Stop guessing. Start knowing.**

</div>

---

**`wtfi`** is a high-performance network diagnostic CLI exclusively built for macOS. It surgically dissects your network stack from Layer 2 (Physical) to Layer 7 (Application) in milliseconds, visually pinpointing exactly where your connection died.

No more `ping 8.8.8.8`. No more clicking through system preferences.

## Why wtfi?

*   **Full-Spectrum Analysis:** Comprehensive checks across L2 (Signal/PHY), L3 (Gateway/WAN), and L7 (DNS/Captive Portal).
*   **Engineered for Speed:** Written in Go with concurrent path discovery. Results in milliseconds.
*   **macOS Optimized:** Deep integration with `system_profiler` and Apple-specific network telemetry.
*   **Modern UX:** Precise, color-coded diagnostic output designed for readability.

---

## See It In Action

```ansi
🚀 wtfi: Starting Network Diagnostics...
--------------------------------------------------
📡 Wi-Fi (Starbucks_Guest)                      OK
🏠 Gateway (10.0.0.1)                          4ms
🌐 Internet (1.1.1.1)                          6ms
🚦 DNS Benchmark                               2ms
🛡️ iCloud Private Relay                        1ms
📍 Fast Trace                                   OK
🍎 Captive Portal                            ERROR
   ├─ Info: Login Required (Captive Portal detected)
   └─ Fix:  Open your browser to sign in to the network.
--------------------------------------------------
```

*(Instantly identify if the issue is a physical signal, a DNS timeout, or a captive portal login.)*

---

## Installation

```bash
go install github.com/kanywst/wtfi/cmd/wtfi@latest
```

*(Requires Go 1.26+)*

---

## Features & Arsenal

### God Mode (`-v`)

Dump raw protocol telemetry for deep troubleshooting.

*   **L2:** PHY mode (802.11ax/ac), Security (WPA3), Channel width, Noise floor, MCS Index.
*   **L3:** ARP tables and raw Interface configurations.
*   **L7:** DNS resolver microsecond comparisons and HTTP response headers.

```bash
wtfi -v
```

### Radar Mode (`-w`)

Real-time polling every 2 seconds to locate Wi-Fi dead zones or monitor connection stability.

```bash
wtfi -w
```

---

## The Diagnostic Pipeline

1.  **Wi-Fi (L2):** Uses `system_profiler` for accurate RSSI, Noise, and SSID, bypassing deprecated tools.
2.  **Gateway (L3):** Automatically resolves your default route and executes high-precision ICMP pings.
3.  **Internet Backbone (L3):** Verifies WAN exit via Cloudflare (`1.1.1.1`).
4.  **DNS Benchmark (L7):** Races your system DNS against Google and Cloudflare to detect slow resolution or hijacking.
5.  **iCloud Private Relay:** Detects if macOS is routing traffic through Apple's proxy nodes.
6.  **Fast Trace:** Concurrent UDP mapping of your hop-by-hop route to the internet.
7.  **Captive Portal (L7):** Checks Apple's hotspot-detect endpoint with memory-safe `io.LimitReader`.

---

## Contributing

```bash
# Clone the repo
git clone https://github.com/kanywst/wtfi.git

# Run the quality pipeline
make fmt lint test build
```

---

<div align="center">
  <i>Built with rage by someone whose internet cut out one too many times.</i><br>
  <b>MIT License. 2026.</b>
</div>

package diagnostic

import (
	"strings"
	"testing"
	"time"
)

func TestStatusConstants(t *testing.T) {
	if StatusOk != 0 {
		t.Errorf("Expected StatusOk to be 0, got %d", StatusOk)
	}
}

func TestParsePing(t *testing.T) {
	output := `PING 1.1.1.1 (1.1.1.1): 56 data bytes
64 bytes from 1.1.1.1: icmp_seq=0 ttl=58 time=12.345 ms

--- 1.1.1.1 ping statistics ---
1 packets transmitted, 1 packets received, 0.0% packet loss
round-trip min/avg/max/stddev = 12.345/12.345/12.345/0.000 ms
`
	lat, err := parsePing(output)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	expected := 12*time.Millisecond + 345*time.Microsecond
	if lat != expected {
		t.Errorf("Expected latency %v, got %v", expected, lat)
	}
}

func TestParseInterface(t *testing.T) {
	output := `   route to: default
destination: default
       mask: default
    gateway: 192.168.0.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
 recvpipe  sendpipe  ssthresh  rtt,msec    rttvar  hopcount      mtu     expire
       0         0         0         0         0         0      1500         0 `
	iface, err := parseInterface(output)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if iface != "en0" {
		t.Errorf("Expected en0, got %s", iface)
	}
}

func TestParseWiFiInfo(t *testing.T) {
	output := `Software Details:
...
      Current Network Information:
        MyHomeWiFi:
          PHY Mode: 802.11ax
          Channel: 36 (5GHz, 80MHz)
          Signal / Noise: -50 dBm / -92 dBm
          Transmit Rate: 1200
`
	res := parseWiFiInfo(output, "en0", true)
	if res.Status != StatusOk {
		t.Errorf("Expected StatusOk, got %d", res.Status)
	}
	if !strings.Contains(res.Name, "MyHomeWiFi") {
		t.Errorf("Expected SSID MyHomeWiFi in Name, got %s", res.Name)
	}
	if len(res.Details) == 0 {
		t.Error("Expected details in verbose mode, got none")
	}
}

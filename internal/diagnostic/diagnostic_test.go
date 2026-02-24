package diagnostic

import (
	"regexp"
	"strconv"
	"testing"
)

func TestStatusConstants(t *testing.T) {
	if StatusOk != 0 {
		t.Errorf("Expected StatusOk to be 0, got %d", StatusOk)
	}
}

func TestPingParsing(t *testing.T) {
	sampleOutput := `PING 1.1.1.1 (1.1.1.1): 56 data bytes
64 bytes from 1.1.1.1: icmp_seq=0 ttl=58 time=12.345 ms

--- 1.1.1.1 ping statistics ---
1 packets transmitted, 1 packets received, 0.0% packet loss
round-trip min/avg/max/stddev = 12.345/12.345/12.345/0.000 ms
`
	re := `min/avg/max/stddev = [\d\.]+/([\d\.]+)`
	match := findAveragePing(sampleOutput, re)
	expected := 12.345
	if match != expected {
		t.Errorf("Expected ping to be %f, got %f", expected, match)
	}
}

func findAveragePing(out string, reg string) float64 {
	re := regexp.MustCompile(reg)
	m := re.FindStringSubmatch(out)
	if len(m) > 1 {
		avg, _ := strconv.ParseFloat(m[1], 64)
		return avg
	}
	return 0
}

package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Counters are what LiveKit's Prometheus endpoint (prometheus_port) says
// about the whole media server since it started (D-82). LiveKit reports
// nothing per room, so these are the server's totals.
type Counters struct {
	BytesIn    float64 // received from publishers
	BytesOut   float64 // sent to subscribers
	CPUSeconds float64 // LiveKit's own process
	At         time.Time
}

// Scrape reads the counters from url (http://127.0.0.1:6789/metrics).
func Scrape(ctx context.Context, client *http.Client, url string) (Counters, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Counters{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Counters{}, fmt.Errorf("media server metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Counters{}, fmt.Errorf("media server metrics: %s", resp.Status)
	}
	c, err := ParseCounters(io.LimitReader(resp.Body, 8<<20))
	c.At = time.Now()
	return c, err
}

// ParseCounters reads Prometheus's text format, adding up the series of
// livekit_packet_bytes by direction (first sends and retransmissions alike).
func ParseCounters(r io.Reader) (Counters, error) {
	var c Counters
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		name, labels, value, ok := splitSample(line)
		if !ok {
			continue
		}
		switch name {
		case "livekit_packet_bytes":
			switch {
			case strings.Contains(labels, `direction="incoming"`):
				c.BytesIn += value
			case strings.Contains(labels, `direction="outgoing"`):
				c.BytesOut += value
			}
		case "process_cpu_seconds_total":
			c.CPUSeconds += value
		}
	}
	return c, sc.Err()
}

// splitSample splits `name{labels} value [timestamp]`.
func splitSample(line string) (name, labels string, value float64, ok bool) {
	rest := line
	if i := strings.IndexByte(line, '{'); i >= 0 {
		j := strings.LastIndexByte(line, '}')
		if j < i {
			return "", "", 0, false
		}
		name, labels, rest = line[:i], line[i+1:j], strings.TrimSpace(line[j+1:])
	} else {
		f := strings.Fields(line)
		if len(f) < 2 {
			return "", "", 0, false
		}
		name, rest = f[0], f[1]
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return "", "", 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	return name, labels, v, err == nil
}

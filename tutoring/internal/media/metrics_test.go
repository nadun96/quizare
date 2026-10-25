package media

import (
	"strings"
	"testing"
)

func TestParseCounters(t *testing.T) {
	in := `# HELP livekit_packet_bytes bytes
# TYPE livekit_packet_bytes counter
livekit_packet_bytes{country="",direction="incoming",node_id="ND_x",node_type="SERVER",transmission="initial"} 2.369397e+06
livekit_packet_bytes{country="",direction="incoming",node_id="ND_x",node_type="SERVER",transmission="retransmit"} 1000
livekit_packet_bytes{country="",direction="outgoing",node_id="ND_x",node_type="SERVER",transmission="initial"} 988135
livekit_node_packet_total{node_type="SERVER",type="out"} 12
process_cpu_seconds_total 12.5
broken line
livekit_packet_bytes{direction="outgoing"} not-a-number
`
	c, err := ParseCounters(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if c.BytesIn != 2370397 || c.BytesOut != 988135 || c.CPUSeconds != 12.5 {
		t.Fatalf("counters = %+v", c)
	}
}

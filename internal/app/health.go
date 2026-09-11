package app

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"time"
)

func dnsQueryPacket(name string, id uint16) []byte {
	q := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(q[0:2], id)
	binary.BigEndian.PutUint16(q[2:4], 0x0100) // recursion desired
	binary.BigEndian.PutUint16(q[4:6], 1)
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 {
			continue
		}
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0, 0, 1, 0, 1) // A / IN
	return q
}

func validDNSResponse(b []byte, id uint16) bool {
	if len(b) < 12 || binary.BigEndian.Uint16(b[0:2]) != id {
		return false
	}
	flags := binary.BigEndian.Uint16(b[2:4])
	if flags&0x8000 == 0 || flags&0x000f != 0 { // response + NOERROR
		return false
	}
	return binary.BigEndian.Uint16(b[6:8]) > 0
}

// CheckDNSListener performs an actual DNS A query through a loopback SmartDNS
// listener that is pinned to one upstream group. This verifies DNS resolution,
// not just socket reachability, and therefore works for UDP/TCP/DoT/DoH/DoQ/DoH3
// without reimplementing every upstream transport in Go.
func CheckDNSListener(ctx context.Context, addr string) bool {
	if addr == "" {
		return false
	}
	id := uint16(time.Now().UnixNano())
	q := dnsQueryPacket("example.com", id)
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return false
	}
	defer c.Close()
	deadline := time.Now().Add(4 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = c.SetDeadline(deadline)
	if _, err = c.Write(q); err != nil {
		return false
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	return err == nil && validDNSResponse(buf[:n], id)
}

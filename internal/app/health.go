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

func checkDNSListenerOnce(ctx context.Context, addr string, timeout time.Duration, id uint16) bool {
	q := dnsQueryPacket("example.com", id)
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return false
	}
	defer c.Close()
	deadline := time.Now().Add(timeout)
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

// CheckDNSListener performs actual DNS A queries through a loopback SmartDNS
// listener pinned to one upstream group. Health is decided by a 2-of-3 quorum:
// one transient lost reply cannot mark a working line down, while one lucky
// reply cannot immediately revive a flapping line. Healthy lines normally
// finish after two fast successful queries.
func CheckDNSListener(ctx context.Context, addr string) bool {
	if addr == "" {
		return false
	}
	const attempts = 3
	const quorum = 2
	const perAttempt = 2 * time.Second
	successes, failures := 0, 0
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return false
		}
		id := uint16(time.Now().UnixNano()) + uint16(i)
		if checkDNSListenerOnce(ctx, addr, perAttempt, id) {
			successes++
			if successes >= quorum { return true }
		} else {
			failures++
			if failures >= quorum { return false }
		}
		if i+1 < attempts {
			t := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				t.Stop()
				return false
			case <-t.C:
			}
		}
	}
	return successes >= quorum
}

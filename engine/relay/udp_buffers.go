package relay

import (
	"fmt"
	"net"
)

// TuneUDPBuffers requests a large UDP socket buffer and degrades to smaller
// values when the host refuses the preferred size. Buffer sizing is a
// throughput optimization; callers should keep the socket usable when every
// requested size is rejected.
func TuneUDPBuffers(conn *net.UDPConn) error {
	if conn == nil {
		return fmt.Errorf("relay: UDP connection is nil")
	}
	var lastErr error
	for _, size := range []int{32 << 20, 8 << 20, 4 << 20, 1 << 20, 256 << 10} {
		if err := conn.SetReadBuffer(size); err != nil {
			lastErr = err
			continue
		}
		if err := conn.SetWriteBuffer(size); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("relay: set UDP socket buffers: %w", lastErr)
}

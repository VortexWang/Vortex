//go:build !linux || (!amd64 && !arm64)

package relay

import "net"

func copyTCPPlatform(src, dst net.Conn, direction Direction, meter Meter) (int64, error) {
	return copyTCP(src, dst, direction, meter)
}

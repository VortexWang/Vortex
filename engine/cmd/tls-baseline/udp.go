package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"vortex/engine/runtime/config"
)

const baselineUDPProtocol = "vortex-baseline-udp/1"
const baselineMaxDatagram = 16384

// The fixture admits one UDP client at a time, matching its single-flow paced
// workload. Intermediate hops only copy bytes; the two boundary roles delimit
// datagrams with a two-byte length. There are no VLink or accounting imports.
func serveBaselineUDP(ctx context.Context, cfg config.Config, tlsConfig *tls.Config) error {
	packet, err := net.ListenPacket("udp", cfg.UDPServiceListen)
	if err != nil {
		return err
	}
	defer packet.Close()
	if udp, ok := packet.(*net.UDPConn); ok {
		_ = udp.SetReadBuffer(4 << 20)
	}
	stop := context.AfterFunc(ctx, func() { packet.Close() })
	defer stop()
	buffer := make([]byte, baselineMaxDatagram+1)
	n, peer, err := packet.ReadFrom(buffer)
	if err != nil {
		return err
	}
	outgoing := dataTLSConfig(tlsConfig)
	outgoing.NextProtos = []string{baselineUDPProtocol}
	link, err := (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: outgoing}).DialContext(ctx, "tcp", cfg.Peer)
	if err != nil {
		return err
	}
	defer link.Close()
	stopLink := context.AfterFunc(ctx, func() { link.Close() })
	defer stopLink()
	responses := make(chan error, 1)
	go func() {
		for {
			data, err := readBaselineDatagram(link)
			if err != nil {
				responses <- err
				packet.Close()
				return
			}
			if _, err = packet.WriteTo(data, peer); err != nil {
				responses <- err
				packet.Close()
				return
			}
		}
	}()
	for {
		if n > 0 && n <= baselineMaxDatagram {
			if err = writeBaselineDatagram(link, buffer[:n]); err != nil {
				break
			}
		}
		var next net.Addr
		n, next, err = packet.ReadFrom(buffer)
		if err != nil {
			break
		}
		if next.String() != peer.String() {
			n = 0
		} // explicit single-client fixture bound
	}
	link.Close()
	<-responses
	return err
}

func bridgeBaselineUDP(link, target net.Conn) {
	if udp, ok := target.(*net.UDPConn); ok {
		_ = udp.SetReadBuffer(4 << 20)
	}
	done := make(chan error, 2)
	go func() {
		for {
			data, err := readBaselineDatagram(link)
			if err != nil {
				done <- err
				return
			}
			n, err := target.Write(data)
			if err != nil || n != len(data) {
				done <- io.ErrShortWrite
				return
			}
		}
	}()
	go func() {
		buffer := make([]byte, baselineMaxDatagram+1)
		for {
			n, err := target.Read(buffer)
			if err != nil {
				done <- err
				return
			}
			if n == 0 || n > baselineMaxDatagram {
				continue
			}
			if err = writeBaselineDatagram(link, buffer[:n]); err != nil {
				done <- err
				return
			}
		}
	}()
	<-done
	link.Close()
	target.Close()
	<-done
}

func readBaselineDatagram(r io.Reader) ([]byte, error) {
	var length [2]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(length[:]))
	if n == 0 || n > baselineMaxDatagram {
		return nil, errors.New("baseline datagram size invalid")
	}
	data := make([]byte, n)
	_, err := io.ReadFull(r, data)
	return data, err
}
func writeBaselineDatagram(w io.Writer, data []byte) error {
	if len(data) == 0 || len(data) > baselineMaxDatagram {
		return errors.New("baseline datagram size invalid")
	}
	encoded := make([]byte, 2+len(data))
	binary.BigEndian.PutUint16(encoded, uint16(len(data)))
	copy(encoded[2:], data)
	for len(encoded) > 0 {
		n, err := w.Write(encoded)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		encoded = encoded[n:]
	}
	return nil
}

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"time"
)

func main() {
	count := flag.Int("count", 20000, "datagrams to send")
	flag.Parse()
	const size = 1200
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(64 << 20)
	target := conn.LocalAddr().(*net.UDPAddr)
	received := make(chan int, 1)
	go func() {
		buf := make([]byte, size)
		seen := 0
		// Go's timer granularity can stretch the nominal 100 Mbps pacing on
		// small hosts. Keep the receiver alive for the whole send window plus
		// a bounded drain period so late packets are not counted as loss.
		readWindow := time.Duration(*count)*time.Millisecond + 10*time.Second
		_ = conn.SetReadDeadline(time.Now().Add(readWindow))
		for seen < *count {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				break
			}
			if n == size {
				seen++
			}
		}
		received <- seen
	}()
	client, err := net.DialUDP("udp", nil, target)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	_ = client.SetWriteBuffer(64 << 20)
	payload := make([]byte, size)
	start := time.Now()
	interval := time.Duration(float64(time.Second) * float64(size*8) / 100_000_000)
	const batchSize = 8
	for i := 0; i < *count; i++ {
		binary.BigEndian.PutUint64(payload, uint64(i))
		if _, err := client.Write(payload); err != nil {
			panic(err)
		}
		if (i+1)%batchSize == 0 || i+1 == *count {
			time.Sleep(interval * time.Duration(batchSize))
		}
	}
	got := <-received
	lost := *count - got
	fmt.Printf("sent=%d received=%d lost=%d loss_pct=%.4f duration=%s payload_bytes=%d target_mbps=100 loopback=true\n", *count, got, lost, float64(lost)*100/float64(*count), time.Since(start), size)
}

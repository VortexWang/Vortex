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
	target := conn.LocalAddr().(*net.UDPAddr)
	received := make(chan int, 1)
	go func() {
		buf := make([]byte, size)
		seen := 0
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
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
	payload := make([]byte, size)
	start := time.Now()
	interval := time.Duration(float64(time.Second) * float64(size*8) / 100_000_000)
	for i := 0; i < *count; i++ {
		binary.BigEndian.PutUint64(payload, uint64(i))
		if _, err := client.Write(payload); err != nil {
			panic(err)
		}
		time.Sleep(interval)
	}
	got := <-received
	lost := *count - got
	fmt.Printf("sent=%d received=%d lost=%d loss_pct=%.4f duration=%s payload_bytes=%d target_mbps=100 loopback=true\n", *count, got, lost, float64(lost)*100/float64(*count), time.Since(start), size)
}

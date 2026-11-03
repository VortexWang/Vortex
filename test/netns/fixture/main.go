package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
)

func main() {
	mode := flag.String("mode", "server", "server or client")
	listen := flag.String("listen", "0.0.0.0:9090", "listen address")
	address := flag.String("address", "", "server address")
	message := flag.String("message", "vortex-phase1", "payload")
	flag.Parse()
	var err error
	if *mode == "server" {
		err = server(*listen, *message)
	} else {
		err = client(*address, *message)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func server(address, expected string) error {
	l, err := net.Listen("tcp", address)
	if err != nil { return err }
	defer l.Close()
	c, err := l.Accept()
	if err != nil { return err }
	defer c.Close()
	b, err := io.ReadAll(io.LimitReader(c, int64(len(expected)+1)))
	if err != nil { return err }
	if string(b) != expected { return fmt.Errorf("payload mismatch: %q", b) }
	_, err = c.Write(b)
	return err
}

func client(address, message string) error {
	c, err := net.Dial("tcp", address)
	if err != nil { return err }
	defer c.Close()
	if _, err = c.Write([]byte(message)); err != nil { return err }
	if tcp, ok := c.(*net.TCPConn); ok {
		if err = tcp.CloseWrite(); err != nil { return err }
	}
	b, err := io.ReadAll(io.LimitReader(c, int64(len(message)+1)))
	if err != nil { return err }
	fmt.Println(string(b))
	return nil
}

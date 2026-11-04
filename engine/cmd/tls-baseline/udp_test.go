package main

import (
	"bytes"
	"testing"
)

func TestBaselineDatagramRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	want := []byte("baseline-udp")
	if err := writeBaselineDatagram(&wire, want); err != nil {
		t.Fatal(err)
	}
	got, err := readBaselineDatagram(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload mismatch: got %q want %q", got, want)
	}
}

func TestBaselineDatagramRejectsInvalidLengths(t *testing.T) {
	for _, wire := range [][]byte{{0, 0}, {0x40, 0x01}} {
		if _, err := readBaselineDatagram(bytes.NewReader(wire)); err == nil {
			t.Fatalf("accepted invalid datagram length %x", wire)
		}
	}
	if err := writeBaselineDatagram(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("accepted empty datagram")
	}
}

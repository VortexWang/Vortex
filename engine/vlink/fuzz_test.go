package vlink

import (
	"bytes"
	"testing"
)

func FuzzReadFrame(f *testing.F) {
	for _, seed := range [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{byte(FramePing), 0, 0, 8, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8},
		{byte(FrameOpenErr), 0, 0, 18, 0, 0, 0, 1},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = ReadFrame(bytes.NewReader(input))
	})
}

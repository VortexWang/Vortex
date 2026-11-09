package control

import "crypto/sha256"

func sha256Bytes(raw []byte) []byte { sum := sha256.Sum256(raw); return sum[:] }

//go:build !linux

package store

func currentProcessWitness() ([]byte, error) { return nil, nil }
func processStopped([]byte) (bool, error)    { return false, nil }

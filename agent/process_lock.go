package agent

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// LockProcess is acquired before opening/migrating SQLite, so status/drain use
// IPC and a second run/enroll cannot create a competing writer or boot.
func LockProcess(database string) (func(), error) {
	path, err := filepath.Abs(database + ".lock")
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	var info unix.Stat_t
	if err = unix.Fstat(fd, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0777 != 0600 || info.Uid != uint32(os.Geteuid()) || info.Nlink != 1 {
		file.Close()
		return nil, fmt.Errorf("agent: insecure process lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("agent: another node process owns this database: %w", err)
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = file.Close() }, nil
}

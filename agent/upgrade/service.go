package upgrade

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"time"
)

// ServiceController belongs to the external upgrader, never to a goroutine in
// the node being replaced. StopAndWait must witness termination of the complete
// service namespace, not merely acknowledge a signal or observe a new boot.
// Start must be idempotent for an already running exact release; Health remains
// the Installer's mandatory current-authority/traffic verifier.
type ServiceController interface {
	StopAndWait(context.Context) error
	Start(context.Context, string) error
}

func (i *Installer) stopService(ctx context.Context) error {
	if i.Service == nil {
		return nil
	}
	stop, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := i.Service.StopAndWait(stop); err != nil {
		return errors.Join(ErrActivationUnknown, fmt.Errorf("upgrade: service termination unproven: %w", err))
	}
	return nil
}
func (i *Installer) startAndCheck(ctx context.Context, path string) error {
	if i.Service != nil {
		if err := i.Service.Start(ctx, path); err != nil {
			return err
		}
	}
	return i.Health(ctx, path)
}

// Multiple upgrader processes/Installer instances share one durable transaction
// journal. Advisory OS lock is held across every mutation and recovery action.
func (i *Installer) lockJournal() (func(), error) {
	if err := i.validate(); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(i.Root, "installer.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "installer.lock")
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		file.Close()
		return nil, errors.New("upgrade: unsafe installer lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("upgrade: another installer owns the journal")
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = file.Close() }, nil
}

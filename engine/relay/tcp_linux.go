//go:build linux && (amd64 || arm64)

package relay

import (
	"errors"
	"net"
	"syscall"
)

const (
	spliceChunkSize = 4096
	spliceFlags     = 2 // SPLICE_F_NONBLOCK
)

func copyTCPPlatform(src, dst net.Conn, direction Direction, meter Meter) (int64, error) {
	from, ok := src.(*net.TCPConn)
	if !ok {
		return copyTCP(src, dst, direction, meter)
	}
	to, ok := dst.(*net.TCPConn)
	if !ok {
		return copyTCP(src, dst, direction, meter)
	}

	pipe := []int{0, 0}
	if err := syscall.Pipe2(pipe, syscall.O_CLOEXEC|syscall.O_NONBLOCK); err != nil {
		return 0, err
	}
	defer syscall.Close(pipe[0])
	defer syscall.Close(pipe[1])

	readConn, err := from.SyscallConn()
	if err != nil {
		return 0, err
	}
	writeConn, err := to.SyscallConn()
	if err != nil {
		return 0, err
	}

	var total int64
	for {
		staged, err := spliceRead(readConn, pipe[1], spliceChunkSize)
		if err != nil {
			return total, err
		}
		if staged == 0 {
			if err := to.CloseWrite(); err != nil {
				return total, err
			}
			return total, nil
		}

		remaining := staged
		for remaining > 0 {
			written, err := spliceWrite(writeConn, pipe[0], remaining)
			if err != nil {
				return total, err
			}
			if written == 0 {
				return total, ErrShortWrite
			}
			remaining -= written
			total += int64(written)
			if meter != nil {
				if err := meter(direction, int64(written)); err != nil {
					return total, err
				}
			}
		}
	}
}

func spliceRead(conn syscall.RawConn, pipeWrite, size int) (int, error) {
	var count int
	var operationErr error
	if err := conn.Read(func(fd uintptr) bool {
		count, operationErr = splice(int(fd), pipeWrite, size)
		return !wouldBlock(operationErr)
	}); err != nil {
		return 0, err
	}
	if operationErr != nil {
		return 0, operationErr
	}
	return count, nil
}

func spliceWrite(conn syscall.RawConn, pipeRead, size int) (int, error) {
	var count int
	var operationErr error
	if err := conn.Write(func(fd uintptr) bool {
		count, operationErr = splice(pipeRead, int(fd), size)
		return !wouldBlock(operationErr)
	}); err != nil {
		return 0, err
	}
	if operationErr != nil {
		return 0, operationErr
	}
	return count, nil
}

func splice(readFD, writeFD, size int) (int, error) {
	count, _, errno := syscall.RawSyscall6(syscall.SYS_SPLICE,
		uintptr(readFD), 0,
		uintptr(writeFD), 0,
		uintptr(size), spliceFlags,
	)
	if errno != 0 {
		return 0, errno
	}
	return int(count), nil
}

func wouldBlock(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

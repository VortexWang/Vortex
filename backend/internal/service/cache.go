package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"vortex/backend/db/generated"
)

// CacheCommand is a bounded RESP command for the optional internal Valkey
// projection. Business decisions never read this result. It adds no new client
// dependency and rejects unexpected/oversized protocol responses.
func CacheCommand(ctx context.Context, address string, args ...string) (string, error) {
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return "", err
	}
	var request strings.Builder
	fmt.Fprintf(&request, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&request, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if request.Len() > 1<<20 {
		return "", errors.New("cache request exceeds bound")
	}
	if _, err = io.WriteString(conn, request.String()); err != nil {
		return "", err
	}
	reader := bufio.NewReader(io.LimitReader(conn, (1<<20)+128))
	line, err := reader.ReadString('\n')
	if err != nil || len(line) > 128 {
		return "", errors.New("invalid cache response")
	}
	if line[0] == '+' {
		return strings.TrimSuffix(line[1:], "\r\n"), nil
	}
	if line[0] != '$' {
		return "", errors.New("unexpected cache response")
	}
	size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || size < 0 || size > 1<<20 {
		return "", errors.New("missing or oversized cache value")
	}
	raw := make([]byte, size+2)
	if _, err = io.ReadFull(reader, raw); err != nil {
		return "", err
	}
	if string(raw[size:]) != "\r\n" {
		return "", errors.New("invalid cache frame")
	}
	return string(raw[:size]), nil
}

func (s *Tracer) RebuildCache(ctx context.Context, scope TenantScope, address string) ([]byte, error) {
	if !scope.Valid() {
		return nil, ErrScope
	}
	rows, err := generated.New(s.Pool).ListAuthority(ctx, scope.tenant)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	_, err = CacheCommand(ctx, address, "SET", "vortex:authority:"+scope.tenant, string(raw))
	return raw, err
}

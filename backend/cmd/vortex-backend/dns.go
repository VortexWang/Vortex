package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/internal/control"
	"vortex/backend/internal/worker"
)

// Keys are deployment-mounted secrets, never HTTP fields, command-line values
// or PG data. The DNS token itself remains in the existing encrypted store.
func dnsCryptoFile(path string) (*control.CredentialCrypto, error) {
	invalid := errors.New("DNS keyring unavailable or invalid")
	file, e := os.Open(path)
	if e != nil {
		return nil, invalid
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64*1024 {
		return nil, invalid
	}
	raw, e := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if e != nil || len(raw) > 64*1024 {
		return nil, invalid
	}
	defer clear(raw)
	var body struct {
		Current int64             `json:"current"`
		Keys    map[string]string `json:"keys"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || len(body.Keys) < 1 || len(body.Keys) > 16 {
		return nil, invalid
	}
	keys := map[int64][]byte{}
	defer func() {
		for _, key := range keys {
			clear(key)
		}
	}()
	for version, value := range body.Keys {
		v, e := strconv.ParseInt(version, 10, 64)
		if e != nil || v < 1 || strconv.FormatInt(v, 10) != version {
			return nil, invalid
		}
		key, e := base64.StdEncoding.Strict().DecodeString(value)
		if e != nil || len(key) != 32 {
			clear(key)
			return nil, invalid
		}
		keys[v] = key
	}
	crypto, e := control.NewCredentialCrypto(body.Current, keys)
	if e != nil {
		return nil, invalid
	}
	return crypto, nil
}
func runDNS(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	f := flag.NewFlagSet("run-dns", flag.ContinueOnError)
	path := f.String("keyring-file", "", "private deployment-mounted versioned AES keyring JSON")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 || *path == "" {
		return errors.New("run-dns requires --keyring-file")
	}
	crypto, e := dnsCryptoFile(*path)
	if e != nil {
		return e
	}
	// Reconcile uses only fixed Cloudflare endpoints and enrolled zone access;
	// token enrollment/policy verification is a separate trusted operation.
	store := control.CredentialStore{Pool: pool, Crypto: crypto, Provider: control.NewCloudflare(nil)}
	e = (worker.DNSWorker{Pool: pool, Store: store}).Run(ctx)
	if ctx.Err() != nil {
		return nil
	}
	return e
}

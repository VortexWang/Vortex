package control

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
)

var ErrCredential = errors.New("DNS credential unavailable or scope invalid")

// Keys are supplied by the deployment secret manager, never PostgreSQL. Keep
// old versions available until all ciphertext and backups have been rotated.
// Loss of a key fails closed and requires explicit token re-enrollment.
type CredentialCrypto struct {
	keys    map[int64][]byte
	current int64
}

func NewCredentialCrypto(current int64, keys map[int64][]byte) (*CredentialCrypto, error) {
	c := &CredentialCrypto{keys: make(map[int64][]byte), current: current}
	for v, k := range keys {
		if v < 1 || len(k) != 32 {
			return nil, ErrCredential
		}
		c.keys[v] = append([]byte(nil), k...)
	}
	if len(c.keys[current]) != 32 {
		return nil, ErrCredential
	}
	return c, nil
}
func credentialAAD(tenant, id, zone string, version int64) []byte {
	b, _ := json.Marshal(struct {
		Tenant, ID, Zone string
		Version          int64
	}{tenant, id, zone, version})
	return b
}
func (c *CredentialCrypto) aead(v int64) (cipher.AEAD, error) {
	if c == nil || len(c.keys[v]) != 32 {
		return nil, ErrCredential
	}
	block, e := aes.NewCipher(c.keys[v])
	if e != nil {
		return nil, ErrCredential
	}
	return cipher.NewGCM(block)
}
func (c *CredentialCrypto) seal(tenant, id, zone string, token []byte) (ciphertext, nonce []byte, version int64, err error) {
	if c == nil {
		err = ErrCredential
		return
	}
	version = c.current
	a, e := c.aead(version)
	if e != nil {
		err = e
		return
	}
	nonce = make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		err = ErrCredential
		return
	}
	ciphertext = a.Seal(nil, nonce, token, credentialAAD(tenant, id, zone, version))
	return
}
func (c *CredentialCrypto) open(tenant, id, zone string, version int64, ciphertext, nonce []byte) ([]byte, error) {
	a, e := c.aead(version)
	if e != nil || len(nonce) != 12 {
		return nil, ErrCredential
	}
	token, e := a.Open(nil, nonce, ciphertext, credentialAAD(tenant, id, zone, version))
	if e != nil {
		return nil, ErrCredential
	}
	return token, nil
}

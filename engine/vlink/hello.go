package vlink

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	ProtocolVersion uint16 = 0x0100
	helloLabel             = "EXPORTER-VORTEX-VLINK-v1"
)

type NodeID [16]byte
type BootID [16]byte
type HelloNonce [32]byte

type HelloPhase uint8

const (
	HelloOffer HelloPhase = iota
	HelloResponse
	HelloConfirm
)

type HelloMessage struct {
	Phase             HelloPhase
	Version           uint16
	Role              uint8
	LocalNodeID       NodeID
	LocalBootID       BootID
	LocalGeneration   uint64
	LocalNonce        HelloNonce
	PeerNodeID        NodeID
	PeerBootID        BootID
	PeerGeneration    uint64
	PeerNonce         HelloNonce
	LocalCapabilities []uint16
	PeerCapabilities  []uint16
	Signature         []byte
}

type PeerVerifier func(context.Context, *x509.Certificate, HelloMessage) error

type HelloConfig struct {
	Client       bool
	NodeID       NodeID
	BootID       BootID
	Generation   uint64
	Capabilities []uint16
	Signer       crypto.Signer
	NewNonce     func() (HelloNonce, error)
	VerifyPeer   PeerVerifier
}

func ParseNodeID(value string) (NodeID, error) {
	var id NodeID
	if len(value) != 36 || strings.ToLower(value) != value || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return id, errors.New("vlink: node_id must be a canonical lowercase UUID")
	}
	compact := strings.ReplaceAll(value, "-", "")
	b, err := hex.DecodeString(compact)
	if err != nil || len(b) != len(id) {
		return id, errors.New("vlink: invalid node_id UUID")
	}
	copy(id[:], b)
	return id, nil
}

func (id NodeID) String() string {
	var text [36]byte
	hex.Encode(text[0:8], id[0:4])
	text[8] = '-'
	hex.Encode(text[9:13], id[4:6])
	text[13] = '-'
	hex.Encode(text[14:18], id[6:8])
	text[18] = '-'
	hex.Encode(text[19:23], id[8:10])
	text[23] = '-'
	hex.Encode(text[24:36], id[10:16])
	return string(text[:])
}

func NodeIDFromCertificate(cert *x509.Certificate) (NodeID, error) {
	var zero NodeID
	if cert == nil {
		return zero, errors.New("vlink: peer certificate is required")
	}
	const prefix = "urn:vortex:node:"
	var found NodeID
	count := 0
	for _, uri := range cert.URIs {
		value := uri.String()
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		id, err := ParseNodeID(strings.TrimPrefix(value, prefix))
		if err != nil {
			return zero, err
		}
		found = id
		count++
	}
	if count != 1 {
		return zero, errors.New("vlink: peer certificate must contain exactly one VLink node_id URI SAN")
	}
	return found, nil
}

func MarshalHello(message HelloMessage) ([]byte, error) {
	if err := validateHelloMessage(message, true); err != nil {
		return nil, err
	}
	capBytes := 2 * (len(message.LocalCapabilities) + len(message.PeerCapabilities))
	out := make([]byte, 1+2+1+16+16+8+32+16+16+8+32+4+capBytes+2+len(message.Signature))
	offset := 0
	out[offset] = byte(message.Phase)
	offset++
	binary.BigEndian.PutUint16(out[offset:offset+2], message.Version)
	offset += 2
	out[offset] = message.Role
	offset++
	copy(out[offset:offset+16], message.LocalNodeID[:])
	offset += 16
	copy(out[offset:offset+16], message.LocalBootID[:])
	offset += 16
	binary.BigEndian.PutUint64(out[offset:offset+8], message.LocalGeneration)
	offset += 8
	copy(out[offset:offset+32], message.LocalNonce[:])
	offset += 32
	copy(out[offset:offset+16], message.PeerNodeID[:])
	offset += 16
	copy(out[offset:offset+16], message.PeerBootID[:])
	offset += 16
	binary.BigEndian.PutUint64(out[offset:offset+8], message.PeerGeneration)
	offset += 8
	copy(out[offset:offset+32], message.PeerNonce[:])
	offset += 32
	offset = putCapabilities(out, offset, message.LocalCapabilities)
	offset = putCapabilities(out, offset, message.PeerCapabilities)
	binary.BigEndian.PutUint16(out[offset:offset+2], uint16(len(message.Signature)))
	offset += 2
	copy(out[offset:], message.Signature)
	return out, nil
}

func ParseHello(payload []byte) (HelloMessage, error) {
	var message HelloMessage
	const fixed = 1 + 2 + 1 + 16 + 16 + 8 + 32 + 16 + 16 + 8 + 32
	if len(payload) < fixed+2+2+2+1 || len(payload) > 922 {
		return message, fmt.Errorf("%w: HELLO payload length is invalid", ErrProtocol)
	}
	offset := 0
	message.Phase = HelloPhase(payload[offset])
	offset++
	message.Version = binary.BigEndian.Uint16(payload[offset : offset+2])
	offset += 2
	message.Role = payload[offset]
	offset++
	copy(message.LocalNodeID[:], payload[offset:offset+16])
	offset += 16
	copy(message.LocalBootID[:], payload[offset:offset+16])
	offset += 16
	message.LocalGeneration = binary.BigEndian.Uint64(payload[offset : offset+8])
	offset += 8
	copy(message.LocalNonce[:], payload[offset:offset+32])
	offset += 32
	copy(message.PeerNodeID[:], payload[offset:offset+16])
	offset += 16
	copy(message.PeerBootID[:], payload[offset:offset+16])
	offset += 16
	message.PeerGeneration = binary.BigEndian.Uint64(payload[offset : offset+8])
	offset += 8
	copy(message.PeerNonce[:], payload[offset:offset+32])
	offset += 32
	var err error
	message.LocalCapabilities, offset, err = readCapabilities(payload, offset)
	if err != nil {
		return HelloMessage{}, err
	}
	message.PeerCapabilities, offset, err = readCapabilities(payload, offset)
	if err != nil {
		return HelloMessage{}, err
	}
	if len(payload)-offset < 2 {
		return HelloMessage{}, fmt.Errorf("%w: truncated HELLO signature length", ErrProtocol)
	}
	signatureLength := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	if signatureLength < 1 || signatureLength > 512 || len(payload)-offset != signatureLength {
		return HelloMessage{}, fmt.Errorf("%w: invalid HELLO signature length", ErrProtocol)
	}
	message.Signature = append([]byte(nil), payload[offset:]...)
	if err := validateHelloMessage(message, true); err != nil {
		return HelloMessage{}, err
	}
	return message, nil
}

func HelloOfferTranscript(message HelloMessage) ([]byte, error) {
	if message.Phase != HelloOffer || message.PeerNodeID != (NodeID{}) || message.PeerBootID != (BootID{}) || message.PeerNonce != (HelloNonce{}) || message.PeerGeneration != 0 || len(message.PeerCapabilities) != 0 {
		return nil, fmt.Errorf("%w: invalid HELLO offer fields", ErrProtocol)
	}
	return encodeHelloTranscript(message), nil
}

func HelloTranscript(client, server HelloMessage) ([]byte, error) {
	if client.Phase == HelloOffer || server.Phase == HelloOffer || client.Role != 0 || server.Role != 1 || client.Version != server.Version || client.LocalNodeID != server.PeerNodeID || server.LocalNodeID != client.PeerNodeID || client.LocalBootID != server.PeerBootID || server.LocalBootID != client.PeerBootID || client.LocalGeneration != server.PeerGeneration || server.LocalGeneration != client.PeerGeneration || client.LocalNonce != server.PeerNonce || server.LocalNonce != client.PeerNonce {
		return nil, fmt.Errorf("%w: inconsistent HELLO transcript", ErrProtocol)
	}
	canonicalClient := client
	canonicalServer := server
	if client.LocalNodeID == server.LocalNodeID {
		return nil, fmt.Errorf("%w: local and peer node IDs are identical", ErrProtocol)
	}
	return encodeClientServerTranscript(canonicalClient, canonicalServer), nil
}

func SignHelloOffer(message HelloMessage, exporter []byte, signer crypto.Signer) ([]byte, error) {
	transcript, err := HelloOfferTranscript(message)
	if err != nil {
		return nil, err
	}
	return signVLink(signer, helloSignatureInput(transcript, exporter))
}

func VerifyHelloOffer(message HelloMessage, exporter []byte, publicKey crypto.PublicKey) error {
	transcript, err := HelloOfferTranscript(message)
	if err != nil {
		return err
	}
	return verifyVLink(publicKey, helloSignatureInput(transcript, exporter), message.Signature)
}

func SignHelloTranscript(client, server HelloMessage, exporter []byte, signer crypto.Signer) ([]byte, error) {
	transcript, err := HelloTranscript(client, server)
	if err != nil {
		return nil, err
	}
	return signVLink(signer, helloSignatureInput(transcript, exporter))
}

func VerifyHelloTranscript(client, server HelloMessage, exporter []byte, publicKey crypto.PublicKey) error {
	return VerifyHelloSignature(client, server, exporter, publicKey, server.Signature)
}

func VerifyHelloSignature(client, server HelloMessage, exporter []byte, publicKey crypto.PublicKey, signature []byte) error {
	transcript, err := HelloTranscript(client, server)
	if err != nil {
		return err
	}
	return verifyVLink(publicKey, helloSignatureInput(transcript, exporter), signature)
}

func HandshakeHello(ctx context.Context, conn *tls.Conn, config HelloConfig) (NodeID, error) {
	if conn == nil || config.Signer == nil || config.VerifyPeer == nil {
		return NodeID{}, errors.New("vlink: TLS connection, signer, and peer verifier are required")
	}
	if config.Generation == 0 || config.NodeID == (NodeID{}) || config.BootID == (BootID{}) || !validCapabilities(config.Capabilities) {
		return NodeID{}, fmt.Errorf("%w: invalid local HELLO configuration", ErrProtocol)
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		return NodeID{}, err
	}
	state := conn.ConnectionState()
	if state.Version != tls.VersionTLS13 || !state.HandshakeComplete || len(state.PeerCertificates) == 0 || len(state.VerifiedChains) == 0 {
		return NodeID{}, fmt.Errorf("%w: TLS 1.3 mutual authentication is required", ErrProtocol)
	}
	peerCert := state.PeerCertificates[0]
	peerID, err := NodeIDFromCertificate(peerCert)
	if err != nil {
		return NodeID{}, err
	}
	clientID, serverID := config.NodeID, peerID
	if !config.Client {
		clientID = peerID
		serverID = config.NodeID
	}
	exporterContext := helloExporterContext(clientID, serverID)
	exporter, err := state.ExportKeyingMaterial(helloLabel, exporterContext, 32)
	if err != nil {
		return NodeID{}, fmt.Errorf("vlink: derive HELLO exporter: %w", err)
	}
	nonceSource := config.NewNonce
	if nonceSource == nil {
		nonceSource = randomHelloNonce
	}
	nonce, err := nonceSource()
	if err != nil {
		return NodeID{}, err
	}
	if nonce == (HelloNonce{}) {
		return NodeID{}, errors.New("vlink: HELLO nonce must be nonzero")
	}
	localRole := uint8(1)
	peerRole := uint8(0)
	if config.Client {
		localRole, peerRole = 0, 1
	}
	local := HelloMessage{Version: ProtocolVersion, Role: localRole, LocalNodeID: config.NodeID, LocalBootID: config.BootID, LocalGeneration: config.Generation, LocalNonce: nonce, LocalCapabilities: append([]uint16(nil), config.Capabilities...)}
	if config.Client {
		local.Phase = HelloOffer
		local.Signature, err = SignHelloOffer(local, exporter, config.Signer)
		if err != nil {
			return NodeID{}, err
		}
		if err = sendHello(ctx, conn, local); err != nil {
			return NodeID{}, err
		}
		response, readErr := receiveHello(ctx, conn)
		if readErr != nil {
			return NodeID{}, readErr
		}
		server := response
		client := response
		client.Phase, client.Role = HelloResponse, 0
		client.LocalNodeID, client.LocalBootID, client.LocalGeneration, client.LocalNonce, client.LocalCapabilities = response.PeerNodeID, response.PeerBootID, response.PeerGeneration, response.PeerNonce, response.PeerCapabilities
		client.PeerNodeID, client.PeerBootID, client.PeerGeneration, client.PeerNonce, client.PeerCapabilities = response.LocalNodeID, response.LocalBootID, response.LocalGeneration, response.LocalNonce, response.LocalCapabilities
		if response.Phase != HelloResponse || response.Role != 1 || response.Version != ProtocolVersion || response.LocalNodeID != peerID || response.PeerNodeID != config.NodeID || response.PeerBootID != config.BootID || response.PeerGeneration != config.Generation || response.PeerNonce != nonce {
			return NodeID{}, fmt.Errorf("%w: invalid HELLO response identity or transcript", ErrProtocol)
		}
		if err = VerifyHelloTranscript(client, server, exporter, peerCert.PublicKey); err != nil {
			return NodeID{}, err
		}
		if err = config.VerifyPeer(ctx, peerCert, server); err != nil {
			return NodeID{}, err
		}
		local.Phase = HelloConfirm
		local.PeerNodeID, local.PeerBootID, local.PeerGeneration, local.PeerNonce = response.LocalNodeID, response.LocalBootID, response.LocalGeneration, response.LocalNonce
		local.PeerCapabilities = append([]uint16(nil), response.LocalCapabilities...)
		local.Signature, err = SignHelloTranscript(local, server, exporter, config.Signer)
		if err != nil {
			return NodeID{}, err
		}
		if err = sendHello(ctx, conn, local); err != nil {
			return NodeID{}, err
		}
		return peerID, nil
	}

	offer, err := receiveHello(ctx, conn)
	if err != nil {
		return NodeID{}, err
	}
	if offer.Phase != HelloOffer || offer.Role != peerRole || offer.Version != ProtocolVersion || offer.LocalNodeID != peerID || offer.PeerNodeID != (NodeID{}) || offer.PeerBootID != (BootID{}) || offer.PeerNonce != (HelloNonce{}) || offer.PeerGeneration != 0 || len(offer.PeerCapabilities) != 0 {
		return NodeID{}, fmt.Errorf("%w: invalid HELLO offer", ErrProtocol)
	}
	if err = VerifyHelloOffer(offer, exporter, peerCert.PublicKey); err != nil {
		return NodeID{}, err
	}
	response := HelloMessage{Phase: HelloResponse, Version: ProtocolVersion, Role: localRole, LocalNodeID: config.NodeID, LocalBootID: config.BootID, LocalGeneration: config.Generation, LocalNonce: nonce, PeerNodeID: offer.LocalNodeID, PeerBootID: offer.LocalBootID, PeerGeneration: offer.LocalGeneration, PeerNonce: offer.LocalNonce, LocalCapabilities: append([]uint16(nil), config.Capabilities...), PeerCapabilities: append([]uint16(nil), offer.LocalCapabilities...)}
	client := offer
	client.Phase = HelloResponse
	client.PeerNodeID, client.PeerBootID, client.PeerGeneration, client.PeerNonce = response.LocalNodeID, response.LocalBootID, response.LocalGeneration, response.LocalNonce
	client.PeerCapabilities = append([]uint16(nil), response.LocalCapabilities...)
	server := response
	response.Signature, err = SignHelloTranscript(client, server, exporter, config.Signer)
	if err != nil {
		return NodeID{}, err
	}
	if err = sendHello(ctx, conn, response); err != nil {
		return NodeID{}, err
	}
	confirm, err := receiveHello(ctx, conn)
	if err != nil {
		return NodeID{}, err
	}
	if confirm.Phase != HelloConfirm || confirm.Role != peerRole || confirm.Version != ProtocolVersion || confirm.LocalNodeID != offer.LocalNodeID || confirm.LocalBootID != offer.LocalBootID || confirm.LocalGeneration != offer.LocalGeneration || confirm.LocalNonce != offer.LocalNonce || confirm.PeerNodeID != config.NodeID || confirm.PeerBootID != config.BootID || confirm.PeerGeneration != config.Generation || confirm.PeerNonce != nonce || !equalCapabilities(confirm.LocalCapabilities, offer.LocalCapabilities) || !equalCapabilities(confirm.PeerCapabilities, config.Capabilities) {
		return NodeID{}, fmt.Errorf("%w: invalid HELLO confirmation", ErrProtocol)
	}
	if err = VerifyHelloSignature(confirm, response, exporter, peerCert.PublicKey, confirm.Signature); err != nil {
		return NodeID{}, err
	}
	if err = config.VerifyPeer(ctx, peerCert, confirm); err != nil {
		return NodeID{}, err
	}
	return peerID, nil
}

func sendHello(ctx context.Context, conn *tls.Conn, message HelloMessage) error {
	payload, err := MarshalHello(message)
	if err != nil {
		return err
	}
	stopDeadline := setContextDeadline(ctx, conn)
	defer stopDeadline()
	return WriteFrame(conn, Frame{Type: FrameHello, Payload: payload})
}

func receiveHello(ctx context.Context, conn *tls.Conn) (HelloMessage, error) {
	stopDeadline := setContextDeadline(ctx, conn)
	defer stopDeadline()
	frame, err := ReadFrame(conn)
	if err != nil {
		return HelloMessage{}, err
	}
	if frame.Type != FrameHello || frame.StreamID != 0 {
		return HelloMessage{}, fmt.Errorf("%w: expected HELLO frame", ErrProtocol)
	}
	return ParseHello(frame.Payload)
}

func setContextDeadline(ctx context.Context, conn *tls.Conn) func() {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	return func() {
		stop()
		_ = conn.SetDeadline(time.Time{})
	}
}

func encodeHelloTranscript(message HelloMessage) []byte {
	buf := make([]byte, 0, 128+2*len(message.LocalCapabilities))
	buf = appendU16(buf, message.Version)
	buf = append(buf, message.Role)
	buf = append(buf, message.LocalNodeID[:]...)
	buf = append(buf, message.LocalBootID[:]...)
	buf = appendU64(buf, message.LocalGeneration)
	buf = append(buf, message.LocalNonce[:]...)
	buf = appendCapabilities(buf, message.LocalCapabilities)
	return buf
}

func encodeClientServerTranscript(client, server HelloMessage) []byte {
	buf := make([]byte, 0, 256+2*(len(client.LocalCapabilities)+len(server.LocalCapabilities)))
	buf = appendU16(buf, client.Version)
	buf = append(buf, 0)
	buf = append(buf, client.LocalNodeID[:]...)
	buf = append(buf, client.LocalBootID[:]...)
	buf = appendU64(buf, client.LocalGeneration)
	buf = append(buf, client.LocalNonce[:]...)
	buf = appendCapabilities(buf, client.LocalCapabilities)
	buf = append(buf, 1)
	buf = append(buf, server.LocalNodeID[:]...)
	buf = append(buf, server.LocalBootID[:]...)
	buf = appendU64(buf, server.LocalGeneration)
	buf = append(buf, server.LocalNonce[:]...)
	buf = appendCapabilities(buf, server.LocalCapabilities)
	return buf
}

func helloSignatureInput(transcript, exporter []byte) []byte {
	input := make([]byte, 0, len("VORTEX-VLINK-HELLO-v1\x00")+len(transcript)+len(exporter))
	input = append(input, "VORTEX-VLINK-HELLO-v1\x00"...)
	input = append(input, transcript...)
	input = append(input, exporter...)
	return input
}

func helloExporterContext(clientID, serverID NodeID) []byte {
	var version [2]byte
	binary.BigEndian.PutUint16(version[:], ProtocolVersion)
	h := sha256.New()
	_, _ = h.Write(version[:])
	_, _ = h.Write(clientID[:])
	_, _ = h.Write(serverID[:])
	return h.Sum(nil)
}

func signVLink(signer crypto.Signer, message []byte) ([]byte, error) {
	if signer == nil {
		return nil, errors.New("vlink: HELLO signer is required")
	}
	switch key := signer.Public().(type) {
	case ed25519.PublicKey:
		return signer.Sign(rand.Reader, message, crypto.Hash(0))
	case *ecdsa.PublicKey:
		if key.Curve.Params().Name != "P-256" {
			return nil, errors.New("vlink: only ECDSA P-256 certificates are supported")
		}
		digest := sha256.Sum256(message)
		return signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	case *rsa.PublicKey:
		if key.N.BitLen() < 2048 {
			return nil, errors.New("vlink: RSA HELLO keys must be at least 2048 bits")
		}
		digest := sha256.Sum256(message)
		return signer.Sign(rand.Reader, digest[:], &rsa.PSSOptions{SaltLength: 32, Hash: crypto.SHA256})
	default:
		return nil, errors.New("vlink: unsupported HELLO signing key")
	}
}

func verifyVLink(publicKey crypto.PublicKey, message, signature []byte) error {
	var err error
	switch key := publicKey.(type) {
	case ed25519.PublicKey:
		if !ed25519.Verify(key, message, signature) {
			err = errors.New("invalid Ed25519 signature")
		}
	case *ecdsa.PublicKey:
		if key.Curve.Params().Name != "P-256" || !ecdsa.VerifyASN1(key, sha256Digest(message), signature) {
			err = errors.New("invalid ECDSA P-256 signature")
		}
	case *rsa.PublicKey:
		if key.N.BitLen() < 2048 {
			err = errors.New("RSA HELLO keys must be at least 2048 bits")
		} else {
			err = rsa.VerifyPSS(key, crypto.SHA256, sha256Digest(message), signature, &rsa.PSSOptions{SaltLength: 32, Hash: crypto.SHA256})
		}
	default:
		err = errors.New("unsupported HELLO verification key")
	}
	if err != nil {
		return fmt.Errorf("%w: HELLO signature verification failed: %v", ErrProtocol, err)
	}
	return nil
}

func sha256Digest(message []byte) []byte {
	digest := sha256.Sum256(message)
	return digest[:]
}

func validateHelloMessage(message HelloMessage, requireSignature bool) error {
	if message.Phase > HelloConfirm || message.Version != ProtocolVersion || message.Role > 1 || message.LocalNodeID == (NodeID{}) || message.LocalBootID == (BootID{}) || message.LocalGeneration == 0 || message.LocalNonce == (HelloNonce{}) || !validCapabilities(message.LocalCapabilities) || !validCapabilities(message.PeerCapabilities) || len(message.Signature) > 512 {
		return fmt.Errorf("%w: invalid HELLO fields", ErrProtocol)
	}
	if requireSignature && len(message.Signature) == 0 {
		return fmt.Errorf("%w: HELLO signature is required", ErrProtocol)
	}
	if message.Phase == HelloOffer && (message.PeerNodeID != (NodeID{}) || message.PeerBootID != (BootID{}) || message.PeerGeneration != 0 || message.PeerNonce != (HelloNonce{}) || len(message.PeerCapabilities) != 0) {
		return fmt.Errorf("%w: HELLO offer peer fields must be zero", ErrProtocol)
	}
	if message.Phase != HelloOffer && (message.PeerNodeID == (NodeID{}) || message.PeerBootID == (BootID{}) || message.PeerGeneration == 0 || message.PeerNonce == (HelloNonce{})) {
		return fmt.Errorf("%w: HELLO transcript peer fields are required", ErrProtocol)
	}
	return nil
}

func validCapabilities(capabilities []uint16) bool { return len(capabilities) <= 64 }

func putCapabilities(dst []byte, offset int, capabilities []uint16) int {
	binary.BigEndian.PutUint16(dst[offset:offset+2], uint16(len(capabilities)))
	offset += 2
	for _, capability := range capabilities {
		binary.BigEndian.PutUint16(dst[offset:offset+2], capability)
		offset += 2
	}
	return offset
}

func appendCapabilities(dst []byte, capabilities []uint16) []byte {
	dst = appendU16(dst, uint16(len(capabilities)))
	for _, capability := range capabilities {
		dst = appendU16(dst, capability)
	}
	return dst
}

func readCapabilities(payload []byte, offset int) ([]uint16, int, error) {
	if len(payload)-offset < 2 {
		return nil, offset, fmt.Errorf("%w: truncated HELLO capability count", ErrProtocol)
	}
	count := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	if count > 64 || len(payload)-offset < count*2 {
		return nil, offset, fmt.Errorf("%w: invalid HELLO capability count", ErrProtocol)
	}
	capabilities := make([]uint16, count)
	for i := range capabilities {
		capabilities[i] = binary.BigEndian.Uint16(payload[offset : offset+2])
		offset += 2
	}
	return capabilities, offset, nil
}

func appendU16(dst []byte, value uint16) []byte {
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	return append(dst, encoded[:]...)
}

func appendU64(dst []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(dst, encoded[:]...)
}

func randomHelloNonce() (HelloNonce, error) {
	var nonce HelloNonce
	_, err := io.ReadFull(rand.Reader, nonce[:])
	return nonce, err
}

func equalCapabilities(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

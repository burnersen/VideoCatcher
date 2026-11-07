// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Checks the OpenPGP signature that yt-dlp puts on the checksum list of every
// release (SHA2-256SUMS.sig). This is the one way VideoCatcher accepts a new
// yt-dlp without pinning its checksum in advance: the list must be signed by
// the yt-dlp release key, and that key is built into the program.
//
// Only what yt-dlp uses is supported, on purpose: a version 4 RSA key and a
// version 4 binary signature with SHA-256 or SHA-512 (RFC 4880). Anything
// else is refused. The standard library does the cryptography; this file
// only reads the packet format, so no OpenPGP library is needed (08 S11).

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha1" // the v4 fingerprint is defined as SHA-1 (RFC 4880 12.2)
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"
)

// OpenPGP packet tags and algorithm numbers (RFC 4880 4.3, 9.1, 9.4).
const (
	pgpTagSignature = 2
	pgpTagPublicKey = 6
	pgpAlgoRSA      = 1
	pgpHashSHA256   = 8
	pgpHashSHA512   = 10
	pgpSigBinary    = 0x00
	pgpSubIssuerKey = 16
	pgpSubIssuerFpr = 33
	pgpMaxPacket    = 1 << 20 // a key or a signature is a few KB; more is not ours
	pgpMinRSABits   = 3072    // the yt-dlp key has 4096; refuse anything weak
)

// pgpKey is a version 4 RSA public key.
type pgpKey struct {
	Fingerprint string // 40 hex digits, upper case
	Pub         *rsa.PublicKey
}

// parseArmoredKey reads the first public key of an ASCII armored key block.
func parseArmoredKey(armored []byte) (pgpKey, error) {
	data, err := dearmor(armored, "PGP PUBLIC KEY BLOCK")
	if err != nil {
		return pgpKey{}, err
	}
	for len(data) > 0 {
		tag, body, rest, err := nextPacket(data)
		if err != nil {
			return pgpKey{}, fmt.Errorf("read key: %w", err)
		}
		if tag == pgpTagPublicKey {
			return parsePublicKey(body)
		}
		data = rest
	}
	return pgpKey{}, errors.New("read key: no public key packet")
}

func parsePublicKey(body []byte) (pgpKey, error) {
	if len(body) < 6 || body[0] != 4 {
		return pgpKey{}, errors.New("read key: not a version 4 key")
	}
	if body[5] != pgpAlgoRSA {
		return pgpKey{}, fmt.Errorf("read key: algorithm %d, only RSA is supported", body[5])
	}
	n, rest, err := readMPI(body[6:])
	if err != nil {
		return pgpKey{}, fmt.Errorf("read key modulus: %w", err)
	}
	e, _, err := readMPI(rest)
	if err != nil {
		return pgpKey{}, fmt.Errorf("read key exponent: %w", err)
	}
	if n.BitLen() < pgpMinRSABits {
		return pgpKey{}, fmt.Errorf("read key: RSA key has %d bits, at least %d needed", n.BitLen(), pgpMinRSABits)
	}
	if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
		return pgpKey{}, errors.New("read key: unusual RSA exponent")
	}
	h := sha1.New()
	h.Write([]byte{0x99, byte(len(body) >> 8), byte(len(body))})
	h.Write(body)
	return pgpKey{
		Fingerprint: strings.ToUpper(hex.EncodeToString(h.Sum(nil))),
		Pub:         &rsa.PublicKey{N: n, E: int(e.Int64())},
	}, nil
}

// verifyDetached checks that sig (a binary .sig file) is a valid signature
// by key over data. Any problem is an error; nil means valid.
func verifyDetached(key pgpKey, data, sig []byte) error {
	tag, body, _, err := nextPacket(sig)
	if err != nil {
		return fmt.Errorf("read signature: %w", err)
	}
	if tag != pgpTagSignature {
		return fmt.Errorf("read signature: packet %d is not a signature", tag)
	}
	if len(body) < 6 || body[0] != 4 {
		return errors.New("read signature: not a version 4 signature")
	}
	sigType, pubAlgo, hashAlgo := body[1], body[2], body[3]
	if sigType != pgpSigBinary {
		return fmt.Errorf("read signature: type 0x%02x, expected a signature of a file", sigType)
	}
	if pubAlgo != pgpAlgoRSA {
		return fmt.Errorf("read signature: algorithm %d, expected RSA", pubAlgo)
	}
	var h hash.Hash
	var ch crypto.Hash
	switch hashAlgo {
	case pgpHashSHA256:
		h, ch = sha256.New(), crypto.SHA256
	case pgpHashSHA512:
		h, ch = sha512.New(), crypto.SHA512
	default:
		return fmt.Errorf("read signature: hash algorithm %d is not supported", hashAlgo)
	}

	hashedLen := int(binary.BigEndian.Uint16(body[4:6]))
	if len(body) < 6+hashedLen+2 {
		return errors.New("read signature: too short")
	}
	hashed := body[6 : 6+hashedLen]
	unhashedLen := int(binary.BigEndian.Uint16(body[6+hashedLen:]))
	pos := 6 + hashedLen + 2
	if len(body) < pos+unhashedLen+2 {
		return errors.New("read signature: too short")
	}
	unhashed := body[pos : pos+unhashedLen]
	pos += unhashedLen
	left16 := body[pos : pos+2]
	s, _, err := readMPI(body[pos+2:])
	if err != nil {
		return fmt.Errorf("read signature value: %w", err)
	}

	// The signature must name our key. The fingerprint is preferred; old
	// signers only give the key ID (the last 16 hex digits).
	issuer, err := signatureIssuer(hashed, unhashed)
	if err != nil {
		return err
	}
	if issuer != "" && !strings.HasSuffix(key.Fingerprint, issuer) {
		return fmt.Errorf("signature is by key %s, not by %s", issuer, key.Fingerprint)
	}

	// RFC 4880 5.2.4: data, then the hashed part of the packet, then a
	// trailer with its length.
	h.Write(data)
	h.Write(body[:6+hashedLen])
	trailer := []byte{4, 0xff, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(trailer[2:], uint32(6+hashedLen))
	h.Write(trailer)
	digest := h.Sum(nil)
	if !bytes.Equal(digest[:2], left16) {
		return errors.New("signature does not match the file")
	}
	// rsa.VerifyPKCS1v15 wants the signature exactly as long as the modulus.
	k := (key.Pub.N.BitLen() + 7) / 8
	raw := s.Bytes()
	if len(raw) > k {
		return errors.New("read signature: value larger than the key")
	}
	padded := make([]byte, k)
	copy(padded[k-len(raw):], raw)
	if err := rsa.VerifyPKCS1v15(key.Pub, ch, digest, padded); err != nil {
		return fmt.Errorf("signature does not match the file: %w", err)
	}
	return nil
}

// signatureIssuer returns the issuer as upper case hex: a fingerprint (40
// digits) or a key ID (16 digits), or "" if the signature names none. An
// unknown subpacket marked critical makes the signature invalid (RFC 4880
// 5.2.3.1): it could change the meaning in a way this code would not see.
func signatureIssuer(hashed, unhashed []byte) (string, error) {
	var fpr, keyID string
	for i, area := range [][]byte{hashed, unhashed} {
		for len(area) > 0 {
			typ, data, rest, err := nextSubpacket(area)
			if err != nil {
				return "", fmt.Errorf("read signature: %w", err)
			}
			critical := typ&0x80 != 0
			typ &= 0x7f
			switch {
			case typ == pgpSubIssuerFpr && len(data) == 21 && data[0] == 4:
				fpr = strings.ToUpper(hex.EncodeToString(data[1:]))
			case typ == pgpSubIssuerKey && len(data) == 8:
				keyID = strings.ToUpper(hex.EncodeToString(data))
			case typ == 2 || typ == 3: // creation time, expiration time: no effect here
			case critical && i == 0:
				return "", fmt.Errorf("read signature: unknown critical subpacket %d", typ)
			}
			area = rest
		}
	}
	if fpr != "" {
		return fpr, nil
	}
	return keyID, nil
}

func nextSubpacket(b []byte) (typ byte, data, rest []byte, err error) {
	var n, hdr int
	switch {
	case len(b) == 0:
		return 0, nil, nil, errors.New("empty subpacket")
	case b[0] < 192:
		n, hdr = int(b[0]), 1
	case b[0] < 255:
		if len(b) < 2 {
			return 0, nil, nil, errors.New("short subpacket length")
		}
		n, hdr = (int(b[0])-192)<<8+int(b[1])+192, 2
	default:
		if len(b) < 5 {
			return 0, nil, nil, errors.New("short subpacket length")
		}
		n, hdr = int(binary.BigEndian.Uint32(b[1:5])), 5
	}
	if n < 1 || n > len(b)-hdr {
		return 0, nil, nil, errors.New("subpacket longer than the signature")
	}
	return b[hdr], b[hdr+1 : hdr+n], b[hdr+n:], nil
}

// nextPacket splits off one packet (old or new header format, RFC 4880 4.2).
// Partial lengths are refused: keys and signatures never use them.
func nextPacket(b []byte) (tag int, body, rest []byte, err error) {
	if len(b) < 2 || b[0]&0x80 == 0 {
		return 0, nil, nil, errors.New("not an OpenPGP packet")
	}
	var n, hdr int
	if b[0]&0x40 == 0 { // old format
		tag = int(b[0]>>2) & 0x0f
		switch b[0] & 3 {
		case 0:
			n, hdr = int(b[1]), 2
		case 1:
			if len(b) < 3 {
				return 0, nil, nil, errors.New("short packet header")
			}
			n, hdr = int(binary.BigEndian.Uint16(b[1:3])), 3
		case 2:
			if len(b) < 5 {
				return 0, nil, nil, errors.New("short packet header")
			}
			n, hdr = int(binary.BigEndian.Uint32(b[1:5])), 5
		default:
			return 0, nil, nil, errors.New("packet of unknown length")
		}
	} else { // new format
		tag = int(b[0] & 0x3f)
		switch o := b[1]; {
		case o < 192:
			n, hdr = int(o), 2
		case o < 224:
			if len(b) < 3 {
				return 0, nil, nil, errors.New("short packet header")
			}
			n, hdr = (int(o)-192)<<8+int(b[2])+192, 3
		case o == 255:
			if len(b) < 6 {
				return 0, nil, nil, errors.New("short packet header")
			}
			n, hdr = int(binary.BigEndian.Uint32(b[2:6])), 6
		default:
			return 0, nil, nil, errors.New("partial packet lengths are not supported")
		}
	}
	if n < 0 || n > pgpMaxPacket || n > len(b)-hdr {
		return 0, nil, nil, errors.New("packet longer than the data")
	}
	return tag, b[hdr : hdr+n], b[hdr+n:], nil
}

// readMPI reads a multiprecision integer: 2 bytes bit count, then the bytes.
func readMPI(b []byte) (*big.Int, []byte, error) {
	if len(b) < 2 {
		return nil, nil, errors.New("short number")
	}
	bits := int(binary.BigEndian.Uint16(b))
	n := (bits + 7) / 8
	if len(b) < 2+n {
		return nil, nil, errors.New("short number")
	}
	return new(big.Int).SetBytes(b[2 : 2+n]), b[2+n:], nil
}

// dearmor decodes an ASCII armored block of the given kind and checks its
// CRC-24 if there is one.
func dearmor(armored []byte, kind string) ([]byte, error) {
	text := strings.ReplaceAll(string(armored), "\r\n", "\n")
	begin, end := "-----BEGIN "+kind+"-----", "-----END "+kind+"-----"
	_, after, ok := strings.Cut(text, begin)
	if !ok {
		return nil, fmt.Errorf("no %q block", kind)
	}
	inner, _, ok := strings.Cut(after, end)
	if !ok {
		return nil, fmt.Errorf("%q block is not closed", kind)
	}
	// Armor headers end at the first empty line.
	if _, body, found := strings.Cut(inner, "\n\n"); found {
		inner = body
	}
	var b64, crc strings.Builder
	for _, line := range strings.Split(inner, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "="):
			crc.WriteString(line[1:])
		default:
			b64.WriteString(line)
		}
	}
	data, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		return nil, fmt.Errorf("decode %q block: %w", kind, err)
	}
	if crc.Len() > 0 {
		want, err := base64.StdEncoding.DecodeString(crc.String())
		if err != nil || len(want) != 3 {
			return nil, fmt.Errorf("%q block has a broken checksum line", kind)
		}
		got := crc24(data)
		if !bytes.Equal(want, []byte{byte(got >> 16), byte(got >> 8), byte(got)}) {
			return nil, fmt.Errorf("%q block is damaged (checksum)", kind)
		}
	}
	return data, nil
}

// crc24 is the checksum of armored data (RFC 4880 6.1).
func crc24(data []byte) uint32 {
	crc := uint32(0xB704CE)
	for _, b := range data {
		crc ^= uint32(b) << 16
		for i := 0; i < 8; i++ {
			crc <<= 1
			if crc&0x1000000 != 0 {
				crc ^= 0x1864CFB
			}
		}
	}
	return crc & 0xFFFFFF
}

// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

// The real thing: the checksum list of yt-dlp 2026.10.26 with its signature,
// checked against the key that is built into the program.
func TestYtDlpSignatureOfARealRelease(t *testing.T) {
	key, err := ytdlpSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	sums := readTestdata(t, "SHA2-256SUMS")
	sig := readTestdata(t, "SHA2-256SUMS.sig")
	if err := verifyDetached(key, sums, sig); err != nil {
		t.Fatalf("real release does not verify: %v", err)
	}

	// One changed checksum must break it.
	bad := []byte(strings.Replace(string(sums), "f5389d36", "f5389d37", 1))
	if err := verifyDetached(key, bad, sig); err == nil {
		t.Fatal("a changed checksum list was accepted")
	}
}

func TestBuiltInKeyHasThePinnedFingerprint(t *testing.T) {
	key, err := ytdlpSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if key.Fingerprint != ytdlpKeyFingerprint || key.Pub.N.BitLen() != 4096 {
		t.Fatalf("fingerprint %s, %d bits", key.Fingerprint, key.Pub.N.BitLen())
	}
}

// A valid signature by another key is refused, even if it is correct in
// itself: only the yt-dlp key counts.
func TestSignatureByAnotherKeyIsRefused(t *testing.T) {
	data := []byte("abc  yt-dlp.exe\n")
	other := newTestKey(t)
	sig := signForTest(t, other, data, true)
	if err := verifyDetached(other.key, data, sig); err != nil {
		t.Fatalf("the test signer itself is broken: %v", err)
	}
	ytdlp, err := ytdlpSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyDetached(ytdlp, data, sig); err == nil {
		t.Fatal("signature by a foreign key accepted")
	}
	// Without an issuer subpacket the RSA check itself must refuse it.
	if err := verifyDetached(ytdlp, data, signForTest(t, other, data, false)); err == nil {
		t.Fatal("signature without issuer by a foreign key accepted")
	}
}

func TestBrokenSignaturesAreErrors(t *testing.T) {
	key, err := ytdlpSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	sums := readTestdata(t, "SHA2-256SUMS")
	sig := readTestdata(t, "SHA2-256SUMS.sig")
	for name, s := range map[string][]byte{
		"empty":     nil,
		"cut":       sig[:40],
		"not pgp":   []byte("hello world, this is no signature"),
		"last byte": append(append([]byte{}, sig[:len(sig)-1]...), sig[len(sig)-1]^1),
	} {
		if err := verifyDetached(key, sums, s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDearmorNoticesDamage(t *testing.T) {
	armored := string(ytdlpKeyArmored)
	// Change one character in the body, keep the checksum line.
	i := strings.Index(armored, "\n\n") + 10
	damaged := armored[:i] + string(rune(armored[i]^1)) + armored[i+1:]
	if _, err := parseArmoredKey([]byte(damaged)); err == nil {
		t.Fatal("damaged key block accepted")
	}
}

type testKey struct {
	priv *rsa.PrivateKey
	key  pgpKey
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{4, 0, 0, 0, 0, pgpAlgoRSA}
	body = append(body, mpi(priv.N.Bytes())...)
	body = append(body, mpi([]byte{1, 0, 1})...)
	k, err := parsePublicKey(body)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, key: k}
}

// signForTest builds a version 4 binary signature packet with SHA-512, the
// way gpg does for yt-dlp.
func signForTest(t *testing.T, k testKey, data []byte, withIssuer bool) []byte {
	t.Helper()
	hashed := []byte{5, 2, 0, 0, 0, 1} // creation time subpacket: length 5, type 2, 4 bytes
	if withIssuer {
		fpr := []byte{22, pgpSubIssuerFpr, 4}
		for i := 0; i < 20; i++ {
			var b byte
			for j := 0; j < 2; j++ {
				c := k.key.Fingerprint[2*i+j]
				v := c - '0'
				if c >= 'A' {
					v = c - 'A' + 10
				}
				b = b<<4 | v
			}
			fpr = append(fpr, b)
		}
		hashed = append(hashed, fpr...)
	}
	head := []byte{4, pgpSigBinary, pgpAlgoRSA, pgpHashSHA512, 0, 0}
	binary.BigEndian.PutUint16(head[4:], uint16(len(hashed)))
	head = append(head, hashed...)
	h := sha512.New()
	h.Write(data)
	h.Write(head)
	trailer := []byte{4, 0xff, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(trailer[2:], uint32(len(head)))
	h.Write(trailer)
	digest := h.Sum(nil)
	s, err := rsa.SignPKCS1v15(rand.Reader, k.priv, crypto.SHA512, digest)
	if err != nil {
		t.Fatal(err)
	}
	body := append(head, 0, 0) // no unhashed subpackets
	body = append(body, digest[:2]...)
	body = append(body, mpi(s)...)
	pkt := []byte{0x80 | pgpTagSignature<<2 | 1, 0, 0}
	binary.BigEndian.PutUint16(pkt[1:], uint16(len(body)))
	return append(pkt, body...)
}

func mpi(b []byte) []byte {
	for len(b) > 0 && b[0] == 0 {
		b = b[1:]
	}
	bits := len(b) * 8
	if len(b) > 0 {
		for m := byte(0x80); b[0]&m == 0; m >>= 1 {
			bits--
		}
	}
	return append([]byte{byte(bits >> 8), byte(bits)}, b...)
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

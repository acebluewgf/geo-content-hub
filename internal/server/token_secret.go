package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// tokenKey derives a stable 32-byte key from SESSION_SECRET. Rotating the
// secret intentionally makes previously stored tokens unreadable (they still
// authenticate by hash); clients should be rotated after a secret change.
func (s *Server) tokenKey() [32]byte {
	return sha256.Sum256([]byte("contenthub-token-v1:" + s.cfg.SessionSecret))
}

func (s *Server) encryptToken(plain string) ([]byte, error) {
	key := s.tokenKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	enc := make([]byte, base64.RawURLEncoding.EncodedLen(len(out)))
	base64.RawURLEncoding.Encode(enc, out)
	return enc, nil
}

func (s *Server) decryptToken(enc []byte) (string, error) {
	raw := make([]byte, base64.RawURLEncoding.DecodedLen(len(enc)))
	n, err := base64.RawURLEncoding.Decode(raw, enc)
	if err != nil {
		return "", err
	}
	raw = raw[:n]
	key := s.tokenKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("cipher too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Package secure 提供私钥的 AES-256-GCM 加密存储。
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// Cipher 使用从主密钥派生的 AES-256-GCM 对私钥做静态加密。
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher 由 ULP_MASTER_KEY 派生 AES-256 密钥（SHA-256）。
func NewCipher(master []byte) (*Cipher, error) {
	if len(master) == 0 {
		return nil, fmt.Errorf("主密钥为空")
	}
	key := sha256.Sum256(master)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal 加密明文，输出 base64(nonce|ciphertext)。nonce 随机生成。
func (c *Cipher) Seal(plain []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open 解密 Seal 产生的密文。
func (c *Cipher) Open(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("私钥密文不是合法 base64: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns+1 {
		return nil, fmt.Errorf("私钥密文长度非法")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("私钥解密失败（主密钥可能已更换）: %w", err)
	}
	return plain, nil
}

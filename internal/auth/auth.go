// Package auth 实现管理员密码哈希、JWT 签发与校验、密码强度校验。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Issuer 是 JWT 的签发方标识。
const Issuer = "unified-license-platform"

// Claims 是平台会话令牌载荷。
type Claims struct {
	Sub        string `json:"sub"`
	Username   string `json:"username"`
	MustChange bool   `json:"must_change"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
	Issuer     string `json:"iss"`
}

// HashPassword 使用 bcrypt 生成密码哈希。
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword 校验明文密码。
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ValidatePassword 校验新密码强度：10~128 位，且同时包含字母与数字。
func ValidatePassword(password string) error {
	if len(password) < 10 || len(password) > 128 {
		return fmt.Errorf("密码长度需为 10~128 位")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("密码需同时包含字母与数字")
	}
	return nil
}

// IssueToken 签发 HS256 JWT。
func IssueToken(secret []byte, adminID, username string, mustChange bool, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		Sub:        adminID,
		Username:   username,
		MustChange: mustChange,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(ttl).Unix(),
		Issuer:     Issuer,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := header + "." + body
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig, nil
}

// ErrInvalidToken 表示令牌非法或已过期。
var ErrInvalidToken = errors.New("令牌无效或已过期")

// ParseToken 校验签名与有效期，返回 Claims。
func ParseToken(secret []byte, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	var head struct {
		Alg string `json:"alg"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &head) != nil || head.Alg != "HS256" {
		return nil, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(want, got) != 1 {
		return nil, ErrInvalidToken
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(pb, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if claims.Issuer != Issuer || claims.Sub == "" {
		return nil, ErrInvalidToken
	}
	if time.Now().UTC().Unix() >= claims.ExpiresAt {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}

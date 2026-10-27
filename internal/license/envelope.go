package license

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
)

// GenerateKeyPair 生成 RSA 密钥对（默认 2048 位）。
func GenerateKeyPair(bits int) (*rsa.PrivateKey, error) {
	if bits < 2048 {
		bits = 2048
	}
	return rsa.GenerateKey(rand.Reader, bits)
}

// PublicKeyPEM 输出 SPKI（PKIX）格式公钥 PEM。
func PublicKeyPEM(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// PrivateKeyPEM 输出 PKCS#8 格式私钥 PEM（仅平台内部使用，绝不落盘明文）。
func PrivateKeyPEM(priv *rsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// ParsePublicKeyPEM 解析公钥，同时支持 SPKI（BEGIN PUBLIC KEY）与 PKCS#1（BEGIN RSA PUBLIC KEY）。
func ParsePublicKeyPEM(pemText string) (*rsa.PublicKey, error) {
	pemText = strings.TrimSpace(pemText)
	if pemText == "" {
		return nil, ErrNoPublicKey
	}
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, newError(CodeNoPublicKey, "公钥不是合法的 PEM 格式")
	}
	var pub any
	var err error
	switch block.Type {
	case "PUBLIC KEY":
		pub, err = x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		pub, err = x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, newError(CodeNoPublicKey, "不支持的公钥 PEM 类型: "+block.Type)
	}
	if err != nil {
		return nil, newError(CodeNoPublicKey, "公钥解析失败: "+err.Error())
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, newError(CodeNoPublicKey, "公钥不是 RSA 类型")
	}
	if rsaPub.N.BitLen() < 2048 {
		return nil, newError(CodeNoPublicKey, "RSA 公钥长度低于 2048 位")
	}
	return rsaPub, nil
}

// ParsePrivateKeyPEM 解析私钥，支持 PKCS#8 与 PKCS#1。
func ParsePrivateKeyPEM(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return nil, fmt.Errorf("私钥不是合法的 PEM 格式")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		priv, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("私钥不是 RSA 类型")
		}
		return priv, nil
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("不支持的私钥 PEM 类型: %s", block.Type)
	}
}

// Fingerprint 返回公钥的 SHA-256 指纹（小写 hex）。
func Fingerprint(pub *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// FingerprintPEM 返回公钥 PEM 的指纹。
func FingerprintPEM(pemText string) string {
	pub, err := ParsePublicKeyPEM(pemText)
	if err != nil {
		return ""
	}
	return Fingerprint(pub)
}

// HasPlaceholder 检测公钥是否仍是占位符（未替换的平台内置公钥）。
func HasPlaceholder(pemText string) bool {
	up := strings.ToUpper(pemText)
	markers := []string{"REPLACE_WITH", "YOUR_PUBLIC_KEY", "TODO", "PLACEHOLDER", "CHANGEME"}
	for _, m := range markers {
		if strings.Contains(up, m) {
			return true
		}
	}
	return false
}

// EncodePayload 以固定字段顺序序列化载荷（不含缩进），返回原始字节。
// 签发端与验签端都以这些字节为签名对象。
func EncodePayload(p *Payload) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // 避免 < > & 被转义，保证可读性
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// SignBytes 对原始字节做 RSA PKCS#1 v1.5 + SHA-256 签名。
func SignBytes(payloadBytes []byte, priv *rsa.PrivateKey) ([]byte, error) {
	sum := sha256.Sum256(payloadBytes)
	return rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
}

// VerifyBytes 对原始字节验签。
func VerifyBytes(payloadBytes, signature []byte, pub *rsa.PublicKey) error {
	sum := sha256.Sum256(payloadBytes)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], signature); err != nil {
		return ErrBadSignature
	}
	return nil
}

// SignPayload 生成完整的 .lic 文件内容。
func SignPayload(p *Payload, priv *rsa.PrivateKey) ([]byte, error) {
	if p.SchemaVersion == 0 {
		p.SchemaVersion = SchemaVersion
	}
	if p.Platform == "" {
		p.Platform = PlatformName
	}
	if p.MachineCode == "" {
		p.MachineCode = MachineCodeWildcard
	}
	payloadBytes, err := EncodePayload(p)
	if err != nil {
		return nil, err
	}
	sig, err := SignBytes(payloadBytes, priv)
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Data: base64.StdEncoding.EncodeToString(payloadBytes),
		Sign: base64.StdEncoding.EncodeToString(sig),
	}
	return json.MarshalIndent(env, "", "  ")
}

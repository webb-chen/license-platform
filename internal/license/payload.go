// Package license 实现统一授权文件（ULP License Format v1）的签发与验签内核。
// 该包只依赖 Go 标准库，可被任意 Go 业务项目直接移植。
package license

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	// PlatformName 是授权文件中的签发方标识，恒为 unified-license-platform。
	PlatformName = "unified-license-platform"
	// SchemaVersion 是当前授权载荷格式版本。
	SchemaVersion = 1
	// MachineCodeWildcard 表示通用授权（不绑定机器）。
	MachineCodeWildcard = "*"
)

// 统一状态码词表。所有接入项目必须原样使用这些字符串。
const (
	CodeOK                  = "ok"
	CodeGrace               = "license_grace"
	CodeDisabled            = "license_disabled"
	CodeNotInit             = "license_not_init"
	CodeMissing             = "license_missing"
	CodeMalformed           = "license_malformed"
	CodeBadSignature        = "license_bad_signature"
	CodeExpired             = "license_expired"
	CodeClockRollback       = "license_clock_rollback"
	CodeMachineMismatch     = "license_machine_mismatch"
	CodeProductMismatch     = "license_product_mismatch"
	CodeNoPublicKey         = "license_no_public_key"
	CodePasswordChangeNeeded = "password_change_required"
)

// IsLicenseCode 判断状态码是否属于授权类错误（供前端拦截器使用）。
func IsLicenseCode(code string) bool {
	switch code {
	case CodeGrace, CodeNotInit, CodeMissing, CodeMalformed, CodeBadSignature,
		CodeExpired, CodeClockRollback, CodeMachineMismatch, CodeProductMismatch,
		CodeNoPublicKey:
		return true
	default:
		return false
	}
}

// ValidCode 判断状态码是否代表授权有效。
func ValidCode(code string) bool {
	return code == CodeOK || code == CodeGrace || code == CodeDisabled
}

// Time 是 RFC3339 UTC 秒级时间。序列化时统一为 2026-10-27T11:26:00Z 形式，
// 保证同一份载荷每次序列化产生完全一致的字节。
type Time struct {
	time.Time
}

// NewTime 构造一个规范化（UTC、秒级）的时间。
func NewTime(t time.Time) Time {
	return Time{Time: t.UTC().Truncate(time.Second)}
}

// Now 返回当前 UTC 时间。
func Now() Time {
	return NewTime(time.Now())
}

func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(time.RFC3339))
}

func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("时间字段必须是 RFC3339 字符串: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("时间字段格式非法: %w", err)
	}
	t.Time = parsed.UTC()
	return nil
}

// String 返回 RFC3339 UTC 字符串。
func (t Time) String() string {
	return t.UTC().Format(time.RFC3339)
}

// Payload 是统一授权载荷（superset schema）。
// 字段顺序即序列化顺序，必须与《统一授权文件格式规范》§3.1 表格保持一致。
type Payload struct {
	SchemaVersion   int            `json:"schema_version"`
	LicenseID       string         `json:"license_id"`
	Platform        string         `json:"platform"`
	Product         string         `json:"product"`
	ProductDisplay  string         `json:"product_display"`
	Customer        string         `json:"customer"`
	CustomerContact string         `json:"customer_contact"`
	IssuedAt        Time           `json:"issued_at"`
	ExpireTime      *Time          `json:"expire_time"`
	GraceDays       int            `json:"grace_days"`
	MachineCode     string         `json:"machine_code"`
	InstanceID      string         `json:"instance_id"`
	Features        map[string]any `json:"features"`
	IssuedBy        string         `json:"issued_by"`
	Note            string         `json:"note"`
}

// IsPermanent 判断是否为永久授权。
func (p *Payload) IsPermanent() bool {
	return p.ExpireTime == nil
}

// StringOrEmpty 返回 RFC3339 字符串；永久授权返回空串。
func (t *Time) StringOrEmpty() string {
	if t == nil {
		return ""
	}
	return t.String()
}

// Envelope 是 .lic 文件的外层信封。
type Envelope struct {
	Data string `json:"data"` // base64(payload 原始字节)
	Sign string `json:"sign"` // base64(RSA PKCS#1 v1.5 SHA-256 签名)
}

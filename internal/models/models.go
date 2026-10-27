// Package models 定义平台的数据模型与 API 传输结构。
package models

// 项目状态。
const (
	ProjectActive   = "active"
	ProjectDisabled = "disabled"
)

// 授权记录状态（存储值）。展示状态由到期时间推导，见 EffectiveStatus。
const (
	LicenseActive     = "active"
	LicenseRevoked    = "revoked"
	LicenseSuperseded = "superseded"
)

// 展示状态（有效 / 宽限期 / 已过期）。
const (
	EffectiveActive  = "active"
	EffectiveGrace   = "grace"
	EffectiveExpired = "expired"
)

// Project 是注册到平台的目标项目。
type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Product       string `json:"product"`
	Status        string `json:"status"`
	KeyID         string `json:"key_id"`
	InstanceHint  string `json:"instance_hint"`
	Remark        string `json:"remark"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	PublicPEM     string `json:"-"`
	KeyFingerprint string `json:"key_fingerprint"`
	KeyCreatedAt  string `json:"key_created_at"`
	LicenseCount  int    `json:"license_count"`
}

// Keypair 是项目密钥对。私钥仅以密文形式存在。
type Keypair struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	Product       string `json:"product,omitempty"`
	ProjectName   string `json:"project_name,omitempty"`
	PublicPEM     string `json:"-"`
	PrivateCipher string `json:"-"`
	Fingerprint   string `json:"fingerprint"`
	CreatedAt     string `json:"created_at"`
	DisabledAt    string `json:"disabled_at,omitempty"`
}

// License 是一条授权签发记录。
type License struct {
	ID              string `json:"id"`
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name,omitempty"`
	Product         string `json:"product,omitempty"`
	Customer        string `json:"customer"`
	CustomerContact string `json:"customer_contact"`
	IssuedAt        string `json:"issued_at"`
	ExpireTime      string `json:"expire_time"` // 空串表示永久
	GraceDays       int    `json:"grace_days"`
	MachineCode     string `json:"machine_code"`
	InstanceID      string `json:"instance_id"`
	Features        string `json:"features"` // JSON 文本
	Note            string `json:"note"`
	IssuedBy        string `json:"issued_by"`
	Status          string `json:"status"`           // 存储状态
	EffectiveStatus string `json:"effective_status"` // 展示状态
	RevokedAt       string `json:"revoked_at"`
	RenewFrom       string `json:"renew_from"`
	DownloadCount   int    `json:"download_count"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	FilePath        string `json:"-"`
}

// Admin 是平台管理员。
type Admin struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	PasswordHash       string `json:"-"`
	MustChangePassword bool   `json:"must_change_password"`
	CreatedAt          string `json:"created_at"`
}

// AuditLog 是敏感操作审计记录。
type AuditLog struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	Operator  string `json:"operator"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

// Page 是通用分页结果。
type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"size"`
}

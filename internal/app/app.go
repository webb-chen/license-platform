// Package app 实现统一授权平台的业务逻辑：项目、密钥、签发、吊销、续期与审计。
package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"unified-license-platform/internal/auth"
	"unified-license-platform/internal/config"
	"unified-license-platform/internal/license"
	"unified-license-platform/internal/models"
	"unified-license-platform/internal/secure"
	"unified-license-platform/internal/store"
)

// Error 是带 HTTP 语义的业务错误。
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func badRequest(msg string) *Error   { return &Error{Status: 400, Code: "bad_request", Message: msg} }
func notFound(msg string) *Error     { return &Error{Status: 404, Code: "not_found", Message: msg} }
func conflict(msg string) *Error     { return &Error{Status: 409, Code: "conflict", Message: msg} }
func unauthorized(msg string) *Error { return &Error{Status: 401, Code: "unauthorized", Message: msg} }

var (
	slugPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
	productPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)
	machinePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)
)

// App 聚合平台依赖。
type App struct {
	cfg    *config.Config
	store  *store.Store
	cipher *secure.Cipher
}

// New 打开数据库并完成管理员初始化。
func New(cfg *config.Config) (*App, error) {
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	cipher, err := secure.NewCipher(cfg.MasterKey)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("初始化私钥加密失败: %w", err)
	}
	a := &App{cfg: cfg, store: st, cipher: cipher}
	if err := a.bootstrapAdmin(); err != nil {
		_ = st.Close()
		return nil, err
	}
	return a, nil
}

// Close 释放资源。
func (a *App) Close() error { return a.store.Close() }

// Store 暴露存储层（仅测试与 CLI 使用）。
func (a *App) Store() *store.Store { return a.store }

// Config 返回配置。
func (a *App) Config() *config.Config { return a.cfg }

func (a *App) bootstrapAdmin() error {
	n, err := a.store.AdminCount()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	user, pass := strings.TrimSpace(a.cfg.AdminUser), a.cfg.AdminPassword
	if user == "" || pass == "" {
		return fmt.Errorf("首次启动必须通过 ULP_ADMIN_USER / ULP_ADMIN_PASSWORD 注入初始管理员")
	}
	if !usernamePattern.MatchString(user) {
		return fmt.Errorf("ULP_ADMIN_USER 非法：需为 3~32 位字母、数字、点、下划线或短横线")
	}
	if err := auth.ValidatePassword(pass); err != nil {
		return fmt.Errorf("ULP_ADMIN_PASSWORD 不符合强度要求: %w", err)
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return err
	}
	admin := &models.Admin{
		ID: newID(), Username: user, PasswordHash: hash,
		MustChangePassword: true, CreatedAt: nowString(),
	}
	if err := a.store.CreateAdmin(admin); err != nil {
		return fmt.Errorf("创建初始管理员失败: %w", err)
	}
	log.Printf("[启动] 已创建初始管理员 %q，首次登录必须修改密码", user)
	return nil
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("随机数生成失败: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func nowString() string { return time.Now().UTC().Format(time.RFC3339) }

// Record 写入审计日志。审计失败只记录到标准错误，不影响主流程。
func (a *App) Record(action, target, detail, operator, ip string) {
	entry := &models.AuditLog{
		ID: newID(), Action: action, Target: target, Detail: detail,
		Operator: operator, IP: ip, CreatedAt: nowString(),
	}
	if err := a.store.InsertAudit(entry); err != nil {
		log.Printf("[审计] 写入失败 action=%s target=%s err=%v", action, target, err)
	}
}

// ---------- 认证 ----------

// LoginResult 是登录成功返回体。
type LoginResult struct {
	Token             string        `json:"token"`
	ExpiresAt         string        `json:"expires_at"`
	Admin             *models.Admin `json:"admin"`
	MustChangePassword bool         `json:"must_change_password"`
}

// Login 校验账号密码并签发 JWT。
func (a *App) Login(username, password, ip string) (*LoginResult, error) {
	admin, err := a.store.GetAdminByUsername(strings.TrimSpace(username))
	if err != nil {
		a.Record("admin.login_failed", username, "用户名不存在", username, ip)
		return nil, unauthorized("用户名或密码错误")
	}
	if !auth.CheckPassword(admin.PasswordHash, password) {
		a.Record("admin.login_failed", admin.Username, "密码错误", admin.Username, ip)
		return nil, unauthorized("用户名或密码错误")
	}
	token, err := auth.IssueToken(a.cfg.JWTSecret, admin.ID, admin.Username, admin.MustChangePassword, a.cfg.TokenTTL)
	if err != nil {
		return nil, err
	}
	a.Record("admin.login", admin.Username, "登录成功", admin.Username, ip)
	return &LoginResult{
		Token:              token,
		ExpiresAt:          time.Now().UTC().Add(a.cfg.TokenTTL).Format(time.RFC3339),
		Admin:              admin,
		MustChangePassword: admin.MustChangePassword,
	}, nil
}

// GetAdmin 按 ID 查询管理员。
func (a *App) GetAdmin(id string) (*models.Admin, error) {
	admin, err := a.store.GetAdminByID(id)
	if err != nil {
		return nil, notFound("管理员不存在")
	}
	return admin, nil
}

// ChangePassword 修改密码并解除强制改密标记。
func (a *App) ChangePassword(adminID, oldPassword, newPassword, ip string) (*models.Admin, error) {
	admin, err := a.store.GetAdminByID(adminID)
	if err != nil {
		return nil, unauthorized("会话对应的管理员不存在")
	}
	if !auth.CheckPassword(admin.PasswordHash, oldPassword) {
		return nil, badRequest("原密码不正确")
	}
	if oldPassword == newPassword {
		return nil, badRequest("新密码不得与原密码相同")
	}
	if err := auth.ValidatePassword(newPassword); err != nil {
		return nil, badRequest(err.Error())
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	if err := a.store.UpdateAdminPassword(admin.ID, hash, false); err != nil {
		return nil, err
	}
	a.Record("admin.password", admin.Username, "修改登录密码", admin.Username, ip)
	admin.MustChangePassword = false
	return admin, nil
}

// IssueTokenFor 在改密后重新签发令牌（解除 must_change 标记）。
func (a *App) IssueTokenFor(admin *models.Admin) (string, string, error) {
	token, err := auth.IssueToken(a.cfg.JWTSecret, admin.ID, admin.Username, admin.MustChangePassword, a.cfg.TokenTTL)
	if err != nil {
		return "", "", err
	}
	return token, time.Now().UTC().Add(a.cfg.TokenTTL).Format(time.RFC3339), nil
}

// ---------- 项目 ----------

func (a *App) decorateProject(p *models.Project) {
	if p.PublicPEM != "" {
		p.KeyFingerprint = license.FingerprintPEM(p.PublicPEM)
	}
	if n, err := a.store.CountLicensesByProject(p.ID); err == nil {
		p.LicenseCount = n
	}
}

// ListProjects 返回项目列表。
func (a *App) ListProjects() ([]models.Project, error) {
	items, err := a.store.ListProjects()
	if err != nil {
		return nil, err
	}
	for i := range items {
		a.decorateProject(&items[i])
	}
	return items, nil
}

// CreateProjectRequest 是新增项目的请求体。
type CreateProjectRequest struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Product      string `json:"product"`
	InstanceHint string `json:"instance_hint"`
	Remark       string `json:"remark"`
}

// CreateProject 新增项目并自动生成独立 RSA-2048 密钥对。
func (a *App) CreateProject(req CreateProjectRequest, operator, ip string) (*models.Project, error) {
	req.ID = strings.ToLower(strings.TrimSpace(req.ID))
	req.Product = strings.ToLower(strings.TrimSpace(req.Product))
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, badRequest("项目显示名不能为空")
	}
	if !slugPattern.MatchString(req.ID) {
		return nil, badRequest("项目 ID 需为 2~64 位小写字母、数字或短横线，且以字母或数字开头")
	}
	if req.Product == "" {
		req.Product = req.ID
	}
	if !productPattern.MatchString(req.Product) {
		return nil, badRequest("product 标识需为 2~64 位小写字母、数字、点、下划线或短横线")
	}
	if len(req.Name) > 64 {
		return nil, badRequest("项目显示名过长（最多 64 个字符）")
	}
	if _, err := a.store.GetProject(req.ID); err == nil {
		return nil, conflict("项目 ID 已存在")
	}

	priv, err := license.GenerateKeyPair(2048)
	if err != nil {
		return nil, fmt.Errorf("生成密钥对失败: %w", err)
	}
	pubPEM, err := license.PublicKeyPEM(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	privPEM, err := license.PrivateKeyPEM(priv)
	if err != nil {
		return nil, err
	}
	privBytes := []byte(privPEM)
	cipherText, err := a.cipher.Seal(privBytes)
	for i := range privBytes {
		privBytes[i] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("加密私钥失败: %w", err)
	}

	now := nowString()
	key := &models.Keypair{ID: newID(), ProjectID: req.ID, PublicPEM: pubPEM, PrivateCipher: cipherText, CreatedAt: now}
	project := &models.Project{
		ID: req.ID, Name: req.Name, Product: req.Product, Status: models.ProjectActive,
		KeyID: key.ID, InstanceHint: strings.TrimSpace(req.InstanceHint),
		Remark: strings.TrimSpace(req.Remark), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.store.CreateProjectWithKey(project, key); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("项目 ID 或 product 已存在")
		}
		return nil, err
	}
	a.Record("project.create", project.ID, "新增项目 "+project.Name+" (product="+project.Product+")", operator, ip)
	a.decorateProject(project)
	return project, nil
}

// UpdateProjectRequest 是项目更新请求体。
type UpdateProjectRequest struct {
	Name         *string `json:"name"`
	Status       *string `json:"status"`
	InstanceHint *string `json:"instance_hint"`
	Remark       *string `json:"remark"`
}

// UpdateProject 更新项目信息。
func (a *App) UpdateProject(id string, req UpdateProjectRequest, operator, ip string) (*models.Project, error) {
	p, err := a.store.GetProject(id)
	if err != nil {
		return nil, notFound("项目不存在")
	}
	name, status, hint, remark := p.Name, p.Status, p.InstanceHint, p.Remark
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 64 {
			return nil, badRequest("项目显示名不能为空且不超过 64 个字符")
		}
	}
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
		if status != models.ProjectActive && status != models.ProjectDisabled {
			return nil, badRequest("项目状态只能是 active 或 disabled")
		}
	}
	if req.InstanceHint != nil {
		hint = strings.TrimSpace(*req.InstanceHint)
	}
	if req.Remark != nil {
		remark = strings.TrimSpace(*req.Remark)
	}
	if err := a.store.UpdateProject(id, name, hint, remark, status); err != nil {
		return nil, err
	}
	a.Record("project.update", id, fmt.Sprintf("更新项目：name=%s status=%s", name, status), operator, ip)
	return a.GetProject(id)
}

// GetProject 返回项目详情（含公钥指纹）。
func (a *App) GetProject(id string) (*models.Project, error) {
	p, err := a.store.GetProject(id)
	if err != nil {
		return nil, notFound("项目不存在")
	}
	a.decorateProject(p)
	return p, nil
}

// ProjectDetail 是项目详情 + 接入片段。
type ProjectDetail struct {
	Project  *models.Project    `json:"project"`
	PublicKey string            `json:"public_key"`
	Snippets map[string]string  `json:"snippets"`
}

// GetProjectDetail 返回项目详情与三种语言接入片段。
func (a *App) GetProjectDetail(id string) (*ProjectDetail, error) {
	p, err := a.GetProject(id)
	if err != nil {
		return nil, err
	}
	return &ProjectDetail{
		Project:   p,
		PublicKey: p.PublicPEM,
		Snippets: map[string]string{
			"go":     buildGoSnippet(p.Product, p.PublicPEM),
			"python": buildPythonSnippet(p.Product, p.PublicPEM),
			"node":   buildNodeSnippet(p.Product, p.PublicPEM),
		},
	}, nil
}

// DeleteProject 删除项目；存在授权记录时拒绝。
func (a *App) DeleteProject(id, operator, ip string) error {
	p, err := a.store.GetProject(id)
	if err != nil {
		return notFound("项目不存在")
	}
	n, err := a.store.CountLicensesByProject(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return conflict(fmt.Sprintf("项目下仍有 %d 条授权记录，无法删除；请先停用项目，删除全部授权记录后再操作", n))
	}
	if err := a.store.DeleteProject(id); err != nil {
		return err
	}
	a.Record("project.delete", id, "删除项目 "+p.Name, operator, ip)
	return nil
}

// PublicKey 返回项目公钥 PEM 与指纹。
func (a *App) PublicKey(id string) (string, string, error) {
	p, err := a.store.GetProject(id)
	if err != nil {
		return "", "", notFound("项目不存在")
	}
	if p.PublicPEM == "" {
		return "", "", notFound("项目尚未生成密钥")
	}
	return p.PublicPEM, license.FingerprintPEM(p.PublicPEM), nil
}

// RotateKey 为项目生成新密钥对并停用旧密钥。
func (a *App) RotateKey(projectID, operator, ip string) (*models.Keypair, error) {
	p, err := a.store.GetProject(projectID)
	if err != nil {
		return nil, notFound("项目不存在")
	}
	priv, err := license.GenerateKeyPair(2048)
	if err != nil {
		return nil, err
	}
	pubPEM, err := license.PublicKeyPEM(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	privPEM, err := license.PrivateKeyPEM(priv)
	if err != nil {
		return nil, err
	}
	privBytes := []byte(privPEM)
	cipherText, err := a.cipher.Seal(privBytes)
	for i := range privBytes {
		privBytes[i] = 0
	}
	if err != nil {
		return nil, err
	}
	key := &models.Keypair{ID: newID(), ProjectID: projectID, PublicPEM: pubPEM, PrivateCipher: cipherText, CreatedAt: nowString()}
	if err := a.store.CreateKeypair(key); err != nil {
		return nil, err
	}
	if err := a.store.SetProjectKey(projectID, key.ID); err != nil {
		return nil, err
	}
	if err := a.store.DisableKeypairs(projectID, key.ID); err != nil {
		return nil, err
	}
	key.Fingerprint = license.FingerprintPEM(pubPEM)
	a.Record("key.rotate", projectID, "为项目 "+p.Name+" 轮换密钥，新指纹 "+key.Fingerprint, operator, ip)
	return key, nil
}

// ListKeys 返回全部密钥（不含私钥）。
func (a *App) ListKeys() ([]models.Keypair, error) {
	items, err := a.store.ListKeypairs()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Fingerprint = license.FingerprintPEM(items[i].PublicPEM)
	}
	return items, nil
}

// ---------- 授权 ----------

// IssueRequest 是签发授权的请求体。
type IssueRequest struct {
	Project         string         `json:"project"`
	Customer        string         `json:"customer"`
	CustomerContact string         `json:"customer_contact"`
	ExpiryMode      string         `json:"expiry_mode"` // date | days | permanent
	ExpireAt        string         `json:"expire_at"`   // RFC3339，精确到分钟
	Days            int            `json:"days"`
	GraceDays       int            `json:"grace_days"`
	MachineMode     string         `json:"machine_mode"` // any | custom
	MachineCode     string         `json:"machine_code"`
	InstanceID      string         `json:"instance_id"`
	Features        map[string]any `json:"features"`
	Note            string         `json:"note"`
}

// LicenseDetail 是授权详情（含签发内容预览）。
type LicenseDetail struct {
	License *models.License `json:"license"`
	Preview string          `json:"preview"`
}

// Issue 签发授权并落盘。
func (a *App) Issue(req IssueRequest, operator, ip string) (*models.License, error) {
	p, err := a.store.GetProject(strings.TrimSpace(req.Project))
	if err != nil {
		return nil, notFound("项目不存在")
	}
	if p.Status != models.ProjectActive {
		return nil, conflict("项目已停用，无法签发授权")
	}
	customer := strings.TrimSpace(req.Customer)
	if customer == "" {
		return nil, badRequest("客户名称不能为空")
	}
	if len([]rune(customer)) > 100 {
		return nil, badRequest("客户名称过长（最多 100 个字符）")
	}
	contact := strings.TrimSpace(req.CustomerContact)
	if len([]rune(contact)) > 200 {
		return nil, badRequest("联系人信息过长（最多 200 个字符）")
	}
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > 2000 {
		return nil, badRequest("备注过长（最多 2000 个字符）")
	}
	if req.GraceDays < 0 || req.GraceDays > 3650 {
		return nil, badRequest("宽限期需为 0~3650 天")
	}

	expire, err := resolveExpiry(req.ExpiryMode, req.ExpireAt, req.Days)
	if err != nil {
		return nil, err
	}

	machineCode, err := resolveMachine(req.MachineMode, req.MachineCode)
	if err != nil {
		return nil, err
	}

	features := req.Features
	if features == nil {
		features = map[string]any{}
	}
	featuresJSON, err := json.Marshal(features)
	if err != nil || len(featuresJSON) > 8192 {
		return nil, badRequest("功能配置非法或过大（JSON 需小于 8KB）")
	}

	instanceID := strings.TrimSpace(req.InstanceID)
	if instanceID == "" {
		instanceID = p.InstanceHint
	}
	if len([]rune(instanceID)) > 128 {
		return nil, badRequest("实例标识过长（最多 128 个字符）")
	}

	keypair, err := a.store.GetKeypair(p.KeyID)
	if err != nil {
		return nil, fmt.Errorf("项目密钥缺失: %w", err)
	}
	if keypair.DisabledAt != "" {
		return nil, conflict("项目当前密钥已停用，请先轮换密钥")
	}
	privPEM, err := a.cipher.Open(keypair.PrivateCipher)
	if err != nil {
		return nil, err
	}
	priv, err := license.ParsePrivateKeyPEM(string(privPEM))
	for i := range privPEM {
		privPEM[i] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("私钥解析失败: %w", err)
	}

	licenseID := newID()
	payload := &license.Payload{
		SchemaVersion:   license.SchemaVersion,
		LicenseID:       licenseID,
		Platform:        license.PlatformName,
		Product:         p.Product,
		ProductDisplay:  p.Name,
		Customer:        customer,
		CustomerContact: contact,
		IssuedAt:        license.Now(),
		ExpireTime:      expire,
		GraceDays:       req.GraceDays,
		MachineCode:     machineCode,
		InstanceID:      instanceID,
		Features:        features,
		IssuedBy:        operator,
		Note:            note,
	}
	raw, err := license.SignPayload(payload, priv)
	if err != nil {
		return nil, fmt.Errorf("签发失败: %w", err)
	}
	filePath, err := a.writeLicenseFile(licenseID, raw)
	if err != nil {
		return nil, err
	}

	now := nowString()
	record := &models.License{
		ID: licenseID, ProjectID: p.ID, Customer: customer, CustomerContact: contact,
		IssuedAt: now, ExpireTime: payload.ExpireTime.StringOrEmpty(), GraceDays: req.GraceDays,
		MachineCode: machineCode, InstanceID: instanceID, Features: string(featuresJSON),
		Note: note, IssuedBy: operator, Status: models.LicenseActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := a.store.InsertLicense(record); err != nil {
		_ = os.Remove(filePath)
		return nil, err
	}
	a.Record("license.issue", licenseID,
		fmt.Sprintf("签发授权 project=%s customer=%s expiry=%s grace=%d machine=%s", p.ID, customer, record.ExpireTime, req.GraceDays, machineCode),
		operator, ip)
	record.ProjectName = p.Name
	record.Product = p.Product
	record.EffectiveStatus = models.EffectiveActive
	return record, nil
}

func resolveExpiry(mode, expireAt string, days int) (*license.Time, error) {
	switch strings.TrimSpace(mode) {
	case "permanent":
		return nil, nil
	case "days":
		if days < 1 || days > 36500 {
			return nil, badRequest("有效天数需为 1~36500 天")
		}
		t := license.NewTime(time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour))
		return &t, nil
	case "date":
		if strings.TrimSpace(expireAt) == "" {
			return nil, badRequest("请选择到期日期时间")
		}
		t, err := time.Parse(time.RFC3339, expireAt)
		if err != nil {
			return nil, badRequest("到期时间格式非法，需为 RFC3339（例如 2027-01-31T18:00:00+08:00）")
		}
		t = t.Truncate(time.Minute)
		if !t.After(time.Now().UTC()) {
			return nil, badRequest("到期时间必须晚于当前时间")
		}
		lt := license.NewTime(t)
		return &lt, nil
	default:
		return nil, badRequest("到期模式只能是 date、days 或 permanent")
	}
}

func resolveMachine(mode, code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	switch strings.TrimSpace(mode) {
	case "", "any":
		return license.MachineCodeWildcard, nil
	case "custom":
		if !machinePattern.MatchString(code) {
			return "", badRequest("机器码需为 32 位小写十六进制字符串（由目标项目 license 模块计算得到）")
		}
		return code, nil
	default:
		return "", badRequest("机器绑定方式只能是 any 或 custom")
	}
}

func (a *App) writeLicenseFile(id string, raw []byte) (string, error) {
	final := filepath.Join(a.cfg.LicenseDir, id+".lic")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", fmt.Errorf("写入授权文件失败: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("授权文件落盘失败: %w", err)
	}
	return final, nil
}

// LicenseFilter 是 HTTP 层传入的筛选条件。
type LicenseFilter = store.LicenseFilter

// ListLicenses 分页查询授权记录。
func (a *App) ListLicenses(f LicenseFilter) (*models.Page[models.License], error) {
	return a.store.ListLicenses(f)
}

// GetLicense 返回授权记录。
func (a *App) GetLicense(id string) (*models.License, error) {
	l, err := a.store.GetLicense(id)
	if err != nil {
		return nil, notFound("授权记录不存在")
	}
	l.EffectiveStatus = store.EffectiveStatus(l, time.Now().UTC())
	return l, nil
}

// GetLicenseDetail 返回授权记录与签发内容预览。
func (a *App) GetLicenseDetail(id string) (*LicenseDetail, error) {
	l, err := a.GetLicense(id)
	if err != nil {
		return nil, err
	}
	preview := ""
	raw, err := os.ReadFile(filepath.Join(a.cfg.LicenseDir, id+".lic"))
	if err == nil {
		preview = payloadPreview(raw)
	} else {
		preview = "（授权文件不存在或已被移出数据目录）"
	}
	return &LicenseDetail{License: l, Preview: preview}, nil
}

func payloadPreview(raw []byte) string {
	var env license.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "（授权文件解析失败）"
	}
	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return "（授权文件载荷解码失败）"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return string(data)
	}
	return buf.String()
}

// Download 读取授权文件并返回内容与建议文件名。
func (a *App) Download(id, operator, ip string) ([]byte, string, error) {
	l, err := a.GetLicense(id)
	if err != nil {
		return nil, "", err
	}
	if l.Status == models.LicenseRevoked {
		return nil, "", conflict("该授权已被吊销，不允许下载")
	}
	path := filepath.Join(a.cfg.LicenseDir, id+".lic")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", notFound("授权文件缺失，请重新签发")
	}
	name := fmt.Sprintf("%s-%s-%s.lic", l.ProjectID, sanitizeFilename(l.Customer), l.ID)
	if err := a.store.IncrementDownload(id); err != nil {
		log.Printf("[下载] 下载计数失败: %v", err)
	}
	a.Record("license.download", id, "下载授权文件 "+name, operator, ip)
	return raw, name, nil
}

func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "customer"
	}
	runes := []rune(out)
	if len(runes) > 60 {
		out = string(runes[:60])
	}
	return out
}

// Revoke 吊销授权。
func (a *App) Revoke(id, reason, operator, ip string) (*models.License, error) {
	l, err := a.store.GetLicense(id)
	if err != nil {
		return nil, notFound("授权记录不存在")
	}
	if l.Status == models.LicenseRevoked {
		return nil, conflict("该授权已被吊销")
	}
	if l.Status == models.LicenseSuperseded {
		return nil, conflict("该授权已被续期替代，无需吊销")
	}
	if err := a.store.UpdateLicenseStatus(id, models.LicenseRevoked, nowString()); err != nil {
		return nil, err
	}
	detail := "吊销授权"
	if strings.TrimSpace(reason) != "" {
		detail += "，原因：" + strings.TrimSpace(reason)
	}
	a.Record("license.revoke", id, detail, operator, ip)
	return a.GetLicense(id)
}

// RenewRequest 是续期请求体，未提供的字段沿用原授权。
type RenewRequest struct {
	ExpiryMode  string `json:"expiry_mode"`
	ExpireAt    string `json:"expire_at"`
	Days        int    `json:"days"`
	GraceDays   *int   `json:"grace_days"`
	MachineMode string `json:"machine_mode"`
	MachineCode string `json:"machine_code"`
	Note        string `json:"note"`
}

// Renew 基于原授权生成新授权，并把原授权标记为 superseded。
func (a *App) Renew(id string, req RenewRequest, operator, ip string) (*models.License, error) {
	old, err := a.store.GetLicense(id)
	if err != nil {
		return nil, notFound("授权记录不存在")
	}
	if old.Status == models.LicenseRevoked {
		return nil, conflict("已吊销的授权不能续期，请重新签发")
	}
	grace := old.GraceDays
	if req.GraceDays != nil {
		grace = *req.GraceDays
	}
	mode := req.ExpiryMode
	if mode == "" {
		return nil, badRequest("续期必须指定新的到期模式")
	}
	machineMode := req.MachineMode
	if machineMode == "" {
		machineMode = "any"
		if old.MachineCode != license.MachineCodeWildcard {
			machineMode = "custom"
		}
	}
	machineCode := req.MachineCode
	if machineMode == "custom" && machineCode == "" {
		machineCode = old.MachineCode
	}
	note := req.Note
	if strings.TrimSpace(note) == "" {
		note = old.Note
	}

	created, err := a.Issue(IssueRequest{
		Project:         old.ProjectID,
		Customer:        old.Customer,
		CustomerContact: old.CustomerContact,
		ExpiryMode:      mode,
		ExpireAt:        req.ExpireAt,
		Days:            req.Days,
		GraceDays:       grace,
		MachineMode:     machineMode,
		MachineCode:     machineCode,
		InstanceID:      old.InstanceID,
		Features:        featuresOf(old.Features),
		Note:            note,
	}, operator, ip)
	if err != nil {
		return nil, err
	}
	if err := a.store.UpdateLicenseStatus(old.ID, models.LicenseSuperseded, ""); err != nil {
		log.Printf("[续期] 标记原授权失败: %v", err)
	}
	// 回填续期来源（新记录插入后单独更新）。
	if _, err := a.store.DB().Exec(`UPDATE licenses SET renew_from=? WHERE id=?`, old.ID, created.ID); err != nil {
		log.Printf("[续期] 回填 renew_from 失败: %v", err)
	}
	created.RenewFrom = old.ID
	a.Record("license.renew", created.ID, "续期自 "+old.ID, operator, ip)
	return created, nil
}

func featuresOf(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// DeleteLicense 删除授权记录与文件。
func (a *App) DeleteLicense(id, operator, ip string) error {
	l, err := a.store.GetLicense(id)
	if err != nil {
		return notFound("授权记录不存在")
	}
	path, err := a.store.DeleteLicense(id)
	if err != nil {
		return err
	}
	if path != "" {
		_ = os.Remove(path)
	}
	a.Record("license.delete", id, fmt.Sprintf("删除授权记录 project=%s customer=%s", l.ProjectID, l.Customer), operator, ip)
	return nil
}

// ---------- 看板与审计 ----------

// Dashboard 是概览看板数据。
type Dashboard struct {
	Total           int                  `json:"total"`
	Active          int                  `json:"active"`
	Grace           int                  `json:"grace"`
	Expired         int                  `json:"expired"`
	Revoked         int                  `json:"revoked"`
	Superseded      int                  `json:"superseded"`
	Projects        int                  `json:"projects"`
	ActiveProjects  int                  `json:"active_projects"`
	IssuedLast30    int                  `json:"issued_last_30_days"`
	ExpiringSoon    []models.License     `json:"expiring_soon"`
	RecentLicenses  []models.License     `json:"recent_licenses"`
	RecentAudits    []models.AuditLog    `json:"recent_audits"`
}

// DashboardData 统计看板。
func (a *App) DashboardData() (*Dashboard, error) {
	licenses, err := a.store.ListAllLicenses()
	if err != nil {
		return nil, err
	}
	projects, err := a.store.ListProjects()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d := &Dashboard{Total: len(licenses), Projects: len(projects)}
	for _, p := range projects {
		if p.Status == models.ProjectActive {
			d.ActiveProjects++
		}
	}
	for _, l := range licenses {
		switch l.EffectiveStatus {
		case models.EffectiveActive:
			d.Active++
			if l.ExpireTime != "" {
				if exp, err := time.Parse(time.RFC3339, l.ExpireTime); err == nil {
					if exp.Sub(now) <= 30*24*time.Hour && exp.After(now) && len(d.ExpiringSoon) < 8 {
						d.ExpiringSoon = append(d.ExpiringSoon, l)
					}
				}
			}
		case models.EffectiveGrace:
			d.Grace++
		case models.EffectiveExpired:
			d.Expired++
		case models.LicenseRevoked:
			d.Revoked++
		case models.LicenseSuperseded:
			d.Superseded++
		}
		if t, err := time.Parse(time.RFC3339, l.IssuedAt); err == nil && now.Sub(t) <= 30*24*time.Hour {
			d.IssuedLast30++
		}
		if len(d.RecentLicenses) < 6 {
			d.RecentLicenses = append(d.RecentLicenses, l)
		}
	}
	audits, err := a.store.ListAudits(store.AuditFilter{Page: 1, Size: 8})
	if err != nil {
		return nil, err
	}
	d.RecentAudits = audits.Items
	return d, nil
}

// AuditFilter 是审计筛选条件。
type AuditFilter = store.AuditFilter

// ListAudits 分页查询审计日志。
func (a *App) ListAudits(f AuditFilter) (*models.Page[models.AuditLog], error) {
	return a.store.ListAudits(f)
}

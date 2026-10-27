// Package store 实现 SQLite 存储层：建表、迁移与各实体的 CRUD。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"unified-license-platform/internal/models"

	_ "modernc.org/sqlite"
)

// 常见存储错误。
var (
	ErrNotFound = errors.New("记录不存在")
	ErrConflict = errors.New("存在冲突记录")
)

// Store 封装数据库连接。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）SQLite 数据库并执行迁移。
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + abs + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者模型：限制为单连接，避免写锁竞争。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			product TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			key_id TEXT,
			instance_hint TEXT,
			remark TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS keypairs (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			public_pem TEXT NOT NULL,
			private_cipher TEXT NOT NULL,
			created_at TEXT NOT NULL,
			disabled_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS licenses (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			customer TEXT NOT NULL DEFAULT '',
			customer_contact TEXT DEFAULT '',
			issued_at TEXT NOT NULL,
			expire_time TEXT,
			grace_days INTEGER NOT NULL DEFAULT 0,
			machine_code TEXT NOT NULL DEFAULT '*',
			instance_id TEXT DEFAULT '',
			features TEXT DEFAULT '{}',
			note TEXT DEFAULT '',
			issued_by TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			revoked_at TEXT,
			renew_from TEXT,
			file_path TEXT NOT NULL,
			download_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS admins (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			must_change_password INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			target TEXT,
			detail TEXT,
			operator TEXT NOT NULL,
			ip TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_licenses_project ON licenses(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_licenses_issued ON licenses(issued_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_keys_project ON keypairs(project_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("数据库迁移失败: %w", err)
		}
	}
	return nil
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func ns(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

// ---------- 管理员 ----------

// AdminCount 返回管理员数量。
func (s *Store) AdminCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

// CreateAdmin 新建管理员。
func (s *Store) CreateAdmin(a *models.Admin) error {
	_, err := s.db.Exec(`INSERT INTO admins(id,username,password_hash,must_change_password,created_at) VALUES(?,?,?,?,?)`,
		a.ID, a.Username, a.PasswordHash, boolInt(a.MustChangePassword), a.CreatedAt)
	return err
}

// GetAdminByUsername 按用户名查询管理员。
func (s *Store) GetAdminByUsername(username string) (*models.Admin, error) {
	var a models.Admin
	var must int
	err := s.db.QueryRow(`SELECT id,username,password_hash,must_change_password,created_at FROM admins WHERE username=?`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &must, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.MustChangePassword = must == 1
	return &a, nil
}

// GetAdminByID 按 ID 查询管理员。
func (s *Store) GetAdminByID(id string) (*models.Admin, error) {
	var a models.Admin
	var must int
	err := s.db.QueryRow(`SELECT id,username,password_hash,must_change_password,created_at FROM admins WHERE id=?`, id).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &must, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.MustChangePassword = must == 1
	return &a, nil
}

// UpdateAdminPassword 更新密码哈希与强制改密标记。
func (s *Store) UpdateAdminPassword(id, hash string, mustChange bool) error {
	res, err := s.db.Exec(`UPDATE admins SET password_hash=?, must_change_password=? WHERE id=?`, hash, boolInt(mustChange), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- 项目 ----------

const projectSelect = `SELECT p.id,p.name,p.product,p.status,COALESCE(p.key_id,''),COALESCE(p.instance_hint,''),COALESCE(p.remark,''),
	p.created_at,p.updated_at,COALESCE(k.public_pem,''),COALESCE(k.created_at,'')
	FROM projects p LEFT JOIN keypairs k ON k.id=p.key_id`

// CreateProject 新建项目。
func (s *Store) CreateProject(p *models.Project) error {
	_, err := s.db.Exec(`INSERT INTO projects(id,name,product,status,key_id,instance_hint,remark,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Product, p.Status, nullable(p.KeyID), nullable(p.InstanceHint), nullable(p.Remark), p.CreatedAt, p.UpdatedAt)
	return err
}

// GetProject 按项目 ID 查询。
func (s *Store) GetProject(id string) (*models.Project, error) {
	row := s.db.QueryRow(projectSelect+` WHERE p.id=?`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ListProjects 列出全部项目。
func (s *Store) ListProjects() ([]models.Project, error) {
	rows, err := s.db.Query(projectSelect + ` ORDER BY p.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (*models.Project, error) {
	var p models.Project
	err := row.Scan(&p.ID, &p.Name, &p.Product, &p.Status, &p.KeyID, &p.InstanceHint, &p.Remark,
		&p.CreatedAt, &p.UpdatedAt, &p.PublicPEM, &p.KeyCreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateProject 更新项目可变字段。
func (s *Store) UpdateProject(id, name, instanceHint, remark, status string) error {
	res, err := s.db.Exec(`UPDATE projects SET name=?, instance_hint=?, remark=?, status=?, updated_at=? WHERE id=?`,
		name, nullable(instanceHint), nullable(remark), status, nowString(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProjectKey 更新项目当前密钥。
func (s *Store) SetProjectKey(id, keyID string) error {
	_, err := s.db.Exec(`UPDATE projects SET key_id=?, updated_at=? WHERE id=?`, keyID, nowString(), id)
	return err
}

// CreateProjectWithKey 在同一事务中写入项目与密钥对。
func (s *Store) CreateProjectWithKey(p *models.Project, k *models.Keypair) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO keypairs(id,project_id,public_pem,private_cipher,created_at) VALUES(?,?,?,?,?)`,
		k.ID, k.ProjectID, k.PublicPEM, k.PrivateCipher, k.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO projects(id,name,product,status,key_id,instance_hint,remark,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Product, p.Status, p.KeyID, nullable(p.InstanceHint), nullable(p.Remark), p.CreatedAt, p.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// DB 暴露底层连接（仅用于少量跨表更新）。
func (s *Store) DB() *sql.DB { return s.db }

// DeleteProject 删除项目及其密钥（授权记录存在时由上层拦截）。
func (s *Store) DeleteProject(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM keypairs WHERE project_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// CountLicensesByProject 统计项目下的授权记录数。
func (s *Store) CountLicensesByProject(projectID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM licenses WHERE project_id=?`, projectID).Scan(&n)
	return n, err
}

// ---------- 密钥 ----------

// CreateKeypair 保存密钥对（私钥为密文）。
func (s *Store) CreateKeypair(k *models.Keypair) error {
	_, err := s.db.Exec(`INSERT INTO keypairs(id,project_id,public_pem,private_cipher,created_at) VALUES(?,?,?,?,?)`,
		k.ID, k.ProjectID, k.PublicPEM, k.PrivateCipher, k.CreatedAt)
	return err
}

// GetKeypair 按 ID 查询密钥（含私钥密文，仅供服务端解密使用）。
func (s *Store) GetKeypair(id string) (*models.Keypair, error) {
	var k models.Keypair
	err := s.db.QueryRow(`SELECT id,COALESCE(project_id,''),public_pem,private_cipher,created_at,COALESCE(disabled_at,'')
		FROM keypairs WHERE id=?`, id).
		Scan(&k.ID, &k.ProjectID, &k.PublicPEM, &k.PrivateCipher, &k.CreatedAt, &k.DisabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// DisableKeypairs 停用项目下除 keepID 之外的密钥。
func (s *Store) DisableKeypairs(projectID, keepID string) error {
	_, err := s.db.Exec(`UPDATE keypairs SET disabled_at=? WHERE project_id=? AND disabled_at IS NULL AND id<>?`,
		nowString(), projectID, keepID)
	return err
}

// ListKeypairs 列出所有项目的密钥（不含私钥密文）。
func (s *Store) ListKeypairs() ([]models.Keypair, error) {
	rows, err := s.db.Query(`SELECT k.id,COALESCE(k.project_id,''),k.public_pem,k.created_at,COALESCE(k.disabled_at,''),
		COALESCE(p.name,''),COALESCE(p.product,'')
		FROM keypairs k LEFT JOIN projects p ON p.id=k.project_id
		ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Keypair, 0)
	for rows.Next() {
		var k models.Keypair
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.PublicPEM, &k.CreatedAt, &k.DisabledAt, &k.ProjectName, &k.Product); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ---------- 授权记录 ----------

// InsertLicense 保存授权记录。
func (s *Store) InsertLicense(l *models.License) error {
	_, err := s.db.Exec(`INSERT INTO licenses(id,project_id,customer,customer_contact,issued_at,expire_time,grace_days,
		machine_code,instance_id,features,note,issued_by,status,revoked_at,renew_from,file_path,download_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.ProjectID, l.Customer, l.CustomerContact, l.IssuedAt, nullable(l.ExpireTime), l.GraceDays,
		l.MachineCode, l.InstanceID, l.Features, l.Note, l.IssuedBy, l.Status, nullable(l.RevokedAt),
		nullable(l.RenewFrom), l.FilePath, l.DownloadCount, l.CreatedAt, l.UpdatedAt)
	return err
}

const licenseSelect = `SELECT l.id,l.project_id,COALESCE(p.name,''),COALESCE(p.product,''),l.customer,COALESCE(l.customer_contact,''),
	l.issued_at,COALESCE(l.expire_time,''),l.grace_days,l.machine_code,COALESCE(l.instance_id,''),COALESCE(l.features,'{}'),
	COALESCE(l.note,''),l.issued_by,l.status,COALESCE(l.revoked_at,''),COALESCE(l.renew_from,''),l.download_count,
	l.created_at,l.updated_at
	FROM licenses l LEFT JOIN projects p ON p.id=l.project_id`

func scanLicense(row scanner) (*models.License, error) {
	var l models.License
	err := row.Scan(&l.ID, &l.ProjectID, &l.ProjectName, &l.Product, &l.Customer, &l.CustomerContact,
		&l.IssuedAt, &l.ExpireTime, &l.GraceDays, &l.MachineCode, &l.InstanceID, &l.Features,
		&l.Note, &l.IssuedBy, &l.Status, &l.RevokedAt, &l.RenewFrom, &l.DownloadCount,
		&l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetLicense 按授权 ID 查询。
func (s *Store) GetLicense(id string) (*models.License, error) {
	return scanLicense(s.db.QueryRow(licenseSelect+` WHERE l.id=?`, id))
}

// LicenseFilter 是授权记录筛选条件。
type LicenseFilter struct {
	Project  string
	Customer string
	Status   string // 展示状态或存储状态
	From     string
	To       string
	Page     int
	Size     int
}

// ListLicenses 分页查询授权记录，status 支持 active/grace/expired/revoked/superseded。
// 状态为派生值，因此在 Go 侧过滤与分页，保证语义与 UI 一致。
func (s *Store) ListLicenses(f LicenseFilter) (*models.Page[models.License], error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Project != "" {
		where = append(where, "l.project_id=?")
		args = append(args, f.Project)
	}
	if f.Customer != "" {
		where = append(where, "l.customer LIKE ?")
		args = append(args, "%"+f.Customer+"%")
	}
	if f.From != "" {
		where = append(where, "substr(l.issued_at,1,10)>=?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "substr(l.issued_at,1,10)<=?")
		args = append(args, f.To)
	}
	rows, err := s.db.Query(licenseSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY l.issued_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	all := make([]models.License, 0)
	for rows.Next() {
		l, err := scanLicense(rows)
		if err != nil {
			return nil, err
		}
		l.EffectiveStatus = EffectiveStatus(l, now)
		if f.Status != "" && l.EffectiveStatus != f.Status && l.Status != f.Status {
			continue
		}
		all = append(all, *l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page, size := f.Page, f.Size
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	total := len(all)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return &models.Page[models.License]{Items: all[start:end], Total: total, Page: page, Size: size}, nil
}

// EffectiveStatus 计算授权的展示状态。
func EffectiveStatus(l *models.License, now time.Time) string {
	if l.Status == models.LicenseRevoked {
		return models.LicenseRevoked
	}
	if l.Status == models.LicenseSuperseded {
		return models.LicenseSuperseded
	}
	if l.ExpireTime == "" {
		return models.EffectiveActive
	}
	exp, err := time.Parse(time.RFC3339, l.ExpireTime)
	if err != nil {
		return models.EffectiveExpired
	}
	if now.Before(exp) {
		return models.EffectiveActive
	}
	graceEnd := exp.Add(time.Duration(l.GraceDays) * 24 * time.Hour)
	if l.GraceDays > 0 && now.Before(graceEnd) {
		return models.EffectiveGrace
	}
	return models.EffectiveExpired
}

// UpdateLicenseStatus 更新授权状态。
func (s *Store) UpdateLicenseStatus(id, status, revokedAt string) error {
	res, err := s.db.Exec(`UPDATE licenses SET status=?, revoked_at=?, updated_at=? WHERE id=?`,
		status, nullable(revokedAt), nowString(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementDownload 累加下载次数。
func (s *Store) IncrementDownload(id string) error {
	_, err := s.db.Exec(`UPDATE licenses SET download_count=download_count+1, updated_at=? WHERE id=?`, nowString(), id)
	return err
}

// DeleteLicense 删除授权记录并返回原文件路径。
func (s *Store) DeleteLicense(id string) (string, error) {
	var path string
	err := s.db.QueryRow(`SELECT file_path FROM licenses WHERE id=?`, id).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`DELETE FROM licenses WHERE id=?`, id); err != nil {
		return "", err
	}
	return path, nil
}

// ListAllLicenses 返回全部授权记录（用于看板统计）。
func (s *Store) ListAllLicenses() ([]models.License, error) {
	rows, err := s.db.Query(licenseSelect + ` ORDER BY l.issued_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := make([]models.License, 0)
	for rows.Next() {
		l, err := scanLicense(rows)
		if err != nil {
			return nil, err
		}
		l.EffectiveStatus = EffectiveStatus(l, now)
		out = append(out, *l)
	}
	return out, rows.Err()
}

// ---------- 审计日志 ----------

// InsertAudit 写入审计日志。
func (s *Store) InsertAudit(a *models.AuditLog) error {
	_, err := s.db.Exec(`INSERT INTO audit_logs(id,action,target,detail,operator,ip,created_at) VALUES(?,?,?,?,?,?,?)`,
		a.ID, a.Action, nullable(a.Target), nullable(a.Detail), a.Operator, nullable(a.IP), a.CreatedAt)
	return err
}

// AuditFilter 是审计日志筛选条件。
type AuditFilter struct {
	Action   string
	Operator string
	From     string
	To       string
	Page     int
	Size     int
}

// ListAudits 分页查询审计日志。
func (s *Store) ListAudits(f AuditFilter) (*models.Page[models.AuditLog], error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Action != "" {
		where = append(where, "action=?")
		args = append(args, f.Action)
	}
	if f.Operator != "" {
		where = append(where, "operator LIKE ?")
		args = append(args, "%"+f.Operator+"%")
	}
	if f.From != "" {
		where = append(where, "substr(created_at,1,10)>=?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "substr(created_at,1,10)<=?")
		args = append(args, f.To)
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, size := f.Page, f.Size
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	rows, err := s.db.Query(`SELECT id,action,COALESCE(target,''),COALESCE(detail,''),operator,COALESCE(ip,''),created_at
		FROM audit_logs WHERE `+cond+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]models.AuditLog, 0)
	for rows.Next() {
		var a models.AuditLog
		if err := rows.Scan(&a.ID, &a.Action, &a.Target, &a.Detail, &a.Operator, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return &models.Page[models.AuditLog]{Items: items, Total: total, Page: page, Size: size}, rows.Err()
}

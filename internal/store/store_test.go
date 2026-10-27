package store

import (
	"path/filepath"
	"testing"
	"time"

	"unified-license-platform/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEffectiveStatus(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-48 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-24 * time.Hour).Format(time.RFC3339)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)

	cases := []struct {
		name string
		lic  models.License
		want string
	}{
		{"永久授权", models.License{Status: models.LicenseActive}, models.EffectiveActive},
		{"未到期", models.License{Status: models.LicenseActive, ExpireTime: future}, models.EffectiveActive},
		{"已过期无宽限", models.License{Status: models.LicenseActive, ExpireTime: past}, models.EffectiveExpired},
		{"已过期在宽限期", models.License{Status: models.LicenseActive, ExpireTime: recent, GraceDays: 7}, models.EffectiveGrace},
		{"宽限期已结束", models.License{Status: models.LicenseActive, ExpireTime: past, GraceDays: 1}, models.EffectiveExpired},
		{"已吊销优先", models.License{Status: models.LicenseRevoked, ExpireTime: future}, models.LicenseRevoked},
		{"已续期替代", models.License{Status: models.LicenseSuperseded, ExpireTime: future}, models.LicenseSuperseded},
		{"时间格式非法", models.License{Status: models.LicenseActive, ExpireTime: "not-a-time"}, models.EffectiveExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveStatus(&tc.lic, now); got != tc.want {
				t.Fatalf("EffectiveStatus=%q，期望 %q", got, tc.want)
			}
		})
	}
}

func seedProject(t *testing.T, s *Store, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	k := &models.Keypair{ID: "key-" + id, ProjectID: id, PublicPEM: "PEM", PrivateCipher: "CIPHER", CreatedAt: now}
	p := &models.Project{ID: id, Name: "项目 " + id, Product: id, Status: models.ProjectActive, KeyID: k.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateProjectWithKey(p, k); err != nil {
		t.Fatalf("创建测试项目失败: %v", err)
	}
}

func seedLicense(t *testing.T, s *Store, id, project, customer, expire string, grace int, status string, issued time.Time) {
	t.Helper()
	now := issued.UTC().Format(time.RFC3339)
	l := &models.License{
		ID: id, ProjectID: project, Customer: customer, IssuedAt: now, ExpireTime: expire,
		GraceDays: grace, MachineCode: "*", Features: "{}", IssuedBy: "tester",
		Status: status, FilePath: "/tmp/" + id + ".lic", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.InsertLicense(l); err != nil {
		t.Fatalf("写入测试授权失败: %v", err)
	}
}

func TestListLicensesFilterAndPaging(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "agent-system")
	seedProject(t, s, "webb-api")

	base := time.Now().UTC().Add(-10 * 24 * time.Hour)
	future := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	past := time.Now().UTC().Add(-2 * 24 * time.Hour).Format(time.RFC3339)

	seedLicense(t, s, "l1", "agent-system", "甲公司", future, 0, models.LicenseActive, base)
	seedLicense(t, s, "l2", "agent-system", "乙公司", past, 7, models.LicenseActive, base.Add(time.Hour))
	seedLicense(t, s, "l3", "agent-system", "丙公司", "", 0, models.LicenseRevoked, base.Add(2*time.Hour))
	seedLicense(t, s, "l4", "webb-api", "甲公司", future, 0, models.LicenseSuperseded, base.Add(3*time.Hour))

	// 按项目筛选。
	page, err := s.ListLicenses(LicenseFilter{Project: "agent-system", Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("按项目筛选结果异常: total=%d len=%d", page.Total, len(page.Items))
	}

	// 按客户模糊筛选。
	page, err = s.ListLicenses(LicenseFilter{Customer: "甲", Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("按客户筛选结果异常: total=%d", page.Total)
	}

	// 按派生状态筛选：l2 处于宽限期。
	page, err = s.ListLicenses(LicenseFilter{Status: models.EffectiveGrace, Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 1 || page.Items[0].ID != "l2" {
		t.Fatalf("按宽限期筛选结果异常: %+v", page.Items)
	}

	// 吊销记录按存储状态筛选。
	page, err = s.ListLicenses(LicenseFilter{Status: models.LicenseRevoked, Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 1 || page.Items[0].ID != "l3" {
		t.Fatalf("按吊销状态筛选结果异常: %+v", page.Items)
	}

	// 分页：按签发时间倒序，每页 2 条。
	page, err = s.ListLicenses(LicenseFilter{Page: 2, Size: 2})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 4 || page.Page != 2 || len(page.Items) != 2 {
		t.Fatalf("分页结果异常: %+v", page)
	}
	if page.Items[0].ID != "l2" || page.Items[1].ID != "l1" {
		t.Fatalf("分页顺序异常: %s,%s", page.Items[0].ID, page.Items[1].ID)
	}

	// 非法分页参数回落到默认值。
	page, err = s.ListLicenses(LicenseFilter{Page: -1, Size: 999})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Page != 1 || page.Size != 20 || len(page.Items) != 4 {
		t.Fatalf("默认分页参数未生效: page=%d size=%d len=%d", page.Page, page.Size, len(page.Items))
	}
}

func TestAuditFilterAndPaging(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	actions := []string{"license.issue", "license.issue", "license.revoke", "project.create", "admin.login"}
	operators := []string{"alice", "alice", "bob", "bob", "alice"}
	for i := range actions {
		a := &models.AuditLog{
			ID: "a" + string(rune('0'+i)), Action: actions[i], Target: "t",
			Detail: "d", Operator: operators[i], IP: "127.0.0.1",
			CreatedAt: base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		}
		if err := s.InsertAudit(a); err != nil {
			t.Fatalf("写入审计失败: %v", err)
		}
	}

	page, err := s.ListAudits(AuditFilter{Action: "license.issue", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("按操作筛选异常: total=%d", page.Total)
	}

	page, err = s.ListAudits(AuditFilter{Operator: "bob", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("按操作人筛选异常: total=%d", page.Total)
	}

	page, err = s.ListAudits(AuditFilter{From: "2026-10-20", To: "2026-10-20", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if page.Total != 5 {
		t.Fatalf("按日期筛选异常: total=%d", page.Total)
	}

	page, err = s.ListAudits(AuditFilter{From: "2026-10-21", To: "2026-10-31", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("按日期范围筛选异常: total=%d", page.Total)
	}

	// 分页：倒序，每页 2 条。
	page, err = s.ListAudits(AuditFilter{Page: 1, Size: 2})
	if err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if page.Total != 5 || len(page.Items) != 2 {
		t.Fatalf("审计分页异常: total=%d len=%d", page.Total, len(page.Items))
	}
	if page.Items[0].Action != "admin.login" {
		t.Fatalf("审计排序异常，首条为 %s", page.Items[0].Action)
	}
}

func TestProjectKeyLifecycle(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "demo")

	p, err := s.GetProject("demo")
	if err != nil {
		t.Fatalf("查询项目失败: %v", err)
	}
	if p.Product != "demo" || p.PublicPEM != "PEM" || p.KeyID != "key-demo" {
		t.Fatalf("项目字段异常: %+v", p)
	}

	n, err := s.CountLicensesByProject("demo")
	if err != nil || n != 0 {
		t.Fatalf("授权计数异常: n=%d err=%v", n, err)
	}

	seedLicense(t, s, "lic-demo", "demo", "客户", "", 0, models.LicenseActive, time.Now().UTC())
	n, err = s.CountLicensesByProject("demo")
	if err != nil || n != 1 {
		t.Fatalf("授权计数异常: n=%d err=%v", n, err)
	}

	if err := s.UpdateProject("demo", "新名称", "hint", "remark", models.ProjectDisabled); err != nil {
		t.Fatalf("更新项目失败: %v", err)
	}
	p, _ = s.GetProject("demo")
	if p.Name != "新名称" || p.Status != models.ProjectDisabled {
		t.Fatalf("项目更新未生效: %+v", p)
	}

	// 轮换密钥后旧密钥应被停用，项目指向新密钥。
	now := time.Now().UTC().Format(time.RFC3339)
	newKey := &models.Keypair{ID: "key-new", ProjectID: "demo", PublicPEM: "PEM2", PrivateCipher: "CIPHER2", CreatedAt: now}
	if err := s.CreateKeypair(newKey); err != nil {
		t.Fatalf("创建密钥失败: %v", err)
	}
	if err := s.SetProjectKey("demo", newKey.ID); err != nil {
		t.Fatalf("切换项目密钥失败: %v", err)
	}
	if err := s.DisableKeypairs("demo", newKey.ID); err != nil {
		t.Fatalf("停用旧密钥失败: %v", err)
	}
	oldKey, _ := s.GetKeypair("key-demo")
	if oldKey.DisabledAt == "" {
		t.Fatal("旧密钥未被停用")
	}
	keys, err := s.ListKeypairs()
	if err != nil || len(keys) != 2 {
		t.Fatalf("密钥列表异常: len=%d err=%v", len(keys), err)
	}
	for _, k := range keys {
		if k.PrivateCipher != "" {
			t.Fatal("密钥列表不得包含私钥密文")
		}
	}

	// 删除项目时连同密钥一并清理。
	if err := s.DeleteProject("demo"); err != nil {
		t.Fatalf("删除项目失败: %v", err)
	}
	if _, err := s.GetProject("demo"); err != ErrNotFound {
		t.Fatalf("项目删除后仍可查询: %v", err)
	}
	if _, err := s.GetKeypair("key-new"); err != ErrNotFound {
		t.Fatalf("项目删除后密钥仍存在: %v", err)
	}
}

func TestAdminCRUD(t *testing.T) {
	s := newTestStore(t)
	a := &models.Admin{ID: "a1", Username: "admin", PasswordHash: "hash", MustChangePassword: true, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := s.CreateAdmin(a); err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	n, err := s.AdminCount()
	if err != nil || n != 1 {
		t.Fatalf("管理员计数异常: n=%d err=%v", n, err)
	}
	got, err := s.GetAdminByUsername("admin")
	if err != nil || !got.MustChangePassword || got.PasswordHash != "hash" {
		t.Fatalf("管理员查询异常: %+v err=%v", got, err)
	}
	if err := s.UpdateAdminPassword("a1", "newhash", false); err != nil {
		t.Fatalf("更新密码失败: %v", err)
	}
	got, _ = s.GetAdminByID("a1")
	if got.PasswordHash != "newhash" || got.MustChangePassword {
		t.Fatalf("密码更新未生效: %+v", got)
	}
	if _, err := s.GetAdminByUsername("missing"); err != ErrNotFound {
		t.Fatalf("不存在管理员应返回 ErrNotFound: %v", err)
	}
}

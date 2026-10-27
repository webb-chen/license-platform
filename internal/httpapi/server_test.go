package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"unified-license-platform/internal/app"
	"unified-license-platform/internal/config"
	"unified-license-platform/internal/httpapi"
	"unified-license-platform/internal/license"
	"unified-license-platform/internal/models"
	"unified-license-platform/web"
)

const (
	initialUser     = "admin"
	initialPassword = "Admin12345x"
	newPassword     = "NewPass123456"
)

type testClient struct {
	t      *testing.T
	base   string
	token  string
	client *http.Client
}

func newTestServer(t *testing.T) (*testClient, *app.App) {
	t.Helper()
	dir := t.TempDir()
	licenseDir := filepath.Join(dir, "licenses")
	if err := os.MkdirAll(licenseDir, 0o700); err != nil {
		t.Fatalf("创建授权目录失败: %v", err)
	}
	cfg := &config.Config{
		Addr:          ":0",
		DataDir:       dir,
		DBPath:        filepath.Join(dir, "ulpd.db"),
		LicenseDir:    licenseDir,
		ClockPath:     filepath.Join(dir, "clock.dat"),
		MasterKey:     []byte("test-master-key-0123456789abcdef"),
		JWTSecret:     []byte("test-jwt-secret-0123456789abcdef-xyz"),
		TokenTTL:      time.Hour,
		AdminUser:     initialUser,
		AdminPassword: initialPassword,
		Version:       "test",
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("初始化应用失败: %v", err)
	}
	ts := httptest.NewServer(httpapi.New(a, web.FS))
	t.Cleanup(func() {
		ts.Close()
		_ = a.Close()
	})
	return &testClient{t: t, base: ts.URL, client: ts.Client()}, a
}

func (c *testClient) do(method, path string, body any) (int, []byte, http.Header) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("序列化请求失败: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("构造请求失败: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("请求 %s %s 失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("读取响应失败: %v", err)
	}
	return resp.StatusCode, raw, resp.Header
}

func (c *testClient) mustStatus(want int, method, path string, body any) []byte {
	c.t.Helper()
	got, raw, _ := c.do(method, path, body)
	if got != want {
		c.t.Fatalf("%s %s 状态码=%d，期望 %d，响应：%s", method, path, got, want, string(raw))
	}
	return raw
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("解析响应失败: %v，响应：%s", err, string(raw))
	}
	return v
}

func errorCode(t *testing.T, raw []byte) string {
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.Code
}

func assertNoPrivateKey(t *testing.T, label string, raw []byte) {
	t.Helper()
	if bytes.Contains(raw, []byte("PRIVATE KEY")) {
		t.Fatalf("%s 响应中出现了私钥内容", label)
	}
}

type loginResponse struct {
	Token              string       `json:"token"`
	MustChangePassword bool         `json:"must_change_password"`
	Admin              models.Admin `json:"admin"`
}

type licenseResponse struct {
	ID              string `json:"id"`
	ProjectID       string `json:"project_id"`
	Customer        string `json:"customer"`
	ExpireTime      string `json:"expire_time"`
	GraceDays       int    `json:"grace_days"`
	MachineCode     string `json:"machine_code"`
	Status          string `json:"status"`
	EffectiveStatus string `json:"effective_status"`
	RenewFrom       string `json:"renew_from"`
	DownloadCount   int    `json:"download_count"`
}

type licensePage struct {
	Items []licenseResponse `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}

type projectDetail struct {
	Project   models.Project    `json:"project"`
	PublicKey string            `json:"public_key"`
	Snippets  map[string]string `json:"snippets"`
}

type auditPage struct {
	Items []models.AuditLog `json:"items"`
	Total int               `json:"total"`
}

func TestPlatformEndToEnd(t *testing.T) {
	c, _ := newTestServer(t)

	// ---------- 健康检查与鉴权 ----------
	raw := c.mustStatus(http.StatusOK, http.MethodGet, "/api/health", nil)
	if !bytes.Contains(raw, []byte(`"status":"ok"`)) {
		t.Fatalf("健康检查响应异常: %s", raw)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/projects", nil); code != http.StatusUnauthorized {
		t.Fatalf("未登录访问应返回 401，实际 %d", code)
	}

	// ---------- 登录与强制改密 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodPost, "/api/admin/login",
		map[string]string{"username": initialUser, "password": initialPassword})
	login := decode[loginResponse](t, raw)
	if login.Token == "" || !login.MustChangePassword {
		t.Fatalf("首次登录响应异常: %+v", login)
	}
	c.token = login.Token

	raw = c.mustStatus(http.StatusForbidden, http.MethodGet, "/api/projects", nil)
	if errorCode(t, raw) != "password_change_required" {
		t.Fatalf("未改密访问业务接口应返回 password_change_required，实际：%s", raw)
	}

	raw = c.mustStatus(http.StatusOK, http.MethodPost, "/api/admin/password",
		map[string]string{"old_password": initialPassword, "new_password": newPassword})
	var changed struct {
		Token              string `json:"token"`
		MustChangePassword bool   `json:"must_change_password"`
	}
	changed = decode[struct {
		Token              string `json:"token"`
		MustChangePassword bool   `json:"must_change_password"`
	}](t, raw)
	if changed.Token == "" || changed.MustChangePassword {
		t.Fatalf("改密响应异常: %s", raw)
	}
	c.token = changed.Token

	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/admin/me", nil)
	if !bytes.Contains(raw, []byte(`"must_change_password":false`)) {
		t.Fatalf("改密后 must_change_password 未解除: %s", raw)
	}

	// 错误密码。
	if code, body, _ := c.do(http.MethodPost, "/api/admin/login",
		map[string]string{"username": initialUser, "password": "WrongPass123"}); code != http.StatusUnauthorized {
		t.Fatalf("错误密码应返回 401，实际 %d：%s", code, body)
	}

	// ---------- 项目管理 ----------
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/projects", map[string]any{
		"id": "agent-system", "name": "Agent System", "product": "agent-system",
		"instance_hint": "prod-01", "remark": "首个接入项目",
	})
	project := decode[models.Project](t, raw)
	if project.ID != "agent-system" || project.Status != models.ProjectActive {
		t.Fatalf("项目创建响应异常: %+v", project)
	}
	assertNoPrivateKey(t, "创建项目", raw)

	// 重复项目 ID。
	raw = c.mustStatus(http.StatusConflict, http.MethodPost, "/api/projects", map[string]any{
		"id": "agent-system", "name": "重复项目",
	})
	if errorCode(t, raw) != "conflict" {
		t.Fatalf("重复项目应返回 conflict: %s", raw)
	}

	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/projects/agent-system", nil)
	detail := decode[projectDetail](t, raw)
	assertNoPrivateKey(t, "项目详情", raw)
	if !strings.HasPrefix(detail.PublicKey, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("项目详情未返回公钥 PEM: %s", detail.PublicKey)
	}
	if detail.Project.KeyFingerprint == "" {
		t.Fatal("项目详情缺少公钥指纹")
	}
	for _, lang := range []string{"go", "python", "node"} {
		if !strings.Contains(detail.Snippets[lang], "agent-system") {
			t.Fatalf("%s 接入片段缺少 product 标识", lang)
		}
	}
	agentPublic := detail.PublicKey

	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/projects/agent-system/public-key", nil)
	if !strings.HasPrefix(string(raw), "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("公钥下载内容异常: %s", raw)
	}

	// 第二个项目用于产品不匹配与删除测试。
	c.mustStatus(http.StatusCreated, http.MethodPost, "/api/projects", map[string]any{
		"id": "webb-api", "name": "Webb API", "product": "webb-api",
	})

	// ---------- 签发：永久授权 ----------
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "测试客户", "customer_contact": "张三 / zhangsan@example.com",
		"expiry_mode": "permanent", "grace_days": 0, "machine_mode": "any",
		"features": map[string]any{"max_nodes": 10}, "note": "永久授权样例",
	})
	perm := decode[licenseResponse](t, raw)
	if perm.ExpireTime != "" || perm.MachineCode != "*" || perm.Status != models.LicenseActive {
		t.Fatalf("永久授权字段异常: %+v", perm)
	}
	assertNoPrivateKey(t, "签发授权", raw)

	// 详情预览：应为脱敏后的 payload JSON。
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+perm.ID, nil)
	licDetail := decode[struct {
		Preview string `json:"preview"`
	}](t, raw)
	if !strings.Contains(licDetail.Preview, `"expire_time": null`) {
		t.Fatalf("详情预览未体现永久授权: %s", licDetail.Preview)
	}
	assertNoPrivateKey(t, "授权详情", raw)

	// 下载并按规范验证文件名与签名。
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+perm.ID+"/download", nil)
	assertNoPrivateKey(t, "授权下载", raw)
	licBytes := raw
	_, _, dlHeader := c.do(http.MethodGet, "/api/licenses/"+perm.ID+"/download", nil)
	disposition := dlHeader.Get("Content-Disposition")
	if !strings.Contains(disposition, "agent-system-") || !strings.Contains(disposition, perm.ID+".lic") {
		t.Fatalf("下载文件名不符合规范: %s", disposition)
	}
	if !strings.Contains(disposition, "filename*=UTF-8''") {
		t.Fatalf("下载文件名缺少 UTF-8 编码：%s", disposition)
	}

	payload, result := license.VerifyAndEvaluate(licBytes, agentPublic, license.EvalOptions{
		ExpectedProduct: "agent-system", MachineCode: "whatever",
	})
	if result.Code != license.CodeOK || !result.Valid || result.RemainingDays != -1 {
		t.Fatalf("永久授权离线校验失败: %+v", result)
	}
	if payload.Customer != "测试客户" || payload.Platform != license.PlatformName || payload.SchemaVersion != 1 {
		t.Fatalf("授权载荷字段异常: %+v", payload)
	}

	// 产品不匹配。
	_, result = license.VerifyAndEvaluate(licBytes, agentPublic, license.EvalOptions{ExpectedProduct: "webb-api"})
	if result.Code != license.CodeProductMismatch {
		t.Fatalf("产品不匹配应返回 license_product_mismatch，实际 %s", result.Code)
	}

	// 篡改任意字节：保持签名不变重新编码载荷。
	var env license.Envelope
	if err := json.Unmarshal(licBytes, &env); err != nil {
		t.Fatalf("授权文件信封解析失败: %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		t.Fatalf("载荷解码失败: %v", err)
	}
	tampered := bytes.Replace(data, []byte("测试客户"), []byte("篡改客户"), 1)
	if bytes.Equal(tampered, data) {
		t.Fatal("测试准备失败：未找到待篡改的客户名")
	}
	env.Data = base64.StdEncoding.EncodeToString(tampered)
	tamperedRaw, _ := json.Marshal(env)
	_, result = license.VerifyAndEvaluate(tamperedRaw, agentPublic, license.EvalOptions{ExpectedProduct: "agent-system"})
	if result.Code != license.CodeBadSignature {
		t.Fatalf("篡改后应返回 license_bad_signature，实际 %s", result.Code)
	}

	// 占位符公钥与损坏文件。
	_, result = license.VerifyAndEvaluate(licBytes, "REPLACE_WITH_PUBLIC_KEY", license.EvalOptions{ExpectedProduct: "agent-system"})
	if result.Code != license.CodeNoPublicKey {
		t.Fatalf("占位符公钥应返回 license_no_public_key，实际 %s", result.Code)
	}
	_, result = license.VerifyAndEvaluate([]byte(`{"data":"","sign":""}`), agentPublic, license.EvalOptions{})
	if result.Code != license.CodeMalformed {
		t.Fatalf("空信封应返回 license_malformed，实际 %s", result.Code)
	}

	// 回拨标记。
	_, result = license.VerifyAndEvaluate(licBytes, agentPublic, license.EvalOptions{
		ExpectedProduct: "agent-system", ClockRollback: true,
	})
	if result.Code != license.CodeClockRollback {
		t.Fatalf("时间回拨应返回 license_clock_rollback，实际 %s", result.Code)
	}

	// ---------- 签发：机器绑定 ----------
	boundCode := "0123456789abcdef0123456789abcdef"
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "绑定客户", "expiry_mode": "days", "days": 30,
		"machine_mode": "custom", "machine_code": boundCode,
	})
	bound := decode[licenseResponse](t, raw)
	if bound.MachineCode != boundCode {
		t.Fatalf("机器绑定授权字段异常: %+v", bound)
	}
	boundBytes := c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+bound.ID+"/download", nil)
	_, result = license.VerifyAndEvaluate(boundBytes, agentPublic, license.EvalOptions{
		ExpectedProduct: "agent-system", MachineCode: boundCode,
	})
	if result.Code != license.CodeOK {
		t.Fatalf("绑定机器授权应在匹配机器上通过，实际 %s", result.Code)
	}
	_, result = license.VerifyAndEvaluate(boundBytes, agentPublic, license.EvalOptions{
		ExpectedProduct: "agent-system", MachineCode: "ffffffffffffffffffffffffffffffff",
	})
	if result.Code != license.CodeMachineMismatch {
		t.Fatalf("换机器应返回 license_machine_mismatch，实际 %s", result.Code)
	}
	raw = c.mustStatus(http.StatusBadRequest, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "非法机器", "expiry_mode": "days", "days": 30,
		"machine_mode": "custom", "machine_code": "ABC",
	})
	if errorCode(t, raw) != "bad_request" {
		t.Fatalf("非法机器码应返回 bad_request: %s", raw)
	}

	// ---------- 签发：指定日期 ----------
	expireAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "到期客户", "expiry_mode": "date",
		"expire_at": expireAt.Format(time.RFC3339), "grace_days": 0,
	})
	dated := decode[licenseResponse](t, raw)
	datedBytes := c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+dated.ID+"/download", nil)
	_, result = license.VerifyAndEvaluate(datedBytes, agentPublic, license.EvalOptions{
		ExpectedProduct: "agent-system", Now: expireAt.Add(-time.Minute),
	})
	if result.Code != license.CodeOK {
		t.Fatalf("到期前应判定有效，实际 %s", result.Code)
	}
	_, result = license.VerifyAndEvaluate(datedBytes, agentPublic, license.EvalOptions{
		ExpectedProduct: "agent-system", Now: expireAt.Add(time.Minute),
	})
	if result.Code != license.CodeExpired {
		t.Fatalf("到期后应返回 license_expired，实际 %s", result.Code)
	}
	// 到期时间必须晚于当前时间。
	raw = c.mustStatus(http.StatusBadRequest, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "过去客户", "expiry_mode": "date",
		"expire_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	if errorCode(t, raw) != "bad_request" {
		t.Fatalf("过去的到期时间应被拒绝: %s", raw)
	}

	// 空客户名与不存在的项目。
	raw = c.mustStatus(http.StatusBadRequest, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "  ", "expiry_mode": "permanent",
	})
	if errorCode(t, raw) != "bad_request" {
		t.Fatalf("空客户名应返回 bad_request: %s", raw)
	}
	c.mustStatus(http.StatusNotFound, http.MethodPost, "/api/licenses", map[string]any{
		"project": "nope", "customer": "客户", "expiry_mode": "permanent",
	})

	// ---------- 续期 ----------
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "续期客户", "expiry_mode": "days", "days": 30,
		"grace_days": 7, "machine_mode": "any",
	})
	origin := decode[licenseResponse](t, raw)
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/licenses/"+origin.ID+"/renew", map[string]any{
		"expiry_mode": "days", "days": 60,
	})
	renewed := decode[licenseResponse](t, raw)
	if renewed.ID == origin.ID || renewed.RenewFrom != origin.ID || renewed.Customer != "续期客户" {
		t.Fatalf("续期结果异常: %+v", renewed)
	}
	if renewed.GraceDays != 7 {
		t.Fatalf("续期应继承宽限期，实际 %d", renewed.GraceDays)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+origin.ID, nil)
	oldDetail := decode[struct {
		License licenseResponse `json:"license"`
	}](t, raw)
	if oldDetail.License.Status != models.LicenseSuperseded || oldDetail.License.EffectiveStatus != models.LicenseSuperseded {
		t.Fatalf("原授权未被标记为 superseded: %+v", oldDetail.License)
	}
	// 已续期替代的授权不能再吊销。
	c.mustStatus(http.StatusConflict, http.MethodPost, "/api/licenses/"+origin.ID+"/revoke", nil)

	// ---------- 吊销 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodPost, "/api/licenses/"+perm.ID+"/revoke",
		map[string]string{"reason": "客户退订"})
	revoked := decode[licenseResponse](t, raw)
	if revoked.Status != models.LicenseRevoked {
		t.Fatalf("吊销后状态异常: %+v", revoked)
	}
	assertNoPrivateKey(t, "吊销响应", raw)

	raw = c.mustStatus(http.StatusConflict, http.MethodGet, "/api/licenses/"+perm.ID+"/download", nil)
	if errorCode(t, raw) != "conflict" {
		t.Fatalf("吊销后下载应返回 409: %s", raw)
	}
	c.mustStatus(http.StatusConflict, http.MethodPost, "/api/licenses/"+perm.ID+"/renew", map[string]any{
		"expiry_mode": "days", "days": 30,
	})
	c.mustStatus(http.StatusConflict, http.MethodPost, "/api/licenses/"+perm.ID+"/revoke", nil)

	// 下载计数。
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+bound.ID, nil)
	boundDetail := decode[struct {
		License licenseResponse `json:"license"`
	}](t, raw)
	if boundDetail.License.DownloadCount != 1 {
		t.Fatalf("下载计数应为 1，实际 %d", boundDetail.License.DownloadCount)
	}

	// ---------- 列表筛选与分页 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses?project=agent-system&page=1&size=100", nil)
	all := decode[licensePage](t, raw)
	if all.Total != 5 {
		t.Fatalf("项目授权总数应为 5，实际 %d", all.Total)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses?customer="+url.QueryEscape("测试"), nil)
	filtered := decode[licensePage](t, raw)
	if filtered.Total != 1 || filtered.Items[0].ID != perm.ID {
		t.Fatalf("客户筛选异常: %+v", filtered)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses?status=revoked", nil)
	revokedPage := decode[licensePage](t, raw)
	if revokedPage.Total != 1 || revokedPage.Items[0].ID != perm.ID {
		t.Fatalf("状态筛选异常: %+v", revokedPage)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses?status=superseded", nil)
	supersededPage := decode[licensePage](t, raw)
	if supersededPage.Total != 1 || supersededPage.Items[0].ID != origin.ID {
		t.Fatalf("续期替代筛选异常: %+v", supersededPage)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses?page=3&size=2", nil)
	paged := decode[licensePage](t, raw)
	if paged.Total != 5 || paged.Page != 3 || len(paged.Items) != 1 {
		t.Fatalf("分页结果异常: %+v", paged)
	}

	// ---------- 密钥管理 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/keys", nil)
	assertNoPrivateKey(t, "密钥列表", raw)
	var keys struct {
		Items []models.Keypair `json:"items"`
		Total int              `json:"total"`
	}
	keys = decode[struct {
		Items []models.Keypair `json:"items"`
		Total int              `json:"total"`
	}](t, raw)
	if keys.Total != 2 {
		t.Fatalf("密钥总数应为 2，实际 %d", keys.Total)
	}
	for _, k := range keys.Items {
		if k.Fingerprint == "" {
			t.Fatalf("密钥 %s 缺少指纹", k.ID)
		}
	}

	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/keys/rotate", map[string]string{"project": "agent-system"})
	assertNoPrivateKey(t, "密钥轮换", raw)
	var rotated models.Keypair
	rotated = decode[models.Keypair](t, raw)
	if rotated.Fingerprint == "" || rotated.ProjectID != "agent-system" {
		t.Fatalf("密钥轮换响应异常: %+v", rotated)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/projects/agent-system", nil)
	newDetail := decode[projectDetail](t, raw)
	if newDetail.PublicKey == agentPublic || newDetail.Project.KeyFingerprint == detail.Project.KeyFingerprint {
		t.Fatal("轮换后公钥未变化")
	}
	// 旧授权在新公钥下应验签失败。
	_, result = license.VerifyAndEvaluate(licBytes, newDetail.PublicKey, license.EvalOptions{ExpectedProduct: "agent-system"})
	if result.Code != license.CodeBadSignature {
		t.Fatalf("轮换密钥后旧授权应验签失败，实际 %s", result.Code)
	}
	// 轮换后新签发的授权应可用新公钥验证。
	raw = c.mustStatus(http.StatusCreated, http.MethodPost, "/api/licenses", map[string]any{
		"project": "agent-system", "customer": "轮换客户", "expiry_mode": "permanent",
	})
	afterRotate := decode[licenseResponse](t, raw)
	afterBytes := c.mustStatus(http.StatusOK, http.MethodGet, "/api/licenses/"+afterRotate.ID+"/download", nil)
	_, result = license.VerifyAndEvaluate(afterBytes, newDetail.PublicKey, license.EvalOptions{ExpectedProduct: "agent-system"})
	if result.Code != license.CodeOK {
		t.Fatalf("轮换后新授权应通过校验，实际 %s", result.Code)
	}

	// ---------- 项目更新与删除 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodPatch, "/api/projects/webb-api", map[string]any{"name": "Webb API 2"})
	updated := decode[models.Project](t, raw)
	if updated.Name != "Webb API 2" {
		t.Fatalf("项目更新未生效: %+v", updated)
	}
	c.mustStatus(http.StatusOK, http.MethodDelete, "/api/projects/webb-api", nil)
	c.mustStatus(http.StatusNotFound, http.MethodGet, "/api/projects/webb-api", nil)
	// 有授权记录的项目不允许删除。
	raw = c.mustStatus(http.StatusConflict, http.MethodDelete, "/api/projects/agent-system", nil)
	if errorCode(t, raw) != "conflict" {
		t.Fatalf("有授权的项目应拒绝删除: %s", raw)
	}

	// ---------- 审计日志 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/audit-logs?action=license.issue&page=1&size=100", nil)
	issuance := decode[auditPage](t, raw)
	if issuance.Total < 6 {
		t.Fatalf("签发审计条数不足: %d", issuance.Total)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/audit-logs?action=license.revoke", nil)
	revocations := decode[auditPage](t, raw)
	if revocations.Total != 1 {
		t.Fatalf("吊销审计条数异常: %d", revocations.Total)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/audit-logs?action=license.download", nil)
	if decode[auditPage](t, raw).Total < 4 {
		t.Fatalf("下载审计条数不足: %s", raw)
	}
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/audit-logs?operator=admin", nil)
	byOperator := decode[auditPage](t, raw)
	if byOperator.Total == 0 {
		t.Fatal("按操作人筛选审计无结果")
	}
	today := time.Now().UTC().Format("2006-01-02")
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/audit-logs?from="+today+"&to="+today, nil)
	if decode[auditPage](t, raw).Total == 0 {
		t.Fatal("按日期筛选审计无结果")
	}

	// ---------- 看板 ----------
	raw = c.mustStatus(http.StatusOK, http.MethodGet, "/api/dashboard", nil)
	var dash struct {
		Total      int `json:"total"`
		Active     int `json:"active"`
		Revoked    int `json:"revoked"`
		Superseded int `json:"superseded"`
		Projects   int `json:"projects"`
	}
	dash = decode[struct {
		Total      int `json:"total"`
		Active     int `json:"active"`
		Revoked    int `json:"revoked"`
		Superseded int `json:"superseded"`
		Projects   int `json:"projects"`
	}](t, raw)
	if dash.Total != 6 || dash.Revoked != 1 || dash.Superseded != 1 || dash.Projects != 1 {
		t.Fatalf("看板统计异常: %+v", dash)
	}

	// ---------- 令牌与静态资源 ----------
	c.token = "invalid.token.value"
	if code, _, _ := c.do(http.MethodGet, "/api/dashboard", nil); code != http.StatusUnauthorized {
		t.Fatalf("非法令牌应返回 401，实际 %d", code)
	}
	c.token = ""
	code, body, header := c.do(http.MethodGet, "/", nil)
	if code != http.StatusOK || !strings.Contains(header.Get("Content-Type"), "text/html") {
		t.Fatalf("UI 入口异常: code=%d type=%s", code, header.Get("Content-Type"))
	}
	if !bytes.Contains(body, []byte("统一授权平台")) {
		t.Fatalf("UI 入口内容异常: %s", body)
	}
	code, _, _ = c.do(http.MethodGet, "/assets/js/app.js", nil)
	if code != http.StatusOK {
		t.Fatalf("静态资源访问失败: %d", code)
	}
	// 未知前端路由回退到入口页（hash 路由）。
	code, body, _ = c.do(http.MethodGet, "/licenses/some-id", nil)
	if code != http.StatusOK || !bytes.Contains(body, []byte("统一授权平台")) {
		t.Fatalf("SPA 回退异常: code=%d", code)
	}
}

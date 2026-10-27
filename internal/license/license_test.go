package license

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	priv, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	return priv
}

func pemOf(t *testing.T, priv *rsa.PrivateKey) string {
	t.Helper()
	s, err := PublicKeyPEM(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func samplePayload() *Payload {
	exp := NewTime(time.Now().Add(365 * 24 * time.Hour))
	return &Payload{
		SchemaVersion:   SchemaVersion,
		LicenseID:       "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
		Platform:        PlatformName,
		Product:         "agent-system",
		ProductDisplay:  "Agent System",
		Customer:        "某某有限公司",
		CustomerContact: "张三 / zhangsan@example.com",
		IssuedAt:        Now(),
		ExpireTime:      &exp,
		GraceDays:       7,
		MachineCode:     MachineCodeWildcard,
		InstanceID:      "",
		Features:        map[string]any{"max_users": 50},
		IssuedBy:        "admin",
		Note:            "单元测试",
	}
}

func signRaw(t *testing.T, p *Payload, priv *rsa.PrivateKey) []byte {
	t.Helper()
	pb, err := EncodePayload(p)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := SignBytes(pb, priv)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(Envelope{
		Data: base64.StdEncoding.EncodeToString(pb),
		Sign: base64.StdEncoding.EncodeToString(sig),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSignAndVerifyOK(t *testing.T) {
	priv := testKey(t)
	raw, err := SignPayload(samplePayload(), priv)
	if err != nil {
		t.Fatal(err)
	}
	p, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if !res.Valid || res.Code != CodeOK {
		t.Fatalf("期望有效，实际 %+v", res)
	}
	if p.Customer != "某某有限公司" || p.GraceDays != 7 {
		t.Fatalf("载荷字段丢失: %+v", p)
	}
	if res.RemainingDays <= 360 || res.RemainingDays > 366 {
		t.Fatalf("剩余天数异常: %d", res.RemainingDays)
	}
}

func TestTamperedPayloadRejected(t *testing.T) {
	priv := testKey(t)
	raw, _ := SignPayload(samplePayload(), priv)
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	pb, _ := base64.StdEncoding.DecodeString(env.Data)
	pb = append([]byte{}, pb...)
	pb[10] ^= 0x01
	env.Data = base64.StdEncoding.EncodeToString(pb)
	tampered, _ := json.Marshal(env)

	_, res := VerifyAndEvaluate(tampered, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeBadSignature || res.Valid {
		t.Fatalf("篡改后应验签失败，实际 %+v", res)
	}
}

func TestWrongKeyRejected(t *testing.T) {
	priv := testKey(t)
	other := testKey(t)
	raw, _ := SignPayload(samplePayload(), priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, other), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeBadSignature {
		t.Fatalf("错误公钥应验签失败，实际 %+v", res)
	}
}

func TestExpiredRejected(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	exp := NewTime(time.Now().Add(-48 * time.Hour))
	p.ExpireTime = &exp
	p.GraceDays = 0
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeExpired || res.Valid {
		t.Fatalf("过期应拒绝，实际 %+v", res)
	}
}

func TestGraceAccepted(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	exp := NewTime(time.Now().Add(-24 * time.Hour))
	p.ExpireTime = &exp
	p.GraceDays = 7
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if !res.Valid || res.Code != CodeGrace || !res.InGrace {
		t.Fatalf("宽限期内应有效，实际 %+v", res)
	}
}

func TestGraceExpiredRejected(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	exp := NewTime(time.Now().Add(-10 * 24 * time.Hour))
	p.ExpireTime = &exp
	p.GraceDays = 7
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeExpired {
		t.Fatalf("宽限期结束后应拒绝，实际 %+v", res)
	}
}

func TestPermanentLicense(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.ExpireTime = nil
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if !res.Valid || res.Code != CodeOK || res.RemainingDays != -1 {
		t.Fatalf("永久授权应有效，实际 %+v", res)
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	pb, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pb), `"expire_time":null`) {
		t.Fatalf("永久授权应序列化为 null: %s", pb)
	}
}

func TestProductMismatch(t *testing.T) {
	priv := testKey(t)
	raw, _ := SignPayload(samplePayload(), priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "webb-api"})
	if res.Code != CodeProductMismatch || res.Valid {
		t.Fatalf("产品不匹配应拒绝，实际 %+v", res)
	}
}

func TestPlatformMismatch(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.Platform = "another-platform"
	raw := signRaw(t, p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeProductMismatch {
		t.Fatalf("签发方不符应拒绝，实际 %+v", res)
	}
}

func TestMachineMismatch(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.MachineCode = "00112233445566778899aabbccddeeff"
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{
		ExpectedProduct: "agent-system",
		MachineCode:     "ffffffffffffffffffffffffffffffff",
	})
	if res.Code != CodeMachineMismatch {
		t.Fatalf("机器不匹配应拒绝，实际 %+v", res)
	}
}

func TestMachineMatched(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.MachineCode = "00112233445566778899aabbccddeeff"
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{
		ExpectedProduct: "agent-system",
		MachineCode:     "00112233445566778899aabbccddeeff",
	})
	if !res.Valid {
		t.Fatalf("机器匹配应放行，实际 %+v", res)
	}
}

func TestLegacyMachineCodeAccepted(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.MachineCode = "LEGACY-64-HEX-VALUE"
	raw, _ := SignPayload(p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{
		ExpectedProduct:    "agent-system",
		MachineCode:        "00112233445566778899aabbccddeeff",
		LegacyMachineCodes: []string{"LEGACY-64-HEX-VALUE"},
	})
	if !res.Valid {
		t.Fatalf("过渡期旧机器码应放行，实际 %+v", res)
	}
}

func TestEmptyMachineCodeMalformed(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.MachineCode = ""
	raw := signRaw(t, p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeMalformed {
		t.Fatalf("空机器码应判为格式损坏，实际 %+v", res)
	}
}

func TestUnsupportedSchemaVersion(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	p.SchemaVersion = 2
	raw := signRaw(t, p, priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeMalformed {
		t.Fatalf("高版本 schema 应拒绝，实际 %+v", res)
	}
}

func TestMalformedEnvelope(t *testing.T) {
	priv := testKey(t)
	cases := [][]byte{
		[]byte("not-json"),
		[]byte(`{"data":"","sign":""}`),
		[]byte(`{"data":"!!!","sign":"!!!"}`),
	}
	for _, raw := range cases {
		_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
		if res.Code != CodeMalformed {
			t.Fatalf("损坏文件应判为 malformed，实际 %+v", res)
		}
	}
}

func TestPublicKeyPKCS1(t *testing.T) {
	priv := testKey(t)
	der := x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: der}))
	if _, err := ParsePublicKeyPEM(pemText); err != nil {
		t.Fatalf("应支持 PKCS#1 公钥: %v", err)
	}
	raw, _ := SignPayload(samplePayload(), priv)
	_, res := VerifyAndEvaluate(raw, pemText, EvalOptions{ExpectedProduct: "agent-system"})
	if !res.Valid {
		t.Fatalf("PKCS#1 公钥验签应通过，实际 %+v", res)
	}
}

func TestPublicKeyPlaceholder(t *testing.T) {
	priv := testKey(t)
	raw, _ := SignPayload(samplePayload(), priv)
	_, res := VerifyAndEvaluate(raw, "-----BEGIN PUBLIC KEY-----\nREPLACE_WITH_YOUR_PUBLIC_KEY\n-----END PUBLIC KEY-----", EvalOptions{ExpectedProduct: "agent-system"})
	if res.Code != CodeNoPublicKey {
		t.Fatalf("占位符公钥应拒绝，实际 %+v", res)
	}
}

func TestMachineCodeAlgorithm(t *testing.T) {
	got := ComputeMachineCode("test-machine")
	if len(got) != 32 {
		t.Fatalf("机器码长度应为 32，实际 %d (%s)", len(got), got)
	}
	// 固定向量由规范算法独立计算：SHA-256("ULP-LICENSE-V1:"+raw) 前 16 字节。
	sum := sha256.Sum256([]byte("ULP-LICENSE-V1:test-machine"))
	want := hex.EncodeToString(sum[:16])
	if got != want {
		t.Fatalf("机器码算法不符: got=%s want=%s", got, want)
	}
	if ComputeMachineCode("a") == ComputeMachineCode("b") {
		t.Fatal("不同原始标识不应产生相同机器码")
	}
}

func TestClockRollbackResult(t *testing.T) {
	priv := testKey(t)
	raw, _ := SignPayload(samplePayload(), priv)
	_, res := VerifyAndEvaluate(raw, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system", ClockRollback: true})
	if res.Code != CodeClockRollback {
		t.Fatalf("回拨应拒绝，实际 %+v", res)
	}
}

func TestDeterministicSerialization(t *testing.T) {
	p := samplePayload()
	b1, err := EncodePayload(p)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := EncodePayload(p)
	if string(b1) != string(b2) {
		t.Fatal("同一载荷两次序列化结果应完全一致")
	}
	if !strings.HasPrefix(string(b1), `{"schema_version":1,"license_id"`) {
		t.Fatalf("字段顺序必须固定: %s", b1)
	}
	priv := testKey(t)
	f1, _ := SignPayload(p, priv)
	f2, _ := SignPayload(p, priv)
	if string(f1) != string(f2) {
		t.Fatal("同一载荷两次签发结果应完全一致")
	}
}

func TestUnknownFieldsIgnored(t *testing.T) {
	priv := testKey(t)
	p := samplePayload()
	raw := signRaw(t, p, priv)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	env["future_field"] = "ignored"
	raw2, _ := json.Marshal(env)
	_, res := VerifyAndEvaluate(raw2, pemOf(t, priv), EvalOptions{ExpectedProduct: "agent-system"})
	if !res.Valid {
		t.Fatalf("额外顶层字段应被忽略，实际 %+v", res)
	}
}

func TestClockGuard(t *testing.T) {
	dir := t.TempDir()
	g := NewClockGuard(dir+"/clock.dat", []byte("secret"))
	base := time.Now().UTC()
	if g.Check(base) {
		t.Fatal("首次运行不应判回拨")
	}
	if g.Check(base.Add(time.Minute)) {
		t.Fatal("时间前进不应判回拨")
	}
	if !g.Check(base.Add(-2 * time.Hour)) {
		t.Fatal("大幅回拨应被检出")
	}
	// 篡改检查点不应误判为回拨
	if err := os.WriteFile(dir+"/clock.dat", []byte("1|deadbeef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if g.Check(base.Add(-3 * time.Hour)) {
		t.Fatal("检查点被篡改时应视为无基准，不误判")
	}
}

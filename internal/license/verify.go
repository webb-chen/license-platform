package license

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// EvalOptions 是业务字段校验参数。
type EvalOptions struct {
	// ExpectedProduct 是当前程序期望的产品标识，必须与载荷中的 product 完全一致。
	ExpectedProduct string
	// MachineCode 是本机机器码（统一算法或环境变量覆盖）。
	MachineCode string
	// LegacyMachineCodes 是过渡期允许的旧格式机器码。
	LegacyMachineCodes []string
	// Now 用于测试注入当前时间；零值表示使用系统时间。
	Now time.Time
	// ClockRollback 由调用方的回拨检测给出。
	ClockRollback bool
}

// Result 是授权校验结果，与统一状态码词表对应。
type Result struct {
	Code          string `json:"code"`
	Valid         bool   `json:"valid"`
	InGrace       bool   `json:"in_grace"`
	RemainingDays int    `json:"remaining_days"` // -1 表示永久授权
	Message       string `json:"message"`
}

// ParseAndVerify 解析信封并对原始字节验签，验签通过后才解析载荷。
func ParseAndVerify(raw []byte, publicKeyPEM string) (*Payload, []byte, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, newError(CodeMalformed, "授权文件不是合法 JSON")
	}
	if strings.TrimSpace(env.Data) == "" || strings.TrimSpace(env.Sign) == "" {
		return nil, nil, newError(CodeMalformed, "授权文件缺少 data 或 sign 字段")
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, nil, newError(CodeMalformed, "data 不是合法的 base64")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Sign)
	if err != nil {
		return nil, nil, newError(CodeMalformed, "sign 不是合法的 base64")
	}
	if HasPlaceholder(publicKeyPEM) {
		return nil, nil, newError(CodeNoPublicKey, "内置公钥仍是占位符，请替换为平台签发的公钥")
	}
	pub, err := ParsePublicKeyPEM(publicKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	if err := VerifyBytes(payloadBytes, sig, pub); err != nil {
		return nil, nil, err
	}
	var p Payload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return nil, nil, newError(CodeMalformed, "载荷不是合法 JSON")
	}
	return &p, payloadBytes, nil
}

// VerifyAndEvaluate 完成「解析 → 验签 → 业务校验」全流程。
func VerifyAndEvaluate(raw []byte, publicKeyPEM string, opt EvalOptions) (*Payload, Result) {
	p, _, err := ParseAndVerify(raw, publicKeyPEM)
	if err != nil {
		return nil, Result{Code: CodeOf(err), Valid: false, Message: err.Error()}
	}
	return p, Evaluate(p, opt)
}

// Evaluate 按规范 §6 的顺序校验业务字段。
func Evaluate(p *Payload, opt EvalOptions) Result {
	if p.SchemaVersion != SchemaVersion {
		return Result{Code: CodeMalformed, Message: fmt.Sprintf("不支持的 schema_version: %d", p.SchemaVersion)}
	}
	if p.Platform != PlatformName {
		return Result{Code: CodeProductMismatch, Message: "授权不是由统一授权平台签发"}
	}
	if p.Product == "" || (opt.ExpectedProduct != "" && !strings.EqualFold(p.Product, opt.ExpectedProduct)) {
		return Result{Code: CodeProductMismatch, Message: fmt.Sprintf("授权项目为 %q，与当前程序 %q 不匹配", p.Product, opt.ExpectedProduct)}
	}
	if p.MachineCode == "" {
		return Result{Code: CodeMalformed, Message: "machine_code 缺失（通用授权必须显式写 \"*\"）"}
	}
	if p.MachineCode != MachineCodeWildcard {
		matched := opt.MachineCode != "" && p.MachineCode == opt.MachineCode
		if !matched {
			for _, legacy := range opt.LegacyMachineCodes {
				if legacy != "" && p.MachineCode == legacy {
					matched = true
					break
				}
			}
		}
		if !matched {
			return Result{Code: CodeMachineMismatch, Message: "授权绑定的机器码与本机不匹配"}
		}
	}
	if opt.ClockRollback {
		return Result{Code: CodeClockRollback, Message: "检测到系统时间回拨，请校准时间后重试"}
	}

	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	if p.IsPermanent() {
		return Result{Code: CodeOK, Valid: true, RemainingDays: -1, Message: "永久授权，长期有效"}
	}
	expire := p.ExpireTime.UTC()
	if now.Before(expire) {
		days := int(math.Ceil(expire.Sub(now).Hours() / 24))
		if days < 0 {
			days = 0
		}
		return Result{
			Code:          CodeOK,
			Valid:         true,
			RemainingDays: days,
			Message:       "授权有效，到期时间 " + expire.Format("2006-01-02 15:04") + " (UTC)",
		}
	}
	grace := p.GraceDays
	if grace < 0 {
		grace = 0
	}
	graceEnd := expire.AddDate(0, 0, grace)
	if grace > 0 && !now.After(graceEnd) {
		return Result{
			Code:          CodeGrace,
			Valid:         true,
			InGrace:       true,
			RemainingDays: 0,
			Message:       "授权已到期，处于宽限期，请尽快续期（宽限至 " + graceEnd.Format("2006-01-02 15:04") + " UTC）",
		}
	}
	return Result{Code: CodeExpired, Valid: false, Message: "授权已到期且宽限期结束"}
}

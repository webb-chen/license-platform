// Command verify 是离线授权校验工具，用于运维排查与客户现场自检。
//
// 用法示例：
//
//	verify -lic demo-客户-xxx.lic -pub demo-public.pem -product demo -clock ./data/clock.dat
//
// 输出 JSON 报告；授权有效（ok / license_grace）退出码 0，否则 2。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"unified-license-platform/internal/license"
)

type report struct {
	Result      license.Result   `json:"result"`
	License     *license.Payload `json:"license,omitempty"`
	MachineCode string           `json:"machine_code"`
	ClockCheck  bool             `json:"clock_check_enabled"`
}

func main() {
	licPath := flag.String("lic", "", "授权文件路径（.lic）")
	pubPath := flag.String("pub", "", "公钥 PEM 文件路径")
	product := flag.String("product", "", "期望的 product 标识（留空则不校验）")
	machine := flag.String("machine", "", "本机机器码（留空则自动计算）")
	legacy := flag.String("legacy", "", "允许的旧格式机器码，逗号分隔")
	clockPath := flag.String("clock", "", "时间回拨检查点文件（留空则跳过检测）")
	clockSecret := flag.String("clock-secret", "", "检查点 HMAC 密钥（留空则由机器码派生）")
	printMachine := flag.Bool("print-machine", false, "仅输出本机机器码后退出")
	flag.Parse()

	if *printMachine {
		fmt.Println(license.LocalMachineCode())
		return
	}
	if strings.TrimSpace(*licPath) == "" || strings.TrimSpace(*pubPath) == "" {
		flag.Usage()
		os.Exit(2)
	}

	code := *machine
	if code == "" {
		code = license.LocalMachineCode()
	}

	raw, err := os.ReadFile(*licPath)
	if err != nil {
		fail("读取授权文件失败: " + err.Error())
	}
	pub, err := os.ReadFile(*pubPath)
	if err != nil {
		fail("读取公钥文件失败: " + err.Error())
	}

	opts := license.EvalOptions{
		ExpectedProduct:    strings.TrimSpace(*product),
		MachineCode:        code,
		LegacyMachineCodes: splitLegacy(*legacy),
		Now:                time.Now(),
		ClockRollback:      false,
	}
	clockEnabled := false
	if strings.TrimSpace(*clockPath) != "" {
		clockEnabled = true
		secret := *clockSecret
		if secret == "" {
			secret = "ULP-CLOCK:" + code
		}
		guard := license.NewClockGuard(*clockPath, []byte(secret))
		opts.ClockRollback = guard.Check(opts.Now)
	}

	payload, result := license.VerifyAndEvaluate(raw, string(pub), opts)
	out := report{Result: result, License: payload, MachineCode: code, ClockCheck: clockEnabled}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))

	if !result.Valid {
		os.Exit(2)
	}
}

func splitLegacy(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func fail(message string) {
	encoded, _ := json.MarshalIndent(map[string]any{
		"result": map[string]any{"valid": false, "code": license.CodeMalformed, "message": message},
	}, "", "  ")
	fmt.Println(string(encoded))
	os.Exit(2)
}

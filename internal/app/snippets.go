package app

import "strings"

// 片段模板中的反引号占位符：模板本身用 Go 反引号字符串承载。
const bt = "__BT__"

func render(tpl, product, publicKey string) string {
	return strings.NewReplacer(
		"__PRODUCT__", product,
		"__PUBLIC_KEY__", publicKey,
		bt, "`",
	).Replace(tpl)
}

// buildGoSnippet 生成可直接编译的 Go 离线验签片段。
func buildGoSnippet(product, publicKey string) string {
	return render(goSnippet, product, publicKey)
}

// buildPythonSnippet 生成 Python 3 离线验签片段。
func buildPythonSnippet(product, publicKey string) string {
	return render(pythonSnippet, product, publicKey)
}

// buildNodeSnippet 生成 Node.js 离线验签片段。
func buildNodeSnippet(product, publicKey string) string {
	return render(nodeSnippet, product, publicKey)
}

const goSnippet = `// 自动生成：统一授权平台接入片段（Go）
// 1) 将本文件放入业务项目（包名可自行调整）；
// 2) 启动时与每 5 分钟定时调用 VerifyFile()；
// 3) 完整规范见 docs/接入统一授权平台-适配开发规范.md。
package licensecheck

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"
)

const LicenseProduct = "__PRODUCT__"

const licensePublicKeyPEM = __BT____PUBLIC_KEY____BT__

type licenseEnvelope struct {
	Data string __BT__json:"data"__BT__
	Sign string __BT__json:"sign"__BT__
}

type licensePayload struct {
	SchemaVersion int        __BT__json:"schema_version"__BT__
	Platform      string     __BT__json:"platform"__BT__
	Product       string     __BT__json:"product"__BT__
	ExpireTime    *time.Time __BT__json:"expire_time"__BT__
	GraceDays     int        __BT__json:"grace_days"__BT__
	MachineCode   string     __BT__json:"machine_code"__BT__
}

// MachineCode 计算本机机器码：SHA-256("ULP-LICENSE-V1:"+raw) 前 16 字节。
func MachineCode() string {
	if v := os.Getenv("LICENSE_MACHINE_CODE"); v != "" {
		return v
	}
	raw, _ := os.ReadFile("/etc/machine-id")
	if len(raw) == 0 {
		raw = []byte("unknown")
	}
	sum := sha256.Sum256([]byte("ULP-LICENSE-V1:" + string(raw)))
	return hex.EncodeToString(sum[:16])
}

// VerifyFile 离线校验授权文件，返回 nil 表示有效。
func VerifyFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("license_missing: %w", err)
	}
	var env licenseEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return errors.New("license_malformed")
	}
	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return errors.New("license_malformed")
	}
	sign, err := base64.StdEncoding.DecodeString(env.Sign)
	if err != nil {
		return errors.New("license_malformed")
	}
	block, _ := pem.Decode([]byte(licensePublicKeyPEM))
	if block == nil {
		return errors.New("license_no_public_key")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return errors.New("license_no_public_key")
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return errors.New("license_no_public_key")
	}
	sum := sha256.Sum256(data)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sign); err != nil {
		return errors.New("license_bad_signature")
	}
	var p licensePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return errors.New("license_malformed")
	}
	if p.SchemaVersion != 1 || p.Platform != "unified-license-platform" {
		return errors.New("license_malformed")
	}
	if p.Product != LicenseProduct {
		return errors.New("license_product_mismatch")
	}
	if p.MachineCode != "*" && p.MachineCode != MachineCode() {
		return errors.New("license_machine_mismatch")
	}
	if p.ExpireTime != nil {
		deadline := p.ExpireTime.Add(time.Duration(p.GraceDays) * 24 * time.Hour)
		if time.Now().After(deadline) {
			return errors.New("license_expired")
		}
	}
	return nil
}
`

const pythonSnippet = `# 自动生成：统一授权平台接入片段（Python 3）
# 依赖：pip install cryptography
# 完整规范见 docs/接入统一授权平台-适配开发规范.md
import base64
import hashlib
import json
import os
import time
from datetime import datetime

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding

LICENSE_PRODUCT = "__PRODUCT__"
LICENSE_PUBLIC_KEY = """__PUBLIC_KEY__"""


def machine_code() -> str:
    override = os.getenv("LICENSE_MACHINE_CODE")
    if override:
        return override
    try:
        raw = open("/etc/machine-id", "rb").read()
    except OSError:
        raw = b"unknown"
    return hashlib.sha256(b"ULP-LICENSE-V1:" + raw).hexdigest()[:32]


def verify_file(path: str) -> str:
    try:
        env = json.load(open(path, "rb"))
    except Exception:
        return "license_missing"
    try:
        data = base64.b64decode(env["data"])
        sign = base64.b64decode(env["sign"])
    except Exception:
        return "license_malformed"
    try:
        key = serialization.load_pem_public_key(LICENSE_PUBLIC_KEY.encode())
        key.verify(sign, data, padding.PKCS1v15(), hashes.SHA256())
    except Exception:
        return "license_bad_signature"
    try:
        payload = json.loads(data)
    except Exception:
        return "license_malformed"
    if payload.get("schema_version") != 1 or payload.get("platform") != "unified-license-platform":
        return "license_malformed"
    if payload.get("product") != LICENSE_PRODUCT:
        return "license_product_mismatch"
    if payload.get("machine_code") != "*" and payload.get("machine_code") != machine_code():
        return "license_machine_mismatch"
    expire = payload.get("expire_time")
    if expire:
        deadline = datetime.fromisoformat(expire.replace("Z", "+00:00")).timestamp()
        deadline += int(payload.get("grace_days", 0)) * 86400
        if time.time() > deadline:
            return "license_expired"
    return "ok"
`

const nodeSnippet = `// 自动生成：统一授权平台接入片段（Node.js / NestJS）
// 完整规范见 docs/接入统一授权平台-适配开发规范.md
const crypto = require('crypto');
const fs = require('fs');

const LICENSE_PRODUCT = '__PRODUCT__';
const LICENSE_PUBLIC_KEY = __BT____PUBLIC_KEY____BT__;

function machineCode() {
  if (process.env.LICENSE_MACHINE_CODE) return process.env.LICENSE_MACHINE_CODE;
  let raw = 'unknown';
  try {
    raw = fs.readFileSync('/etc/machine-id', 'utf8');
  } catch (_) {}
  return crypto.createHash('sha256').update('ULP-LICENSE-V1:' + raw).digest('hex').slice(0, 32);
}

function verifyFile(file) {
  let env;
  try {
    env = JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch (_) {
    return 'license_missing';
  }
  let data, sign;
  try {
    data = Buffer.from(env.data, 'base64');
    sign = Buffer.from(env.sign, 'base64');
  } catch (_) {
    return 'license_malformed';
  }
  let ok = false;
  try {
    ok = crypto.verify('sha256', data, LICENSE_PUBLIC_KEY, sign);
  } catch (_) {
    return 'license_no_public_key';
  }
  if (!ok) return 'license_bad_signature';

  let payload;
  try {
    payload = JSON.parse(data.toString('utf8'));
  } catch (_) {
    return 'license_malformed';
  }
  if (payload.schema_version !== 1 || payload.platform !== 'unified-license-platform') {
    return 'license_malformed';
  }
  if (payload.product !== LICENSE_PRODUCT) return 'license_product_mismatch';
  if (payload.machine_code !== '*' && payload.machine_code !== machineCode()) {
    return 'license_machine_mismatch';
  }
  if (payload.expire_time) {
    const deadline = Date.parse(payload.expire_time) + (payload.grace_days || 0) * 86400000;
    if (Date.now() > deadline) return 'license_expired';
  }
  return 'ok';
}
`

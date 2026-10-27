package license

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"sync"
)

const machineCodePrefix = "ULP-LICENSE-V1:"

// ComputeMachineCode 按统一算法计算机器码：
// SHA-256("ULP-LICENSE-V1:" + raw) 的前 16 字节 → 32 位小写 hex。
func ComputeMachineCode(raw string) string {
	sum := sha256.Sum256([]byte(machineCodePrefix + raw))
	return hex.EncodeToString(sum[:16])
}

// RawMachineIdentity 按规范顺序取第一个非空的机器原始标识。
func RawMachineIdentity() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v
			}
		}
	}
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
				continue
			}
			return iface.HardwareAddr.String()
		}
	}
	return "unknown"
}

var (
	localOnce sync.Once
	localCode string
)

// LocalMachineCode 返回本机机器码。支持 LICENSE_MACHINE_CODE 环境变量覆盖（容器场景）。
func LocalMachineCode() string {
	localOnce.Do(func() {
		if v := strings.TrimSpace(os.Getenv("LICENSE_MACHINE_CODE")); v != "" {
			localCode = v
			return
		}
		localCode = ComputeMachineCode(RawMachineIdentity())
	})
	return localCode
}

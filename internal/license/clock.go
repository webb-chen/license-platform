package license

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClockGuard 提供带 HMAC 的时间回拨检测，检查点文件应位于持久化卷。
type ClockGuard struct {
	Path      string
	Secret    []byte
	Tolerance time.Duration
	mu        sync.Mutex
}

// NewClockGuard 创建回拨检测器；默认容差 10 分钟。
func NewClockGuard(path string, secret []byte) *ClockGuard {
	return &ClockGuard{Path: path, Secret: secret, Tolerance: 10 * time.Minute}
}

func (g *ClockGuard) mac(unix int64) string {
	h := hmac.New(sha256.New, g.Secret)
	fmt.Fprintf(h, "%d", unix)
	return hex.EncodeToString(h.Sum(nil))
}

// Check 检测是否发生回拨，并在时间前进时更新检查点。
// 返回 true 表示检测到时间回拨。
func (g *ClockGuard) Check(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Path == "" {
		return false
	}
	now = now.UTC()
	if last, ok := g.read(); ok {
		if now.Add(g.Tolerance).Before(last) {
			return true
		}
		if !now.After(last) {
			return false
		}
	}
	g.write(now)
	return false
}

func (g *ClockGuard) read() (time.Time, bool) {
	raw, err := os.ReadFile(g.Path)
	if err != nil {
		return time.Time{}, false
	}
	parts := strings.Split(strings.TrimSpace(string(raw)), "|")
	if len(parts) != 2 {
		return time.Time{}, false
	}
	unix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	if !hmac.Equal([]byte(parts[1]), []byte(g.mac(unix))) {
		// 检查点被篡改：视为无历史基准，不误判为回拨。
		return time.Time{}, false
	}
	return time.Unix(unix, 0).UTC(), true
}

func (g *ClockGuard) write(now time.Time) {
	unix := now.Unix()
	_ = os.WriteFile(g.Path, []byte(fmt.Sprintf("%d|%s", unix, g.mac(unix))), 0o600)
}

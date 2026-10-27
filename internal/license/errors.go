package license

import "fmt"

// Error 是携带统一状态码的授权错误。
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Msg) }

func newError(code, msg string) *Error { return &Error{Code: code, Msg: msg} }

// 常见错误。业务项目可直接用 CodeOf 取统一状态码。
var (
	ErrMissing       = newError(CodeMissing, "找不到授权文件")
	ErrMalformed     = newError(CodeMalformed, "授权文件结构或编码损坏")
	ErrBadSignature  = newError(CodeBadSignature, "授权签名校验失败")
	ErrExpired       = newError(CodeExpired, "授权已到期")
	ErrClockRollback = newError(CodeClockRollback, "检测到系统时间回拨")
	ErrMachine       = newError(CodeMachineMismatch, "授权与本机机器码不匹配")
	ErrProduct       = newError(CodeProductMismatch, "授权项目与当前程序不匹配")
	ErrNoPublicKey   = newError(CodeNoPublicKey, "未配置可用的验签公钥")
	ErrNotInit       = newError(CodeNotInit, "授权模块未初始化")
)

// CodeOf 从错误中提取统一状态码；非授权错误返回 "error"。
func CodeOf(err error) string {
	if err == nil {
		return CodeOK
	}
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return "error"
}

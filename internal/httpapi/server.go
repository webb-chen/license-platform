// Package httpapi 实现统一授权平台的 HTTP 接口与静态资源托管。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"unified-license-platform/internal/app"
	"unified-license-platform/internal/auth"
	"unified-license-platform/internal/models"
)

type ctxKey string

const adminKey ctxKey = "ulp.admin"

// Server 持有路由与依赖。
type Server struct {
	app *app.App
	web fs.FS
}

// New 构建 HTTP 处理器。
func New(a *app.App, webFS fs.FS) http.Handler {
	s := &Server{app: a, web: webFS}
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(s.recoverer)
	r.Use(s.securityHeaders)
	r.Use(s.apiLogger)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Post("/admin/login", s.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/admin/me", s.handleMe)
			r.Post("/admin/password", s.handleChangePassword)

			r.Group(func(r chi.Router) {
				r.Use(s.requirePasswordChanged)

				r.Get("/dashboard", s.handleDashboard)

				r.Get("/projects", s.handleListProjects)
				r.Post("/projects", s.handleCreateProject)
				r.Get("/projects/{id}", s.handleProjectDetail)
				r.Patch("/projects/{id}", s.handleUpdateProject)
				r.Delete("/projects/{id}", s.handleDeleteProject)
				r.Get("/projects/{id}/public-key", s.handlePublicKey)

				r.Get("/licenses", s.handleListLicenses)
				r.Post("/licenses", s.handleIssueLicense)
				r.Get("/licenses/{id}", s.handleLicenseDetail)
				r.Get("/licenses/{id}/download", s.handleDownloadLicense)
				r.Post("/licenses/{id}/revoke", s.handleRevokeLicense)
				r.Post("/licenses/{id}/renew", s.handleRenewLicense)
				r.Delete("/licenses/{id}", s.handleDeleteLicense)

				r.Get("/keys", s.handleListKeys)
				r.Post("/keys/rotate", s.handleRotateKey)

				r.Get("/audit-logs", s.handleAuditLogs)
			})
		})
	})

	r.NotFound(s.serveSPA)
	return r
}

// ---------- 中间件 ----------

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[panic] %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, &app.Error{Status: 500, Code: "internal_error", Message: "服务器内部错误"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; "+
				"connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) apiLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, &app.Error{Status: 401, Code: "unauthorized", Message: "未登录或会话已过期"})
			return
		}
		claims, err := auth.ParseToken(s.app.Config().JWTSecret, strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			writeError(w, &app.Error{Status: 401, Code: "unauthorized", Message: "未登录或会话已过期"})
			return
		}
		admin, err := s.app.GetAdmin(claims.Sub)
		if err != nil {
			writeError(w, &app.Error{Status: 401, Code: "unauthorized", Message: "账号不存在"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminKey, admin)))
	})
}

func (s *Server) requirePasswordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if admin := adminFrom(r.Context()); admin != nil && admin.MustChangePassword {
			writeError(w, &app.Error{Status: 403, Code: "password_change_required", Message: "首次登录必须修改密码"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminFrom(ctx context.Context) *models.Admin {
	admin, _ := ctx.Value(adminKey).(*models.Admin)
	return admin
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// ---------- 响应工具 ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[响应] 编码失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	var ae *app.Error
	if errors.As(err, &ae) {
		writeJSON(w, ae.Status, map[string]string{"code": ae.Code, "message": ae.Message})
		return
	}
	log.Printf("[响应] 未预期错误: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "internal_error", "message": "服务器内部错误"})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return &app.Error{Status: 400, Code: "bad_request", Message: "请求体不是合法 JSON：" + err.Error()}
	}
	return nil
}

// ---------- 公共与认证 ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "unified-license-platform",
		"version": s.app.Config().Version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeError(w, &app.Error{Status: 400, Code: "bad_request", Message: "请输入用户名与密码"})
		return
	}
	res, err := s.app.Login(req.Username, req.Password, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"admin": adminFrom(r.Context())})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	admin := adminFrom(r.Context())
	updated, err := s.app.ChangePassword(admin.ID, req.OldPassword, req.NewPassword, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	token, expiresAt, err := s.app.IssueTokenFor(updated)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token, "expires_at": expiresAt, "admin": updated, "must_change_password": false,
	})
}

// ---------- 看板 ----------

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.app.DashboardData()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// ---------- 项目 ----------

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListProjects()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req app.CreateProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	admin := adminFrom(r.Context())
	p, err := s.app.CreateProject(req, admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.app.GetProjectDetail(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var req app.UpdateProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	admin := adminFrom(r.Context())
	p, err := s.app.UpdateProject(chi.URLParam(r, "id"), req, admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	if err := s.app.DeleteProject(chi.URLParam(r, "id"), admin.Username, clientIP(r)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handlePublicKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	pemText, _, err := s.app.PublicKey(id)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+id+"-public.pem\"")
	_, _ = w.Write([]byte(pemText))
}

// ---------- 授权 ----------

func parseIntQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) handleListLicenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := s.app.ListLicenses(app.LicenseFilter{
		Project:  q.Get("project"),
		Customer: q.Get("customer"),
		Status:   q.Get("status"),
		From:     q.Get("from"),
		To:       q.Get("to"),
		Page:     parseIntQuery(r, "page", 1),
		Size:     parseIntQuery(r, "size", 20),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleIssueLicense(w http.ResponseWriter, r *http.Request) {
	var req app.IssueRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	admin := adminFrom(r.Context())
	l, err := s.app.Issue(req, admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleLicenseDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.app.GetLicenseDetail(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleDownloadLicense(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	raw, name, err := s.app.Download(chi.URLParam(r, "id"), admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	ascii := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 128 && r != '"' && r != '\\' {
			ascii = append(ascii, r)
		} else {
			ascii = append(ascii, '_')
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", string(ascii), url.PathEscape(name)))
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	_, _ = w.Write(raw)
}

func (s *Server) handleRevokeLicense(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, err)
			return
		}
	}
	admin := adminFrom(r.Context())
	l, err := s.app.Revoke(chi.URLParam(r, "id"), req.Reason, admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleRenewLicense(w http.ResponseWriter, r *http.Request) {
	var req app.RenewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	admin := adminFrom(r.Context())
	l, err := s.app.Renew(chi.URLParam(r, "id"), req, admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleDeleteLicense(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	if err := s.app.DeleteLicense(chi.URLParam(r, "id"), admin.Username, clientIP(r)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- 密钥与审计 ----------

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListKeys()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	admin := adminFrom(r.Context())
	key, err := s.app.RotateKey(strings.TrimSpace(req.Project), admin.Username, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := s.app.ListAudits(app.AuditFilter{
		Action:   q.Get("action"),
		Operator: q.Get("operator"),
		From:     q.Get("from"),
		To:       q.Get("to"),
		Page:     parseIntQuery(r, "page", 1),
		Size:     parseIntQuery(r, "size", 20),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// ---------- 静态资源 ----------

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(s.web, path); err == nil {
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		http.FileServerFS(s.web).ServeHTTP(w, r)
		return
	}
	// 前端为 hash 路由：未知路径一律返回入口页。
	w.Header().Set("Cache-Control", "no-store")
	data, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		http.Error(w, "界面资源缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

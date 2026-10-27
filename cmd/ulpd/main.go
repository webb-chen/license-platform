// Command ulpd 启动统一授权平台 HTTP 服务。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"unified-license-platform/internal/app"
	"unified-license-platform/internal/config"
	"unified-license-platform/internal/httpapi"
	"unified-license-platform/web"
)

const version = "1.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix("[ulpd] ")

	cfg, err := config.Load(version)
	if err != nil {
		log.Fatalf("配置错误: %v", err)
	}
	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	defer func() {
		if err := a.Close(); err != nil {
			log.Printf("关闭数据库失败: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(a, web.FS),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("统一授权平台 v%s 已启动，监听 %s，数据目录 %s", version, cfg.Addr, cfg.DataDir)
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("服务异常退出: %v", err)
		}
	case sig := <-stop:
		log.Printf("收到信号 %s，开始优雅关闭", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("优雅关闭超时，强制退出: %v", err)
		}
	}
	log.Print("服务已停止")
}

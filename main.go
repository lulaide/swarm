package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lulaide/swarm/api"
	"github.com/lulaide/swarm/config"
	"github.com/lulaide/swarm/dispatcher"
	"github.com/lulaide/swarm/pool"
	"github.com/lulaide/swarm/proxy"
	"github.com/lulaide/swarm/subscribe"
)

func main() {
	configPath := flag.String("c", "config.yaml", "配置文件路径")
	debug := flag.Bool("debug", false, "启用 debug 日志")
	flag.Parse()

	// 结构化日志
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	// 初始化代理池
	proxyPool := pool.New(cfg.HealthCheck.MaxFailures)

	// 初始化订阅
	subscribers := make([]*subscribe.Subscriber, 0, len(cfg.Subscribe))
	for _, subCfg := range cfg.Subscribe {
		interval := time.Duration(subCfg.Interval) * time.Second
		sub := subscribe.New(subCfg.Name, subCfg.URL, interval)
		subscribers = append(subscribers, sub)
	}

	// 拉取订阅
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	for _, sub := range subscribers {
		if err := sub.Start(ctx); err != nil {
			slog.Error("订阅初始化失败", "name", sub.Name(), "err", err)
			os.Exit(1)
		}
	}
	cancel()

	allProxies := api.CollectProxies(subscribers)
	if len(allProxies) == 0 {
		slog.Error("没有可用的代理节点")
		os.Exit(1)
	}
	fmt.Printf("已解析 %d 个节点，正在测试连通性...\n", len(allProxies))

	// 启动前连通性测试
	hcTimeout := time.Duration(cfg.HealthCheck.Timeout) * time.Second
	alive := pool.FilterAlive(allProxies, cfg.HealthCheck.URL, hcTimeout)
	if len(alive) == 0 {
		slog.Error("没有节点通过连通性测试")
		os.Exit(1)
	}
	fmt.Printf("连通性测试完成: %d/%d 个节点可用\n", len(alive), len(allProxies))

	proxyPool.Update(alive)

	// 启动后台健康检查
	hcInterval := time.Duration(cfg.HealthCheck.Interval) * time.Second
	hc := pool.NewHealthChecker(proxyPool, cfg.HealthCheck.URL, hcInterval, hcTimeout)
	hc.Start()
	defer hc.Close()

	// 创建调度器（用于统计）
	disp := dispatcher.New(proxyPool)

	// 启动 HTTP 代理服务
	proxyServer, err := proxy.New(cfg.Listen, proxyPool)
	if err != nil {
		slog.Error("启动代理监听失败", "err", err)
		os.Exit(1)
	}
	defer proxyServer.Close()
	slog.Info("代理监听", "addr", proxyServer.Address())

	// 启动 API 服务
	apiServer := api.New(proxyPool, disp, subscribers)
	go func() {
		slog.Info("API 监听", "addr", cfg.API)
		if err := http.ListenAndServe(cfg.API, apiServer); err != nil {
			slog.Error("API 服务错误", "err", err)
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("正在关闭...")

	for _, sub := range subscribers {
		sub.Close()
	}
}

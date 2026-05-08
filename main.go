package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/metacubex/mihomo/listener/mixed"

	"github.com/lulaide/swarm/api"
	"github.com/lulaide/swarm/config"
	"github.com/lulaide/swarm/dispatcher"
	"github.com/lulaide/swarm/pool"
	"github.com/lulaide/swarm/subscribe"
)

func main() {
	configPath := flag.String("c", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
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

	// 拉取订阅并填充代理池
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	for _, sub := range subscribers {
		if err := sub.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "订阅 %s 初始化失败: %v\n", sub.Name(), err)
			os.Exit(1)
		}
	}
	cancel()

	proxies := api.CollectProxies(subscribers)
	if len(proxies) == 0 {
		fmt.Fprintln(os.Stderr, "没有可用的代理节点")
		os.Exit(1)
	}
	proxyPool.Update(proxies)
	fmt.Printf("已加载 %d 个代理节点\n", len(proxies))

	// 启动健康检查
	hcInterval := time.Duration(cfg.HealthCheck.Interval) * time.Second
	hcTimeout := time.Duration(cfg.HealthCheck.Timeout) * time.Second
	hc := pool.NewHealthChecker(proxyPool, cfg.HealthCheck.URL, hcInterval, hcTimeout)
	hc.Start()
	defer hc.Close()

	// 创建调度器
	disp := dispatcher.New(proxyPool)

	// 启动 Mixed 监听 (HTTP + SOCKS5)
	listener, err := mixed.New(cfg.Listen, disp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动监听失败: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Printf("代理监听: %s (HTTP + SOCKS5)\n", listener.Address())

	// 启动 API 服务
	apiServer := api.New(proxyPool, disp, subscribers)
	go func() {
		fmt.Printf("API 监听: %s\n", cfg.API)
		if err := http.ListenAndServe(cfg.API, apiServer); err != nil {
			fmt.Fprintf(os.Stderr, "API 服务错误: %v\n", err)
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\n正在关闭...")

	for _, sub := range subscribers {
		sub.Close()
	}
}

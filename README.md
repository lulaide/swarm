# Swarm

高并发多出口代理池，专为安全扫描和爬虫场景设计。

每个请求随机选择出口节点，自动绕过目标站点的 IP 限速。支持 Clash 订阅链接，复用 [mihomo](https://github.com/MetaCubeX/mihomo) 全部代理协议。

## 特性

- **随机出口** — 每个请求自动分配不同出口 IP
- **启动前连通性测试** — 只保留实际可用的节点
- **失败自动重试** — 单次请求最多重试 3 个节点
- **被动 + 主动健康检查** — 连续失败自动剔除，后台定时恢复探测
- **Clash 订阅** — 支持 Clash YAML 和 V2Ray base64 格式
- **全协议支持** — SS、Trojan、VMess、VLESS、Hysteria2、TUIC、WireGuard 等 20+ 协议
- **REST API** — 节点列表、统计信息、手动刷新订阅

## 快速开始

```bash
# 编译
go build -o swarm .

# 编辑配置，填入订阅链接
vim config.yaml

# 启动
./swarm -c config.yaml
```

输出示例：

```
已解析 38 个节点，正在测试连通性...
[check] 🇭🇰|香港-IEPL 03                            OK
[check] 🇯🇵|日本-IEPL 02                            OK
[check] 🇺🇸|美国-IEPL 02                            OK
[check] 剩余流量：10 GB                               FAIL
连通性测试完成: 8/38 个节点可用
代理监听: [::]:7890 (HTTP Proxy)
API 监听: :9090
```

## 使用

上游程序设置 HTTP 代理即可，每次请求自动走不同出口：

```bash
# curl
curl -x http://127.0.0.1:7890 https://httpbin.org/ip

# 环境变量
export HTTP_PROXY=http://127.0.0.1:7890
export HTTPS_PROXY=http://127.0.0.1:7890

# Python requests
import requests
r = requests.get("https://httpbin.org/ip", proxies={"https": "http://127.0.0.1:7890"})

# nuclei
nuclei -proxy http://127.0.0.1:7890 -l targets.txt

# httpx
httpx -proxy http://127.0.0.1:7890 -l targets.txt
```

## 配置

```yaml
listen: ":7890"          # 代理监听端口
api: ":9090"             # API 端口

subscribe:
  - url: "https://example.com/clash/sub"
    interval: 3600       # 自动刷新间隔（秒）
    name: "main"

health_check:
  url: "https://www.gstatic.com/generate_204"
  interval: 300          # 健康检查间隔（秒）
  timeout: 5             # 超时（秒）
  max_failures: 3        # 连续失败次数后标记不可用
```

## API

```bash
# 节点列表
curl http://127.0.0.1:9090/nodes

# 统计信息
curl http://127.0.0.1:9090/stats

# 手动刷新订阅
curl -X POST http://127.0.0.1:9090/subscribe/refresh
```

## 压测对比

目标：ip-api.com（每 IP 每分钟 45 次限速），20 并发，20 秒

| 指标 | 直连 | Swarm (8 节点) |
|---|---|---|
| 成功 QPS | 2.0 | **9.2 (4.6x)** |
| 成功率 | 38.3% | **84.7%** |
| 429 限速 | 31 次 | **8 次** |

```bash
# 运行压测
go build -o bench ./benchmark/

# 直连
./bench -url "http://ip-api.com/json/" -c 20 -d 20

# 通过 swarm
./bench -url "http://ip-api.com/json/" -proxy "http://127.0.0.1:7890" -c 20 -d 20
```

## 项目结构

```
├── main.go              # 入口
├── config/              # 配置解析
├── subscribe/           # 订阅拉取与解析
├── pool/                # 代理池、随机选择、健康检查
├── proxy/               # HTTP 代理服务器
├── dispatcher/          # 调度器（C.Tunnel 接口实现）
├── api/                 # REST API
└── benchmark/           # 压测工具
```

## License

MIT

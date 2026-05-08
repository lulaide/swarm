# Swarm

高并发多出口代理池，专为安全扫描和爬虫场景设计。

每个请求随机选择出口节点，自动绕过目标站点的 IP 限速。支持 Clash 订阅链接，复用 [mihomo](https://github.com/MetaCubeX/mihomo) 全部代理协议。

## 特性

- **随机出口** — 每个请求自动分配不同出口 IP
- **Dial Race** — 并行建连取最快节点，大幅砍尾延迟（可选开关）
- **P2C 选择** — 基于 EWMA 延迟的智能节点选择
- **启动前连通性测试** — 只保留实际可用的节点
- **失败自动重试** — 单次请求最多重试 3 个节点
- **被动 + 主动健康检查** — 连续失败自动剔除，后台定时恢复探测
- **HTTP Keep-Alive** — 复用客户端连接，减少握手
- **高性能** — 零分配节点选择（15ns/op）、buffer 池、4096 并发限制
- **Clash 订阅** — 支持 Clash YAML 和 V2Ray base64 格式
- **全协议支持** — SS、Trojan、VMess、VLESS、Hysteria2、TUIC、WireGuard 等 20+ 协议
- **REST API** — 节点列表、统计信息、手动刷新订阅

## 快速开始

### Docker（推荐）

```bash
# 编辑配置
vim config.yaml

# 启动
docker run -d --name swarm \
  -p 7890:7890 -p 9090:9090 \
  -v $(pwd)/config.yaml:/etc/swarm/config.yaml:ro \
  ghcr.io/lulaide/swarm:latest

# 或使用 docker-compose
docker compose up -d
```

### 源码编译

```bash
# 编译
go build -o swarm .

# 编辑配置，填入订阅链接
vim config.yaml

# 启动
./swarm -c config.yaml

# debug 模式
./swarm -c config.yaml -debug
```

输出示例：

```
已解析 38 个节点，正在测试连通性...
[check] 🇭🇰|香港-IEPL 03                            OK
[check] 🇯🇵|日本-IEPL 02                            OK
[check] 🇺🇸|美国-IEPL 02                            OK
[check] 剩余流量：10 GB                               FAIL
连通性测试完成: 8/38 个节点可用
INFO dial race 已启用
INFO 代理监听 addr=[::]:7890
INFO API 监听 addr=:9090
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
dial_race: true          # 并行建连取最快（增加一次连接开销换低延迟）

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

### Dial Race 说明

开启 `dial_race: true` 后，每个请求会同时向 2 个随机节点发起连接，采用先成功的那个，关闭慢的。

**优势**：尾延迟（P95/P99）大幅下降，节点质量差异大时效果明显。

**代价**：每个请求多消耗一次 dial（输掉的连接会关闭）。对付费节点注意连接频率限制，可关闭。

```
无 dial race:  选节点A → 建连(慢) → 失败 → 选节点B → 建连 → 成功
                总耗时 = A超时 + B建连

有 dial race:  同时建连 节点A ──→ 慢(丢弃)
               同时建连 节点B ──→ 快(采用) ✓
                总耗时 = min(A, B)
```

## API

```bash
# 节点列表（含延迟、存活状态）
curl http://127.0.0.1:9090/nodes

# 统计信息
curl http://127.0.0.1:9090/stats

# 手动刷新订阅
curl -X POST http://127.0.0.1:9090/subscribe/refresh
```

## 压测数据

### IP 限速绕过（ip-api.com，20 并发，20 秒）

| 指标 | 直连 | Swarm (8 节点) |
|---|---|---|
| 成功 QPS | 2.0 | **9.2 (4.6x)** |
| 成功率 | 38.3% | **84.7%** |
| 429 限速 | 31 次 | **8 次** |

### 延迟对比（httpbin.org，15 次）

| 指标 | 无 Dial Race | 有 Dial Race |
|---|---|---|
| P50 | 250ms | 252ms |
| 平均 | 328ms | 359ms |
| P95 | 1214ms | 1116ms |

### 吞吐量（httpbin.org，30 并发，15 秒）

| 指标 | 无 Dial Race | 有 Dial Race |
|---|---|---|
| 成功 QPS | 19.4 | **20.7** |
| 成功率 | 100% | 99.4% |

> Dial Race 在节点延迟差异大的环境下收益更明显（如混合香港 200ms + 美国 600ms 节点时）。当前测试节点均为 IEPL 专线，延迟差异小，因此收益有限。

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
├── pool/                # 代理池、P2C/随机选择、EWMA 延迟、健康检查
├── proxy/               # HTTP 代理服务器、dial race、buffer 池
├── dispatcher/          # 调度器（C.Tunnel 接口实现）
├── api/                 # REST API
└── benchmark/           # 压测工具
```

## License

MIT

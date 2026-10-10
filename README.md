# vcpp-emulator

vcpp 充电桩设备模拟器，完全模拟桩侧数据通信，作为 client 端连接服务端 [`vcpp`](https://github.com/vbeats/vcpp)。技术栈为 **Go + go-zero**，零外部依赖（不引入 redis / 数据库 / MQ）。

当前实现云快充 **V1.7**（服务端端口 9849），可同时模拟多个桩；同一时刻只连接一个 server 端。

> 原始目标（长期）：
>
> 1. 支持云快充及多种协议版本，通过配置与 HTTP API 控制模拟器启停。
> 2. 建立 TCP 长连接，按周期上报桩侧数据，接收平台指令。
> 3. 按平台指令与电价模型自行模拟电压、电流、温度等实时数据，最终生成模拟账单。

## 快速开始

```powershell
# 1. 安装最新 go-zero 与 goctl（由 go.mod 锁定版本）
go get github.com/zeromicro/go-zero@latest
go install github.com/zeromicro/go-zero/tools/goctl@latest

# 2. 修改配置（server 地址、桩号、枪数量等）
vim etc\emulator.yaml

# 3. 启动（先启动 vcpp 服务端，再启动模拟器）
go run emulator.go -f etc/emulator.yaml
# 或
go build -o bin/emulator.exe . && .\bin\emulator.exe -f etc\emulator.yaml
```

启动后 HTTP 控制面默认监听 `http://127.0.0.1:10010`。

## 配置文件 `etc/emulator.yaml`

> 帧格式、帧类型码、序列号字节序、交易记录帧类型等**由协议规定的内容不提供配置项**，一律由 `protocol/ykc` 按版本决定。

| 配置段 | 关键项 | 说明 |
| --- | --- | --- |
| `Host` / `Port` | `0.0.0.0` / `10010` | 模拟器自身 HTTP 控制面端口 |
| `Log` | `Mode` / `Level` | go-zero logx 日志配置 |
| `Server` | `Ip` / `Port` | server 端（vcpp）地址，同一时刻仅一个 |
| `Protocol` | `Type` | `ykc`（OCPP 仅预留入口） |
| | `Version` | `V15` / `V16` / `V17` / `V18` / `V21`（当前仅实现 V17） |
| `Timing` | `ReconnectSec` | server 不在线时的重连间隔，默认 10s |
| | `HeartbeatSec` | 每把枪的心跳周期，默认 10s |
| | `HeartbeatTimeoutSec` | 连续未收到心跳应答则断开，默认 30s |
| | `ChargeReportSec` | 充电过程上报周期，默认 15s |
| | `ChargeDurationSec` | 充电到点自动结束，默认 600s |
| | `TradeRetry` / `TradeRetrySec` | 交易记录未确认的最大重传次数与间隔 |
| `Billing` | `ChargePowerKw` 等 | 模拟充电功率、电池容量、电表起始值、初始 SOC |
| `FrameLog` | `Hex` / `Parsed` | 是否打印十六进制原文与字段解析结果 |
| `Devices[]` | `DeviceNo` | 桩号，必须 14 位数字 |
| | `GunCount` / `AutoStart` / `VoltageV` 等 | 枪数量、是否随进程自启、模拟输出电压 |

## HTTP API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/devices` | 查询所有桩与枪的状态（阶段、连接、计费模型、充电进度） |
| POST | `/api/v1/devices/start` | 启动指定桩 `{"deviceNo":"..."}` |
| POST | `/api/v1/devices/stop` | 停止指定桩（正在充电会先走终止与交易记录上送） |
| POST | `/api/v1/charge/start` | 手动触发启机 `{"deviceNo":"...","gunNo":1}` |
| POST | `/api/v1/charge/stop` | 手动终止充电 `{"deviceNo":"...","gunNo":1}` |

```powershell
Invoke-RestMethod http://127.0.0.1:10010/api/v1/devices | ConvertTo-Json -Depth 6

Invoke-RestMethod http://127.0.0.1:10010/api/v1/charge/start -Method Post `
  -ContentType "application/json" -Body '{"deviceNo":"20260501000001","gunNo":1}'
```

响应统一为 `{"code":0,"msg":"ok"}`，`code != 0` 时 `msg` 为失败原因。

## 运行行为

- **重连**：server 不在线时，每 `ReconnectSec` 秒重试一次；连接建立后序列号归 0 并重新走登录流程。
- **心跳**：连接建立后，每 `HeartbeatSec` 秒为**每把枪**发送一次心跳（0x03）；连续 `HeartbeatTimeoutSec` 秒未收到心跳应答（0x04）则主动断开并重连。
- **上电链路**：登录认证（0x01）→ 登录应答（0x02）→ 对时（0x56/0x55）→ 计费模型验证（0x05/0x06）→ 计费模型请求（0x09/0x0A）→ 内存缓存计费模型。计费模型未就绪前**拒绝启机**（协议要求）。
- **充电流程**：收到远程启机（0x34）或 HTTP 触发后 → 启机回复（0x33）→ 充电握手（0x15）→ 参数配置（0x17）→ 每 `ChargeReportSec` 秒上报 BMS 信息（0x25）、BMS 需求与充电机输出（0x23）、实时监测数据（0x13）。
- **结束方式**（三种都支持）：平台远程停机（0x36）、充电时长到点、HTTP 主动终止。结束后发送充电结束（0x19）与交易记录（0x3D/0x3B），未收到确认（0x40）时按配置重传。
- **账单生成**：按缓存的 48 时段电价模型（尖/峰/平/谷）累计分段电量与金额，全部采用 int64 定点运算。

## 日志

每条通信报文输出三段，`⬆️` 为上行（模拟器 → server），`⬇️` 为下行（server → 模拟器）：

```
【云快充】V17 桩号:20260501000001 枪号:01 ⬆️ 充电握手（0x15）
HEX: 68 4D EF 01 00 15 20 26 05 01 00 00 01 ...
解析: 交易流水号=... 桩号=... 枪号=01 BMS协议版本=1.1 电池类型=3[磷酸铁锂] ...
---------------------------------
```

可通过 `FrameLog.Hex` / `FrameLog.Parsed` 关闭其中任一段。

## 测试

```powershell
go test ./...                 # 编解码、拆帧、计费、桩端到端流程
go vet ./...
```

协议编解码的基线数据来自 `doc/云快充/测试数据.md` 与 V1.7 协议正文样例；`internal/sim/pile_test.go` 用一个桩服务端完成登录→启机→充电→交易记录的端到端验证。

## 目录结构

```
vcpp-emulator/
├── emulator.go              主入口（goctl 脚手架，已接入模拟器生命周期）
├── etc/emulator.yaml        统一配置
├── api/emulator.api         goctl API 定义（改动后需重新生成）
├── internal/
│   ├── config/              配置结构与校验
│   ├── handler/ logic/ svc/ types/   go-zero HTTP 层（goctl 生成，logic 仅转发到 sim.Manager）
│   ├── sim/                 桩 Actor、状态机、会话与注册表
│   ├── transport/           TCP 连接、流式拆帧、重连
│   ├── protocol/ykc/        云快充帧编解码（纯函数）
│   ├── billing/             48 时段计费模型与定点计费
│   └── framelog/            方向 + HEX + 解析 的通信日志
└── doc/云快充/              协议文档（实现以服务端 vcpp 的行为为准）
```

重新生成 HTTP 层：

```powershell
goctl api go -api api/emulator.api -dir . --style gozero
```

> 生成代码会覆盖 `emulator.go` 之外的 handler/logic/svc/types；`emulator.go` 中的模拟器启动与优雅退出逻辑需要在生成后保留。

## 已知限制

- 仅实现云快充 V1.7；V15/V16/V18/V21 与 OCPP 预留了扩展点但未实现。
- 交易记录帧类型由版本决定：V15/V16 发 `0x3B`，V17 起发 `0x3D`。当前 `vcpp` 只注册了 V15/V16 的 `0x3B` 执行器，因此用 V1.7 上送交易记录时服务端不会回 `0x40` 确认，模拟器会在重传次数用尽后告警；待服务端补齐 `0x3D` 执行器即可（已登记在 [`vcpp`](https://github.com/vbeats/vcpp) 仓库的 `AGENTS.md` 待办中）。
- 数据仅内存与日志，进程重启后不保留。

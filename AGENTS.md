# AGENTS.md

## 定位

充电桩模拟器，**client 端**。用软件完整模拟一台/多台真实充电桩的通信行为，连接服务端 `vcpp`，用于联调、压测与回归验证，不依赖硬件。

- 统一配置文件驱动 + go-zero HTTP 控制面（**只有** 状态查询 + 远程启机/停机；桩的启停由配置 `Devices[].AutoStart` 决定，不提供 HTTP 接口）。
- 每桩一条 TCP 长连接、一个 Actor goroutine、一个状态机。
- 零外部依赖：仅内存 + 日志，不引入 redis / 数据库 / MQ。

## 技术栈

| 项 | 选型 |
| --- | --- |
| 语言 | Go（当前稳定版） |
| 框架 | **go-zero**（`rest` + `conf` + `logx`，goctl 生成 HTTP 层；保持最新 release） |
| 协议 | 云快充 **V1.5 / V1.6 / V1.7**（V18 / V21 未实现） |
| 网络 | 标准库 `net` + 自研流式拆帧（go-zero 不管 TCP） |
| 金额 | **int64 定点**（电量/金额 ×0.0001，费率/单价 ×0.00001），不引第三方 decimal |
| 依赖策略 | `go get github.com/zeromicro/go-zero@latest` + `goctl@latest`，由 `go.mod` 锁定；不写死版本号 |

go-zero 只负责 HTTP 控制面、配置与日志；**协议栈、连接管理、状态机、计费全部自研**。

## 分层与目录

```
emulator.go               主入口：加载配置 → 启动 Manager → 启动 rest → 优雅退出
etc/emulator.yaml         统一配置
api/emulator.api          goctl API 定义
internal/
  config/                 配置结构与 Validate（单 server、多桩、周期阈值）
  handler/ logic/ svc/ types/   go-zero HTTP 层（goctl 生成，logic 仅转发到 sim.Manager）
  sim/                    manager.go 桩注册表 / pile.go Actor / session.go 会话
  transport/              conn.go 连接与重连 / framer.go 流式拆帧
  protocol/protocol.go    协议入口（ykc 已实现，ocpp 预留）
  protocol/ykc/           帧编解码：frame.go codec.go up.go down.go bin.go crc.go cp56.go
  billing/                48 时段计费模型、缓存、定点计费
  framelog/               方向 + HEX + 解析 的通信日志
```

分层边界：`transport`（字节流，无协议语义）→ `protocol/ykc`（帧编解码，纯函数）→ `sim`（时序与状态机）→ `billing`（计费）→ `logic`（仅转发）。

## 实现约定

- **一个桩一个 Actor 串行处理所有事件**（帧到达、定时器、API 指令），状态无共享可变、无锁；读协程只负责拆帧后投递事件。
- **编解码写成 `[]byte` ⇄ struct 纯函数**，便于用 `doc/云快充/测试数据.md` 的样例做单测。
- **拆帧必须先拷贝再解码**：`bufio.Peek` 返回的是读缓冲内部切片，`Discard` 后会被覆盖，帧异步交给 Actor 前必须 `copy` 出来（曾因此读到脏数据）。
- **Windows 上必须自己关 HTTP 服务**（`emulator.go` 的 `httpServerHolder`）：`core/proc/signals.go` 的构建约束是 `linux || darwin || freebsd`，Windows 不接管信号；`core/proc/shutdown+polyfill.go`（`//go:build windows`）里 `AddShutdownListener` 是空实现，而 go-zero 正是用它注册 `server.Shutdown`；`rest.Server.Stop()` 又只做 `logx.Close()`。三者叠加会导致 Ctrl+C 后 `ListenAndServe` 永不返回、进程不退出、端口持续占用。**不要改回 `server.Start()`**，必须走 `StartWithOpts` 捕获 `*http.Server` 再自行 `Shutdown`。
- **`Pile.Submit` 的投递与回执都必须带超时**：Actor 可能已随 ctx 取消先退出，无限等回执会把退出流程卡死。
- **协议规定的内容不进配置文件**：帧格式、帧类型码、序列号字节序、交易记录帧类型（V15/V16 用 0x3B、V17 起用 0x3D）一律写在 `protocol/ykc` 里（`codec.go` 的 `upSeqOrder`/`downSeqOrder`、`frame.go` 的 `TradeFrameType`），配置文件只放「选哪个协议、哪个版本、连哪个 server」。
- 序列号字节序按方向不同是为了对齐 `vcpp` 现有实现（上行 `readUnsignedShortLE` 小端、下行 `YkcAckUtil.writeShort` 大端），服务端统一后改上面两个常量即可。
- 金额/电量用 int64 定点换算，集中在 `billing` 包内，禁止散落 magic number。
- **桩号用整型**：云快充桩号在线上是 7 字节 BCD，即 14 位十进制数字，配置与代码一律用整型（`config.DeviceConfig.DeviceNo int64`、`ykc` 侧 `uint64`），编码时用 `ykc.FormatDeviceNo`（`%014d`）补零再写 BCD；HTTP 层按字符串接收后用 `logic.parseDeviceNo` 转整型。
- **版本差异用纯函数表达**：`ykc.ProtoVerByte` / `TradeFrameType` / `TradeWithMeter` / `SupportsParallel` 按版本标签推导，禁止在业务代码里散落 `if version == ...`。
- **费率模式按帧长度识别**：计费模型 90 字节为尖峰平谷（4 段），98 字节为尖峰平谷深谷（5 段，深谷索引 4，排在谷之后）。协议没有「模式/段数」字段，不要用配置项表达。
- 状态机显式建模（`sim/pile.go` 的 `updateStage`），非法阶段收到帧只记 warn 并忽略，不 panic。
- `handler/`、`types/`、`routes.go` 为 goctl 生成，不要手改；业务逻辑放 `sim`。

## 常用命令

```powershell
go build ./... && go vet ./...
go test ./...                                  # 编解码、拆帧、计费、桩端到端流程
goctl api go -api api/emulator.api -dir . --style gozero
go run emulator.go -f etc/emulator.yaml
```

升级 go-zero：`go get -u github.com/zeromicro/go-zero` 后 `go build ./... && go test ./...` 通过为准。

## 当前行为（V1.5 / V1.6 / V1.7）

- 重连 10s；每枪心跳 10s；连续 30s 无心跳应答断开；充电上报 15s。
- 上电链路：0x01 → 0x02 → 0x56/0x55 → 0x05/0x06 → 0x09/0x0A → 缓存计费模型（90B=4 段 / 98B=5 段）。
- 充电链路：0x34 → 0x33 → 0x15 → 0x17 → 每 15s（0x25 / 0x23 / 0x13）→ 结束（0x19）→ 交易记录 0x3D（V17）/ 0x3B（V15、V16）→ 等 0x40。
- 三种结束方式：平台远程停机 0x36、充电时长到点、HTTP 主动终止。
- 计费模型未就绪时按协议拒绝启机。
- 刷卡启动路径：0x31 申请 → 0x32 平台确认（交易流水号由平台下发）→ 进入充电链路。
- 平台下行指令均按协议应答：0x42 余额、0x44/0x46/0x48 离线卡、0x52 工作参数、0x62 地锁、0x94 远程更新、0x9C/0x9E/0xF0 二维码、0xA4 并充启机（V1.6+）。

## 协议文档索引（改动协议代码前必读）

- 云快充各版本原文：[`doc/云快充/`](doc/云快充/)
  - [V1.5](doc/云快充/云快充平台协议V1.5.md) · [V1.5补充](doc/云快充/云快充平台协议V1.5补充.md) · [V1.6](doc/云快充/云快充平台协议V1.6.md) · [V1.6补充](doc/云快充/云快充平台协议V1.6补充.md) · [V1.7](doc/云快充/云快充平台协议V1.7.md)（当前实现依据） · [V1.8.1](doc/云快充/云快充平台协议V1.8.1.md) · [V2.1.0](doc/云快充/云快充平台协议V2.1.0.md)
  - 深谷电价方案：[方案](<doc/云快充/云快充V1.5-增加深谷（云电）_方案.md>)
  - 报文样例（单测基线）：[`doc/云快充/测试数据.md`](doc/云快充/测试数据.md)
  - 真实设备运行日志（行为与字段取值的实测依据）：`doc/device-2026-10-10.0.log`
- OCPP 尚未启动，仅占位：[`doc/OCPP.md`](doc/OCPP.md)；国标参考 `doc/T／CEC 102.1~102.4—2016 *.pdf`。
- 帧结构、端口、桩号规则速览见根目录 `AGENTS.md`；服务端实现细节见 `vcpp/AGENTS.md`。

> 本目录 `doc/` 与 `vcpp/doc/` 是同一批协议文档。文档样例存在长度域与字节数不一致的情况；**语义以 `vcpp` 的实现为准**，冲突时先对拍服务端代码。

## 未完成

- V18 / V21 编解码、OCPP。
- MQTT 上报、数据持久化。
- 并充（0xA1~0xA4）仅做了「收到 0xA4 回复 0xA3 后按普通充电推进」，未模拟双枪协同与功率分配。
- 远程更新 0x94 只登记文件信息并应答成功，不做真实下载与版本切换。

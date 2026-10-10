# AGENTS.md

## 定位

充电桩模拟器，**client 端**。用软件完整模拟一台/多台真实充电桩的通信行为，连接服务端 `vcpp`，用于联调、压测与回归验证，不依赖硬件。

- 统一配置文件驱动 + go-zero HTTP 控制面（启停、查询、触发启机/终止）。
- 每桩一条 TCP 长连接、一个 Actor goroutine、一个状态机。
- 零外部依赖：仅内存 + 日志，不引入 redis / 数据库 / MQ。

## 技术栈

| 项 | 选型 |
| --- | --- |
| 语言 | Go（当前稳定版） |
| 框架 | **go-zero**（`rest` + `conf` + `logx`，goctl 生成 HTTP 层） |
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
- **协议规定的内容不进配置文件**：帧格式、帧类型码、序列号字节序、交易记录帧类型（V15/V16 用 0x3B、V17 起用 0x3D）一律写在 `protocol/ykc` 里（`codec.go` 的 `upSeqOrder`/`downSeqOrder`、`frame.go` 的 `TradeFrameType`），配置文件只放「选哪个协议、哪个版本、连哪个 server」。
- 序列号字节序按方向不同是为了对齐 `vcpp` 现有实现（上行 `readUnsignedShortLE` 小端、下行 `YkcAckUtil.writeShort` 大端），服务端统一后改上面两个常量即可。
- 金额/电量用 int64 定点换算，集中在 `billing` 包内，禁止散落 magic number；桩号、枪号用 `string`。
- 桩号 7 字节 BCD = **14 位十进制**，不用 `int64`。
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

## 当前行为（V1.7）

- 重连 10s；每枪心跳 10s；连续 30s 无心跳应答断开；充电上报 15s。
- 上电链路：0x01 → 0x02 → 0x56/0x55 → 0x05/0x06 → 0x09/0x0A → 缓存计费模型。
- 充电链路：0x34 → 0x33 → 0x15 → 0x17 → 每 15s（0x25 / 0x23 / 0x13）→ 结束（0x19）→ 交易记录 0x3D → 等 0x40。
- 三种结束方式：平台远程停机 0x36、充电时长到点、HTTP 主动终止。
- 计费模型未就绪时按协议拒绝启机。

## 协议文档索引（改动协议代码前必读）

- 云快充各版本原文：[`doc/云快充/`](doc/云快充/)
  - [V1.5](doc/云快充/云快充平台协议V1.5.md) · [V1.5补充](doc/云快充/云快充平台协议V1.5补充.md) · [V1.6](doc/云快充/云快充平台协议V1.6.md) · [V1.6补充](doc/云快充/云快充平台协议V1.6补充.md) · [V1.7](doc/云快充/云快充平台协议V1.7.md)（当前实现依据） · [V1.8.1](doc/云快充/云快充平台协议V1.8.1.md) · [V2.1.0](doc/云快充/云快充平台协议V2.1.0.md)
  - 深谷电价方案：[方案](<doc/云快充/云快充V1.5-增加深谷（云电）_方案.md>)
  - 报文样例（单测基线）：[`doc/云快充/测试数据.md`](doc/云快充/测试数据.md)
- OCPP 尚未启动，仅占位：[`doc/OCPP.md`](doc/OCPP.md)；国标参考 `doc/T／CEC 102.1~102.4—2016 *.pdf`。
- 帧结构、端口、桩号规则速览见根目录 `AGENTS.md`；服务端实现细节见 `vcpp/AGENTS.md`。

> 本目录 `doc/` 与 `vcpp/doc/` 是同一批协议文档。文档样例存在长度域与字节数不一致的情况；**语义以 `vcpp` 的实现为准**，冲突时先对拍服务端代码。

## 未完成

- V15/V16/V18/V21 编解码、OCPP。
- MQTT 上报、数据持久化。

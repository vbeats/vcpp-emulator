# AGENTS.md

本文件面向 AI 编码助手：在本仓库中做任何修改前请先阅读，遵守其中的分层约定与已知陷阱。

## 项目定位

`vcpp-emulator` 是**设备模拟器**桌面客户端，内置两套并行设备模拟（左侧 tab 切换，互不干扰）：

1. **充电桩（云快充协议设备侧）**：单进程可同时模拟多台充电桩（每桩可配多枪），
   自动完成登录、心跳、计费模型获取、启停充电、实时数据上报、交易记录上送全流程，并实时展示通信日志。
   它模拟的是协议的**设备端**，对端平台服务端实现见：<https://github.com/vbeats/vcpp>
2. **加油站（加油机）**：单进程模拟多台加油机、每机多把油枪，维护「提枪 → 加油 → 挂枪结算」状态机，
   支持批量生成油机油枪、按油号区分油品与颜色、统一油价、定额 / 定量自动停枪，
   并将加油进度与加油订单以 MQTT 或 HTTP POST JSON 形式推送（通道已预留，实际外发未实现）。
   油机**没有启用 / 停用状态**，配置中存在即投入运行；加油量与金额**只按枪展示**，油机层面不做汇总。

协议文档（云快充 1.4 / 1.5 / 1.6 / 1.7 / 1.8.1 / 2.1）放在 `doc/云快充/`（PDF + Markdown 摘录）。

## 技术栈与版本策略

| 层次 | 选型 |
| --- | --- |
| 桌面框架 | Wails **v3.0.0**（应用模型：`application.New` + Service + `RegisterEvent`） |
| 前端 | Vue 3 + TypeScript + Vite + Tailwind CSS v4 |
| 语言 | Go 1.26+ |
| 构建 | `wails3` CLI + `task`（go-task）驱动 `Taskfile.yml` |
| 桌面构建目标 | **Windows 11 x64（amd64）**；不支持更早的 Windows 版本 |

- 依赖一律使用**各软件最新正式发布版**，禁止 alpha / beta / rc / nightly 等快照版本。
- 升级依赖后必须重新生成前端绑定并完整构建验证（见「构建与验证」）。

## 构建与验证

```bash
wails3 dev                 # 开发模式，直接运行（不产出安装包）
wails3 build               # 构建 Windows 11 x64（ARCH 默认 amd64），产物 bin/
wails3 build ARCH=arm64    # 非发布目标：其它架构需显式指定
task windows:package       # NSIS 安装包（会先构建）
task windows:package FORMAT=msix   # MSIX 包（需先 task install:msix:tools）
wails3 build GOOS=darwin   # macOS 交叉构建（非发布目标）
```

> CLI 位于 `C:\Users\bootv\go\bin\wails3.exe`，`task` 位于同目录；未加入 PATH 时需用完整路径。
> 桌面发布目标为 **Windows 11 x64（amd64）**：`build/windows/Taskfile.yml` 的 `ARCH` 默认 `amd64`，
> 应用清单只声明 Windows 11 兼容项，不再考虑低版本 Windows。

```bash
# 离线单测：帧编解码 / CRC / BCD / CP56Time2a（含真实桩报文样本）
go test ./internal/ykc/ -v

# 离线单测：加油站定额 / 定量 / 加满自动停枪、订单结算、油机油枪编号、推送通道
go test ./internal/gas/ -v

# 全部离线单测
go test ./internal/... .

# 联网冒烟：9847~9851 分别对应 V1.5~V2.1
VCPP_SMOKE_HOST=10.88.0.81 VCPP_SMOKE_PORT=9847 go test ./internal/emu -run TestSmokeServer -v

# 完整充电流程：启动 → 实时数据 → 达标停止 → 交易记录确认
VCPP_SMOKE_HOST=10.88.0.81 VCPP_SMOKE_PORT=9847 go test ./internal/emu -run TestFullChargeFlow -v -timeout 120s

# 前端：类型检查 + 打包
cd frontend && npm run build
```

## 目录结构

```
├── main.go                 # 应用入口：RegisterEvent 注册、Options、窗口
├── app.go                  # 绑定服务层（配置 / 桩管理 / 充电控制 / 加油站 / 日志）
├── Taskfile.yml            # 构建、打包、开发任务（平台资源在 build/）
├── build/config.yml        # 产品元信息（名称、版本、标识符）
├── doc/                    # 架构图与协议文档
└── internal
    ├── ykc/                # 协议层：帧编解码、版本能力 Spec、报文构造与解析
    ├── emu/                # 充电桩模拟器核心：多桩多枪状态机、电池模型、费率统计
    ├── gas/                # 加油站模拟器核心：多油机多枪状态机、加油推进与订单、数据推送
    ├── logger/             # 日志总线：环形 100 条 + 每日文件 + 3 天清理
    └── store/              # 配置持久化
```

## 分层约定

- **`internal/ykc`（纯协议层）**：不依赖 `emu` / `store` / `logger`。
  新增报文必须**同时**提供构造（`BuildXxx`）与解析（`ParseXxx`）函数，命令码常量写入 `frame.go`，
  并在 `codec_test.go` 补充真实报文样本（现场抓包）做回归。
- **`internal/emu`（充电桩模拟器核心）**：
  - `Manager` 负责桩实例生命周期（新增 / 重建 / 移除）与节流推送；
  - `Pile` 每桩一个事件循环 goroutine + 一个读协程，全部 I/O 收在事件循环内；
  - `Session` 负责单次充电的电池模型推进、费率段统计与达标判定。
- **`internal/gas`（加油站模拟器核心）**：
  - `Manager` 负责油机实例生命周期、推送通道重建与订单历史（最近 50 条）；
  - `Dispenser` 每台油机一个事件循环 goroutine，按 1 秒步长推进各枪加油过程；
  - `Pusher` 为数据推送抽象（`none` / `mqtt` / `http`），当前实现只回传待推送内容，
    实际网络发送返回 `ErrNotImplemented`，调用方需忽略该错误；
  - 油价、油号目录统一由 `store.GasConfig.Prices` / `store.FuelOptions()` 提供，油号为字符串。
- **`internal/logger`**：环形缓冲固定 100 条，异步队列 4096，**队列满则丢弃**，绝不阻塞业务协程。
- **`internal/store`**：配置读写，先写 `.tmp` 再 `rename` 原子替换；配置损坏时回退默认值保证可启动。
- **`app.go` / `main.go`**：服务层只做参数校验、转发与事件推送，不放业务逻辑。

## 前端约定

- 只允许通过 `frontend/bindings/` 调用后端。该目录是 **wails 生成产物**（已 gitignore），不要手工编辑或提交。
- `src/api.ts` 统一封装绑定调用：`async` 收敛错误文案，并把 Go 侧 nil 切片（`null`）收敛成空数组/默认值。
- `src/types.ts` 复用绑定生成的模型类型，不要在前端重复定义结构。
- 组件职责：`TopBar`（服务端配置 / 数据推送入口）/ `ModuleRail`（模块 tab）/ `Sidebar`（桩列表）/
  `PileDetail`（桩详情）/ `GunCard`（枪实时参数）/ `LogPanel`（日志）/ `PileDialog`、`StartChargeDialog`（表单）；
  加油站：`GasSidebar`（油机列表）/ `GasStationView`（油机详情、油价、订单、推送记录）/ `NozzleCard`（枪实时参数）/
  `DispenserDialog`、`StartFuelDialog`、`GasConfigDialog`（表单）。
- 事件订阅集中在 `App.vue` 的 `onMounted`，卸载时调用返回的取消函数。

## 事件与状态

- 共四个自定义事件：`log`（单条 `logger.Entry`）、`state`（`[]emu.PileView` 快照）、
  `gasState`（`[]gas.DispenserView` 快照）、`gasPush`（单条 `gas.PushMessage` 推送记录）。
- `application.RegisterEvent[T](name)` 只能在 `init()` 中以**常量字符串**直接调用，否则绑定生成器识别不到。
- 状态变更经 `Manager.notify` 做 300ms 节流后再推送，避免实时数据高频刷新界面。

## 协议要点

- 版本能力由 `ykc.Spec` 统一描述（长度域宽度、发送时间域、交易记录/申请启动命令码、登录形态、实时数据扩展字段）。
- 端口映射：9847→V1.5、9848→V1.6、9849→V1.7、9850→V1.8、9851→V2.1。
- **已知平台行为**：服务端对所有版本（含 V2.1）的下行帧都按 V1.x 布局下发（1 字节长度域、无发送时间域）。
  因此拆帧器提供 `Spec.DecodeDown`：先按版本规则解析，失败时回退 V1.x 布局。**不要"修正"成严格按版本解析。**
- 上行序列号小端、下行按大端回写；CRC16-Modbus 低字节在前。

## 日志与持久化

- 界面只展示最近 100 条；文件 `logs/emu-YYYY-MM-DD.log` 按天切分，仅保留最近 3 天。
- 日志目录与配置目录均位于**可执行文件同目录**（`logs/`、`data/`），失败时回退系统临时目录。
- **配置文件为 YAML**：`data/config.yml`（首次运行生成带注释的默认模板），解析方式对齐服务端 vcpp：
  - 发现顺序：环境变量 `VCPP_EMU_CONFIG` → `data/config.yml` → 旧版 `data/config.json`（读取后自动迁移，原文件保留 `.bak`）；
  - 缺值 / 空值 / 越界取值在「取值点」回退默认值（`normalize`），并记录到 `Store.Notes()` 由启动日志输出，
    **只有文件结构性损坏才整体回退默认值**，绝不因单项配置异常拒绝启动；
  - 新增配置项时同步：结构体标签（`json` 给前端、`yaml` 给文件，键名用 kebab-case）、`Default()` 默认值、
    `normalize` 的区间校验、以及文件头模板注释。
- **用户填写项一律持久化**（只保留最近一份，不做历史）：
  - 服务端配置（地址 / 端口 / 版本）与桩配置由 `App.SaveConfig` 写入配置文件；
  - 「启动充电」表单（启动方式、卡号 / VIN、电池容量、起始 SOC、最高电压 / 电流、四种充电目标）
    在每次 `StartCharge` 时写入 `AppConfig.LastStartCharge`，下次打开表单回填。
  - 加油站配置由 `App.SaveConfig` 全站统一写入 `AppConfig.Gas`：数据推送形式、MQTT 服务器 /
    客户端与 topic、HTTP 接口基址与路径、油价、推送周期、油箱容积，以及各油机油号；
    「开始加油」表单（加油方式、卡号、车牌）在每次 `StartFuel` 时写入
    `AppConfig.Gas.LastStartFuel`，下次打开表单回填。
  - 油机台数与每台枪数属于「批量生成」的界面输入，**不持久化**（生成结果落在
    `gas.dispensers`，配置文件中不出现 `dispenser-count` / `nozzle-count`）。
  - 新增用户可填写字段时，必须同时接入 `store.AppConfig` 持久化并在界面回填，**不允许只活在前端状态里**。
- 表单字段在 `store.Save` 前经 `normalizeStartCharge` 归一化：数值 ≤0 或越界的 SOC 会被修正为默认值，
  字符串（卡号 / VIN）允许为空且不强制回填，以尊重用户清空。整份缺失时才套用 `DefaultStartCharge()`。

## 已知陷阱

1. `wails3 build` 依赖 `task`（go-task）。未安装时用 `go install github.com/go-task/task/v3/cmd/task@latest`。
2. **不要用 `wails3 generate build-assets`**：它会在当前目录生成平台资源并**覆盖根 `Taskfile.yml`**。
   更新平台资源请用 `task common:update:build-assets`（定义在 `build/Taskfile.yml`，以 `common` 命名空间引入，内部 `dir: build`）。
3. `frontend/bindings/` 是生成产物，切换 dev / production 模式会重新生成，不要提交。
4. 暴露给前端的结构**不要直接放 `time.Time`**（绑定层类型不友好），改用格式化字符串，如 `PileView.ConnectedAt`。
5. Go 侧切片可能为 nil，前端接收时字段类型为 `T[] | null`，必须在 `src/api.ts` 中收敛，避免界面空指针。
6. `wails3 build` 会自动生成绑定并执行前端构建，无需手工运行 `wails3 generate bindings`（除非只需更新绑定）。
7. **Windows 目标是 Win11 x64**：`build/windows/Taskfile.yml` 的 `ARCH` 默认为 `amd64`，不要改回宿主机 `ARCH`
   （否则在 ARM64 等宿主上会产出非目标架构）。
   `task common:update:build-assets` 会按 `build/config.yml` 覆盖 `windows/wails.exe.manifest`、`windows/info.json`、
   `windows/nsis/wails_tools.nsh`，执行后需重新套用其中的 Win11 专属设定：只保留 Windows 的
   `supportedOS` GUID（`{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}`，Win10/11 共用）、DPI 只留 `permonitorv2`
   （删掉 Win7/8 的 `dpiAware` 回退）、`assemblyIdentity` 的 `processorArchitecture="amd64"`。
   `windows/msix/` 下的 `template.xml`、`app_manifest.xml` 不在该任务的更新范围内，需手工维护
   （`TargetDeviceFamily` 的 `MinVersion=10.0.22000.0`，即 Windows 11）。

## 关联项目

- 云快充平台服务端（协议对端实现）：<https://github.com/vbeats/vcpp>
- 架构图：`doc/vcpp-emulator-architecture.html`（交互式，含源码说明卡片）+ `doc/vcpp-emulator-architecture.png`（README 顶部预览图）。
  由 archify 生成，源定义在 `.archify/architecture-device-emulator-*/candidate.json`；
  重新生成：`node <archify>/bin/archify.mjs finalize architecture <candidate.json> doc/vcpp-emulator-architecture.html --repo-root . --quality showcase`，
  再用 Chrome 无头模式导出 PNG（`--headless=new --window-size=1440,980 --screenshot=...`）。

## 环境陷阱（本机）

1. **archify 原子发布在本机 `doc/` 目录不可用**：`finalize` / `deliver` 写入 `doc/` 会因
   硬链接安全校验失败（`finalize-receipt-hardlinked` / `output/target-hardlinked` / `output/target-indeterminate`），
   而写入 `$env:TEMP` 正常。做法：先输出到临时目录完成全部门禁（validate / deliver / check / browser-check 全过），
   再用 `Copy-Item` 复制回 `doc/`，并清理残留的 `*.delivery-*.json`、`.archify-delivery-lock.json`。
2. archify 的仓库证据（`sources` 行号引用）按**固定 revision（HEAD）**校验；未提交的改动无法通过
   `--repo-root .` 的引用校验。提交后才能带上带源码引用的版本，否则去掉 `meta.repository` 与 `sources`。

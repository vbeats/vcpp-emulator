package ykc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// 帧起始符
const StartFlag = 0x68

// 命令码（帧类型）。桩侧上行为奇数，平台下行一般为上行码 +1。
const (
	CmdLogin                byte = 0x01 // 充电桩登录认证 ⬆️
	CmdLoginAck             byte = 0x02 // 登录认证应答 ⬇️
	CmdHeartbeat            byte = 0x03 // 心跳包 ⬆️
	CmdHeartbeatAck         byte = 0x04 // 心跳包应答 ⬇️
	CmdBillingVerify        byte = 0x05 // 计费模型验证请求 ⬆️
	CmdBillingVerifyAck     byte = 0x06 // 计费模型验证应答 ⬇️
	CmdBillingModel         byte = 0x09 // 计费模型请求 ⬆️
	CmdBillingModelAck      byte = 0x0A // 计费模型请求应答 ⬇️
	CmdReadRealTime         byte = 0x12 // 读取实时监测数据 ⬇️
	CmdRealTimeData         byte = 0x13 // 上传实时监测数据 ⬆️
	CmdHandshake            byte = 0x15 // 充电握手 ⬆️
	CmdParamConfig          byte = 0x17 // 参数配置 ⬆️
	CmdChargeFinish         byte = 0x19 // 充电结束 ⬆️
	CmdError                byte = 0x1B // 错误报文 ⬆️
	CmdBmsStop              byte = 0x1D // 充电阶段 BMS 中止 ⬆️
	CmdMotorStop            byte = 0x21 // 充电阶段充电机中止 ⬆️
	CmdApplyCharge          byte = 0x31 // 申请启动充电 ⬆️（V1.5~V1.7）
	CmdApplyChargeAck       byte = 0x32 // 确认启动充电 ⬇️
	CmdRemoteChargeAck      byte = 0x33 // 远程启机命令回复 ⬆️
	CmdRemoteCharge         byte = 0x34 // 远程控制启机 ⬇️
	CmdRemoteStopAck        byte = 0x35 // 远程停机命令回复 ⬆️
	CmdRemoteStop           byte = 0x36 // 远程停机 ⬇️
	CmdTradeUp              byte = 0x3B // 交易记录 ⬆️（V1.5/V1.6）
	CmdTradeUp2             byte = 0x3D // 交易记录 ⬆️（V1.7+）
	CmdTradeAck             byte = 0x40 // 交易记录确认 ⬇️
	CmdBalanceUpdateAck     byte = 0x41 // 余额更新应答 ⬆️
	CmdBalanceUpdate        byte = 0x42 // 远程账户余额更新 ⬇️
	CmdCardSyncAck          byte = 0x43 // 卡数据同步应答 ⬆️
	CmdCardSync             byte = 0x44 // 离线卡数据同步 ⬇️
	CmdCardClearAck         byte = 0x45 // 离线卡数据清除应答 ⬆️
	CmdCardClear            byte = 0x46 // 离线卡数据清除 ⬇️
	CmdCardQueryAck         byte = 0x47 // 离线卡数据查询应答 ⬆️
	CmdCardQuery            byte = 0x48 // 离线卡数据查询 ⬇️
	CmdDeviceError          byte = 0x50 // 设备故障上送 ⬆️（V2.1）
	CmdDeviceErrorAck       byte = 0x49 // 设备故障上送回复确认 ⬇️
	CmdTradeLogAck          byte = 0x4C // 交易记录召唤确认 ⬆️
	CmdTradeLog             byte = 0x4D // 交易记录召唤 ⬇️
	CmdMotorStart           byte = 0x4F // 充电机启动完成 ⬆️（V2.1）
	CmdMotorStartAck        byte = 0x4E // 充电机启动完成应答 ⬇️
	CmdParamSettingAck      byte = 0x51 // 工作参数设置应答 ⬆️
	CmdParamSetting         byte = 0x52 // 工作参数设置 ⬇️
	CmdTimeSyncAck          byte = 0x55 // 对时设置应答 ⬆️
	CmdTimeSync             byte = 0x56 // 对时设置 ⬇️
	CmdBillingSettingAck    byte = 0x57 // 计费模型设置应答 ⬆️
	CmdBillingSetting       byte = 0x58 // 计费模型设置 ⬇️
	CmdMaxPowerAck          byte = 0x59 // 默认最大功率下发应答 ⬆️
	CmdMaxPower             byte = 0x60 // 默认最大功率下发 ⬇️
	CmdQrSettingAck         byte = 0x5A // 二维码设置应答 ⬆️
	CmdQrSetting            byte = 0x5B // 二维码设置 ⬇️
	CmdPlatformConnectAck   byte = 0x5C // 平台连接设置应答 ⬆️
	CmdPlatformConnect      byte = 0x5D // 平台连接设置 ⬇️
	CmdParamSetting2Ack     byte = 0x5E // 参数设置应答 ⬆️
	CmdParamSetting2        byte = 0x5F // 参数设置 ⬇️
	CmdLockUp               byte = 0x61 // 地锁数据上送 ⬆️
	CmdRemoteLock           byte = 0x62 // 遥控地锁 ⬇️
	CmdRemoteLockAck        byte = 0x63 // 遥控地锁应答 ⬆️
	CmdRemoteRestartAck     byte = 0x91 // 远程重启应答 ⬆️
	CmdRemoteRestart        byte = 0x92 // 远程重启 ⬇️
	CmdRemoteUpdateAck      byte = 0x93 // 远程更新应答 ⬆️
	CmdRemoteUpdate         byte = 0x94 // 远程更新 ⬇️
	CmdKeyUpdateAck         byte = 0x95 // 密钥更新应答 ⬆️
	CmdKeyUpdate            byte = 0x96 // 密钥更新 ⬇️
	CmdLogAck               byte = 0x97 // 日志召唤应答 ⬆️
	CmdLog                  byte = 0x98 // 日志召唤 ⬇️
	CmdApplyCharge2         byte = 0xA5 // 申请启动充电 ⬆️（V1.8+）
	CmdApplyCharge2Ack      byte = 0xA6 // 确认启动充电 ⬇️
	CmdRemoteCharge2Ack     byte = 0xA7 // 远程启机命令回复 ⬆️（V1.8+）
	CmdRemoteCharge2        byte = 0xA8 // 远程控制启机 ⬇️
	CmdVinUpload            byte = 0xA9 // VIN 上报 ⬆️（V2.1）
	CmdVinUploadAck         byte = 0xAA // VIN 上报确认 ⬇️
	CmdQrPrefix             byte = 0xF0 // 二维码前缀下发 ⬇️（V1.5 补充）
	CmdQrPrefixAck          byte = 0xF1 // 二维码前缀应答 ⬆️
)

// CmdDesc 命令码描述
func CmdDesc(cmd byte) string {
	switch cmd {
	case CmdLogin:
		return "登录认证"
	case CmdLoginAck:
		return "登录认证应答"
	case CmdHeartbeat:
		return "心跳包"
	case CmdHeartbeatAck:
		return "心跳包应答"
	case CmdBillingVerify:
		return "计费模型验证请求"
	case CmdBillingVerifyAck:
		return "计费模型验证应答"
	case CmdBillingModel:
		return "计费模型请求"
	case CmdBillingModelAck:
		return "计费模型请求应答"
	case CmdReadRealTime:
		return "读取实时监测数据"
	case CmdRealTimeData:
		return "上传实时监测数据"
	case CmdHandshake:
		return "充电握手"
	case CmdParamConfig:
		return "参数配置"
	case CmdChargeFinish:
		return "充电结束"
	case CmdError:
		return "错误报文"
	case CmdBmsStop:
		return "充电阶段BMS中止"
	case CmdMotorStop:
		return "充电阶段充电机中止"
	case CmdApplyCharge, CmdApplyCharge2:
		return "申请启动充电"
	case CmdApplyChargeAck, CmdApplyCharge2Ack:
		return "确认启动充电"
	case CmdRemoteChargeAck, CmdRemoteCharge2Ack:
		return "远程启机命令回复"
	case CmdRemoteCharge, CmdRemoteCharge2:
		return "远程控制启机"
	case CmdRemoteStopAck:
		return "远程停机命令回复"
	case CmdRemoteStop:
		return "远程停机"
	case CmdTradeUp, CmdTradeUp2:
		return "交易记录"
	case CmdTradeAck:
		return "交易记录确认"
	case CmdBalanceUpdateAck:
		return "余额更新应答"
	case CmdBalanceUpdate:
		return "远程账户余额更新"
	case CmdCardSyncAck:
		return "卡数据同步应答"
	case CmdCardSync:
		return "离线卡数据同步"
	case CmdCardClearAck:
		return "离线卡数据清除应答"
	case CmdCardClear:
		return "离线卡数据清除"
	case CmdCardQueryAck:
		return "离线卡数据查询应答"
	case CmdCardQuery:
		return "离线卡数据查询"
	case CmdDeviceError:
		return "设备故障上送"
	case CmdDeviceErrorAck:
		return "设备故障回复确认"
	case CmdTradeLogAck:
		return "交易记录召唤确认"
	case CmdTradeLog:
		return "交易记录召唤"
	case CmdMotorStart:
		return "充电机启动完成"
	case CmdMotorStartAck:
		return "充电机启动完成应答"
	case CmdParamSettingAck:
		return "工作参数设置应答"
	case CmdParamSetting:
		return "工作参数设置"
	case CmdTimeSyncAck:
		return "对时设置应答"
	case CmdTimeSync:
		return "对时设置"
	case CmdBillingSettingAck:
		return "计费模型设置应答"
	case CmdBillingSetting:
		return "计费模型设置"
	case CmdMaxPowerAck:
		return "最大功率下发应答"
	case CmdMaxPower:
		return "默认最大功率下发"
	case CmdQrSettingAck:
		return "二维码设置应答"
	case CmdQrSetting:
		return "二维码设置"
	case CmdPlatformConnectAck:
		return "平台连接设置应答"
	case CmdPlatformConnect:
		return "平台连接设置"
	case CmdParamSetting2Ack:
		return "参数设置应答"
	case CmdParamSetting2:
		return "参数设置"
	case CmdLockUp:
		return "地锁数据上送"
	case CmdRemoteLock:
		return "遥控地锁"
	case CmdRemoteLockAck:
		return "遥控地锁应答"
	case CmdRemoteRestartAck:
		return "远程重启应答"
	case CmdRemoteRestart:
		return "远程重启"
	case CmdRemoteUpdateAck:
		return "远程更新应答"
	case CmdRemoteUpdate:
		return "远程更新"
	case CmdKeyUpdateAck:
		return "密钥更新应答"
	case CmdKeyUpdate:
		return "密钥更新"
	case CmdLogAck:
		return "日志召唤应答"
	case CmdLog:
		return "日志召唤"
	case CmdVinUpload:
		return "VIN 上报"
	case CmdVinUploadAck:
		return "VIN 上报确认"
	case CmdQrPrefix:
		return "二维码前缀下发"
	case CmdQrPrefixAck:
		return "二维码前缀应答"
	default:
		return fmt.Sprintf("未知帧(0x%02X)", cmd)
	}
}

// Frame 一帧完整报文
type Frame struct {
	Seq      uint16    // 序列号
	Encrypt  byte      // 加密标志：0 不加密
	Cmd      byte      // 命令码
	Body     []byte    // 消息体
	SendTime time.Time // 发送时间（仅 V2.1 存在）
}

// Encode 组上行帧。
//
// 帧结构：[0x68][数据长度 1B|2B][序列号 2B][发送时间 7B(仅V2.1)][加密标志 1B][帧类型 1B][消息体][CRC16 2B]
//   - 数据长度 = 序列号 + 发送时间 + 加密标志 + 帧类型 + 消息体
//   - 上行序列号按小端写入（服务端按 readUnsignedShortLE 解析，实测真实桩亦如此）
//   - CRC16-Modbus 计算范围自序列号首字节至消息体末字节，结果低字节在前
func (v *Spec) Encode(seq uint16, cmd byte, body []byte, ts time.Time) []byte {
	infoLen := 4
	if v.SendTimeField {
		infoLen += 7
	}
	dataLen := infoLen + len(body)

	out := make([]byte, 0, 1+v.LengthFieldLen+dataLen+2)
	out = append(out, StartFlag)
	if v.LengthFieldLen == 2 {
		out = append(out, byte(dataLen>>8), byte(dataLen))
	} else {
		out = append(out, byte(dataLen))
	}
	// 上行序列号小端
	out = append(out, byte(seq), byte(seq>>8))
	if v.SendTimeField {
		out = append(out, EncodeCP56(ts)...)
	}
	out = append(out, 0x00, cmd)
	out = append(out, body...)

	crc := CRC16Modbus(out[1+v.LengthFieldLen:])
	out = append(out, byte(crc), byte(crc>>8))
	return out
}

// ErrIncomplete 数据不足一帧，需继续接收
var ErrIncomplete = errors.New("ykc: 报文不完整")

// Decode 解析下行帧，返回帧与消耗的字节数。
// 下行序列号按大端解析（服务端以 writeShort 回写，与上行字节序相反）。
func (v *Spec) Decode(buf []byte) (Frame, int, error) {
	if len(buf) < 2 {
		return Frame{}, 0, ErrIncomplete
	}
	if buf[0] != StartFlag {
		return Frame{}, 0, fmt.Errorf("ykc: 非法起始符 0x%02X", buf[0])
	}
	headerLen := 1 + v.LengthFieldLen
	if len(buf) < headerLen {
		return Frame{}, 0, ErrIncomplete
	}
	var dataLen int
	if v.LengthFieldLen == 2 {
		dataLen = int(binary.BigEndian.Uint16(buf[1:3]))
	} else {
		dataLen = int(buf[1])
	}
	infoLen := 4
	if v.SendTimeField {
		infoLen += 7
	}
	if dataLen < infoLen {
		return Frame{}, 0, fmt.Errorf("ykc: 数据长度非法 %d", dataLen)
	}
	total := headerLen + dataLen + 2
	if len(buf) < total {
		return Frame{}, 0, ErrIncomplete
	}

	seg := buf[headerLen : headerLen+dataLen]
	want := binary.LittleEndian.Uint16(buf[total-2 : total])
	if got := CRC16Modbus(seg); got != want {
		return Frame{}, total, fmt.Errorf("ykc: CRC 校验失败, 期望 %04X 实际 %04X", want, got)
	}

	f := Frame{
		Seq:     binary.BigEndian.Uint16(seg[0:2]),
		Encrypt: seg[2],
		Cmd:     seg[3],
	}
	if len(seg) > infoLen {
		f.Body = append([]byte(nil), seg[infoLen:]...)
	}
	if v.SendTimeField {
		if t, err := DecodeCP56(seg[2:9]); err == nil {
			f.SendTime = t
		}
		f.Encrypt = seg[9]
		f.Cmd = seg[10]
		if len(seg) > infoLen {
			f.Body = append([]byte(nil), seg[infoLen:]...)
		}
	}
	return f, total, nil
}

// DecodeDown 自适应解析下行帧。
//
// 服务端对所有版本（含 V2.1）的下行帧均按 V1.x 布局下发：
// 1 字节数据长度域、无发送时间域；而上行必须按版本规则组包。
// 因此先按版本规则解析，失败时回退 V1.x 布局，保证两种形态都能正确拆帧。
func (v *Spec) DecodeDown(buf []byte) (Frame, int, error) {
	f, n, err := v.Decode(buf)
	if err == nil {
		return f, n, nil
	}
	if v.LengthFieldLen != 1 {
		if f2, n2, err2 := (&specV15).Decode(buf); err2 == nil {
			return f2, n2, nil
		}
	}
	return f, n, err
}

// FrameReader 从 TCP 字节流中拆帧，处理粘包与半包。
type FrameReader struct {
	spec *Spec
	buf  []byte
}

// NewFrameReader 创建拆帧器
func NewFrameReader(spec *Spec) *FrameReader {
	return &FrameReader{spec: spec, buf: make([]byte, 0, 512)}
}

// Feed 输入新数据，返回解析出的完整帧列表
func (r *FrameReader) Feed(data []byte) ([]Frame, error) {
	r.buf = append(r.buf, data...)
	frames := make([]Frame, 0, 4)
	for {
		f, n, err := r.spec.DecodeDown(r.buf)
		if err != nil {
			if errors.Is(err, ErrIncomplete) {
				break
			}
			// 非法帧：丢弃首字节重新同步
			r.buf = r.buf[1:]
			if len(r.buf) == 0 {
				break
			}
			continue
		}
		frames = append(frames, f)
		r.buf = append(r.buf[:0], r.buf[n:]...)
		if len(r.buf) == 0 {
			break
		}
	}
	// 防止半包无限堆积
	if len(r.buf) > 65536 {
		r.buf = r.buf[:0]
	}
	return frames, nil
}

// Reset 清空缓冲
func (r *FrameReader) Reset() { r.buf = r.buf[:0] }

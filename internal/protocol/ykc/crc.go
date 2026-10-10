package ykc

// CRC16 计算 CRC16-Modbus（多项式 0xA001，初值 0xFFFF），与 vcpp 侧 Hutool CRC16Modbus 一致。
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// CheckCRC 校验数据域与帧尾的 CRC（帧尾为低字节在前）。
func CheckCRC(data []byte, crc uint16) bool {
	return CRC16(data) == crc
}

// Package logger 提供设备模拟器的日志能力：
//   - 内存环形缓冲（仅保留最近 100 条，供界面滚动展示）
//   - 按天切分的日志文件（应用安装目录/logs），仅保留最近 3 天
//   - 变更回调（用于向前端实时推送）
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level 日志级别
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Entry 单条日志
type Entry struct {
	ID      int64  `json:"id"`      // 自增序号，前端用于排序/去重
	Time    string `json:"time"`    // HH:MM:SS.mmm
	Level   string `json:"level"`   // info / warn / error
	Pile    string `json:"pile"`    // 桩号，全局日志为空
	Gun     int    `json:"gun"`     // 枪号，0 表示不区分
	Dir     string `json:"dir"`     // TX(桩→平台) / RX(平台→桩) / --(本地)
	Cmd     string `json:"cmd"`     // 帧类型描述，如 0x01 登录认证
	Message string `json:"message"` // 摘要
	Hex     string `json:"hex"`     // 报文 HEX（可为空）
}

const (
	ringCapacity  = 100  // 界面滚动展示的最大条数
	retainDays    = 3    // 日志文件保留天数
	queueCapacity = 4096 // 异步写队列容量，满则丢弃，避免内存无限增长
)

// Bus 日志总线
type Bus struct {
	mu       sync.Mutex
	ring     []Entry
	seq      int64
	emitter  func(Entry)
	queue    chan Entry
	stopCh   chan struct{}
	wg       sync.WaitGroup
	dir      string
	file     *os.File
	fileDay  string
	closeOne sync.Once
}

// New 创建日志总线，dir 为日志文件存放目录（应用安装目录/logs）。
// 返回的 Bus 必须在使用结束时调用 Close 释放文件与协程资源。
func New(dir string) (*Bus, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	b := &Bus{
		ring:   make([]Entry, 0, ringCapacity),
		queue:  make(chan Entry, queueCapacity),
		stopCh: make(chan struct{}),
		dir:    dir,
	}
	if err := b.rotate(time.Now()); err != nil {
		return nil, err
	}
	b.cleanup(time.Now())
	b.wg.Add(1)
	go b.writeLoop()
	return b, nil
}

// SetEmitter 设置日志变更回调（由 Wails 侧注入事件推送函数）
func (b *Bus) SetEmitter(fn func(Entry)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.emitter = fn
}

// Dir 日志目录
func (b *Bus) Dir() string { return b.dir }

// Infof 记录本地信息日志
func (b *Bus) Infof(format string, args ...any) {
	b.push(Entry{Level: string(LevelInfo), Dir: "--", Message: fmt.Sprintf(format, args...)})
}

// Warnf 记录本地告警日志
func (b *Bus) Warnf(format string, args ...any) {
	b.push(Entry{Level: string(LevelWarn), Dir: "--", Message: fmt.Sprintf(format, args...)})
}

// Errorf 记录本地错误日志
func (b *Bus) Errorf(format string, args ...any) {
	b.push(Entry{Level: string(LevelError), Dir: "--", Message: fmt.Sprintf(format, args...)})
}

// Frame 记录一帧通信日志
func (b *Bus) Frame(level Level, pile string, gun int, dir string, cmd string, message string, hex string) {
	b.push(Entry{
		Level:   string(level),
		Pile:    pile,
		Gun:     gun,
		Dir:     dir,
		Cmd:     cmd,
		Message: message,
		Hex:     hex,
	})
}

// Recent 返回最近的日志（按时间正序，最多 ringCapacity 条）
func (b *Bus) Recent() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Entry, len(b.ring))
	copy(out, b.ring)
	return out
}

// Close 关闭日志总线，flush 队列并释放资源
func (b *Bus) Close() {
	b.closeOne.Do(func() {
		close(b.stopCh)
	})
	b.wg.Wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file != nil {
		_ = b.file.Sync()
		_ = b.file.Close()
		b.file = nil
	}
}

// push 写入环形缓冲、异步队列并推送事件
func (b *Bus) push(e Entry) {
	now := time.Now()
	e.Time = now.Format("15:04:05.000")

	b.mu.Lock()
	b.seq++
	e.ID = b.seq
	if len(b.ring) >= ringCapacity {
		b.ring = b.ring[1:]
	}
	b.ring = append(b.ring, e)
	emitter := b.emitter
	b.mu.Unlock()

	// 异步落盘：队列满时丢弃，绝不阻塞业务协程
	select {
	case b.queue <- e:
	default:
	}

	if emitter != nil {
		emitter(e)
	}
}

func (b *Bus) writeLoop() {
	defer b.wg.Done()
	for {
		select {
		case <-b.stopCh:
			// 关闭前排空剩余队列
			for {
				select {
				case e := <-b.queue:
					b.write(e)
				default:
					return
				}
			}
		case e := <-b.queue:
			b.write(e)
		}
	}
}

func (b *Bus) write(e Entry) {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()

	day := now.Format("2006-01-02")
	if day != b.fileDay {
		if err := b.rotate(now); err != nil {
			fmt.Fprintf(os.Stderr, "logger: 切换日志文件失败: %v\n", err)
			return
		}
		b.cleanup(now)
	}
	if b.file == nil {
		return
	}
	_, _ = fmt.Fprintf(b.file, "%s [%s] %s | %s | %s | %s\n",
		now.Format("2006-01-02 15:04:05.000"),
		strings.ToUpper(e.Level),
		e.Dir,
		pileLabel(e),
		e.Cmd,
		e.Message)
	if e.Hex != "" {
		_, _ = fmt.Fprintf(b.file, "        HEX: %s\n", e.Hex)
	}
}

func pileLabel(e Entry) string {
	if e.Pile == "" {
		return "system"
	}
	if e.Gun > 0 {
		return fmt.Sprintf("%s/枪%d", e.Pile, e.Gun)
	}
	return e.Pile
}

// rotate 切换到当天日志文件
func (b *Bus) rotate(now time.Time) error {
	if b.file != nil {
		_ = b.file.Sync()
		_ = b.file.Close()
		b.file = nil
	}
	day := now.Format("2006-01-02")
	f, err := os.OpenFile(filepath.Join(b.dir, "emu-"+day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	b.file = f
	b.fileDay = day
	return nil
}

// cleanup 清理超过保留天数的日志文件
func (b *Bus) cleanup(now time.Time) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return
	}
	deadline := now.AddDate(0, 0, -retainDays)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "emu-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		// 仅保留最近 3 天内的日志
		if info.ModTime().Before(deadline) {
			_ = os.Remove(filepath.Join(b.dir, entry.Name()))
		}
	}
}

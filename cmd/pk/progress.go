package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// progressBar 是下载进度渲染器（方案 §4.1：TTY 用 \r，非 TTY 退化为逐行）。
//
// 不引第三方进度条库：这里就 60 行，且要能精确控制"非 TTY 时不刷屏"。
type progressBar struct {
	out    io.Writer
	isTTY  bool
	label  string
	last   time.Time
	lastAt int64
	start  time.Time
	wrote  bool
}

// newProgress 判断输出是否是终端（字符设备）。
func newProgress(out io.Writer, label string) *progressBar {
	isTTY := false
	if f, ok := out.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			isTTY = st.Mode()&os.ModeCharDevice != 0
		}
	}
	return &progressBar{out: out, isTTY: isTTY, label: label, start: time.Now()}
}

// Update 由 fetch 的 Progress 回调驱动。
func (p *progressBar) Update(done, total int64) {
	if p == nil {
		return
	}
	now := time.Now()
	if p.isTTY {
		if now.Sub(p.last) < 80*time.Millisecond && done != total {
			return
		}
	} else {
		// 非 TTY：每 25% 一行，避免日志爆炸
		if total > 0 {
			step := total / 4
			if step > 0 && done-p.lastAt < step && done != total {
				return
			}
		} else if p.wrote || done < 1<<20 {
			return
		}
	}
	p.last, p.lastAt, p.wrote = now, done, true
	p.render(done, total)
}

// render 打印一行/刷新一行。
func (p *progressBar) render(done, total int64) {
	elapsed := time.Since(p.start).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(done) / elapsed / (1 << 20)
	}
	if total > 0 {
		pct := float64(done) / float64(total) * 100
		line := fmt.Sprintf("%s %s/%s %3.0f%% (%.1f MB/s)",
			p.label, humanBytes(done), humanBytes(total), pct, speed)
		if p.isTTY {
			p.clear()
			fmt.Fprintf(p.out, "\r%s", line)
		} else {
			fmt.Fprintln(p.out, line)
		}
		return
	}
	line := fmt.Sprintf("%s %s (%.1f MB/s)", p.label, humanBytes(done), speed)
	if p.isTTY {
		p.clear()
		fmt.Fprintf(p.out, "\r%s", line)
	} else {
		fmt.Fprintln(p.out, line)
	}
}

// Done 收尾（换行，避免后面的日志接在进度行后面）。
func (p *progressBar) Done() {
	if p == nil || !p.wrote {
		return
	}
	if p.isTTY {
		p.clear()
	}
	p.wrote = false
}

// clear 用空格覆盖上一行（不打 ANSI 控制符，Windows 老终端也安全）。
func (p *progressBar) clear() {
	fmt.Fprint(p.out, "\r"+strings.Repeat(" ", 78)+"\r")
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, u := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

// genicon 生成 GUI 的图标资源（纯标准库，无外部依赖）：
//
//	assets/icon.png         512×512，彩色（Windows/Linux 窗口与应用图标）
//	assets/tray-template.png 32×32，纯黑+透明（macOS 托盘模板图标，系统会自动反色）
//
// 设计：一个"巢"的抽象——两个叠起来的圆角方块 + 缺口，代表"把运行时装进一个根目录"。
// 用 go run ./packaging/genicon 重新生成（GUI 不在行数门禁里，这里怎么画都无所谓）。
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

func main() {
	if err := os.MkdirAll("assets", 0o755); err != nil {
		log.Fatal(err)
	}
	write("assets/icon.png", render(512, color.RGBA{0x0a, 0x7d, 0x4f, 0xff}, false))
	write("assets/tray-template.png", render(32, color.RGBA{0, 0, 0, 0xff}, true))
	log.Println("已生成 assets/icon.png 与 assets/tray-template.png")
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

// render 画"巢"标记：外层圆角方框 + 内层实心圆角块，右上角留一个缺口（打开的口）。
func render(size int, c color.RGBA, template bool) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	f := float64(size)
	outer := roundedRect{0.08 * f, 0.08 * f, 0.92 * f, 0.92 * f, 0.22 * f}
	inner := roundedRect{0.26 * f, 0.26 * f, 0.74 * f, 0.74 * f, 0.14 * f}
	notch := circle{0.74 * f, 0.26 * f, 0.20 * f}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			var a uint8
			switch {
			case notch.contains(px, py):
				a = 0 // 缺口
			case outer.contains(px, py) && !inner.contains(px, py):
				a = 0xff // 环
			case inner.contains(px, py):
				a = 0xb0 // 实心块（略透，层次感）
			}
			if template && a > 0 {
				a = 0xff
			}
			if a > 0 {
				img.Set(x, y, color.RGBA{c.R, c.G, c.B, a})
			}
		}
	}
	return img
}

type roundedRect struct{ x0, y0, x1, y1, r float64 }

// contains 用"到内缩矩形的距离"做圆角判定（够用且无依赖）。
func (r roundedRect) contains(x, y float64) bool {
	if x < r.x0 || x > r.x1 || y < r.y0 || y > r.y1 {
		return false
	}
	dx := math.Max(r.x0+r.r-x, 0) + math.Max(x-(r.x1-r.r), 0)
	dy := math.Max(r.y0+r.r-y, 0) + math.Max(y-(r.y1-r.r), 0)
	return math.Hypot(dx, dy) <= r.r
}

type circle struct{ cx, cy, r float64 }

func (c circle) contains(x, y float64) bool {
	return math.Hypot(x-c.cx, y-c.cy) <= c.r
}

//go:build ignore

// Command genicon renders the BunkrDownloader application icon.
//
// The mark is a rounded square with the violet→indigo→cyan gradient and a
// white download arrow, matching the web UI's accent ramp.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var (
	gradStart = color.NRGBA{0x7C, 0x5C, 0xFF, 0xFF}
	gradMid   = color.NRGBA{0x4F, 0x8C, 0xFF, 0xFF}
	gradEnd   = color.NRGBA{0x22, 0xD3, 0xEE, 0xFF}
)

func lerp(a, b uint8, t float64) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t) }

func gradientAt(x, y, size int) color.NRGBA {
	t := float64(x) / float64(size)
	if v := float64(y) / float64(size) * 0.6; v > t {
		t = v
	}
	switch {
	case t < 0.5:
		u := t / 0.5
		return color.NRGBA{lerp(gradStart.R, gradMid.R, u), lerp(gradStart.G, gradMid.G, u),
			lerp(gradStart.B, gradMid.B, u), 0xFF}
	default:
		u := (t - 0.5) / 0.5
		return color.NRGBA{lerp(gradMid.R, gradEnd.R, u), lerp(gradMid.G, gradEnd.G, u),
			lerp(gradMid.B, gradEnd.B, u), 0xFF}
	}
}

// roundedRect reports whether (x,y) is inside a rounded square.
func roundedRect(x, y, size, radius int) bool {
	fx, fy := float64(x)+0.5, float64(y)+0.5
	s, r := float64(size), float64(radius)
	cx := math.Min(math.Max(fx, r), s-r)
	cy := math.Min(math.Max(fy, r), s-r)
	dx, dy := fx-cx, fy-cy
	return dx*dx+dy*dy <= r*r
}

// inArrow reports whether the pixel belongs to the white download glyph.
func inArrow(x, y, size int) bool {
	u := float64(x) / float64(size)
	v := float64(y) / float64(size)

	// Shaft: a vertical bar in the middle.
	if math.Abs(u-0.5) < 0.055 && v > 0.24 && v < 0.60 {
		return true
	}
	// Head: a downward triangle.
	if v >= 0.52 && v <= 0.70 {
		halfWidth := 0.20 * (1 - (v-0.52)/0.18)
		if math.Abs(u-0.5) <= halfWidth {
			return true
		}
	}
	// Base: the tray the arrow lands on.
	if math.Abs(u-0.5) < 0.26 && v >= 0.76 && v <= 0.845 {
		return true
	}
	return false
}

func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	radius := size / 5
	if radius < 2 {
		radius = 2
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !roundedRect(x, y, size, radius) {
				img.Set(x, y, color.NRGBA{})
				continue
			}
			c := gradientAt(x, y, size)
			if inArrow(x, y, size) {
				c = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// icoFromPNG wraps the PNG data in an ICO container holding a single image.
func icoFromPNG(pngData []byte, size int) []byte {
	const header = 6
	const entry = 16
	out := make([]byte, 0, header+entry+len(pngData))
	put16 := func(v int) { out = append(out, byte(v), byte(v>>8)) }
	put32 := func(v int) { out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }

	put16(0)                                                // reserved
	put16(1)                                                // type: icon
	put16(1)                                                // image count
	out = append(out, byte(size%256), byte(size/256), 0, 0) // width, height
	put16(1)                                                // colour planes
	put16(32)                                               // bits per pixel
	put32(len(pngData))                                     // data size
	put32(header + entry)                                   // data offset
	out = append(out, pngData...)
	return out
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: genicon <output-dir>")
		os.Exit(1)
	}
	dir := os.Args[1]

	sizes := []int{16, 24, 32, 48, 64, 128, 256, 512, 1024}
	largest := 0
	var largestPNG []byte
	for _, s := range sizes {
		img := render(s)
		{
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("icon-%d.png", s)))
			if err != nil {
				panic(err)
			}
			if err := png.Encode(f, img); err != nil {
				panic(err)
			}
			_ = f.Close()
		}
		if s >= 256 {
			largest = s
			largestPNG = mustPNG(img)
		}
	}

	// appicon.png is the macOS / generic source.
	if err := os.WriteFile(filepath.Join(dir, "appicon.png"), largestPNG, 0o644); err != nil {
		panic(err)
	}
	// icon.ico is what the Windows build embeds in the executable.
	if err := os.WriteFile(filepath.Join(dir, "icon.ico"), icoFromPNG(largestPNG, largest), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %d sizes to %s (appicon %dx%d)\n", len(sizes), dir, largest, largest)
}

func mustPNG(img image.Image) []byte {
	f, err := os.CreateTemp("", "icon*.png")
	if err != nil {
		panic(err)
	}
	defer os.Remove(f.Name())
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	_ = f.Close()
	data, err := os.ReadFile(f.Name())
	if err != nil {
		panic(err)
	}
	return data
}

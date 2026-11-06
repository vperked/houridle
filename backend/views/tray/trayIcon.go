package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// iconSizes are the bitmaps packed into the .ico; Windows picks the best fit
// for the tray's DPI.
var iconSizes = []int{16, 32, 48}

// Icon returns houridle's ◆ tray icon as .ico file bytes (PNG-compressed entries).
func Icon() []byte {
	images := make([][]byte, len(iconSizes))
	for i, size := range iconSizes {
		images[i] = diamondPNG(size)
	}

	const headerSize, entrySize = 6, 16
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(images))}) // reserved, type=icon, count
	offset := headerSize + entrySize*len(images)
	for i, data := range images {
		size := byte(iconSizes[i])
		buf.Write([]byte{size, size, 0, 0})                       // width, height, palette, reserved
		binary.Write(&buf, binary.LittleEndian, [2]uint16{1, 32}) // planes, bits per pixel
		binary.Write(&buf, binary.LittleEndian, [2]uint32{uint32(len(data)), uint32(offset)})
		offset += len(data)
	}
	for _, data := range images {
		buf.Write(data)
	}
	return buf.Bytes()
}

// diamondPNG draws an antialiased diamond with a soft top-to-bottom gradient.
func diamondPNG(size int) []byte {
	const samples = 4 // per axis, for antialiasing
	top := color.NRGBA{0xA8, 0xE4, 0xFF, 0xFF}
	bottom := color.NRGBA{0x3F, 0xA7, 0xE0, 0xFF}

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	center := float64(size) / 2
	radius := float64(size) * 0.47
	for y := range size {
		t := float64(y) / float64(size-1)
		fill := color.NRGBA{
			R: lerp(top.R, bottom.R, t),
			G: lerp(top.G, bottom.G, t),
			B: lerp(top.B, bottom.B, t),
		}
		for x := range size {
			inside := 0
			for sy := range samples {
				for sx := range samples {
					px := float64(x) + (float64(sx)+0.5)/samples - center
					py := float64(y) + (float64(sy)+0.5)/samples - center
					if math.Abs(px)+math.Abs(py) <= radius {
						inside++
					}
				}
			}
			if inside > 0 {
				fill.A = uint8(255 * inside / (samples * samples))
				img.SetNRGBA(x, y, fill)
			}
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

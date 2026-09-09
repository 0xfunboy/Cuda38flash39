// Command favicon converts a square PNG into a multiresolution ICO.
// No image generation, recoloring or background removal occurs here.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	in := flag.String("input", "web/assets/haloclu-icon.png", "square source PNG")
	out := flag.String("output", "web/favicon.ico", "output ICO")
	flag.Parse()
	if err := convert(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func convert(input, output string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	b := src.Bounds()
	if b.Dx() != b.Dy() || b.Dx() < 256 {
		return fmt.Errorf("source must be a square PNG at least 256px")
	}
	sizes := []int{16, 32, 48, 64, 128, 256}
	frames := make([][]byte, len(sizes))
	for i, size := range sizes {
		// Area average premultiplied color preserves transparent edge coverage.
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				x0, x1 := x*b.Dx()/size, (x+1)*b.Dx()/size
				y0, y1 := y*b.Dy()/size, (y+1)*b.Dy()/size
				var red, green, blue, alpha, n uint64
				for sy := y0; sy < y1; sy++ {
					for sx := x0; sx < x1; sx++ {
						r, g, bl, a := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
						red += uint64(r)
						green += uint64(g)
						blue += uint64(bl)
						alpha += uint64(a)
						n++
					}
				}
				dst.SetRGBA(x, y, color.RGBA{uint8(red / n / 257), uint8(green / n / 257), uint8(blue / n / 257), uint8(alpha / n / 257)})
			}
		}
		var frame bytes.Buffer
		if err := png.Encode(&frame, dst); err != nil {
			return err
		}
		frames[i] = frame.Bytes()
	}
	buf := make([]byte, 6+16*len(sizes))
	binary.LittleEndian.PutUint16(buf[2:4], 1)
	binary.LittleEndian.PutUint16(buf[4:6], uint16(len(sizes)))
	offset := len(buf)
	for i, size := range sizes {
		entry := buf[6+i*16 : 6+(i+1)*16]
		entry[0], entry[1] = byte(size), byte(size) // ICO uses zero for 256px.
		binary.LittleEndian.PutUint16(entry[4:6], 1)
		binary.LittleEndian.PutUint16(entry[6:8], 32)
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(frames[i])))
		binary.LittleEndian.PutUint32(entry[12:16], uint32(offset))
		offset += len(frames[i])
	}
	for _, frame := range frames {
		buf = append(buf, frame...)
	}
	return os.WriteFile(output, buf, 0644)
}

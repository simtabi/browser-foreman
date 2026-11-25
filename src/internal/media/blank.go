package media

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
)

// A blank snapshot is a page Chrome pictured before it drew anything, or one that draws nothing without scripts it
// was not given time for: one colour, nearly edge to edge.
const (
	blankShare = 0.985 // the share of sampled pixels that must match the commonest colour
	blankTone  = 12    // how far, per channel (0-255), a pixel may stray and still match
	// smallThumb is the size under which a kept snapshot may be blank and is worth decoding to find out. Measured
	// 2026-11-25: a one-colour 640x400 JPEG at the kept quality is under 5 KB; the median real snapshot is 27 KB.
	smallThumb = 12 << 10
)

// Blank reports whether a picture is one colour, nearly edge to edge.
func Blank(img image.Image) bool {
	b := img.Bounds()
	step := max(1, min(b.Dx(), b.Dy())/80)
	var counts [4096]int // 16 levels per channel
	var samples [][3]uint8
	bucket := func(p [3]uint8) int { return int(p[0]>>4)<<8 | int(p[1]>>4)<<4 | int(p[2]>>4) }
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			p := [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}
			samples = append(samples, p)
			counts[bucket(p)]++
		}
	}
	if len(samples) == 0 {
		return true
	}
	mode := 0
	for k, n := range counts {
		if n > counts[mode] {
			mode = k
		}
	}
	// The commonest colour, as the average of the samples in its bucket.
	var sum [3]int
	n := 0
	for _, p := range samples {
		if bucket(p) == mode {
			sum[0], sum[1], sum[2], n = sum[0]+int(p[0]), sum[1]+int(p[1]), sum[2]+int(p[2]), n+1
		}
	}
	c := [3]int{sum[0] / n, sum[1] / n, sum[2] / n}
	near := 0
	for _, p := range samples {
		if abs(int(p[0])-c[0]) <= blankTone && abs(int(p[1])-c[1]) <= blankTone && abs(int(p[2])-c[2]) <= blankTone {
			near++
		}
	}
	return float64(near) >= blankShare*float64(len(samples))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// blankFile reports whether a kept snapshot is blank. Only small files are decoded; an unreadable one counts as blank,
// so it is taken again.
func blankFile(path string, size int64) bool {
	if size > smallThumb {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	return err != nil || Blank(img)
}

// MayBeBlank reports, without reading it, whether a page's kept snapshot is small enough to be blank (BlankThumb
// then tells). Measured 2026-11-25 on 4,796 kept snapshots: 1,779 of the 1,821 blank ones are under smallThumb.
func (x Index) MayBeBlank(u string) bool {
	size, ok := x.thumbs[ThumbKey(u)]
	return ok && size <= smallThumb
}

// BlankThumb reports whether a page's kept snapshot is blank, decoding it when it is small enough to be.
func (x Index) BlankThumb(u string) bool {
	k := ThumbKey(u)
	size, ok := x.thumbs[k]
	return ok && blankFile(filepath.Join(x.dir, k+".jpg"), size)
}

package designs

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"

	// The decoders are registered for their side effect: the standard library's PNG, JPEG and GIF.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/keymap"
)

// maxPixels bounds what a picture may decode to (a hostile or accidental huge file must not
// exhaust the memory of the process that paints it).
const (
	maxPixels    = 64 << 20
	maxFileBytes = 64 << 20
)

// ImageCellsFile paints a picture file across the keys: it is scaled to the 22 x 6 key grid.
// PNG, JPEG and GIF are read (the Python tool also read whatever Pillow knew).
func ImageCellsFile(path string) ([]keymap.RGB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, firmware.Errorf("cannot read the image: %v", err)
	}
	defer func() { _ = f.Close() }() // read-only: nothing to lose on close
	return ImageCells(f)
}

// ImageCells scales a decoded picture to the key grid. Transparency is ignored the way
// Pillow's convert("RGB") ignores it: the colour channels are used as stored.
func ImageCells(r io.Reader) ([]keymap.RGB, error) {
	img, err := decode(r)
	if err != nil {
		return nil, firmware.Errorf("cannot read the image: %v", err)
	}
	b := img.Bounds()
	src := make([]byte, 0, b.Dx()*b.Dy()*3)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			src = append(src, c.R, c.G, c.B)
		}
	}
	small := resizeLanczos(src, b.Dx(), b.Dy(), keymap.Columns, keymap.Rows)
	cv := make([]keymap.RGB, keymap.NumCells)
	for col := 0; col < keymap.Columns; col++ {
		for row := 0; row < keymap.Rows; row++ {
			if i := col*keymap.Rows + row; i < keymap.LitCells {
				o := (row*keymap.Columns + col) * 3
				cv[i] = keymap.RGB{small[o], small[o+1], small[o+2]}
			}
		}
	}
	return cv, nil
}

func decode(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("the file is larger than %d MiB", maxFileBytes>>20)
	}
	// DecodeConfig reads the header only, so the size can be refused before anything is allocated.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("a %dx%d picture is too large", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// resizeLanczos scales an RGB image with Pillow's LANCZOS resampling (a = 3, support widened
// by the shrink factor so a big picture is averaged, not sampled), horizontal pass first,
// in the same 22-bit fixed point, so pictures paint exactly as they did with Pillow.
func resizeLanczos(src []byte, w, h, outW, outH int) []byte {
	tmp := src
	if w != outW {
		tmp = resizeAxis(tmp, w, h, outW, true)
	}
	if h != outH {
		tmp = resizeAxis(tmp, outW, h, outH, false)
	}
	return tmp
}

const precisionBits = 22

func lanczos(x float64) float64 {
	if x <= -3 || x >= 3 {
		return 0
	}
	return sinc(x) * sinc(x/3)
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	x *= math.Pi
	return math.Sin(x) / x
}

type kernel struct {
	min    int
	coeffs []int64
}

func kernels(inSize, outSize int) []kernel {
	scale := float64(inSize) / float64(outSize)
	filterScale := math.Max(scale, 1)
	support := 3 * filterScale
	out := make([]kernel, outSize)
	for xx := range out {
		center := (float64(xx) + 0.5) * scale
		ss := 1 / filterScale
		xmin := int(center - support + 0.5)
		if xmin < 0 {
			xmin = 0
		}
		xmax := int(center + support + 0.5)
		if xmax > inSize {
			xmax = inSize
		}
		xmax -= xmin
		w := make([]float64, xmax)
		var sum float64
		for x := range w {
			w[x] = lanczos((float64(x+xmin) - center + 0.5) * ss)
			sum += w[x]
		}
		k := kernel{min: xmin, coeffs: make([]int64, xmax)}
		for x := range w {
			if sum != 0 {
				w[x] /= sum
			}
			f := w[x] * (1 << precisionBits)
			if w[x] < 0 {
				k.coeffs[x] = int64(f - 0.5)
			} else {
				k.coeffs[x] = int64(f + 0.5)
			}
		}
		out[xx] = k
	}
	return out
}

func clip8(v int64) byte {
	v >>= precisionBits
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return byte(v)
}

// resizeAxis resamples one axis of an RGB image of w x h to outSize along it.
func resizeAxis(src []byte, w, h, outSize int, horizontal bool) []byte {
	if horizontal {
		ks := kernels(w, outSize)
		dst := make([]byte, outSize*h*3)
		for y := 0; y < h; y++ {
			for xx, k := range ks {
				for c := 0; c < 3; c++ {
					acc := int64(1) << (precisionBits - 1)
					for i, coef := range k.coeffs {
						acc += int64(src[(y*w+k.min+i)*3+c]) * coef
					}
					dst[(y*outSize+xx)*3+c] = clip8(acc)
				}
			}
		}
		return dst
	}
	ks := kernels(h, outSize)
	dst := make([]byte, w*outSize*3)
	for yy, k := range ks {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				acc := int64(1) << (precisionBits - 1)
				for i, coef := range k.coeffs {
					acc += int64(src[((k.min+i)*w+x)*3+c]) * coef
				}
				dst[(yy*w+x)*3+c] = clip8(acc)
			}
		}
	}
	return dst
}

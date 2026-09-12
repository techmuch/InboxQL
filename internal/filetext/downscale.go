package filetext

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // so a PNG page image decodes too
)

// Downscale shrinks a JPEG so its longest edge is at most maxEdge pixels.
//
// # Why this is necessary rather than tidy
//
// A scanned page in this mailbox is 6800x8800 — two megabytes of JPEG, and
// about sixty million pixels. Sending that to a model means base64-encoding
// three megabytes per page and asking it to tile an image far beyond what it
// will attend to; the practical result is a slow request that reads worse than
// a smaller one would. Every vision model resizes internally anyway, so the
// only question is whether the resizing happens before or after the bytes
// cross the wire.
//
// Returns the original bytes unchanged when the image is already small enough,
// so a caller can always use the result.
func Downscale(data []byte, maxEdge int) ([]byte, error) {
	if maxEdge <= 0 {
		return data, nil
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("reading the image header: %w", err)
	}
	if config.Width <= maxEdge && config.Height <= maxEdge {
		return data, nil
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding the image: %w", err)
	}

	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := float64(maxEdge) / float64(max(width, height))
	dstW := max(1, int(float64(width)*scale))
	dstH := max(1, int(float64(height)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	// Box filter: average every source pixel that falls in a destination
	// pixel. Nearest-neighbour would be a line of code less and would drop
	// most of the ink on a page of small print — which, for something whose
	// entire purpose is to be read afterwards, is the whole ballgame.
	for y := 0; y < dstH; y++ {
		y0 := bounds.Min.Y + y*height/dstH
		y1 := bounds.Min.Y + (y+1)*height/dstH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dstW; x++ {
			x0 := bounds.Min.X + x*width/dstW
			x1 := bounds.Min.X + (x+1)*width/dstW
			if x1 <= x0 {
				x1 = x0 + 1
			}

			var r, g, b, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					sr, sg, sb, _ := src.At(sx, sy).RGBA()
					r += uint64(sr >> 8)
					g += uint64(sg >> 8)
					b += uint64(sb >> 8)
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i] = uint8(r / n)
			dst.Pix[i+1] = uint8(g / n)
			dst.Pix[i+2] = uint8(b / n)
			dst.Pix[i+3] = 0xFF
		}
	}

	var out bytes.Buffer
	// Quality 85: text survives it, and the difference from 100 is roughly
	// half the bytes on a page of scanned print.
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("re-encoding the image: %w", err)
	}
	return out.Bytes(), nil
}

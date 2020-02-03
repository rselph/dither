// dither project main.go
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"hash/crc64"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/nfnt/resize"
	"golang.org/x/image/tiff"
)

type imageParams struct {
	xBlocks       int
	yBlocks       int
	seed          int64
	smooth        bool
	rescaleOutput bool
	gamma         float64
	colorDither   bool
	blurRadius    float64
	decodeLUT     []uint16
}

var config imageParams

const A = 0.985

func main() {
	flag.IntVar(&config.xBlocks, "x", 0, "Blocks on horizontal side.")
	flag.IntVar(&config.yBlocks, "y", 0, "Blocks on vertical side.")
	flag.Int64Var(&config.seed, "r", 0, "Random number seed for dithering.")
	flag.BoolVar(&config.smooth, "s", false, "Produce smoother look.")
	flag.BoolVar(&config.rescaleOutput, "o", false, "Output image is one pixel per block.")
	flag.Float64Var(&config.gamma, "g", 0.0, "Gamma of input image. If 0.0, then assume sRGB.")
	flag.BoolVar(&config.colorDither, "c", false, "Dither in color.")
	flag.Float64Var(&config.blurRadius, "b", 1.0, "Blur radius (zero to disable)")
	flag.Parse()

	outputGammaInit()

	var expandedList []string
	for _, fname := range flag.Args() {
		matches, err := filepath.Glob(fname)
		if err == nil && matches != nil {
			expandedList = append(expandedList, matches...)
		} else {
			expandedList = append(expandedList, fname)
		}
	}

	await := &sync.WaitGroup{}
	for _, fname := range expandedList {
		await.Add(1)
		go func(filename string) {
			defer await.Done()
			config.do(filename)
		}(fname)
	}
	await.Wait()
}

func (p *imageParams) do(filename string) {
	p.gammaInit()
	dithered := p.ditherImage(p.imgFromFName(filename))
	if p.blurRadius != 0.0 {
		dithered = gaussianBlur(dithered, p.blurRadius)
	}
	p.save(dithered, filename)
}

func (p *imageParams) imgFromFName(fname string) image.Image {
	f, err := os.Open(fname)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	return transcode(img, p.decodeLUT)
}

func (p *imageParams) save(i image.Image, name string) {
	var typeName string
	if p.smooth {
		typeName = "s"
	} else {
		typeName = "d"
	}

	var (
		sizeX int
		sizeY int
	)

	switch {
	case p.xBlocks != 0 && p.yBlocks != 0:
		sizeX = p.xBlocks
		sizeY = p.yBlocks

	case p.xBlocks == 0 && p.yBlocks != 0:
		sizeX = p.yBlocks * i.Bounds().Size().X / i.Bounds().Size().Y
		sizeY = p.yBlocks

	case p.xBlocks != 0 && p.yBlocks == 0:
		sizeX = p.xBlocks
		sizeY = p.xBlocks * i.Bounds().Size().Y / i.Bounds().Size().X

	default:
		sizeX = i.Bounds().Size().X
		sizeY = i.Bounds().Size().Y
	}

	w, err := os.Create(fmt.Sprintf("%s.%s%04dx%04d.tiff", name, typeName, sizeX, sizeY))
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	out := transcode(i, encodeLUT)
	err = tiff.Encode(w, out, &tiff.Options{Compression: tiff.Deflate, Predictor: true})
	if err != nil {
		log.Fatal(err)
	}
}

var white = color.Gray16{Y: 65535}
var black = color.Gray16{Y: 0}

func (p *imageParams) ditherImage(i image.Image) image.Image {
	if p.xBlocks == 0 && p.yBlocks == 0 {
		return p.ditherImage1to1(i)
	}

	smaller := resize.Resize(uint(p.xBlocks), uint(p.yBlocks), i, resize.Lanczos3)
	dith := p.ditherImage1to1(smaller)
	finalWidth := uint(i.Bounds().Size().X)
	finalHeight := uint(i.Bounds().Size().Y)
	if p.smooth {
		return resize.Resize(finalWidth, finalHeight, dith, resize.Bicubic)
	} else {
		if p.rescaleOutput {
			return dith
		} else {
			return resize.Resize(finalWidth, finalHeight, dith, resize.NearestNeighbor)
		}
	}
}

func (p *imageParams) ditherImage1to1(i image.Image) image.Image {
	if p.colorDither {
		return p.ditherImage1to1Color(i)
	}

	b := i.Bounds()
	d := image.NewGray16(b)

	wg := sync.WaitGroup{}
	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		wg.Add(1)
		go func(y int) {
			defer wg.Done()
			r := rand.New(newRandSource(p.seed, int64(y)))
			for x := b.Min.X; x < b.Max.X; x += 1 {
				value := color.Gray16Model.Convert(i.At(x, y)).(color.Gray16)
				randVal := uint16(r.Uint32())
				if randVal < value.Y {
					d.Set(x, y, white)
				} else {
					d.Set(x, y, black)
				}
			}
		}(y)
	}
	wg.Wait()

	return d
}

func (p *imageParams) ditherImage1to1Color(i image.Image) image.Image {
	b := i.Bounds()
	d := image.NewRGBA64(b)

	wg := sync.WaitGroup{}
	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		wg.Add(1)
		go func(y int) {
			defer wg.Done()
			r := rand.New(newRandSource(p.seed, int64(y)))
			for x := b.Min.X; x < b.Max.X; x += 1 {
				rval, gval, bval, aval := i.At(x, y).RGBA()
				if uint16(r.Uint32()) < uint16(rval) {
					rval = 65535
				} else {
					rval = 0
				}
				if uint16(r.Uint32()) < uint16(gval) {
					gval = 65535
				} else {
					gval = 0
				}
				if uint16(r.Uint32()) < uint16(bval) {
					bval = 65535
				} else {
					bval = 0
				}
				d.SetRGBA64(x, y, color.RGBA64{
					R: uint16(rval),
					G: uint16(gval),
					B: uint16(bval),
					A: uint16(aval),
				})
			}
		}(y)
	}
	wg.Wait()

	return d
}

var crcTable = crc64.MakeTable(crc64.ECMA)

func newRandSource(seed, delta int64) (s rand.Source) {
	w := &bytes.Buffer{}
	_ = binary.Write(w, binary.LittleEndian, seed)
	_ = binary.Write(w, binary.LittleEndian, delta)
	h := crc64.Checksum(w.Bytes(), crcTable)
	s = rand.NewSource(int64(h))
	return
}

func gammaDecode(in, gamma float64) float64 {
	return A * math.Pow(in, gamma)
}

const (
	a  = 0.055
	a1 = a + 1.0
	e  = 1.0 / 2.4
)

func sRGBDecode(in float64) float64 {
	if in <= 0.04045 {
		return in / 12.92
	}
	return math.Pow((in+a)/a1, 2.4)
}

func sRGBEncode(in float64) float64 {
	if in <= 0.0031308 {
		return in * 12.92
	}
	return a1*math.Pow(in, e) - a
}

var (
	encodeLUT []uint16
)

func outputGammaInit() {
	encodeLUT = make([]uint16, 65536)
	for i := 0; i < 65536; i++ {
		encodeLUT[i] = uint16(sRGBEncode(float64(i)/65535.0) * 65535.0)
	}
}

func (p *imageParams) gammaInit() {
	p.decodeLUT = make([]uint16, 65536)
	if p.gamma == 0.0 {
		for i := 0; i < 65536; i++ {
			p.decodeLUT[i] = uint16(sRGBDecode(float64(i)/65535.0) * 65535.0)
		}
	} else {
		for i := 0; i < 65536; i++ {
			p.decodeLUT[i] = uint16(gammaDecode(float64(i)/65535.0, p.gamma) * 65535.0)
		}
	}
}

func transcode(in image.Image, lut []uint16) image.Image {
	b := in.Bounds()
	out := image.NewRGBA64(b)

	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		for x := b.Min.X; x < b.Max.X; x += 1 {
			rval, gval, bval, aval := in.At(x, y).RGBA()
			out.SetRGBA64(x, y, color.RGBA64{
				R: lut[rval],
				G: lut[gval],
				B: lut[bval],
				A: uint16(aval),
			})
		}
	}

	return out
}

// See http://blog.ivank.net/fastest-gaussian-blur.html
func gaussianBlur(in image.Image, radius float64) image.Image {
	bxs := boxesForGauss(radius, 3)

	out := image.NewRGBA64(in.Bounds())
	tmp := image.NewRGBA64(in.Bounds())
	scratch := image.NewRGBA64(in.Bounds())

	boxBlur(in, scratch, out, (bxs[0]-1)/2)
	boxBlur(out, scratch, tmp, (bxs[1]-1)/2)
	boxBlur(tmp, scratch, out, (bxs[2]-1)/2)

	return out
}

func boxBlur(in image.Image, scratch, out *image.RGBA64, r int) {
	boxBlurHorizontal(in, scratch, r)
	boxBlurVertical(scratch, out, r)
}

func boxBlurHorizontal(in image.Image, out *image.RGBA64, r int) {
	//var iarr = 1 / (r+r+1);
	var (
		iarr   = 1.0 / float64(2*r+1)
		top    = in.Bounds().Min.Y
		bottom = in.Bounds().Max.Y
		left   = in.Bounds().Min.X
		right  = in.Bounds().Max.X
	)

	wg := sync.WaitGroup{}
	//for(var i=0; i<h; i++) {
	for y := top; y < bottom; y++ {
		wg.Add(1)
		go func(y int) {
			defer wg.Done()
			//var ti = i*w, li = ti, ri = ti+r;
			//var fv = scl[ti], lv = scl[ti+w-1], val = (r+1)*fv;
			var (
				tx  = left
				lx  = left
				rx  = left + r
				fv  = newColorVal(in.At(left, y))
				lv  = newColorVal(in.At(right-1, y))
				val = fv.times(float64(r + 1))
			)

			//for(var j=0; j<r; j++) val += scl[ti+j];
			for x := left; x < left+r; x++ {
				val.incrementInt(in.At(x, y))
			}

			//for(var j=0  ; j<=r ; j++) { val += scl[ri++] - fv       ;   tcl[ti++] = Math.round(val*iarr); }
			for x := left; x <= left+r; x++ {
				val.incrementInt(in.At(rx, y))
				val.decrement(fv)
				rx++
				out.Set(tx, y, val.asColor(iarr))
				tx++
			}

			//for(var j=r+1; j<w-r; j++) { val += scl[ri++] - scl[li++];   tcl[ti++] = Math.round(val*iarr); }
			for x := left + r + 1; x < right-r; x++ {
				val.incrementInt(in.At(rx, y))
				val.decrementInt(in.At(lx, y))
				rx++
				lx++
				out.Set(tx, y, val.asColor(iarr))
				tx++
			}

			//for(var j=w-r; j<w  ; j++) { val += lv        - scl[li++];   tcl[ti++] = Math.round(val*iarr); }
			for x := right - r; x < right; x++ {
				val.increment(lv)
				val.decrementInt(in.At(lx, y))
				lx++
				out.Set(tx, y, val.asColor(iarr))
				tx++
			}
		}(y)
	}
	wg.Wait()
}

func boxBlurVertical(in image.Image, out *image.RGBA64, r int) {
	//var iarr = 1 / (r+r+1);
	var (
		iarr   = 1.0 / float64(2*r+1)
		top    = in.Bounds().Min.Y
		bottom = in.Bounds().Max.Y
		left   = in.Bounds().Min.X
		right  = in.Bounds().Max.X
	)

	wg := sync.WaitGroup{}
	//for(var i=0; i<w; i++) {
	for x := left; x < right; x++ {
		wg.Add(1)
		go func(x int) {
			defer wg.Done()
			//var ti = i, li = ti, ri = ti+r*w;
			//var fv = scl[ti], lv = scl[ti+w*(h-1)], val = (r+1)*fv;
			var (
				ty  = top
				ly  = top
				ry  = top + r
				fv  = newColorVal(in.At(x, top))
				lv  = newColorVal(in.At(x, bottom-1))
				val = fv.times(float64(r + 1))
			)

			//for(var j=0; j<r; j++) val += scl[ti+j*w];
			for y := top; y < top+r; y++ {
				val.incrementInt(in.At(x, y))
			}

			//for(var j=0  ; j<=r ; j++) { val += scl[ri] - fv     ;  tcl[ti] = Math.round(val*iarr);  ri+=w; ti+=w; }
			for y := top; y <= top+r; y++ {
				val.incrementInt(in.At(x, ry))
				val.decrement(fv)
				out.Set(x, ty, val.asColor(iarr))
				ry++
				ty++
			}

			//for(var j=r+1; j<h-r; j++) { val += scl[ri] - scl[li];  tcl[ti] = Math.round(val*iarr);  li+=w; ri+=w; ti+=w; }
			for y := top + r + 1; y < bottom-r; y++ {
				val.incrementInt(in.At(x, ry))
				val.decrementInt(in.At(x, ly))
				out.Set(x, ty, val.asColor(iarr))
				ly++
				ry++
				ty++
			}

			//for(var j=h-r; j<h  ; j++) { val += lv      - scl[li];  tcl[ti] = Math.round(val*iarr);  li+=w; ti+=w; }
			for y := bottom - r; y < bottom; y++ {
				val.increment(lv)
				val.decrementInt(in.At(x, ly))
				out.Set(x, ty, val.asColor(iarr))
				ly++
				ty++
			}
		}(x)
	}
	wg.Wait()
}

func boxesForGauss(sigma float64, n int) (sizes []int) {
	wIdeal := math.Sqrt((12.0 * sigma * sigma / float64(n)) + 1.0) // Ideal averaging filter width
	wl := int(math.Floor(wIdeal))
	if wl%2 == 0 {
		wl--
	}
	wu := wl + 2

	mIdeal := (12.0*sigma*sigma - float64(n*wl*wl+4*n*wl+3*n)) / float64(-4*wl-4)
	m := math.Round(mIdeal)
	// var sigmaActual = Math.sqrt( (m*wl*wl + (n-m)*wu*wu - n)/12 );

	sizes = make([]int, n)
	for i := range sizes {
		if float64(i) < m {
			sizes[i] = wl
		} else {
			sizes[i] = wu
		}
	}
	return
}

type colorVal struct {
	r, g, b, a float64
}

func newColorVal(c color.Color) (out *colorVal) {
	r, g, b, a := c.RGBA()
	out = &colorVal{
		r: float64(r),
		g: float64(g),
		b: float64(b),
		a: float64(a),
	}
	return
}

func (v *colorVal) times(n float64) (product *colorVal) {
	product = &colorVal{
		r: v.r * n,
		g: v.g * n,
		b: v.b * n,
		a: v.a,
	}
	return
}

func (v *colorVal) increment(n *colorVal) {
	v.r += n.r
	v.g += n.g
	v.b += n.b
}

func (v *colorVal) incrementInt(n color.Color) {
	r, g, b, _ := n.RGBA()
	v.r += float64(r)
	v.g += float64(g)
	v.b += float64(b)
}

func (v *colorVal) decrement(n *colorVal) {
	v.r -= n.r
	v.g -= n.g
	v.b -= n.b
}

func (v *colorVal) decrementInt(n color.Color) {
	r, g, b, _ := n.RGBA()
	v.r -= float64(r)
	v.g -= float64(g)
	v.b -= float64(b)
}

func (v *colorVal) asColor(factor float64) color.Color {
	return &color.RGBA64{
		R: uint16(math.Round(v.r * factor)),
		G: uint16(math.Round(v.g * factor)),
		B: uint16(math.Round(v.b * factor)),
		A: uint16(math.Round(v.a)),
	}
}

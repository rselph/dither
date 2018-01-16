// dither project main.go
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"sync"

	"math/rand"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/nfnt/resize"
	"golang.org/x/image/tiff"
)

var (
	xBlocks       int
	yBlocks       int
	seed          int64
	smooth        bool
	rescaleOutput bool
	gamma         float64
)

const A = 0.985

func main() {
	flag.IntVar(&xBlocks, "x", 0, "Block pixels on horizontal side")
	flag.IntVar(&yBlocks, "y", 0, "Block pixels on vertical side")
	flag.Int64Var(&seed, "r", 0, "Random number seed for dithering")
	flag.BoolVar(&smooth, "s", false, "Produce smoother look")
	flag.BoolVar(&rescaleOutput, "o", false, "Output image is one pixel per block")
	flag.Float64Var(&gamma, "g", 0.0, "Gamma of input image. If 0.0, then assume sRGB.")
	flag.Parse()
	gammaInit()

	await := &sync.WaitGroup{}
	for _, fname := range flag.Args() {
		await.Add(1)
		go func(filename string) {
			defer await.Done()
			dithered := ditherImage(imgFromFName(filename))
			save(dithered, filename)
		}(fname)
	}
	await.Wait()
}

func imgFromFName(fname string) image.Image {
	f, err := os.Open(fname)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	return transcode(img, decodeLUT)
}

func save(i image.Image, name string) {
	var typeName string
	if smooth {
		typeName = "s"
	} else {
		typeName = "d"
	}

	var (
		sizeX int
		sizeY int
	)

	switch {
	case xBlocks != 0 && yBlocks != 0:
		sizeX = xBlocks
		sizeY = yBlocks

	case xBlocks == 0 && yBlocks != 0:
		sizeX = yBlocks * i.Bounds().Size().X / i.Bounds().Size().Y
		sizeY = yBlocks

	case xBlocks != 0 && yBlocks == 0:
		sizeX = xBlocks
		sizeY = xBlocks * i.Bounds().Size().Y / i.Bounds().Size().X

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

func ditherImage(i image.Image) image.Image {
	if xBlocks == 0 && yBlocks == 0 {
		return ditherImage1to1(i)
	}

	smaller := resize.Resize(uint(xBlocks), uint(yBlocks), i, resize.Lanczos3)
	dith := ditherImage1to1(smaller)
	finalWidth := uint(i.Bounds().Size().X)
	finalHeight := uint(i.Bounds().Size().Y)
	if smooth {
		return resize.Resize(finalWidth, finalHeight, dith, resize.Lanczos3)
	} else {
		if rescaleOutput {
			return dith
		} else {
			return resize.Resize(finalWidth, finalHeight, dith, resize.NearestNeighbor)
		}
	}
}

func ditherImage1to1(i image.Image) image.Image {
	b := i.Bounds()
	d := image.NewGray16(b)
	r := rand.New(rand.NewSource(seed))

	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		for x := b.Min.X; x < b.Max.X; x += 1 {
			value := color.Gray16Model.Convert(i.At(x, y)).(color.Gray16)
			randVal := uint16(r.Uint32())
			if randVal < value.Y {
				d.Set(x, y, white)
			} else {
				d.Set(x, y, black)
			}
		}
	}

	return d
}

func gammaDecode(in float64) float64 {
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
	decodeLUT []uint16
	encodeLUT []uint16
)

func gammaInit() {
	decodeLUT = make([]uint16, 65536)
	if gamma == 0.0 {
		for i := 0; i < 65536; i++ {
			decodeLUT[i] = uint16(sRGBDecode(float64(i)/65536.0) * 65536.0)
		}
	} else {
		for i := 0; i < 65536; i++ {
			decodeLUT[i] = uint16(gammaDecode(float64(i)/65536.0) * 65536.0)
		}
	}
	encodeLUT = make([]uint16, 65536)
	for i := 0; i < 65536; i++ {
		encodeLUT[i] = uint16(sRGBEncode(float64(i)/65536.0) * 65536.0)
	}
}

func transcode(in image.Image, lut []uint16) image.Image {
	b := in.Bounds()
	out := image.NewGray16(b)

	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		for x := b.Min.X; x < b.Max.X; x += 1 {
			value := color.Gray16Model.Convert(in.At(x, y)).(color.Gray16)
			out.SetGray16(x, y, color.Gray16{lut[value.Y]})
		}
	}

	return out
}

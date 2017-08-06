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
	xBlocks int
	yBlocks int
	seed    int64
	smooth  bool
	gamma   float64
)

const A = 0.985

func main() {
	flag.IntVar(&xBlocks, "x", 0, "Block pixels on horizontal side")
	flag.IntVar(&yBlocks, "y", 0, "Block pixels on vertical side")
	flag.Int64Var(&seed, "r", 0, "Random number seed for dithering")
	flag.BoolVar(&smooth, "s", false, "Produce smoother look")
	flag.Float64Var(&gamma, "g", 2.2, "Gamma of input image")
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

	return img
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

	err = tiff.Encode(w, i, &tiff.Options{Compression: tiff.Deflate, Predictor: true})
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
		return resize.Resize(finalWidth, finalHeight, dith, resize.NearestNeighbor)
	}
}

func ditherImage1to1(i image.Image) image.Image {
	b := i.Bounds()
	d := image.NewGray16(b)
	r := rand.New(rand.NewSource(seed))

	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		for x := b.Min.X; x < b.Max.X; x += 1 {
			value := color.Gray16Model.Convert(i.At(x, y)).(color.Gray16)
			rand := uint16(r.Uint32())
			if rand < lut[value.Y] {
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

var lut []uint16

func gammaInit() {
	lut = make([]uint16, 65536)
	for i := 0; i < 65536; i++ {
		lut[i] = uint16(gammaDecode(float64(i)/65536.0) * 65536.0)
	}
}

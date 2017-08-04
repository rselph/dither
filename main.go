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
	horzontalBlocks uint
	seed            int64
	smooth          bool
)

func main() {
	flag.UintVar(&horzontalBlocks, "b", 0, "Block pixels on horizontal side")
	flag.Int64Var(&seed, "r", 0, "Random number seed for dithering")
	flag.BoolVar(&smooth, "s", false, "Produce smoother look")
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
	w, err := os.Create(fmt.Sprintf("%s.%s%04d.tiff", name, typeName, horzontalBlocks))
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
	if horzontalBlocks == 0 {
		return ditherImage1to1(i)
	}

	smaller := resize.Resize(horzontalBlocks, 0, i, resize.Lanczos3)
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

const A = 0.985
const gamma = 2.2

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

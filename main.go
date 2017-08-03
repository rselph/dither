// dither project main.go
package main

import (
	"flag"
	"math"
	"sync"
	//	"fmt"
	"image"
	"image/color"
	"log"
	"os"

	"math/rand"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/tiff"
)

func main() {
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
	w, err := os.Create(name + ".dith.tiff")
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
	b := i.Bounds()
	d := image.NewGray16(b)

	for y := b.Min.Y; y < b.Max.Y; y += 1 {
		for x := b.Min.X; x < b.Max.X; x += 1 {
			value := color.Gray16Model.Convert(i.At(x, y)).(color.Gray16)
			rand := uint16(rand.Uint32())
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

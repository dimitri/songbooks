package main

import (
	"fmt"
	"os"

	"app/internal/tab"
)

func main() {
	data, _ := os.ReadFile("/Users/dim/dev/songbooks/Other Lies/2-Surfacing/06-Kawasaki/bass.ly")
	riff, err := tab.ParseScore(string(data))
	if err != nil {
		panic(err)
	}
	fmt.Printf("tuning=%v minFret=%d\n", riff.Tuning, riff.MinimumFret)
	for bi, bar := range riff.Bars {
		fmt.Printf("bar %d:\n", bi)
		for _, b := range bar.Beats {
			if b.Rest {
				fmt.Printf("  rest dur=%s\n", b.Duration)
				continue
			}
			fmt.Printf("  dur=%s notes=", b.Duration)
			for _, n := range b.Notes {
				fmt.Printf("(s%d f%d) ", n.String, n.Fret)
			}
			fmt.Println()
		}
	}
}

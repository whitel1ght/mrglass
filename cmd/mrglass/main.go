package main

import "fmt"

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	fmt.Printf("mrglass %s\n", version)
}

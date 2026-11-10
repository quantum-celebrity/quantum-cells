package main

import (
	"fmt"
	"os"

	"github.com/blackbeardONE/CELLS/pkg/buildinfo"
)

func init() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(buildinfo.String("CELLScli"))
		os.Exit(0)
	}
}

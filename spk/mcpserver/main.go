package main

import (
	"context"
	"log"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		if err := runServeMode(context.Background(), os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 {
		log.Fatal("unknown mode")
	}
	if err := newServer(defaultConfig()).Run(context.Background(), os.Stdin, os.Stdout, os.Stderr); err != nil {
		log.Fatal(err)
	}
}

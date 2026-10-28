package main

import (
	"fmt"
	"log"
	"time"

	"github.com/dubi-project/dubi-ex/cache"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	// This short example starts the singleton without waiting for an OS signal.
	// Long-running applications normally use dubi.Run instead.
	if err = dubi.DefaultRuntime().Run(); err != nil {
		return err
	}
	defer func() {
		if closeErr := dubi.End(); err == nil {
			err = closeErr
		}
	}()
	key := cache.Key("user", 123)
	if err = cache.Write(key, Map{"name": "Ada"}, time.Minute); err != nil {
		return err
	}
	value, err := cache.Read(key)
	if err != nil {
		return err
	}
	fmt.Printf("%s = %v\n", key, value)
	sequence, err := cache.SequenceMany("order:id", 100, 1, 3)
	if err != nil {
		return err
	}
	fmt.Printf("order ids = %v\n", sequence)
	fmt.Printf("cache stats = %+v\n", cache.Stats())
	return nil
}

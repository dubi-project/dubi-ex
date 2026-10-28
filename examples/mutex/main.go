package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/dubi-project/dubi-ex/mutex"
	dubi "github.com/dubi-project/dubi-go"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	if err = dubi.DefaultRuntime().Run(); err != nil {
		return err
	}
	defer func() {
		if endErr := dubi.End(); err == nil {
			err = endErr
		}
	}()
	lock, err := mutex.Lock("order", 42, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := lock.Unlock(); err == nil {
			err = unlockErr
		}
	}()
	fmt.Printf("locked: %s\n", mutex.Key("order", 42))
	if _, err := mutex.TryLock("order", 42); !errors.Is(err, mutex.ErrLocked) {
		return fmt.Errorf("expected contention, got %v", err)
	}
	fmt.Println("another acquisition: already locked")
	if err := lock.KeepAlive(); err != nil {
		return err
	}
	fmt.Println("automatic renewal enabled")
	if err := lock.Unlock(); err != nil {
		return err
	}
	locked, err := mutex.Check("order", 42)
	if err != nil {
		return err
	}
	fmt.Printf("locked after release: %v\n", locked)
	fmt.Printf("stats: lock=%d contention=%d\n", mutex.Stats().Lock, mutex.Stats().Contention)
	return nil
}

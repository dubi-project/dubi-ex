package main

import (
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/dubi-project/dubi-ex/storage"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func init() {
	dubi.Register("default", storage.Config{Type: "file", Prefix: "example", Settings: Map{"base_url": "https://files.example.test"}})
}
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
	file, err := storage.UploadReader(strings.NewReader("hello storage"), storage.UploadOption{Name: "hello.txt"})
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := storage.Remove(file.Code()); err == nil {
			err = removeErr
		}
	}()
	fmt.Printf("uploaded: %s (%d bytes)\n", file.Name(), file.Size())
	body, err := storage.Fetch(file.Code(), storage.FetchOption{Range: &storage.Range{Start: 0, End: 0}})
	if err != nil {
		return err
	}
	first, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		return err
	}
	fmt.Printf("first byte: %s\n", first)
	copy, err := storage.Download(file.Code())
	if err != nil {
		return err
	}
	fmt.Printf("download copy: %s\n", copy)
	local, err := storage.Local(file.Code())
	if err != nil {
		return err
	}
	fmt.Printf("stored original: %s\n", local)
	url, err := storage.Browse(file.Code())
	if err != nil {
		return err
	}
	fmt.Printf("URL: %s\n", url)
	return nil
}

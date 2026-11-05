package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSHA1ContentAddressing(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	opt := UploadOption{Name: "example.txt"}
	file, err := UploadReader(strings.NewReader("abc"), opt)
	if err != nil {
		t.Fatal(err)
	}
	// Published SHA-1 test vector: the key encodes the complete 160-bit digest.
	if file.Hash() != "a9993e364706816aba3e25717850c26c9cd0d89d" || file.Key() != "qZk-NkcGgWq6PiVxeFDCbJzQ2J0" || file.Prefix() != "a9/99" {
		t.Fatalf("unexpected SHA-1 addressing: %+v", file)
	}
	again, err := UploadReader(strings.NewReader("abc"), UploadOption{Name: "renamed.txt"})
	if err != nil || again.Code() != file.Code() || read(t, file.Code()) != "abc" {
		t.Fatalf("content deduplication: %+v %v", again, err)
	}
	other, err := UploadReader(strings.NewReader("abcd"), opt)
	if err != nil || other.File() == file.File() || len(other.Key()) != 27 {
		t.Fatalf("different content addressing: %+v %v", other, err)
	}

	// An existing SHA-256 location is still readable through its original Code.
	sum := sha256.Sum256([]byte("abc"))
	hash := hex.EncodeToString(sum[:])
	old, err := UploadReader(strings.NewReader("abc"), UploadOption{
		Key: base64.RawURLEncoding.EncodeToString(sum[:]), Prefix: hash[:2] + "/" + hash[2:4], Type: "txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if old.File() == file.File() || read(t, old.Code()) != "abc" {
		t.Fatal("legacy SHA-256 reference was rerouted")
	}
	if _, err := Download(old.Code()); err != nil {
		t.Fatal(err)
	}
	if err := Remove(old.Code()); err != nil {
		t.Fatal(err)
	}
	if read(t, file.Code()) != "abc" {
		t.Fatal("removing SHA-256 object affected SHA-1 object")
	}
}

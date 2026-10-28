package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func objectID(file *File) string { return digest(file.base + "\x00" + file.File()) }
func randomName() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return ".upload-" + hex.EncodeToString(data[:])
}

// atomicRootWrite never truncates the committed target. It also handles a
// source referring to the same inode as the destination without losing data.
func atomicRootWrite(ctx context.Context, root *os.Root, target string, reader io.Reader) (n int64, err error) {
	err = atomicRootGenerate(ctx, root, target, func(writer io.Writer) error {
		var e error
		n, e = io.Copy(writer, contextReader{ctx, reader})
		return e
	})
	return n, err
}
func atomicRootGenerate(ctx context.Context, root *os.Root, target string, generate func(io.Writer) error) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(target)
	if err = root.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp := filepath.Join(dir, randomName())
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = root.Remove(temp)
		}
	}()
	if err = generate(file); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = file.Chmod(0644); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return root.Rename(temp, target)
}
func atomicPathWrite(ctx context.Context, target string, reader io.Reader) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(filepath.Dir(target))
	if err != nil {
		return 0, err
	}
	defer root.Close()
	return atomicRootWrite(ctx, root, filepath.Base(target), reader)
}

type managedStream struct {
	Stream
	ctx     context.Context
	release func()
	once    sync.Once
	mu      sync.Mutex
	stop    func() bool
	err     error
}

func (s *managedStream) Read(p []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	return s.Stream.Read(p)
}
func (s *managedStream) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		stop := s.stop
		s.mu.Unlock()
		if stop != nil {
			stop()
		}
		s.err = s.Stream.Close()
		s.release()
	})
	return s.err
}

type managedRandomStream struct {
	*managedStream
	access RandomAccessStream
}

func (s *managedRandomStream) Seek(offset int64, whence int) (int64, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	return s.access.Seek(offset, whence)
}
func (s *managedRandomStream) ReadAt(p []byte, offset int64) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	return s.access.ReadAt(p, offset)
}
func manage(ctx context.Context, body Stream, release func()) Stream {
	s := &managedStream{Stream: body, ctx: ctx, release: release}
	s.mu.Lock()
	s.stop = context.AfterFunc(ctx, func() { _ = s.Close() })
	s.mu.Unlock()
	if access, ok := body.(RandomAccessStream); ok {
		return &managedRandomStream{managedStream: s, access: access}
	}
	return s
}

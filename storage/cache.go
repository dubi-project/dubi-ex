package storage

import "os"

// cacheRoot is called only while a module operation is active, so Close waits
// until all users have released their roots. Concurrent first use opens once.
func (m *Module) cacheRoot(kind string) (*os.Root, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if root := m.cache[kind]; root != nil {
		return root, nil
	}
	var directory string
	switch kind {
	case "download":
		directory = m.options.Download
	case "thumbnail":
		directory = m.options.Thumbnail
	case "preview":
		directory = m.options.Preview
	default:
		return nil, ErrInvalidOption
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	m.cache[kind] = root
	return root, nil
}

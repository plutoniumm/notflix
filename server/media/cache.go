package media

import (
	"os"
	"path/filepath"
)

type Cache struct {
	Dir string
}

var Store = &Cache{Dir: hlsCacheDir}

func (c *Cache) Path(parts ...string) string {
	all := append([]string{c.Dir}, parts...)

	return filepath.Join(all...)
}

func (c *Cache) Ensure() {
	os.MkdirAll(c.Dir, 0755)
}

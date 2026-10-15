package ossstore

import (
	"io/fs"
	"os"
)

var fileInfo = func(filename string) (fs.FileInfo, error) {
	return os.Stat(filename)
}

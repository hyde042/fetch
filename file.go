package fetch

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"
)

type file struct {
	*fileInfo
	body io.ReadCloser
}

func (t *file) Stat() (os.FileInfo, error) { return t.fileInfo, nil }
func (t *file) Read(p []byte) (int, error) { return t.body.Read(p) }
func (t *file) Close() error               { return t.body.Close() }

type fileInfo struct {
	url    *url.URL
	header http.Header
}

func (t fileInfo) Name() string {
	name := path.Base(t.url.Path)
	if name == "" || name == "/" {
		name = "."
	}
	if path.Ext(name) == "" {
		exts, _ := mime.ExtensionsByType(t.header.Get("Content-Type"))
		if len(exts) > 0 {
			name += exts[0]
		}
	}
	return name
}

func (t fileInfo) Size() int64 {
	size, _ := strconv.ParseInt(t.header.Get("Conten-Length"), 10, 64)
	return size
}

func (t fileInfo) Mode() os.FileMode { return os.FileMode(0700) }

func (t fileInfo) ModTime() time.Time {
	modTime, _ := time.Parse(http.TimeFormat, t.header.Get("Last-Modified"))
	return modTime
}

func (t fileInfo) IsDir() bool      { return false }
func (t fileInfo) Sys() interface{} { return nil }

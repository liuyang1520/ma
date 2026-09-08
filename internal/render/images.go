package render

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func localImage(destination, baseDir string, budget *int64) image.Image {
	u, err := url.Parse(destination)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path == "" || baseDir == "" || filepath.IsAbs(u.Path) {
		return nil
	}
	base, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return nil
	}
	base, _ = filepath.Abs(base)
	path, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(u.Path)))
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil
	}
	config, _, err := image.DecodeConfig(io.LimitReader(f, 8<<20))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil
	}
	pixels := int64(config.Width) * int64(config.Height)
	if pixels > 32_000_000 || pixels*4 > *budget {
		return nil
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil
	}
	im, _, err := image.Decode(io.LimitReader(f, 8<<20))
	if err != nil {
		return nil
	}
	*budget -= pixels * 4
	return im
}

func (e *Engine) picture(path string) image.Image {
	if im, ok := e.images[path]; ok {
		return im
	}
	im := localImage(path, e.baseDir, &e.imageBudget)
	e.images[path] = im
	return im
}

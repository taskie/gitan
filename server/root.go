package server

import (
	"os"
	"path/filepath"

	"github.com/saracen/walker"
)

// see https://github.com/x-motemen/ghq/blob/master/local_repository.go

type RootConfig struct {
	SiteName string `json:"site_name" toml:"site_name"`
	Path     string `json:"path" toml:"path"`
}

type Root struct {
	siteName string
	path     string
}

type FoundRepo struct {
	Name string
	Path string
}

func NewRoot(conf *RootConfig) (*Root, error) {
	return &Root{
		siteName: conf.SiteName,
		path:     conf.Path,
	}, nil
}

var vcsContentDirNameSet = map[string]struct{}{
	".git":           {},
	".hg":            {},
	".svn":           {},
	"_darcs":         {},
	".bzr":           {},
	".fslckout":      {},
	"_FOSSIL_":       {},
	"CVS/Repository": {},
}

func (r *Root) Collect() []*FoundRepo {
	paths := make([]*FoundRepo, 0)

	walkFn := func(pathname string, fi os.FileInfo) error {
		isSymlink := false
		if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
			isSymlink = true
			realpath, err := filepath.EvalSymlinks(pathname)
			if err != nil {
				return nil
			}
			fi, err = os.Stat(realpath)
			if err != nil {
				return nil
			}
		}
		if !fi.IsDir() {
			return nil
		}
		basename := filepath.Base(pathname)
		if basename != ".git" {
			if _, ok := vcsContentDirNameSet[basename]; ok {
				return filepath.SkipDir
			}
		}
		gitpath := filepath.Join(pathname, ".git")
		gitFi, err := os.Stat(gitpath)
		if err != nil || !gitFi.IsDir() {
			return nil
		}
		relpath, err := filepath.Rel(r.path, pathname)
		if err != nil {
			return err
		}
		paths = append(paths, &FoundRepo{
			Name: relpath,
			Path: gitpath,
		})
		if isSymlink {
			return nil
		}
		return filepath.SkipDir
	}

	errorCallbackOption := walker.WithErrorCallback(func(pathname string, err error) error {
		if os.IsPermission(err) {
			return nil
		}
		return err
	})

	walker.Walk(r.path, walkFn, errorCallbackOption)

	return paths
}

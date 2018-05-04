package resolver

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/taskie/gitan/repo"
)

type Resolver interface {
	Resolve(args []string) (repo.FileOpener, interface{}, error)
}

type RevResolver struct {
	Repo *repo.Repo
}

func (r *RevResolver) Resolve(args []string) (repo.FileOpener, interface{}, error) {
	if len(args) == 2 {
		path, rev := args[1], args[0]
		return r.Repo.Get(path, rev)
	} else {
		return nil, nil, errors.New("RevResolver#Resolve requires 2 args")
	}
}

type WorktreeResolver struct {
	ProjectPath string
}

func (r *WorktreeResolver) Resolve(args []string) (repo.FileOpener, interface{}, error) {
	if len(args) == 1 {
		fname := args[0]
		fpath := filepath.Join(r.ProjectPath, fname)
		if !filepath.HasPrefix(fpath, r.ProjectPath) {
			return nil, nil, errors.New("invalid path: " + fname)
		}
		return func() (io.ReadCloser, error) {
			return os.Open(fpath)
		}, nil, nil
	} else {
		return nil, nil, errors.New("WorktreeResolver#Resolve requires 1 args")
	}
}

type ExternalResolver struct {
	PrefixWhitelist []string
}

func (r *ExternalResolver) Resolve(args []string) (repo.FileOpener, interface{}, error) {
	if len(args) == 1 {
		fpath := filepath.Clean(args[0])
		ok := false
		for _, prefix := range r.PrefixWhitelist {
			if filepath.HasPrefix(fpath, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, nil, errors.New(fpath + " is not in the white list")
		}
		return func() (io.ReadCloser, error) {
			return os.Open(fpath)
		}, nil, nil
	} else {
		return nil, nil, errors.New("ExternalResolver#Resolve requires 1 args")
	}
}

type RouteResolver struct {
	ResolverMap map[string]Resolver
}

func (r *RouteResolver) Resolve(args []string) (repo.FileOpener, interface{}, error) {
	if len(args) < 1 {
		return nil, nil, errors.New("RouteResolver#Resolve requires 1 args at least")
	}
	var name = args[0]
	if subr, ok := r.ResolverMap[name]; ok {
		return subr.Resolve(args[1:])
	} else {
		return nil, nil, errors.New("Resolver '" + name + "' not found")
	}
}

func NewDefaultResolver(projectPath string, rp *repo.Repo) Resolver {
	rev := RevResolver{Repo: rp}
	work := WorktreeResolver{ProjectPath: projectPath}
	ext := ExternalResolver{}
	res := RouteResolver{
		ResolverMap: map[string]Resolver{
			"rev":  &rev,
			"work": &work,
			"ext":  &ext,
		},
	}
	return &res
}

func Main(args []string) {
	rp, err := repo.Open(args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer rp.Close()
	res := NewDefaultResolver(args[1], rp)
	fo, _, err := res.Resolve(args[2:])
	if err != nil {
		log.Fatal(err)
	}
	f, err := fo()
	if err != nil {
		log.Fatal(err)
	}
	buf := bytes.Buffer{}
	io.Copy(&buf, f)
	fmt.Println(buf.String())
}

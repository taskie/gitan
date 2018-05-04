package repo

import (
	"bytes"
	"fmt"
	"io"
	"log"

	"github.com/pkg/errors"

	"gopkg.in/src-d/go-git.v4/plumbing"

	"gopkg.in/src-d/go-git.v4"
)

// FileOpener is delayed file opener
type FileOpener func() (io.ReadCloser, error)

// GitRepo wraps Git repository
type Repo struct {
	repository *git.Repository
}

// OpenGitRepo opens Git repository
func Open(repoPath string) (*Repo, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, errors.Wrap(err, "opening repo failed")
	}
	repo := Repo{
		repository: r,
	}
	return &repo, nil
}

// Get resolves revison and file name
func (r *Repo) Get(path string, rev string) (FileOpener, interface{}, error) {
	h, err := r.repository.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, nil, errors.Wrap(err, "resolving rev failed")
	}
	ci, err := r.repository.CommitObject(*h)
	if err != nil {
		return nil, nil, errors.Wrap(err, "obtaining commit failed")
	}
	file, err := ci.File(path)
	if err != nil {
		tree, err := ci.Tree()
		if err != nil {
			return nil, nil, errors.Wrap(err, "obtaining tree from commit failed")
		} else {
			_, err := tree.Tree(path)
			if err != nil {
				return nil, nil, errors.Wrap(err, "obtaining file or directory failed")
			} else {
				return nil, nil, errors.New("obtaining directory")
			}
		}
	}
	fileOpener := func() (io.ReadCloser, error) {
		r, err := file.Reader()
		if err != nil {
			return nil, errors.Wrap(err, "opening file failed")
		}
		return r, nil
	}
	return fileOpener, nil, nil
}

// Close closes repository (do nothing)
func (r *Repo) Close() error {
	return nil
}

// Main is entry point
func Main(args []string) {
	repo, err := Open(args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	fo, _, err := repo.Get(args[3], args[2])
	if err != nil {
		log.Fatal(err)
	}
	r, err := fo()
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()
	buf := bytes.Buffer{}
	io.Copy(&buf, r)
	fmt.Println(buf.String())
}

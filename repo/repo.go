package repo

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

// FileOpener is delayed file opener
type FileOpener func() (io.ReadCloser, error)

// GitRepo wraps Git repository
type Repo struct {
	repository *git.Repository
}

// NewRepo opens Git repository
func NewRepo(repoPath string) (*Repo, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, errors.Wrapf(err, "opening repo failed: %s", repoPath)
	}
	repo := Repo{
		repository: r,
	}
	return &repo, nil
}

type FileStat struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
	IsBinary bool   `json:"is_binary"`
}

func NewFileStat(f *object.File) (*FileStat, error) {
	isBinary, err := f.IsBinary()
	if err != nil {
		return nil, err
	}
	buf := bytes.NewReader(f.Mode.Bytes())
	var mode uint32
	err = binary.Read(buf, binary.LittleEndian, &mode)
	if err != nil {
		return nil, err
	}
	return &FileStat{
		ID:       f.ID().String(),
		Name:     f.Name,
		Mode:     mode,
		Size:     f.Size,
		IsBinary: isBinary,
	}, nil
}

type TreeEntry struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Mode uint32 `json:"mode"`
}

func NewTreeEntry(te *object.TreeEntry) (*TreeEntry, error) {
	buf := bytes.NewReader(te.Mode.Bytes())
	var mode uint32
	err := binary.Read(buf, binary.LittleEndian, &mode)
	if err != nil {
		return nil, err
	}
	return &TreeEntry{
		Hash: te.Hash.String(),
		Name: te.Name,
		Mode: mode,
	}, nil
}

func gitPathJoin(elems ...string) string {
	sep := "/"
	xs := make([]string, 0)
	for _, elem := range elems {
		if elem != "" {
			xs = append(xs, elem)
		}
	}
	return strings.Join(xs, sep)
}

func (r *Repo) Find(path string, rev string, maxDepth int) ([]*TreeEntry, error) {
	results := make([]*TreeEntry, 0)
	stack := []string{""}
	for len(stack) > 0 {
		idx := len(stack) - 1
		if maxDepth > 0 && len(stack) > maxDepth {
			stack = stack[:idx]
			continue
		}
		p := stack[idx]
		stack = stack[:idx]
		treePath := gitPathJoin(path, p)
		tes, err := r.GetTree(treePath, rev)
		if err != nil {
			return nil, err
		}
		for _, te := range tes {
			childPath := gitPathJoin(p, te.Name)
			if te.Mode&0040000 != 0 {
				stack = append(stack, childPath)
			}
			results = append(results, &TreeEntry{
				Hash: te.Hash,
				Name: childPath,
				Mode: te.Mode,
			})
		}
	}
	return results, nil
}

func (r *Repo) GetTree(path string, rev string) ([]*TreeEntry, error) {
	h, err := r.repository.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, errors.Wrap(err, "resolving rev failed")
	}
	ci, err := r.repository.CommitObject(*h)
	if err != nil {
		return nil, errors.Wrap(err, "obtaining commit failed")
	}
	tree, err := ci.Tree()
	if err != nil {
		return nil, errors.Wrap(err, "obtaining tree from commit failed")
	}
	var targetTree *object.Tree = tree
	if path != "" {
		targetTree, err = tree.Tree(path)
		if err != nil {
			return nil, errors.Wrap(err, "obtaining file or directory failed")
		}
	}
	results := make([]*TreeEntry, 0)
	for _, te := range targetTree.Entries {
		result, err := NewTreeEntry(&te)
		if err != nil {
			return nil, errors.Wrap(err, "invalid tree entry")
		}
		results = append(results, result)
	}
	return results, nil
}

// Get resolves revison and file name
func (r *Repo) GetFileOpener(path string, rev string) (FileOpener, *FileStat, error) {
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
	fileStat, err := NewFileStat(file)
	if err != nil {
		return nil, nil, err
	}
	return fileOpener, fileStat, nil
}

func (r *Repo) GetFile(path string, rev string) ([]byte, *FileStat, error) {
	opener, stat, err := r.GetFileOpener(path, rev)
	if err != nil {
		return nil, nil, err
	}
	reader, err := opener()
	if err != nil {
		return nil, stat, err
	}
	defer reader.Close()
	bs, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, stat, err
	}
	return bs, stat, nil
}

func (r *Repo) GetBlobOpener(hash string) (FileOpener, error) {
	blob, err := r.repository.BlobObject(plumbing.NewHash(hash))
	if err != nil {
		return nil, errors.Wrap(err, "obtaining blob object failed")
	}
	return func() (io.ReadCloser, error) { return blob.Reader() }, nil
}

func (r *Repo) GetBlob(hash string) ([]byte, error) {
	opener, err := r.GetBlobOpener(hash)
	if err != nil {
		return nil, err
	}
	reader, err := opener()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	bs, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return bs, nil
}

func (r *Repo) GetCommitHash(rev string) (string, error) {
	h, err := r.repository.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return "", errors.Wrap(err, "resolving rev failed")
	}
	ci, err := r.repository.CommitObject(*h)
	if err != nil {
		return "", errors.Wrap(err, "obtaining commit failed")
	}
	return ci.String(), nil
}

type Commit struct {
	ID           string      `json:"id"`
	Message      string      `json:"message"`
	Author       *Signature  `json:"author"`
	Committer    *Signature  `json:"committer"`
	ParentHashes []string    `json:"parent_hashes"`
	Files        []*FileStat `json:"files"`
}

type Signature struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	When  time.Time `json:"when"`
}

func NewSignature(sign object.Signature) (*Signature, error) {
	return &Signature{
		Name:  sign.Name,
		Email: sign.Email,
		When:  sign.When,
	}, nil
}

func (r *Repo) GetCommit(rev string) (*Commit, error) {
	h, err := r.repository.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, errors.Wrap(err, "resolving rev failed")
	}
	ci, err := r.repository.CommitObject(*h)
	if err != nil {
		return nil, errors.Wrap(err, "obtaining commit failed")
	}
	fi, err := ci.Files()
	if err != nil {
		return nil, errors.Wrap(err, "obtaining files failed")
	}
	files := make([]*FileStat, 0)
	fi.ForEach(func(o *object.File) error {
		file, err := NewFileStat(o)
		files = append(files, file)
		if err != nil {
			return err
		}
		return nil
	})
	author, err := NewSignature(ci.Author)
	committer, err := NewSignature(ci.Committer)
	parentHashes := make([]string, 0)
	for _, hash := range ci.ParentHashes {
		parentHashes = append(parentHashes, hash.String())
	}
	newCi := &Commit{
		ID:           ci.ID().String(),
		Message:      ci.Message,
		Author:       author,
		Committer:    committer,
		ParentHashes: parentHashes,
		Files:        files,
	}
	return newCi, nil
}

// Close closes repository (do nothing)
func (r *Repo) Close() error {
	return nil
}

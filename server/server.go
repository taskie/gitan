package server

import (
	"mime"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/k0kubun/pp"
	log "github.com/sirupsen/logrus"
	"github.com/taskie/gitan/repo"
	"github.com/taskie/jc"
)

type Config struct {
	Address   string                 `json:"address"`
	Repos     map[string]*RepoConfig `json:"repos"`
	MultiUser bool                   `json:"multi_user"`
	BlobOnly  bool                   `json:"blob_only"`
}

type RepoConfig struct {
	Path string `json:"path"`
}

func NewServer(conf *Config) (*Server, error) {
	m := make(map[string]*repo.Repo)
	for k, v := range conf.Repos {
		r, err := repo.NewRepo(v.Path)
		if err != nil {
			return nil, err
		}
		m[k] = r
	}
	srv := Server{
		Address:   conf.Address,
		Registry:  m,
		MultiUser: conf.MultiUser,
		BlobOnly:  conf.BlobOnly,
	}
	return &srv, nil
}

type Server struct {
	Address   string
	Registry  map[string]*repo.Repo
	MultiUser bool
	BlobOnly  bool
}

func catHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		bs, err := s.Registry[c.Param("repo")].GetBlob(c.Param("hash"))
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			c.Data(200, "text/plain", bs)
		}
	}
}

func blobHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		repoKey := c.Param("repo")
		if s.MultiUser {
			repoKey = c.Param("user") + "/" + repoKey
		}
		path := strings.TrimLeft(c.Param("path"), "/")
		rev := c.Param("rev")
		repo := s.Registry[repoKey]
		pp.Println(s)
		log.Println(repoKey, path, rev, repo)
		bs, stat, err := repo.GetFile(path, rev)
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			ty := mime.TypeByExtension(filepath.Ext(path))
			if ty == "" {
				if stat.IsBinary {
					ty = "application/octet-stream"
				} else {
					ty = "text/plain"
				}
			}
			c.Data(200, ty, bs)
		}
	}
}

func (s *Server) Run() {
	r := gin.Default()
	r.GET("/", func(c *gin.Context) { c.Data(200, "text/html", []byte("<h1>gitan</h1>")) })
	repoGroup := r.Group("/:repo")
	if s.MultiUser {
		repoGroup = r.Group("/:user/:repo")
	}
	if s.BlobOnly {
		repoGroup.GET("/:rev/*path", blobHandler(s))
	} else {
		repoGroup.GET("/blob/:rev/*path", blobHandler(s))
		repoGroup.GET("/cat/:hash", catHandler(s))
		repoGroup.GET("/commit/:rev", commitHandler(s))
	}
	if s.Address != "" {
		r.Run(s.Address)
	} else {
		r.Run()
	}
}

func commitHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		ci, err := s.Registry[c.Param("repo")].GetCommit(c.Param("rev"))
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			c.JSON(200, ci)
		}
	}
}

func Main(args []string) {
	path := "gitan.json"
	if len(args) > 1 {
		path = args[1]
	}
	conf := &Config{}
	err := jc.DecodeFile(path, "", conf)
	if err != nil {
		log.Fatal(err)
	}
	srv, err := NewServer(conf)
	if err != nil {
		log.Fatal(err)
	}
	srv.Run()
}

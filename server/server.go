package server

import (
	"mime"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/taskie/gitan/repo"
	"github.com/taskie/jc"
)

type Config struct {
	Address      string                 `json:"address" toml:"address" yaml:"address"`
	Repos        map[string]*RepoConfig `json:"repos" toml:"repos" yaml:"address"`
	MultiUser    bool                   `json:"multi_user" toml:"multi_user" yaml:"address"`
	BlobOnly     bool                   `json:"blob_only" toml:"blob_only" yaml:"address"`
	TreeMaxDepth int                    `json:"tree_max_depth" toml:"tree_max_depth" yaml:"address"`
	BathPath     string                 `json:"base_path" toml:"base_path" yaml:"base_path"`
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
	basePath := conf.BathPath
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	srv := Server{
		Address:      conf.Address,
		Registry:     m,
		MultiUser:    conf.MultiUser,
		BlobOnly:     conf.BlobOnly,
		TreeMaxDepth: conf.TreeMaxDepth,
		BathPath:     basePath,
	}
	return &srv, nil
}

type Server struct {
	Address      string
	Registry     map[string]*repo.Repo
	MultiUser    bool
	BlobOnly     bool
	TreeMaxDepth int
	BathPath     string
}

type RepoSpec struct {
	Name string `json:"name"`
}

func listHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		repos := make([]*RepoSpec, 0)
		for repoKey := range s.Registry {
			if s.MultiUser {
				if !strings.HasPrefix(repoKey, c.Param("user")+"/") {
					continue
				}
				repoKey = strings.TrimPrefix(repoKey, c.Param("user")+"/")
			}
			repos = append(repos, &RepoSpec{repoKey})
		}
		sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
		c.JSON(200, gin.H{"ok": true, "repos": repos})
	}
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
		// pp.Println(s)
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

func treeHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		repoKey := c.Param("repo")
		if s.MultiUser {
			repoKey = c.Param("user") + "/" + repoKey
		}
		path := strings.TrimLeft(c.Param("path"), "/")
		rev := c.Param("rev")
		r := s.Registry[repoKey]
		// pp.Println(s)
		log.Println(repoKey, path, rev, r)
		var tes []*repo.TreeEntry
		var err error
		if s.TreeMaxDepth != 0 && c.Query("recursive") == "true" {
			tes, err = r.Find(path, rev, s.TreeMaxDepth)
		} else {
			tes, err = r.GetTree(path, rev)
		}
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			c.JSON(200, gin.H{"ok": true, "entries": tes})
		}
	}
}

func (s *Server) Run() {
	r := gin.Default()
	rootGroup := r.Group(s.BathPath)
	var repoGroup *gin.RouterGroup
	if s.MultiUser {
		rootGroup.GET("/:user/", listHandler(s))
		repoGroup = rootGroup.Group("/:user/:repo")
	} else {
		rootGroup.GET("/", listHandler(s))
		repoGroup = rootGroup.Group("/:repo")
	}
	if s.BlobOnly {
		repoGroup.GET("/:rev/*path", blobHandler(s))
	} else {
		repoGroup.GET("/blob/:rev/*path", blobHandler(s))
		repoGroup.GET("/tree/:rev/*path", treeHandler(s))
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
		conf = &Config{
			Repos: map[string]*RepoConfig{
				"gitan": &RepoConfig{
					Path: ".git",
				},
			},
		}
	}
	srv, err := NewServer(conf)
	if err != nil {
		log.Fatal(err)
	}
	srv.Run()
}

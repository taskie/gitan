package server

import (
	"fmt"
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
	Address      string                 `json:"address" toml:"address"`
	Roots        []*RootConfig          `json:"roots" toml:"roots"`
	Sites        map[string]*SiteConfig `json:"sites" toml:"sites"`
	BlobOnly     bool                   `json:"blob_only" toml:"blob_only"`
	TreeMaxDepth int                    `json:"tree_max_depth" toml:"tree_max_depth"`
	BathPath     string                 `json:"base_path" toml:"base_path"`
}

type SiteConfig struct {
	Registry map[string]*RepoConfig `json:"registry" toml:"registry"`
}

type RepoConfig struct {
	Path string `json:"path" toml:"path"`
}

func NewServer(conf *Config) (*Server, error) {
	sites := make(map[string]*Site)
	if conf.Roots != nil {
		for _, rootConf := range conf.Roots {
			root, err := NewRoot(rootConf)
			if err != nil {
				return nil, err
			}
			site := sites[root.site]
			if site == nil {
				site = &Site{Registry: make(map[string]*repo.Repo)}
				sites[root.site] = site
			}
			for _, v := range root.Collect() {
				r, err := repo.NewRepo(v.Path)
				if err != nil {
					return nil, err
				}
				log.Infof("found %s: %s", v.Name, v.Path)
				site.Registry[v.Name] = r
			}
		}
	}
	for siteKey, siteConf := range conf.Sites {
		for k, v := range siteConf.Registry {
			r, err := repo.NewRepo(v.Path)
			if err != nil {
				return nil, err
			}
			site := sites[siteKey]
			if site == nil {
				site = &Site{Registry: make(map[string]*repo.Repo)}
				sites[siteKey] = site
			}
			site.Registry[k] = r
		}
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
		Sites:        sites,
		BlobOnly:     conf.BlobOnly,
		TreeMaxDepth: conf.TreeMaxDepth,
		BathPath:     basePath,
	}
	return &srv, nil
}

type Server struct {
	Address      string
	Sites        map[string]*Site
	BlobOnly     bool
	TreeMaxDepth int
	BathPath     string
}

type Site struct {
	Registry map[string]*repo.Repo
}

type RepoSpec struct {
	Name string `json:"name"`
}

func listHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		site := s.Sites[c.Param("site")]
		if site == nil {
			c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", c.Param("site"))})
			return
		}
		repos := make([]*RepoSpec, 0)
		for repoKey := range site.Registry {
			if !strings.HasPrefix(repoKey, c.Param("user")+"/") {
				continue
			}
			repoKey = strings.TrimPrefix(repoKey, c.Param("user")+"/")
			repos = append(repos, &RepoSpec{repoKey})
		}
		sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
		c.JSON(200, gin.H{"ok": true, "repos": repos})
	}
}

func catHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		site := s.Sites[c.Param("site")]
		if site == nil {
			c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", c.Param("site"))})
			return
		}
		bs, err := site.Registry[c.Param("repo")].GetBlob(c.Param("hash"))
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			c.Data(200, "text/plain", bs)
		}
	}
}

func blobHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		site := s.Sites[c.Param("site")]
		if site == nil {
			c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", c.Param("site"))})
			return
		}
		repoKey := c.Param("user") + "/" + c.Param("repo")
		path := strings.TrimLeft(c.Param("path"), "/")
		rev := c.Param("rev")
		repo := site.Registry[repoKey]
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

func revsHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		site := s.Sites[c.Param("site")]
		if site == nil {
			c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", c.Param("site"))})
			return
		}
		repoKey := c.Param("user") + "/" + c.Param("repo")
		repo := site.Registry[repoKey]
		if repo == nil {
			c.JSON(404, gin.H{"ok": false, "error": "not found"})
		}
		// pp.Println(s)
		log.Println(repoKey)
		branches, err := repo.GetBranches()
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			c.JSON(200, gin.H{"ok": true, "branches": branches})
		}
	}
}

func treeHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		site := s.Sites[c.Param("site")]
		if site == nil {
			c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", c.Param("site"))})
			return
		}
		repoKey := c.Param("user") + "/" + c.Param("repo")
		r := site.Registry[repoKey]
		path := strings.TrimLeft(c.Param("path"), "/")
		rev := c.Param("rev")
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
	siteGroup := rootGroup.Group("/:site/")
	siteGroup.GET("/:user/", listHandler(s))
	repoGroup = siteGroup.Group("/:user/:repo")
	if s.BlobOnly {
		repoGroup.GET("/:rev/*path", blobHandler(s))
	} else {
		repoGroup.GET("", revsHandler(s))
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
		site := s.Sites[c.Param("site")]
		if site == nil {
			c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", c.Param("site"))})
			return
		}
		ci, err := site.Registry[c.Param("repo")].GetCommit(c.Param("rev"))
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
			Sites: map[string]*SiteConfig{
				"-": {
					Registry: map[string]*RepoConfig{
						"-": {
							Path: ".git",
						},
					},
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

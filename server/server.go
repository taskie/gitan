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
	UserRegistries map[string]*UserRegistryConfig `json:"user_registries" toml:"user_registries"`
}

type UserRegistryConfig struct {
	Repos map[string]*RepoConfig `json:"repos" toml:"repos"`
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
			site := sites[root.siteName]
			if site == nil {
				site = NewSite()
				sites[root.siteName] = site
			}
			for _, foundRepo := range root.Collect() {
				r, err := repo.NewRepo(foundRepo.Path)
				if err != nil {
					return nil, err
				}
				log.Infof("found %s: %s", foundRepo.Name, foundRepo.Path)
				parts := strings.SplitN(foundRepo.Name, "/", 2)
				var userName, repoName string
				if len(parts) == 2 {
					userName = parts[0]
					repoName = parts[1]
				} else {
					userName = "-"
					repoName = foundRepo.Name
				}
				user := site.UserRegistries[userName]
				if user == nil {
					user = NewUserRegistry()
					site.UserRegistries[userName] = user
				}
				user.Repos[repoName] = r
			}
		}
	}
	for siteName, siteConf := range conf.Sites {
		for userName, userConf := range siteConf.UserRegistries {
			for repoName, repoConf := range userConf.Repos {
				r, err := repo.NewRepo(repoConf.Path)
				if err != nil {
					return nil, err
				}
				site := sites[siteName]
				if site == nil {
					site = NewSite()
					sites[siteName] = site
				}
				user := site.UserRegistries[userName]
				if user == nil {
					user = NewUserRegistry()
					site.UserRegistries[userName] = user
				}
				user.Repos[repoName] = r
			}
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
	UserRegistries map[string]*UserRegistry
}

func NewSite() *Site {
	return &Site{
		UserRegistries: make(map[string]*UserRegistry),
	}
}

type UserRegistry struct {
	Repos map[string]*repo.Repo
}

func NewUserRegistry() *UserRegistry {
	return &UserRegistry{
		Repos: make(map[string]*repo.Repo),
	}
}

func (s *Server) Run() {
	r := gin.Default()
	rootGroup := r.Group(s.BathPath)
	rootGroup.GET("/", listSitesHandler(s))
	var repoGroup *gin.RouterGroup
	siteGroup := rootGroup.Group("/:siteName/")
	siteGroup.GET("/", listUsersHandler(s))
	siteGroup.GET("/:userName/", listReposHandler(s))
	repoGroup = siteGroup.Group("/:userName/:repoName")
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

func siteNotFound(c *gin.Context, siteName string) {
	c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no site: %s", siteName)})
}

func userNotFound(c *gin.Context, userName string) {
	c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no user: %s", userName)})
}

func repoNotFound(c *gin.Context, repoName string) {
	c.JSON(404, gin.H{"ok": false, "error": fmt.Sprintf("no repo: %s", repoName)})
}

type SiteSpec struct {
	Name string `json:"name"`
}

type UserSpec struct {
	Name string `json:"name"`
}

type RepoSpec struct {
	Name string `json:"name"`
}

func listSitesHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		sites := make([]*SiteSpec, 0)
		for siteName := range s.Sites {
			sites = append(sites, &SiteSpec{siteName})
		}
		sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })
		c.JSON(200, gin.H{"ok": true, "sites": sites})
	}
}

func listUsersHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		users := make([]*UserSpec, 0)
		for userName := range site.UserRegistries {
			users = append(users, &UserSpec{userName})
		}
		sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })
		c.JSON(200, gin.H{"ok": true, "users": users})
	}
}

func listReposHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		userName := c.Param("userName")
		user := site.UserRegistries[userName]
		if user == nil {
			userNotFound(c, userName)
			return
		}
		repos := make([]*RepoSpec, 0)
		for repoName := range user.Repos {
			repos = append(repos, &RepoSpec{repoName})
		}
		sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
		c.JSON(200, gin.H{"ok": true, "repos": repos})
	}
}

func catHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		userName := c.Param("userName")
		user := site.UserRegistries[userName]
		if user == nil {
			userNotFound(c, userName)
			return
		}
		repoName := c.Param("repoName")
		repo := user.Repos[repoName]
		if repo == nil {
			repoNotFound(c, userName)
			return
		}
		bs, err := repo.GetBlob(c.Param("hash"))
		if err != nil {
			c.JSON(404, gin.H{"ok": false, "error": err.Error()})
		} else {
			c.Data(200, "text/plain", bs)
		}
	}
}

func blobHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		userName := c.Param("userName")
		user := site.UserRegistries[userName]
		if user == nil {
			userNotFound(c, userName)
			return
		}
		repoName := c.Param("repoName")
		repo := user.Repos[repoName]
		if repo == nil {
			repoNotFound(c, userName)
			return
		}
		rev := c.Param("rev")
		path := strings.TrimLeft(c.Param("path"), "/")
		// pp.Println(s)
		log.Println(repoName, path, rev, repo)
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
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		userName := c.Param("userName")
		user := site.UserRegistries[userName]
		if user == nil {
			userNotFound(c, userName)
			return
		}
		repoName := c.Param("repoName")
		repo := user.Repos[repoName]
		if repo == nil {
			repoNotFound(c, userName)
			return
		}
		// pp.Println(s)
		log.Println(repoName)
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
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		userName := c.Param("userName")
		user := site.UserRegistries[userName]
		if user == nil {
			userNotFound(c, userName)
			return
		}
		repoName := c.Param("repoName")
		r := user.Repos[repoName]
		if r == nil {
			repoNotFound(c, userName)
			return
		}
		path := strings.TrimLeft(c.Param("path"), "/")
		rev := c.Param("rev")
		// pp.Println(s)
		log.Println(repoName, path, rev, r)
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

func commitHandler(s *Server) func(c *gin.Context) {
	return func(c *gin.Context) {
		siteName := c.Param("siteName")
		site := s.Sites[siteName]
		if site == nil {
			siteNotFound(c, siteName)
			return
		}
		userName := c.Param("userName")
		user := site.UserRegistries[userName]
		if user == nil {
			userNotFound(c, userName)
			return
		}
		repoName := c.Param("repoName")
		repo := user.Repos[repoName]
		if repo == nil {
			repoNotFound(c, userName)
			return
		}
		rev := c.Param("rev")
		ci, err := repo.GetCommit(rev)
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
					UserRegistries: map[string]*UserRegistryConfig{
						"-": {
							Repos: map[string]*RepoConfig{
								"-": {
									Path: ".git",
								},
							},
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

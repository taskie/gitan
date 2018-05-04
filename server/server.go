package server

import (
	"bytes"
	"io"
	"log"
	"mime"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/taskie/gitan/repo"
	"github.com/taskie/gitan/resolver"
)

type Server struct {
	Resolver resolver.Resolver
}

func (s *Server) Run() {
	r := gin.Default()
	genGETHandler := func(args []string) func(c *gin.Context) {
		return func(c *gin.Context) {
			p := strings.TrimPrefix(c.Param("path"), "/")
			newArgs := append(args, p)
			fo, _, err := s.Resolver.Resolve(newArgs)
			if err != nil {
				c.JSON(500, gin.H{
					"error": err.Error(),
				})
				return
			}
			file, err := fo()
			if err != nil {
				c.JSON(500, gin.H{
					"error": err.Error(),
				})
				return
			}
			defer file.Close()
			buf := bytes.Buffer{}
			io.Copy(&buf, file)
			c.Data(200, mime.TypeByExtension(filepath.Ext(p)), buf.Bytes())
		}
	}
	r.GET("/")
	r.GET("/work/*path", genGETHandler([]string{"work"}))
	r.GET("/dev/*path", genGETHandler([]string{"rev", "develop"}))
	r.GET("/prod/*path", genGETHandler([]string{"rev", "master"}))
	r.Run()
}

func Main(args []string) {
	rp, err := repo.Open(args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer rp.Close()
	res := resolver.NewDefaultResolver(args[1], rp)
	s := Server{
		Resolver: res,
	}
	s.Run()
}

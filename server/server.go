package server

import (
	"fmt"

	ytrelay "github.com/mirror-media/yt-relay"
	"github.com/mirror-media/yt-relay/cache"
	"github.com/mirror-media/yt-relay/config"
	"github.com/mirror-media/yt-relay/whitelist"
	log "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
)

type Server struct {
	APIWhitelist ytrelay.APIWhitelist
	Cache        cache.Rediser
	conf         *config.Conf
	Engine       *gin.Engine
}

func init() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetReportCaller(true)
}

func (s *Server) Run() error {
	return s.Engine.Run(fmt.Sprintf("%s:%d", s.conf.Address, s.conf.Port))
}

func New(c config.Conf) (s *Server, err error) {

	engine := gin.Default()

	var redis cache.Rediser

	if c.Redis != nil {
		redis, err = cache.NewRedis(c)
		if err != nil {
			return nil, err
		}
	}

	var cache cache.Rediser
	if c.Cache.IsEnabled {
		if redis == nil {
			return nil, fmt.Errorf("there is no cache provider")
		}
		cache = redis
	}

	s = &Server{
		APIWhitelist: &whitelist.YouTubeAPI{
			Whitelist: c.Whitelists,
			CmsURL:    c.CmsURL,
		},
		Cache:  cache,
		conf:   &c,
		Engine: engine,
	}
	return s, nil
}

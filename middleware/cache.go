package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"

	"github.com/gin-gonic/gin"
	"github.com/mirror-media/yt-relay/api"
	"github.com/mirror-media/yt-relay/cache"
	"github.com/mirror-media/yt-relay/config"
	log "github.com/sirupsen/logrus"
)

func Cache(namespace string, cacheConf config.Cache, cacheProvider cache.Rediser) gin.HandlerFunc {
	return func(c *gin.Context) {
		url := c.Request.URL

		// check blacklist
		if isDisabled := cacheConf.DisabledAPIs[url.Path]; isDisabled {
			log.Infof("cache is disabled for %s", url.Path)
			c.Next()
			return
		}
		// read cache
		uri := c.Request.URL.String()
		key, err := cache.GetCacheKey(namespace, uri)
		if err != nil {
			err = errors.Wrap(err, "Fail to create cache key in cache middleware")
			log.Error(err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, api.ErrorResp{Error: err.Error()})
			return
		}
		result, err := cacheProvider.Get(c.Request.Context(), key).Result()
		if err != nil {
			err = errors.Wrapf(err, "Fail to get cache value for %s in cache middleware", key)
			log.Info(err)
			c.Next()
			return
		}

		var cacheResp cache.HTTP

		err = json.Unmarshal([]byte(result), &cacheResp)
		if err != nil {
			err = errors.Wrap(err, "Fail to unmarshal cache in cache middleware")
			log.Error(err)
			c.Next()
			return
		}

		log.Infof("respond with cache for %s", uri)
		c.AbortWithStatusJSON(cacheResp.StatusCode, json.RawMessage(cacheResp.Response))
	}
}

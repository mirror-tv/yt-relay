package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ytrelay "github.com/mirror-media/yt-relay"
	"github.com/mirror-media/yt-relay/api"
	"github.com/mirror-media/yt-relay/cache"
	"github.com/mirror-media/yt-relay/config"
	"github.com/mirror-media/yt-relay/middleware"
	"github.com/mirror-media/yt-relay/relay"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/youtube/v3"
)

const (
	ErrorEmptyPart = "part cannot be empty"
	ErrorEmptyID   = "id cannot be empty"
)

const TTLHeader = "Cache-Set-TTL"

func getResponseCacheTTL(apiLogger *log.Entry, cacheConf config.Cache, request http.Request) (ttl time.Duration, isDisabled bool) {

	seconds, ok := cacheConf.OverwriteTTL[request.RequestURI]
	if ok {
		ttl = time.Duration(seconds) * time.Second
	} else {
		ttl = time.Duration(cacheConf.TTL) * time.Second
	}

	if headerTTL, isPresenting, err := getHeaderTTL(apiLogger, request); err != nil {
		apiLogger.Error(err)
	} else if isPresenting {
		ttl = headerTTL
	}

	return ttl, cacheConf.DisabledAPIs[request.RequestURI]
}

func getHeaderTTL(apiLogger *log.Entry, request http.Request) (ttl time.Duration, isPresenting bool, err error) {
	var values []string
	if values, isPresenting = request.Header[http.CanonicalHeaderKey(TTLHeader)]; isPresenting {
		var headerTTL string
		if len(values) > 0 {
			headerTTL = values[0]
		} else {
			return ttl, isPresenting, errors.Errorf("header(%s) has empty value", TTLHeader)
		}

		var intTTL int
		intTTL, err = strconv.Atoi(headerTTL)
		if err != nil {
			err = errors.Wrap(err, fmt.Sprintf("converting %s(%s) to int encountered error", headerTTL, TTLHeader))
		} else if intTTL <= 0 {
			err = errors.Errorf("the value(%d) of %s is not positive", intTTL, TTLHeader)
		} else {
			apiLogger.Infof("client requests to set cache ttl to %d via %s", intTTL, TTLHeader)
			ttl = time.Duration(intTTL) * time.Second
		}
	}
	return ttl, isPresenting, err
}

func saveOKCache(isEnabled bool, cacheConf config.Cache, cacheProvider cache.Rediser, apiLogger *log.Entry, appName string, request http.Request, resp interface{}) {

	if cacheConf.IsEnabled {
		ttl, isCacheDisabledForAPI := getResponseCacheTTL(apiLogger, cacheConf, request)
		if !isCacheDisabledForAPI {
			saveCache(cacheConf, cacheProvider, apiLogger, appName, request, http.StatusOK, resp, ttl)
		} else {
			apiLogger.Infof("cache is disabled for %s", request.URL.String())
		}
	}
}
func saveErrCache(isEnabled bool, cacheConf config.Cache, cacheProvider cache.Rediser, apiLogger *log.Entry, appName string, request http.Request, httpResponseCode uint, resp interface{}) {

	if cacheConf.IsEnabled {
		_, isCacheDisabledForAPI := getResponseCacheTTL(apiLogger, cacheConf, request)
		if !isCacheDisabledForAPI {
			ttl := time.Duration(cacheConf.ErrorTTL) * time.Second
			saveCache(cacheConf, cacheProvider, apiLogger, appName, request, http.StatusOK, resp, ttl)
		} else {
			apiLogger.Infof("cache is disabled for %s", request.URL.String())
		}
	}
}

func saveCache(cacheConf config.Cache, cacheProvider cache.Rediser, apiLogger *log.Entry, appName string, request http.Request, respCode int, resp interface{}, ttl time.Duration) {
	s, err := json.Marshal(resp)
	if err != nil {
		apiLogger.Errorf("Cannot marshal resp for %s: %s", request.URL.String(), err)
		return
	}
	s, err = json.Marshal(cache.HTTP{
		StatusCode: respCode,
		Response:   s,
	})
	if err != nil {
		apiLogger.Errorf("Cannot marshal http resp cache for %s: %s", request.URL.String(), err)
		return
	}
	key, err := cache.GetCacheKey(appName, request.URL.String())
	if err != nil {
		apiLogger.Errorf("GetCacheKey for %s encounter error:%v", request.URL.String(), err)
	}
	err = cacheProvider.SetNX(request.Context(), key, string(s), ttl).Err()
	if err != nil {
		apiLogger.Errorf("setting cache encountered error for %s: %v ", request.URL.String(), err)
		return
	} else {
		apiLogger.Infof("cache for %s is set for ttl(%d)", request.URL.String(), int(ttl.Seconds()))
	}
}

// Set sets the routing for the gin engine
// TODO move whitelist to YouTube relay service
func Set(r *gin.Engine, appName string, relayService ytrelay.VideoRelay, whitelist ytrelay.APIWhitelist, cacheConf config.Cache, cacheProvider cache.Rediser) error {

	// rewrite /api/youtube/* to /youtube/v3/*
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/youtube/") {
			c.Request.URL.Path = strings.Replace(c.Request.URL.Path, "/api/youtube/", "/youtube/v3/", 1)
			r.HandleContext(c)
			c.Abort()
		}
	})

	// health check api
	// As more resources and component are used, they should be checked in the api
	r.GET("/health", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusOK)
	})

	ytRouter := r.Group("/youtube/v3")

	if cacheConf.IsEnabled {
		ytRouter.Use(middleware.Cache(appName, cacheConf, cacheProvider))
	}

	// search videos. ChannelID is required
	ytRouter.GET("/search", func(c *gin.Context) {

		apiLogger := log.WithFields(log.Fields{
			"path": c.FullPath(),
		})

		queries, err := parseQueries(c)
		if err != nil {
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		// Check the mandatory parameters
		if queries.Part == "" {
			apiLogger.Error(ErrorEmptyPart)
			resp := api.ErrorResp{Error: ErrorEmptyPart}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		// Check whitelist
		if !whitelist.ValidateChannelID(queries.ChannelID) {
			err = fmt.Errorf("channelId(%s) is invalid", queries.ChannelID)
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		resp, err := relayService.Search(queries)
		if err != nil {
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusInternalServerError, resp)
			c.AbortWithStatusJSON(http.StatusInternalServerError, api.ErrorResp{Error: err.Error()})
			return
		}
		saveOKCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, resp)
		c.JSON(http.StatusOK, resp)
	})

	// list video by video id
	// IDs of videos is required
	ytRouter.GET("/videos", func(c *gin.Context) {

		apiLogger := log.WithFields(log.Fields{
			"path": c.FullPath(),
		})

		queries, err := parseQueries(c)
		if err != nil {
			apiLogger.Error(err)
			c.AbortWithStatusJSON(http.StatusBadRequest, api.ErrorResp{Error: err.Error()})
			return
		}

		// Check the mandatory parameters
		if queries.Part == "" {
			apiLogger.Error(ErrorEmptyPart)
			resp := api.ErrorResp{Error: ErrorEmptyPart}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}
		if queries.IDs == "" {
			apiLogger.Error(ErrorEmptyID)
			resp := api.ErrorResp{Error: ErrorEmptyID}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		resp, err := relayService.ListByVideoIDs(queries)
		if err != nil {
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusInternalServerError, resp)
			c.AbortWithStatusJSON(http.StatusInternalServerError, resp)
			return
		}

		// verify channel id for YouTube
		_, isYouTube := relayService.(*relay.YouTubeServiceV3)
		if isYouTube {
			if err = validateYouTubeVideoListResponse(whitelist, resp); err != nil {
				err = errors.Wrap(err, "some video's channel id is invalid")
				apiLogger.Error(err)
				resp := api.ErrorResp{Error: err.Error()}
				saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
				c.AbortWithStatusJSON(http.StatusBadRequest, resp)
				return
			}
		}

		saveOKCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, resp)
		c.JSON(http.StatusOK, resp)
	})

	// list video by playlistID
	ytRouter.GET("/playlistItems", func(c *gin.Context) {

		apiLogger := log.WithFields(log.Fields{
			"path": c.FullPath(),
		})

		queries, err := parseQueries(c)
		if err != nil {
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		// Check the mandatory parameters
		if queries.Part == "" {
			apiLogger.Error(ErrorEmptyPart)
			resp := api.ErrorResp{Error: ErrorEmptyPart}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		// Check whitelist
		if !whitelist.ValidatePlaylistIDs(queries.PlaylistID) {
			err = fmt.Errorf("playlistId(%s) is invalid", queries.PlaylistID)
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusBadRequest, resp)
			c.AbortWithStatusJSON(http.StatusBadRequest, resp)
			return
		}

		resp, err := relayService.ListPlaylistVideos(queries)
		if err != nil {
			apiLogger.Error(err)
			resp := api.ErrorResp{Error: err.Error()}
			saveErrCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, http.StatusInternalServerError, resp)
			c.AbortWithStatusJSON(http.StatusInternalServerError, resp)
			return
		}

		saveOKCache(cacheConf.IsEnabled, cacheConf, cacheProvider, apiLogger, appName, *c.Request, resp)
		c.JSON(http.StatusOK, resp)
	})

	return nil
}

func parseQueries(c *gin.Context) (ytrelay.Options, error) {
	var queries ytrelay.Options
	err := c.BindQuery(&queries)

	return queries, err
}

func validateYouTubeVideoListResponse(whitelist ytrelay.APIWhitelist, resp interface{}) (err error) {
	for _, item := range resp.(*youtube.VideoListResponse).Items {
		if !whitelist.ValidateChannelID(item.Snippet.ChannelId) {
			err = fmt.Errorf("channelId(%s) is invalid", item.Snippet.ChannelId)
			return err
		}
	}
	return nil
}

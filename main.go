package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/httplog"
	"github.com/gocolly/colly"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type (
	JishoConceptMeaning struct {
		Value string   `json:"value"`
		Tags  []string `json:"tags"`
	}

	JishoConcept struct {
		Writing  string                `json:"writing"`
		Reading  string                `json:"reading"`
		Meanings []JishoConceptMeaning `json:"meanings"`
		Tags     []string              `json:"tags"`
	}
)

func main() {
	// figure out log level and logging related flags
	useJsonLogging, _ := strconv.ParseBool(os.Getenv("JISHO_API_LOG_JSON"))
	useConciseLogging, _ := strconv.ParseBool(os.Getenv("JISHO_API_LOG_CONCISE"))
	logLevel := os.Getenv("JISHO_API_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info" // default to "info"
	}

	// set up logger
	logger := httplog.NewLogger("jisho-api", httplog.Options{
		JSON:     useJsonLogging,
		LogLevel: logLevel,
		Concise:  useConciseLogging,
	})

	// figure out whether or not we want to use a redis cache to help with response times / no hammer jisho.org
	useRedisCache := false
	var redisClient *redis.Client = nil

	redisAddr := os.Getenv("JISHO_API_REDIS_ADDR")
	redisPass := os.Getenv("JISHO_API_REDIS_PASS")
	redisDb := int(0)

	redisDbStr := os.Getenv("JISHO_API_REDIS_DB")
	if num, err := strconv.Atoi(redisDbStr); err != nil {
		redisDb = int(num)
	}

	if redisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: redisPass,
			DB:       redisDb,
		})

		// check if we can access the configured redis server
		pingCtx, pingCancel := context.WithTimeout(context.Background(), time.Duration(5*time.Second))
		res := redisClient.Ping(pingCtx)
		if err := res.Err(); err != nil {
			logger.Fatal().Msg(fmt.Sprintf("failed to ping redis '%s': %s", redisAddr, err.Error()))
		}
		useRedisCache = true
		pingCancel()
	}

	// figure out where we want to make the API listen for requests
	apiHost := os.Getenv("JISHO_API_HOST")
	apiPort := os.Getenv("JISHO_API_PORT")
	if apiPort == "" {
		apiPort = "8080"
	}
	apiAddr := apiHost + ":" + apiPort

	// set up a chi router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(httplog.RequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/health"))

	// api search endpoint, just capture everything after /search/ and use that as the jisho search query
	r.Get("/search/*", func(w http.ResponseWriter, r *http.Request) {

		queryParams := r.URL.Query()
		searchPageParam, _ := strconv.Atoi(queryParams.Get("page"))
		searchQuery := chi.URLParam(r, "*")

		// jisho counts pages starting from '1'
		if searchPageParam == 0 {
			searchPageParam = 1
		}

		var jishoConcepts []JishoConcept = make([]JishoConcept, 0)

		if useRedisCache {
			logger.Info().Msg("trying redis cache")

			cacheKey := fmt.Sprintf("jisho-concept-query:%s@%d", searchQuery, searchPageParam)
			cacheResp, err := redisClient.Get(r.Context(), cacheKey).Result()
			if err == nil {
				logger.Info().Msg("cache hit, skipped querying jisho.org")
				_ = json.Unmarshal([]byte(cacheResp), &jishoConcepts)
			} else {
				logger.Info().Msg("cache miss, querying jisho.org")
				jishoConcepts = SearchJisho(searchQuery, searchPageParam, logger)
				jishoConceptsJson, _ := json.Marshal(jishoConcepts)

				opLog := logger.Info()
				opLog.Str("redis:key", cacheKey).Msg("caching jisho.org result for query '" + searchQuery + "' ")
				redisClient.Set(r.Context(), cacheKey, string(jishoConceptsJson), 0)
			}
		} else {
			logger.Info().Msg("cache disabled, querying jisho.org")
			jishoConcepts = SearchJisho(searchQuery, searchPageParam, logger)
		}

		jishoConceptsJson, _ := json.Marshal(jishoConcepts)
		w.Header().Add("content-type", "application/json")
		w.Write(jishoConceptsJson)
	})

	logger.Info().Msg("launching jisho-api on '" + apiAddr + "'")
	logger.Info().Msg(fmt.Sprintf("use redis cache: %v", useRedisCache))
	logger.Info().Msg(fmt.Sprintf("use json logging: %v", useJsonLogging))
	logger.Info().Msg(fmt.Sprintf("use concise logging: %v", useConciseLogging))

	http.ListenAndServe(apiAddr, r)
}

// SearchJisho will launch a jisho search request and scrape the response page for JishoConcepts
func SearchJisho(query string, page int, logger zerolog.Logger) []JishoConcept {

	// TODO: Kanji results
	// TODO: Inflections

	concepts := make([]JishoConcept, 0)

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
	)

	c.OnHTML("div.exact_block > div.concept_light", func(e *colly.HTMLElement) {
		// create a word + "reading" into a computer friendly format.... <kanji>(<kana reading>)
		writingEl := e.DOM.Find("div.concept_light-readings > div.concept_light-representation > span.text")
		writing := strings.TrimSpace(writingEl.Text())

		reading := ""
		readingEl := e.DOM.Find("div.concept_light-readings > div.concept_light-representation > span.furigana > span")
		readingEl.Each(func(i int, s *goquery.Selection) {
			charReading := s.Text()
			if len(charReading) > 0 {
				reading += string([]rune(writing)[i]) + "(" + s.Text() + ")"
			} else {
				reading += string([]rune(writing)[i])
			}
		})

		// collect entry meanings (note that meaning tags and actual meaning elements are siblings in the same parent node)
		meanings := make([]JishoConceptMeaning, 0)
		meaningsContentEl := e.DOM.Find("div.concept_light-meanings > div.meanings-wrapper")
		meaningsTagsEls := meaningsContentEl.Find("div.meaning-tags")

		meaningsContentEl.Find("div.meaning-definition").Each(func(i int, ee *goquery.Selection) {
			meaningTagsEl := meaningsTagsEls.Get(i)
			meaningTagsElText := meaningTagsEl.FirstChild.Data // this is a text node...

			// ignore any meaning entry that has "Other forms" or "Notes" as tags. These "special" elements have
			// the same structure as the other meaning elements but don't actually define a meaning.
			if !strings.Contains(meaningTagsElText, "Other forms") && !strings.Contains(meaningTagsElText, "Notes") {
				meaningTags := strings.Split(meaningTagsElText, ", ")
				meaning := ee.Find("span.meaning-meaning").Text()
				meanings = append(meanings, JishoConceptMeaning{
					Value: meaning,
					Tags:  meaningTags,
				})
			}
		})

		// collect entry tags
		tags := make([]string, 0)
		tagsEl := e.DOM.Find("div.concept_light-status")
		tagsEl.Find("span.concept_light-tag").Each(func(i int, ee *goquery.Selection) {
			tags = append(tags, strings.TrimSpace(ee.Text()))
		})

		// TODO check for the "more words" link (this means there's another page available)

		scrapedConcept := JishoConcept{
			Writing:  writing,
			Reading:  reading,
			Meanings: meanings,
			Tags:     tags,
		}

		concepts = append(concepts, scrapedConcept)
	})

	err := c.Visit(fmt.Sprintf("https://jisho.org/search/%s?page=%d", query, page))
	if err != nil {
		logger.Fatal().Err(err).Msg("error occurred while trying to scrape jisho.org")
	}

	return concepts
}

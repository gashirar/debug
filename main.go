package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"
)

type Specification struct {
	Version                 string `default:"undefined"`
	Port                    int    `default:"8080"`
	BackendService          string `default:"undefined" split_words:"true"`
	DelayResponseMsec       int    `default:"0" split_words:"true"`
	DelayResponsePercentage int    `default:"0" split_words:"true"`
	RandomDelay             bool   `default:"false" split_words:"true"`
	FaultResponsePercentage int    `default:"0" split_words:"true"`
	K8sUid                  string `default:"undefined" split_words:"true"`
	K8sNodeName             string `default:"undefined" split_words:"true"`
	K8sHostIp               string `default:"undefined" split_words:"true"`
	K8sPodName              string `default:"undefined" split_words:"true"`
	K8sNamespace            string `default:"undefined" split_words:"true"`
	K8sPodIp                string `default:"undefined" split_words:"true"`
	K8sServiceAccountName   string `default:"undefined" split_words:"true"`
	K8sContainerName        string `default:"undefined" split_words:"true"`
	K8sCpuRequest           string `default:"undefined" split_words:"true"`
	K8sCpuLimit             string `default:"undefined" split_words:"true"`
	K8sMemoryRequest        string `default:"undefined" split_words:"true"`
	K8sMemoryLimit          string `default:"undefined" split_words:"true"`
}

var env Specification

type key int

const (
	requestIDKey key = 0
)

var (
	Version      string = ""
	GitTag       string = ""
	GitCommit    string = ""
	GitTreeState string = ""
	listenAddr   string
	healthy      int32
)

func main() {
	envconfig.Process("", &env)
	listenAddr := fmt.Sprintf("0.0.0.0:%d", env.Port)

	logger := log.New(os.Stdout, "http: ", log.LstdFlags)

	logger.Println(env.BackendService)
	logger.Println("Simple go server")
	logger.Println("Version:", Version)
	logger.Println("GitTag:", GitTag)
	logger.Println("GitCommit:", GitCommit)
	logger.Println("GitTreeState:", GitTreeState)

	logger.Println("Server is starting...")

	router := http.NewServeMux()
	router.Handle("/", index())
	router.Handle("/backend", backend())
	router.Handle("/livenessz", livenessz())
	router.Handle("/readinessz", readinessz())
	router.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("/probe"))))

	nextRequestID := func() string {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      tracing(nextRequestID)(logging(logger)(router)),
		ErrorLog:     logger,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	done := make(chan bool)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	go func() {
		<-quit
		logger.Println("Server is shutting down...")
		atomic.StoreInt32(&healthy, 0)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		server.SetKeepAlivesEnabled(false)
		if err := server.Shutdown(ctx); err != nil {
			logger.Fatalf("Could not gracefully shutdown the server: %v\n", err)
		}
		close(done)
	}()

	logger.Println("Server is ready to handle requests at", listenAddr)
	atomic.StoreInt32(&healthy, 1)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("Could not listen on %s: %v\n", listenAddr, err)
	}

	<-done
	logger.Println("Server stopped")
}

func delay() {
	rand.Seed(time.Now().UnixNano())
	if rand.Intn(100)+1 < env.DelayResponsePercentage {
		if env.RandomDelay {
			time.Sleep(time.Duration(rand.Intn(env.DelayResponseMsec)+1) * time.Millisecond)
		} else {
			time.Sleep(time.Duration(env.DelayResponseMsec) * time.Millisecond)
		}
	}
}

func isFault() bool {
	rand.Seed(time.Now().UnixNano())
	if rand.Intn(100)+1 < env.FaultResponsePercentage {
		return true
	}
	return false
}

func index() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		res := make(map[string]interface{})
		delay()
		if isFault() {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusServiceUnavailable)
			res["version"] = env.Version
			res["body"] = "Fault."
			s, _ := json.Marshal(res)
			fmt.Fprintf(w, "%s", s)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		res["version"] = env.Version
		res["body"] = env.K8sPodName
		s, _ := json.Marshal(res)
		fmt.Fprintf(w, "%s", s)
	})
}

func backend() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		backends := strings.Split(env.BackendService, ",")
		body := make(map[string]interface{})
		backendBody := make(map[string]interface{})

		for _, url := range backends {
			log.Print(url)
			req, _ := http.NewRequest("GET", url, nil)
			tracingHeader := []string{
				"X-Request-Id",
				"X-B3-Traceid",
				"X-B3-Spanid",
				"X-B3-Parentspanid",
				"X-B3-Sampled",
				"X-B3-Flags",
				"B3",
				"X-Ot-Span-Context",
			}
			for _, h := range tracingHeader {
				val := r.Header.Get(h)
				if val != "" {
					req.Header.Set(h, val)
				}
			}
			for k, vals := range r.Header {
				log.Printf("%s", k)
				for _, v := range vals {
					log.Printf("\t%s", v)
				}
			}
			client := new(http.Client)
			resp, err := client.Do(req)
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			defer resp.Body.Close()
			b, _ := ioutil.ReadAll(resp.Body)
			m := make(map[string]interface{})
			json.Unmarshal(b, &m)
			backendBody[url] = m
		}
		w.WriteHeader(http.StatusOK)
		body["version"] = env.Version
		body["body"] = env.K8sPodName
		body["backend"] = backendBody
		s, _ := json.Marshal(body)
		fmt.Fprintf(w, "%s", s)
	})
}

func readinessz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		response := make(map[string]interface{})
		response["version"] = env.Version
		response["body"] = "Readiness Check OK."
		s, _ := json.Marshal(response)
		fmt.Fprintf(w, "%s", s)
	})
}
func livenessz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		response := make(map[string]interface{})
		response["version"] = env.Version
		response["body"] = "Liveness Check OK."
		s, _ := json.Marshal(response)
		fmt.Fprintf(w, "%s", s)
	})
}

func logging(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				requestID, ok := r.Context().Value(requestIDKey).(string)
				if !ok {
					requestID = "unknown"
				}
				logger.Println(requestID, r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent())
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func tracing(nextRequestID func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-Id")
			if requestID == "" {
				requestID = nextRequestID()
			}
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			w.Header().Set("X-Request-Id", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"time"

	_ "embed"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

var version = "v0.0.1"

//go:embed "refresh.html"
var refresh string

//go:embed "cmds.json"
var cmdInput []byte

type Proxy struct {
	mu          sync.Mutex
	lastUpdated time.Time
	waitTime    time.Duration
	proxy       *httputil.ReverseProxy
}

type Resetter struct {
	router *mux.Router
	proxy  *Proxy
}

type Command struct {
	Command string   `json:"cmd"`
	Args    []string `json:"args"`
}

type CommandList struct {
	Commands []Command `json:"cmd_list"`
}

func main() {
	upstream := flag.String("upstream", "localhost:8080", "upstream url")
	addr := flag.String("addr", "localhost", "This url")
	logLevel := flag.String("log_level", "error", "WHat level of logging")
	waitTime := flag.Int("wait_time", 60, "How many seconds should the service be down")
	printVersion := flag.Bool("version", false, "print version number")

	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	ll, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Error(err)
	}
	log.SetLevel(ll)

	url, err := url.Parse("http://" + *upstream)
	if err != nil {
		log.Fatal(err)
	}

	proxy := Proxy{
		mu:          sync.Mutex{},
		lastUpdated: time.Now(),
		waitTime:    time.Duration(*waitTime) * time.Second,
		proxy:       httputil.NewSingleHostReverseProxy(url),
	}

	server := NewServer(&proxy)
	server.router.HandleFunc("/api/map/reload", server.reloadMap())
	server.router.PathPrefix("/").HandlerFunc(server.DefaultEndPoint())
	http.ListenAndServe(*addr, server.router)

}

func NewServer(page *Proxy) *Resetter {
	return &Resetter{
		router: mux.NewRouter(),
		proxy:  page,
	}

}

func (v Resetter) reloadMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v.proxy.mu.Lock()
		defer v.proxy.mu.Unlock()
		v.proxy.lastUpdated = time.Now()
		err := runMapReload()
		if err != nil {
			log.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}
}

func (v Resetter) DefaultEndPoint() http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		v.proxy.mu.Lock()
		defer v.proxy.mu.Unlock()

		if time.Now().Sub(v.proxy.lastUpdated) < v.proxy.waitTime {
			fmt.Fprint(w, refresh)
			return
		}

		v.proxy.proxy.ServeHTTP(w, r)

	}
}

func runMapReload() error {

	cmds := &CommandList{}
	err := json.Unmarshal(cmdInput, cmds)
	if err != nil {
		log.Error(err)
	}
	for _, cmd := range cmds.Commands {
		out, err := exec.Command(cmd.Command, cmd.Args...).Output()
		log.Debug(out)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

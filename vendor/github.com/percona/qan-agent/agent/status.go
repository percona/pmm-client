package agent

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/instance"
)

type LocalInterface struct {
	addr  string
	agent *Agent
	repo  *instance.Repo
}

func NewLocalInterface(addr string, agent *Agent, repo *instance.Repo) *LocalInterface {
	lo := &LocalInterface{
		addr:  addr,
		agent: agent,
		repo:  repo,
	}
	return lo
}

func (lo *LocalInterface) Run() {
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := lo.agent.AllStatus()
		JSONResponse(w, 200, status)
	})
	http.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		configs, _ := lo.agent.GetAllConfigs()
		JSONResponse(w, 200, configs)
	})
	http.HandleFunc("/defaults", func(w http.ResponseWriter, r *http.Request) {
		defaults, _ := lo.agent.GetDefaults()
		JSONResponse(w, 200, defaults)
	})
	http.HandleFunc("/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(lo.agent.config.UUID))
	})
	http.HandleFunc("/instances", func(w http.ResponseWriter, r *http.Request) {
		instances := map[string][]proto.Instance{
			"os":    lo.repo.List("os"),
			"mysql": lo.repo.List("mysql"),
		}
		JSONResponse(w, 200, instances)
	})
	log.Fatal(http.ListenAndServe(lo.addr, nil))
}

func JSONResponse(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(statusCode)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			panic(err)
		}
	}
}

package main

import (
    "os"
    "fmt"
    "syscall"
    "net/http"

    flags "github.com/jessevdk/go-flags"
    log "github.com/Sirupsen/logrus"

    "github.com/nomad-ci/ci-job-builder-service/internal/app/job_builder"

    nomadapi "github.com/hashicorp/nomad/api"

    "github.com/gorilla/mux"
)

var version string = "undef"

type Options struct {
    Debug      bool   `env:"DEBUG"     long:"debug"    description:"enable debug"`
    LogFile    string `env:"LOG_FILE"  long:"log-file" description:"path to JSON log file"`

    HttpPort   int    `env:"HTTP_PORT" long:"port"     description:"port to accept requests on" default:"8080"`

    NomadAddr  string `env:"NOMAD_ADDR"  long:"nomad-addr"  description:"address of the Nomad server"     required:"true"`
    // NomadToken string `env:"NOMAD_TOKEN" long:"nomad-token" description:"auth token for this application" required:"true"`
}

func Log(handler http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Infof("%s %s %s", r.RemoteAddr, r.Method, r.URL)
        handler.ServeHTTP(w, r)
    })
}

func checkError(msg string, err error) {
    if err != nil {
        log.Fatalf("%s: %+v", msg, err)
    }
}

func main() {
    var opts Options

    _, err := flags.Parse(&opts)
    if err != nil {
        os.Exit(1)
    }

    if opts.Debug {
        log.SetLevel(log.DebugLevel)
    }

    if opts.LogFile != "" {
        logFp, err := os.OpenFile(opts.LogFile, os.O_WRONLY | os.O_APPEND | os.O_CREATE, 0600)
        checkError(fmt.Sprintf("error opening %s", opts.LogFile), err)

        defer logFp.Close()

        // ensure panic output goes to log file
        syscall.Dup2(int(logFp.Fd()), 1)
        syscall.Dup2(int(logFp.Fd()), 2)

        // log as JSON
        log.SetFormatter(&log.JSONFormatter{})

        // send output to file
        log.SetOutput(logFp)
    }

    log.Infof("version: %s", version)

    nomadClient, err := nomadapi.NewClient(&nomadapi.Config{
        Address: opts.NomadAddr,
    })
    checkError("creating Nomad client", err)

    router := mux.NewRouter()

    handler := job_builder.NewJobBuilder(nomadClient.Jobs())

    handler.InstallHandlers(router.PathPrefix("/").Subrouter())

    httpServer := &http.Server{
        Addr: fmt.Sprintf(":%d", opts.HttpPort),
        Handler: Log(router),
    }

    checkError("launching HTTP server", httpServer.ListenAndServe())
}

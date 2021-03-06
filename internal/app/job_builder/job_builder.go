package job_builder

import (
    "fmt"
    "time"
    "io/ioutil"
    "net"
    "net/http"

    log "github.com/Sirupsen/logrus"

    "github.com/gorilla/mux"

    nomadapi "github.com/hashicorp/nomad/api"
    "github.com/nomad-ci/ci-job-builder-service/internal/pkg/interfaces"

    "encoding/json"
    "github.com/mitchellh/mapstructure"
    "github.com/ghodss/yaml"
)

// mapstructure used so that we can use the same serialization format as used in
// the Nomad source.  this allows for dropping in Artifacts without having to
// define our own type for it.
type JobSpec struct {
    Driver    string                   `mapstructure:"driver"`
    Config    map[string]interface{}   `mapstructure:"config"`
    Artifacts []*nomadapi.TaskArtifact `mapstructure:"artifacts"`
    Env       map[string]string        `mapstructure:"env"`
    Resources *nomadapi.Resources      `mapstructure:"resources"`
}

type JobBuilderPayload struct {
    JobSpec       string `json:"job_spec"`
    SourceArchive string `json:"source_archive"`
}

type JobBuilder struct {
    nomad interfaces.NomadJobs
}

func NewJobBuilder(nomad interfaces.NomadJobs) *JobBuilder {
    return &JobBuilder{
        nomad: nomad,
    }
}

func (self *JobBuilder) InstallHandlers(router *mux.Router) {
    router.
        Methods("POST").
        Path("/build-job").
        Headers(
            "Content-Type", "application/json",
        ).
        HandlerFunc(self.BuildJob)
}

func (self *JobBuilder) BuildJob(resp http.ResponseWriter, req *http.Request) {
    var err error
    var remoteAddr string

    if xff, ok := req.Header["X-Forwarded-For"]; ok {
        remoteAddr = xff[0]
    } else {
        remoteAddr, _, err = net.SplitHostPort(req.RemoteAddr)
        if err != nil {
            log.Warnf("unable to parse RemoteAddr '%s': %s", remoteAddr, err)
            remoteAddr = req.RemoteAddr
        }
    }

    logEntry := log.WithField("remote_ip", remoteAddr)

    body, err := ioutil.ReadAll(req.Body)
    if err != nil {
        logEntry.Errorf("unable to read body: %s", err)
        resp.WriteHeader(http.StatusBadRequest)
        return
    }

    var payload JobBuilderPayload
    err = json.Unmarshal(body, &payload)
    if err != nil {
        logEntry.Errorf("unable to unmarshal body: %s", err)
        resp.WriteHeader(http.StatusBadRequest)
        return
    }

    var untypedJobSpec map[string]interface{}
    err = yaml.Unmarshal([]byte(payload.JobSpec), &untypedJobSpec)
    if err != nil {
        logEntry.Errorf("unable to unmarshal job spec: %s", err)
        resp.WriteHeader(http.StatusBadRequest)
        return
    }

    var jobSpec JobSpec
    err = mapstructure.Decode(untypedJobSpec, &jobSpec)
    if err != nil {
        logEntry.Errorf("unable to decode untyped job spec: %s", err)
        resp.WriteHeader(http.StatusBadRequest)
        return
    }

    jobId := fmt.Sprintf("ci-job/%d", time.Now().Unix())

    // if the user hasn't already told us what to do with the clone_source
    // archive, add a default artifact to the job.
    defaultArtifactSource := StringToPtr("${NOMAD_META_nomadci_clone_source}")
    defaultArtifactSatisfied := false
    for _, artifact := range jobSpec.Artifacts {
        if *artifact.GetterSource == *defaultArtifactSource {
            defaultArtifactSatisfied = true
            break
        }
    }

    if ! defaultArtifactSatisfied {
        jobSpec.Artifacts = append(
            // this is the default artifact that provides the source that was
            // previously cloned
            []*nomadapi.TaskArtifact{
                &nomadapi.TaskArtifact{
                    GetterSource: StringToPtr("${NOMAD_META_nomadci_clone_source}"),
                },
            },
            jobSpec.Artifacts...,
        )
    }

    // https://www.nomadproject.io/api/json-jobs.html
    job := &nomadapi.Job{
        ID: StringToPtr(jobId),
        Name: StringToPtr(jobId),

        Datacenters: []string{"dc1"},

        Type: StringToPtr("batch"),

        TaskGroups: []*nomadapi.TaskGroup{
            &nomadapi.TaskGroup{
                Name: StringToPtr("builder"),
                RestartPolicy: &nomadapi.RestartPolicy{
                    Attempts: IntToPtr(0),
                    Mode: StringToPtr("fail"),
                },

                Tasks: []*nomadapi.Task{
                    &nomadapi.Task{
                        Name: "builder",

                        // @todo only allow a limited set of config params,
                        // based on driver
                        Driver: jobSpec.Driver,
                        Config: jobSpec.Config,

                        Meta: map[string]string{
                            "nomadci.clone_source": payload.SourceArchive,
                        },

                        Env: jobSpec.Env,
                        Resources: jobSpec.Resources,
                        Artifacts: jobSpec.Artifacts,
                    },
                },
            },
        },

    }

    jobResp, _, err := self.nomad.Register(job, nil)
    if err != nil {
        logEntry.Errorf("unable to submit job: %s", err)
        resp.WriteHeader(http.StatusInternalServerError)
        return
    }

    logEntry.Infof("submitted job with eval id %s", jobResp.EvalID)

    resp.WriteHeader(http.StatusNoContent)
}

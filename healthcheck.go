package main

import (
	"sync"
	"time"

	health "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/service-status-go/gtg"
)

const healthPath = "/__health"

type healthService struct {
	*sync.Mutex
	checks []health.Check
	config healthConfig
	stop   chan bool
}

type healthConfig struct {
	appSystemCode string
	appName       string
	port          string
}

type healthStatus struct {
	message string
	err     error
	time    time.Time
}

var splunkHealth healthStatus

func newHealthService(config healthConfig, check func() healthStatus) *healthService {
	service := &healthService{&sync.Mutex{}, nil, config, nil}
	service.checks = []health.Check{
		service.splunkCheck(check),
	}
	return service
}

func (hs *healthService) splunkCheck(check func() healthStatus) health.Check {
	return health.Check{
		BusinessImpact:   "Monitoring of publishing events is hindered. SLA compliance cannot be tracked",
		Name:             "Splunk healthcheck",
		PanicGuide:       "https://dewey.ft.com/splunk-event-reader.html",
		Severity:         2,
		TechnicalSummary: "Splunk is not able to return results, therefore publishing transactions can not be processed. Check Splunk REST API availability.",
		Checker: func() (msg string, err error) {
			hs.Lock()
			splunkHealth = check()
			msg = splunkHealth.message
			err = splunkHealth.err
			hs.Unlock()
			return msg, err
		},
	}
}

func (hs *healthService) gtgCheck() gtg.Status {
	for _, check := range hs.checks {
		if _, err := check.Checker(); err != nil {
			return gtg.Status{GoodToGo: false, Message: err.Error()}
		}
	}
	return gtg.Status{GoodToGo: true}
}

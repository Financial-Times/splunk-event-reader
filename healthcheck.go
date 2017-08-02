package main

import (
	health "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/service-status-go/gtg"
)

const healthPath = "/__health"

type healthService struct {
	config *healthConfig
	checks []health.Check
}

type healthConfig struct {
	appSystemCode string
	appName       string
	port          string
}

func newHealthService(config *healthConfig, splunkService SplunkServiceI) *healthService {
	service := &healthService{config: config}
	service.checks = []health.Check{
		service.splunkCheck(splunkService),
	}
	return service
}

func (service *healthService) splunkCheck(splunkService SplunkServiceI) health.Check {
	return health.Check{
		BusinessImpact:   "Monitoring of publishing events is hindered. SLA compliance cannot be tracked",
		Name:             "Splunk healthcheck",
		PanicGuide:       "https://dewey.ft.com/splunk-event-reader.html",
		Severity:         1,
		TechnicalSummary: "Splunk is not able to return results, therefore publishing transactions can not be processed. Check Splunk REST API availability.",
		Checker:          splunkService.IsHealthy,
	}
}

func (service *healthService) gtgCheck() gtg.Status {
	for _, check := range service.checks {
		if _, err := check.Checker(); err != nil {
			return gtg.Status{GoodToGo: false, Message: err.Error()}
		}
	}
	return gtg.Status{GoodToGo: true}
}

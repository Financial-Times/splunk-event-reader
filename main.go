package main

import (
	"fmt"
	health "github.com/Financial-Times/go-fthealth/v1_1"
	status "github.com/Financial-Times/service-status-go/httphandlers"
	log "github.com/Sirupsen/logrus"
	"github.com/davecgh/go-spew/spew"
	"github.com/jawher/mow.cli"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

const appDescription = "Reads Splunk events via the Splunk REST API"

func main() {
	app := cli.App("splunk-event-reader", appDescription)

	appSystemCode := app.String(cli.StringOpt{
		Name:   "app-system-code",
		Value:  "splunk-event-reader",
		Desc:   "System Code of the application",
		EnvVar: "APP_SYSTEM_CODE",
	})

	appName := app.String(cli.StringOpt{
		Name:   "app-name",
		Value:  "Splunk Event Reader",
		Desc:   "Application name",
		EnvVar: "APP_NAME",
	})

	port := app.String(cli.StringOpt{
		Name:   "port",
		Value:  "8080",
		Desc:   "Port to listen on",
		EnvVar: "APP_PORT",
	})

	splunkUser := app.String(cli.StringOpt{
		Name:   "splunk-user",
		Value:  "upp-api",
		Desc:   "Splunk user name",
		EnvVar: "SPLUNK_USER",
	})

	splunkPassword := app.String(cli.StringOpt{
		Name:   "splunk-password",
		Value:  "",
		Desc:   "Splunk password",
		EnvVar: "SPLUNK_PASSWORD",
	})

	splunkURL := app.String(cli.StringOpt{
		Name:   "splunk-url",
		Value:  "https://financialtimes.splunkcloud.com:8089",
		Desc:   "Splunk URL",
		EnvVar: "SPLUNK_URL",
	})

	log.SetLevel(log.InfoLevel)
	log.Infof("[Startup] splunk-event-reader is starting ")

	app.Action = func() {
		log.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)

		go func() {
			serveAdminEndpoints(*appSystemCode, *appName, *port)
		}()

		splunkService := newSplunkService(splunkAccessConfig{user: *splunkUser, password: *splunkPassword, restURL: *splunkURL})

		demoSplunkCall(splunkService)

		waitForSignal()
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Errorf("App could not start, error=[%s]\n", err)
		return
	}
}

func demoSplunkCall(service SplunkServiceI) {
	//results, err := splunkService.GetEvents(monitoringQuery{Environment: "xp", ContentType: "annotations", EarliestTime: "-5m"})
	results, err := service.GetTransactions(monitoringQuery{Environment: "xp", ContentType: "annotations", EarliestTime: "-5m"})
	if err != nil {
		log.Errorf("Could not get splunk results, error=[%s]\n", err)
	}
	fmt.Printf("Received %d events\n", len(results))
	spew.Dump(results)
}

func serveAdminEndpoints(appSystemCode string, appName string, port string) {
	healthService := newHealthService(&healthConfig{appSystemCode: appSystemCode, appName: appName, port: port})

	serveMux := http.NewServeMux()

	hc := health.HealthCheck{SystemCode: appSystemCode, Name: appName, Description: appDescription, Checks: healthService.checks}
	serveMux.HandleFunc(healthPath, health.Handler(hc))
	serveMux.HandleFunc(status.GTGPath, status.NewGoodToGoHandler(healthService.gtgCheck))
	serveMux.HandleFunc(status.BuildInfoPath, status.BuildInfoHandler)

	if err := http.ListenAndServe(":"+port, serveMux); err != nil {
		log.Fatalf("Unable to start: %v", err)
	}
}

func waitForSignal() {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

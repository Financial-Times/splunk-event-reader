package main

import (
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	health "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/http-handlers-go/httphandlers"
	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/Sirupsen/logrus"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/jawher/mow.cli"
	"github.com/rcrowley/go-metrics"
)

const appDescription = "Reads Splunk events via the Splunk REST API"

func main() {
	app := initApp()
	err := app.Run(os.Args)
	if err != nil {
		log.Errorf("App could not start, error=[%s]\n", err)
		return
	}
}
func initApp() *cli.Cli {
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
	environment := app.String(cli.StringOpt{
		Name:   "environment",
		Value:  "",
		Desc:   "Name of the cluster",
		EnvVar: "ENVIRONMENT",
	})
	splunkUser := app.String(cli.StringOpt{
		Name:   "splunk-user",
		Desc:   "Splunk user name",
		EnvVar: "SPLUNK_USER",
	})
	splunkPassword := app.String(cli.StringOpt{
		Name:   "splunk-password",
		Desc:   "Splunk password",
		EnvVar: "SPLUNK_PASSWORD",
	})
	splunkURL := app.String(cli.StringOpt{
		Name:   "splunk-url",
		Desc:   "Splunk REST API URL",
		EnvVar: "SPLUNK_URL",
	})
	log.SetLevel(log.InfoLevel)
	log.Infof("[Startup] splunk-event-reader is starting ")
	app.Action = func() {
		log.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)

		splunkService := newSplunkService(splunkAccessConfig{user: *splunkUser, password: *splunkPassword, restURL: *splunkURL, environment: *environment})
		healthService := newHealthService(healthConfig{appSystemCode: *appSystemCode, appName: *appName, port: *port}, splunkService.IsHealthy)

		go func() {
			routeRequests(splunkService, healthService, *port)
		}()

		waitForSignal()
		healthService.stop <- true

	}
	return app
}

func routeRequests(splunkService SplunkServiceI, healthService *healthService, port string) {
	requestHandler := requestHandler{splunkService: splunkService}

	serveMux := http.NewServeMux()

	hc := health.HealthCheck{SystemCode: healthService.config.appSystemCode, Name: healthService.config.appName, Description: appDescription, Checks: healthService.checks}
	serveMux.HandleFunc(healthPath, health.Handler(hc))
	serveMux.HandleFunc(status.GTGPath, status.NewGoodToGoHandler(healthService.gtgCheck))
	serveMux.HandleFunc(status.BuildInfoPath, status.BuildInfoHandler)

	servicesRouter := mux.NewRouter()
	servicesRouter.HandleFunc("/{contentType}/transactions", requestHandler.getTransactions).Methods("GET")
	servicesRouter.HandleFunc("/{contentType}/events", requestHandler.getLastEvent).Methods("GET")

	var monitoringRouter http.Handler = servicesRouter
	monitoringRouter = httphandlers.TransactionAwareRequestLoggingHandler(logrus.StandardLogger(), monitoringRouter)
	monitoringRouter = httphandlers.HTTPMetricsHandler(metrics.DefaultRegistry, monitoringRouter)

	serveMux.Handle("/", monitoringRouter)

	server := &http.Server{Addr: ":" + port, Handler: serveMux}

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			logrus.Infof("HTTP server closing with message: %v", err)
		}
		wg.Done()
	}()

	waitForSignal()
	logrus.Infof("[Shutdown] SplunkEventReader is shutting down")

	if err := server.Close(); err != nil {
		logrus.Errorf("Unable to stop http server: %v", err)
	}

	wg.Wait()
}

func waitForSignal() {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

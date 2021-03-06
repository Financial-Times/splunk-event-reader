package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/jawher/mow.cli"
	"github.com/rcrowley/go-metrics"

	health "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/go-logger/v2"
	"github.com/Financial-Times/http-handlers-go/v2/httphandlers"
	status "github.com/Financial-Times/service-status-go/httphandlers"
)

const appDescription = "Reads Splunk events via the Splunk REST API"

func main() {

	app := initApp()

	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("App could not start, error=[%s]\n", err)
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

	splunkIndex := app.String(cli.StringOpt{
		Name:   "splunk-index",
		Desc:   "Splunk index name",
		EnvVar: "SPLUNK_INDEX",
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

	logLevel := app.String(cli.StringOpt{
		Name:   "logLevel",
		Value:  "INFO",
		Desc:   "Logging level (DEBUG, INFO, WARN, ERROR)",
		EnvVar: "LOG_LEVEL",
	})

	uppLogger := logger.NewUPPLogger(*appSystemCode, *logLevel)
	uppLogger.Infof("[Startup] splunk-event-reader is starting ")

	app.Action = func() {

		uppLogger.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)
		splunkService := newSplunkService(splunkAccessConfig{user: *splunkUser, password: *splunkPassword, restURL: *splunkURL, environment: *environment, index: *splunkIndex})
		healthService := newHealthService(healthConfig{appSystemCode: *appSystemCode, appName: *appName, port: *port}, splunkService.IsHealthy)

		go func() {
			routeRequests(healthService, *port, requestHandler{
				splunkService: splunkService,
				log:           uppLogger,
			}, )
		}()

		waitForSignal()
		healthService.stop <- true

	}

	return app
}

func routeRequests(healthService *healthService, port string, rh requestHandler) {

	serveMux := http.NewServeMux()

	log := rh.log

	hc := health.TimedHealthCheck{
		HealthCheck: health.HealthCheck{
			SystemCode:  healthService.config.appSystemCode,
			Name:        healthService.config.appName,
			Description: appDescription,
			Checks:      healthService.checks,
		},
		Timeout: 10 * time.Second,
	}

	serveMux.HandleFunc(healthPath, health.Handler(hc))
	serveMux.HandleFunc(status.GTGPath, status.NewGoodToGoHandler(healthService.gtgCheck))
	serveMux.HandleFunc(status.BuildInfoPath, status.BuildInfoHandler)

	servicesRouter := mux.NewRouter()
	servicesRouter.HandleFunc("/{contentType}/transactions", rh.getTransactions).Methods("GET")
	servicesRouter.HandleFunc("/{contentType}/events", rh.getLastEvent).Methods("GET")

	var monitoringRouter http.Handler = servicesRouter
	monitoringRouter = httphandlers.TransactionAwareRequestLoggingHandler(log, monitoringRouter)
	monitoringRouter = httphandlers.HTTPMetricsHandler(metrics.DefaultRegistry, monitoringRouter)

	serveMux.Handle("/", monitoringRouter)

	server := &http.Server{Addr: ":" + port, Handler: serveMux}

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Infof("HTTP server closing with message: %v", err)
		}
		wg.Done()
	}()

	waitForSignal()
	log.Infof("[Shutdown] SplunkEventReader is shutting down")

	if err := server.Close(); err != nil {
		log.Errorf("Unable to stop http server: %v", err)
	}

	wg.Wait()
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

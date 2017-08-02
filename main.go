package main

import (
	"fmt"
	health "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/http-handlers-go/httphandlers"
	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/Sirupsen/logrus"
	log "github.com/Sirupsen/logrus"
	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
	"github.com/jawher/mow.cli"
	"github.com/rcrowley/go-metrics"
	"net/http"
	"os"
	"os/signal"
	"sync"
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

	environment := app.String(cli.StringOpt{
		Name:   "environment",
		Value:  "xp",
		Desc:   "Name of the cluster",
		EnvVar: "ENVIRONMENT",
	})

	splunkUser := app.String(cli.StringOpt{
		Name:   "splunk-user",
		Value:  "",
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
		Value:  "",
		Desc:   "Splunk URL",
		EnvVar: "SPLUNK_URL",
	})

	log.SetLevel(log.InfoLevel)
	log.Infof("[Startup] splunk-event-reader is starting ")

	app.Action = func() {
		log.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)

		splunkService := newSplunkService(splunkAccessConfig{user: *splunkUser, password: *splunkPassword, restURL: *splunkURL, environment: *environment})

		go func() {
			serveAdminEndpoints(*appSystemCode, *appName, *port, requestHandler{splunkService: splunkService})
		}()

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
	results, err := service.GetTransactions(monitoringQuery{ContentType: "annotations", EarliestTime: "-5m"})
	if err != nil {
		log.Errorf("Could not get splunk results, error=[%s]\n", err)
	}
	fmt.Printf("Received %d events\n", len(results))
	spew.Dump(results)
}

func serveAdminEndpoints(appSystemCode string, appName string, port string, requestHandler requestHandler) {
	healthService := newHealthService(&healthConfig{appSystemCode: appSystemCode, appName: appName, port: port})

	serveMux := http.NewServeMux()

	hc := health.HealthCheck{SystemCode: appSystemCode, Name: appName, Description: appDescription, Checks: healthService.checks}
	serveMux.HandleFunc(healthPath, health.Handler(hc))
	serveMux.HandleFunc(status.GTGPath, status.NewGoodToGoHandler(healthService.gtgCheck))
	serveMux.HandleFunc(status.BuildInfoPath, status.BuildInfoHandler)

	servicesRouter := mux.NewRouter()
	servicesRouter.HandleFunc("/{contentType}/transactions", requestHandler.getTransactions).Methods("GET")
	servicesRouter.HandleFunc("/{contentType}/transactions/{transactionId}", requestHandler.getTransactionsByID).Methods("GET")

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
	logrus.Infof("[Shutdown] PostPublicationCombiner is shutting down")

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

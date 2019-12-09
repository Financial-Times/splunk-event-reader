package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	fthealth "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/go-logger/v2"
	cli "github.com/jawher/mow.cli"
	"github.com/stretchr/testify/assert"
)

type flags struct {
	error     bool
	noResults bool
}

var testFlags = flags{}

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

func TestMain(m *testing.M) {

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if testFlags.error {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			status := http.StatusOK
			var inputFile string
			var inputJSON []byte
			switch {
			case strings.Contains(r.RequestURI, "audit_sid/results"):
				inputFile = "testdata/splunk_audit_response.json"
			case strings.Contains(r.RequestURI, "last_event_sid/results"):
				inputFile = "testdata/splunk_publish_end_sample.json"
			case strings.Contains(r.RequestURI, "transactions_sid/results"):
				inputFile = "testdata/splunk_response_sample.json"
			case strings.Contains(r.RequestURI, "_sid"):
				inputJSON = []byte(`{
										"entry": [
											{
												"content":
												{
													"dispatchState": "DONE",
													"isDone": true,
													"messages": []
												}
											}
										]
									}
									`)
			case strings.Contains(r.PostForm.Get("search"), "audit"):
				inputJSON = []byte(`{"sid":"audit_sid"}`)
				status = http.StatusCreated
			case strings.Contains(r.PostForm.Get("search"), "head"):
				inputJSON = []byte(`{"sid":"last_event_sid"}`)
				status = http.StatusCreated
			default:
				inputJSON = []byte(`{"sid":"transactions_sid"}`)
				status = http.StatusCreated
			}

			w.WriteHeader(status)

			if inputJSON == nil {
				inputJSON, _ = ioutil.ReadFile(inputFile)
			}
			if testFlags.noResults && strings.Contains(r.RequestURI, "/results") {
				w.Write([]byte(`{"results":[]}`))
			} else {
				w.Write(inputJSON)
			}
		}
	}))

	defer splunkServer.Close()

	args := []string{
		`--app-system-code=splunk-event-reader`,
		`--app-name=Splunk Event Reader`,
		`--port=8080`,
		`--environment=ci`,
		`--splunk-user=dummy`,
		`--splunk-password=dummy`,
		fmt.Sprintf(`--splunk-url=%s`, splunkServer.URL),
	}

	app := initApp()

	go func() {
		app.Run(args)
	}()

	client := &http.Client{}

	retryCount := 0

	for {
		retryCount++
		if retryCount > 5 {
			fmt.Printf("Unable to start server")
			os.Exit(-1)
		}
		time.Sleep(100 * time.Millisecond)
		req, _ := http.NewRequest("GET", "http://localhost:8080/__gtg", nil)
		res, err := client.Do(req)
		if err == nil && res.StatusCode == http.StatusOK {
			break
		}
	}

	os.Exit(m.Run())
}

func Test_GetGtg(t *testing.T) {
	tests := []struct {
		url            string
		expectedStatus int
		flags          flags
	}{
		{url: "http://localhost:8080/__gtg", expectedStatus: http.StatusOK},
		// health status is cached, so we need to force it to fail before
		{url: "http://localhost:8080/annotations/transactions", flags: flags{error: true}, expectedStatus: http.StatusInternalServerError},
		{url: "http://localhost:8080/__gtg", expectedStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		testFlags = test.flags

		client := &http.Client{}

		req, _ := http.NewRequest("GET", test.url, nil)
		res, err := client.Do(req)
		assert.NoError(t, err)

		assert.Equal(t, test.expectedStatus, res.StatusCode)

		testFlags = flags{}
	}
}

func Test_Health(t *testing.T) {
	tests := []struct {
		url            string
		expectedStatus int
		expectedHealth bool
		flags          flags
	}{

		{url: "http://localhost:8080/annotations/transactions", expectedStatus: http.StatusOK},
		{url: "http://localhost:8080/__health", expectedStatus: http.StatusOK, expectedHealth: true},
		// health status is cached, so we need to force it to fail before
		{url: "http://localhost:8080/annotations/transactions", flags: flags{error: true}, expectedStatus: http.StatusInternalServerError},
		{url: "http://localhost:8080/__health", expectedStatus: http.StatusOK, expectedHealth: false},
	}

	for _, test := range tests {
		testFlags = test.flags

		client := &http.Client{}

		req, _ := http.NewRequest("GET", test.url, nil)
		res, err := client.Do(req)
		assert.NoError(t, err)

		if test.url == "http://localhost:8080/__health" {
			rBody := make([]byte, res.ContentLength)
			res.Body.Read(rBody)
			res.Body.Close()

			health := fthealth.HealthResult{}
			json.Unmarshal(rBody, &health)
			assert.Equal(t, test.expectedHealth, health.Ok)
		}

		assert.Equal(t, test.expectedStatus, res.StatusCode)

		testFlags = flags{}
	}
}

func Test_GetTransactions(t *testing.T) {
	tests := []struct {
		url            string
		expectedStatus int
		flags          flags
	}{
		{url: "http://localhost:8080/annotations/transactions", expectedStatus: http.StatusOK},
		{url: "http://localhost:8080/annotations/transactions", flags: flags{error: true}, expectedStatus: http.StatusInternalServerError},
		{url: "http://localhost:8080/INVALID_CONTENT_TYPE/transactions", expectedStatus: http.StatusBadRequest},
		{url: "http://localhost:8080/annotations/transactions?earliestTime=-10m", expectedStatus: http.StatusOK},
		{url: "http://localhost:8080/annotations/transactions?earliestTime=-1year", expectedStatus: http.StatusBadRequest},
		{url: "http://localhost:8080/annotations/transactions?uuid=191b9e5e-3356-4ae9-801f-0ce8d34f6cbe&uuid=0dd0a85f-2926-4371-a0d8-2ae13d738476", expectedStatus: http.StatusOK},
		{url: "http://localhost:8080/annotations/transactions?uuid=INVALID_UUID&uuid=0dd0a85f-2926-4371-a0d8-2ae13d738476", expectedStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		testFlags = test.flags
		expectedJSON, err := ioutil.ReadFile("testdata/splunk_transaction_output.json")
		expectedTx := []transactionEvent{}
		json.Unmarshal(expectedJSON, &expectedTx)

		client := &http.Client{}

		req, _ := http.NewRequest("GET", test.url, nil)
		res, err := client.Do(req)
		assert.NoError(t, err)

		assert.Equal(t, test.expectedStatus, res.StatusCode)

		if test.expectedStatus == http.StatusOK {

			rBody := make([]byte, res.ContentLength)
			res.Body.Read(rBody)
			res.Body.Close()

			tx := []transactionEvent{}
			json.Unmarshal(rBody, &tx)
			assert.Equal(t, expectedTx, tx)
		}
		testFlags = flags{}
	}
}

func Test_GetLastEvent(t *testing.T) {
	tests := []struct {
		url            string
		expectedStatus int
		flags          flags
	}{
		{url: "http://localhost:8080/annotations/events?lastEvent=true", expectedStatus: http.StatusOK},
		{url: "http://localhost:8080/annotations/events?lastEvent=true", flags: flags{error: true}, expectedStatus: http.StatusInternalServerError},
		{url: "http://localhost:8080/annotations/events?lastEvent=true", flags: flags{noResults: true}, expectedStatus: http.StatusNotFound},
		{url: "http://localhost:8080/annotations/events?lastEvent=INVALID", expectedStatus: http.StatusBadRequest},
		{url: "http://localhost:8080/annotations/events", expectedStatus: http.StatusBadRequest},
		{url: "http://localhost:8080/INVALID_CONTENT_TYPE/events?lastEvent=true", expectedStatus: http.StatusBadRequest},
		{url: "http://localhost:8080/annotations/events?lastEvent=true&earliestTime=-10m", expectedStatus: http.StatusOK},
		{url: "http://localhost:8080/annotations/events?lastEvent=true&earliestTime=-1year", expectedStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		testFlags = test.flags
		expectedJSON, err := ioutil.ReadFile("testdata/splunk_publish_end_output.json")
		expectedEvent := publishEvent{}
		json.Unmarshal(expectedJSON, &expectedEvent)

		client := &http.Client{}

		req, _ := http.NewRequest("GET", test.url, nil)
		res, err := client.Do(req)
		assert.NoError(t, err)

		assert.Equal(t, test.expectedStatus, res.StatusCode)

		if test.expectedStatus == http.StatusOK {
			rBody := make([]byte, res.ContentLength)
			res.Body.Read(rBody)
			res.Body.Close()

			event := publishEvent{}
			json.Unmarshal(rBody, &event)
			assert.Equal(t, expectedEvent, event)
		}
		testFlags = flags{}
	}
}

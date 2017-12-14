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
	"github.com/stretchr/testify/assert"
)

type flags struct {
	error     bool
	noResults bool
}

var testFlags = flags{}

func TestMain(m *testing.M) {

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if testFlags.error {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			var inputFile string
			if strings.Contains(r.PostForm.Get("search"), "audit") {
				inputFile = "testdata/splunk_publish_end_sample.json"
			} else if strings.Contains(r.PostForm.Get("search"), "head 1") {
				inputFile = "testdata/splunk_publish_end_sample.json"
			} else {
				inputFile = "testdata/splunk_response_sample.json"
			}
			inputJSON, _ := ioutil.ReadFile(inputFile)
			if !testFlags.noResults {
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

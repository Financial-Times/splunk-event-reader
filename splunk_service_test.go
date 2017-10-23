package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplunkService_GetTransactions(t *testing.T) {
	tests := []struct {
		inputFile  string
		outputFile string
		query      monitoringQuery
		status     int
		hasError   bool
	}{
		{"testdata/splunk_response_sample.json", "testdata/splunk_transaction_output.json", monitoringQuery{}, http.StatusOK, false},
		{"testdata/splunk_response_sample.json", "testdata/splunk_transaction_output.json", monitoringQuery{UUIDs: []string{"27355ee6-e280-4fb8-b825-8f14be1be9d3"}, EarliestTime: "-5m"}, http.StatusOK, false},
		{"testdata/splunk_response_sample.json", "", monitoringQuery{}, http.StatusNotFound, true},
	}

	for _, test := range tests {
		expectedJSON, err := ioutil.ReadFile(test.outputFile)
		expectedTx := []transactionEvent{}
		json.Unmarshal(expectedJSON, &expectedTx)

		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			r.ParseForm()

			for _, uuid := range test.query.UUIDs {

				assert.Contains(t, r.Form.Get("search"), uuid)
			}
			if test.query.EarliestTime != "" {
				assert.Contains(t, r.Form.Get("earliest_time"), test.query.EarliestTime)
			}
			w.WriteHeader(test.status)
			if test.status == http.StatusOK {
				inputJSON, err := ioutil.ReadFile(test.inputFile)
				assert.NoError(t, err, "Unexpected error")
				w.Write(inputJSON)
			}
		}))

		defer splunkServer.Close()

		splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
		tx, err := splunkReader.GetTransactions(test.query)
		if test.hasError {
			assert.Error(t, err)
		} else {
			assert.Equal(t, expectedTx, tx)
		}
	}
}

func TestSplunkService_GetLastEvent(t *testing.T) {
	var expectedEvent = &publishEvent{
		Time:            "2017-09-19T15:11:31.795334198Z",
		ContentType:     "Annotations",
		Event:           "PublishEnd",
		IsValid:         "true",
		Level:           "info",
		MonitoringEvent: "true",
		ServiceName:     "annotations-monitoring-service",
		TransactionID:   "tid_evjm9gls5a",
		UUID:            "ed08f771-db28-4d63-b566-0d49c6595111",
	}

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		inputJSON, err := ioutil.ReadFile("testdata/splunk_publish_end_sample.json")
		assert.NoError(t, err, "Unexpected error")
		w.Write(inputJSON)
	}))

	defer splunkServer.Close()

	splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
	event, err := splunkReader.GetLastEvent(monitoringQuery{})
	if err != nil {
		t.Fail()
	}

	assert.Equal(t, expectedEvent, event)
}

func TestSplunkService_GetLastEventError(t *testing.T) {
	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		assert.Equal(t, "-5m", r.Form.Get("earliest_time"))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	defer splunkServer.Close()

	splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
	_, err := splunkReader.GetLastEvent(monitoringQuery{EarliestTime: "-5m"})
	assert.Error(t, err)
}

func TestSplunkService_GetLastEventRetry(t *testing.T) {
	var expectedEvent = &publishEvent{
		Time:            "2017-09-19T15:11:31.795334198Z",
		ContentType:     "Annotations",
		Event:           "PublishEnd",
		IsValid:         "true",
		Level:           "info",
		MonitoringEvent: "true",
		ServiceName:     "annotations-monitoring-service",
		TransactionID:   "tid_evjm9gls5a",
		UUID:            "ed08f771-db28-4d63-b566-0d49c6595111",
	}

	splunkCallCount := 0

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		splunkCallCount++
		if splunkCallCount == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
			inputJSON, err := ioutil.ReadFile("testdata/splunk_publish_end_sample.json")
			assert.NoError(t, err, "Unexpected error")
			w.Write(inputJSON)
		}
	}))

	defer splunkServer.Close()

	splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
	event, err := splunkReader.GetLastEvent(monitoringQuery{})
	if err != nil {
		t.Fail()
	}

	assert.Equal(t, expectedEvent, event)
}

func TestSplunkService_IsHealthy(t *testing.T) {

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		inputJSON, err := ioutil.ReadFile("testdata/splunk_audit_response.json")
		assert.NoError(t, err, "Unexpected error")
		w.Write(inputJSON)
	}))

	defer splunkServer.Close()

	splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
	health := splunkReader.IsHealthy()
	assert.NoError(t, health.err)
	assert.Equal(t, "Splunk is ok", health.message)
}

func TestSplunkService_IsHealthyFail(t *testing.T) {

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	defer splunkServer.Close()

	splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
	health := splunkReader.IsHealthy()
	assert.Error(t, health.err)
	assert.Equal(t, "Splunk error", health.message)
}

func TestSplunkService_IsHealthyCached(t *testing.T) {

	splunkCallCount := 0

	splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		splunkCallCount++
		assert.Equal(t, 1, splunkCallCount)
		r.ParseForm()
		assert.Contains(t, r.PostForm.Get("search"), "monitoring_event")
		w.WriteHeader(http.StatusOK)
		inputJSON, err := ioutil.ReadFile("testdata/splunk_response_sample.json")
		assert.NoError(t, err, "Unexpected error")
		w.Write(inputJSON)
	}))

	defer splunkServer.Close()

	splunkReader := newSplunkService(splunkAccessConfig{restURL: splunkServer.URL, environment: "test"})
	splunkReader.GetTransactions(monitoringQuery{})
	health := splunkReader.IsHealthy()
	assert.NoError(t, health.err)
	assert.Equal(t, "Splunk is ok", health.message)
}

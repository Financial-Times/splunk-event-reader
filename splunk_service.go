package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/giantswarm/retry-go"
)

const (
	splunkEndpoint            = "/services/search/jobs"
	defaultEarliestTime       = "-10m"
	transactionsQueryTemplate = `search index="%s" monitoring_event=true (environment="%s" OR environment="%s-publish*") (content_type="%s" OR content_type="") transaction_id!="SYNTHETIC*" transaction_id!="*carousel*"  | fields content_type, event, isValid, level, service_name, @time, transaction_id, uuid`
	latestEventQueryTemplate  = `search index="%s" monitoring_event=true (environment="%s" OR environment="%s-publish*") content_type="%s" event="PublishEnd" | fields content_type, event, isValid, level, service_name, @time, transaction_id, uuid | head 1`
	healthcheckQuery          = `search index=_audit | head 1`
	healthCachePeriod         = time.Minute
)

// ErrNoResults returned when the Splunk query yields no results
var ErrNoResults = errors.New("No results")
var regionRegex = regexp.MustCompile("-delivery-(eu|us)$")

// SplunkServiceI Splunk based event reader service
type SplunkServiceI interface {
	GetTransactions(query monitoringQuery) ([]transactionEvent, error)
	GetLastEvent(query monitoringQuery) (*publishEvent, error)
	doQuery(queryString string) (*http.Response, error)
	IsHealthy() healthStatus
}

type splunkAccessConfig struct {
	user        string
	password    string
	restURL     string
	environment string
	region      string
	index       string
}

type splunkService struct {
	sync.RWMutex
	HTTPClient *http.Client
	Config     splunkAccessConfig
	lastHealth healthStatus
}

type monitoringQuery struct {
	ContentType  string
	EarliestTime string
	LatestTime   string
	UUIDs        []string
}

type searchResponse struct {
	Results []publishEvent `json:"results"`
}

type jobDetails struct {
	Entry []jobDetailsEntry `json:"entry"`
}

type jobDetailsEntry struct {
	Content jobDetailsContent `json:"content"`
}

type jobDetailsContent struct {
	DispatchState string       `json:"dispatchState"`
	Messages      []jobMessage `json:"messages"`
	IsDone        bool         `json:"isDone"`
}

type jobMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Help string `json:"help"`
}

type sidResponse struct {
	Sid string `json:"sid"`
}

type JobFailure struct {
	message string
}

func (jobFailure *JobFailure) Error() string {
	return jobFailure.message
}

func NewJobFailure(message string) error {
	return &JobFailure{message: message}
}

func (service *splunkService) GetTransactions(query monitoringQuery) ([]transactionEvent, error) {
	queryString := fmt.Sprintf(transactionsQueryTemplate, service.Config.index, service.Config.environment, regionRegex.ReplaceAllString(service.Config.environment, ""), query.ContentType)

	if len(query.UUIDs) > 0 {
		queryString += " | search uuid IN ("
		for _, uuid := range query.UUIDs {
			queryString += fmt.Sprintf(`"%s",`, uuid)
		}
		queryString += ")"
	}

	v := url.Values{}
	v.Set("search", queryString)
	if query.EarliestTime != "" {
		v.Set("earliest_time", query.EarliestTime)
	} else {

		v.Set("earliest_time", defaultEarliestTime)
	}

	if query.LatestTime != "" {
		v.Set("latest_time", query.LatestTime)
	}

	resp, err := service.doQuery(v.Encode())

	if err != nil {
		return nil, err
	}

	transactions := []transactionEvent{}

	txMap := make(map[string]*transactionEvent)
	decoder := json.NewDecoder(bufio.NewReader(resp.Body))
	response := searchResponse{}
	err = decoder.Decode(&response)
	if err != nil {
		return nil, err
	}
	for _, event := range response.Results {

		transaction := txMap[event.TransactionID]

		if transaction == nil {
			transaction = &transactionEvent{}
			transaction.TransactionID = event.TransactionID
			transaction.ClosedTxn = "0"
			txMap[event.TransactionID] = transaction
		}
		if event.UUID != "" {
			transaction.UUID = event.UUID
		}

		transaction.Events = append(transaction.Events, event)
		transaction.EventCount++
		if event.Event == "PublishStart" {
			transaction.StartTime = event.Time
		}
		if event.Event == "PublishEnd" {
			transaction.ClosedTxn = "1"
		}
	}

	for _, transaction := range txMap {
		if transaction.ClosedTxn != "1" {
			// if transaction has at least one event with the required content type: keep it
			for _, event := range transaction.Events {
				if strings.EqualFold(event.ContentType, query.ContentType) {
					transactions = append(transactions, *transaction)
					break
				}
			}
		}
	}

	return transactions, nil
}

func (service *splunkService) GetLastEvent(query monitoringQuery) (*publishEvent, error) {
	queryString := fmt.Sprintf(latestEventQueryTemplate, service.Config.index, service.Config.environment, regionRegex.ReplaceAllString(service.Config.environment, ""), query.ContentType)

	v := url.Values{}
	v.Set("search", queryString)
	if query.EarliestTime != "" {
		v.Set("earliest_time", query.EarliestTime)
	}

	resp, err := service.doQuery(v.Encode())
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bufio.NewReader(resp.Body))
	response := searchResponse{}
	err = decoder.Decode(&response)
	if err != nil {
		return nil, err
	}

	if len(response.Results) > 0 {
		publishEvent := response.Results[0]
		return &publishEvent, nil
	}

	return nil, ErrNoResults
}

func (service *splunkService) doQuery(query string) (*http.Response, error) {
	var resp *http.Response
	// call blocks until job finishes
	query = query + "&exec_mode=blocking&output_mode=json"
	httpCall := func() error {
		sid, err := service.newJob(query)
		if err != nil {
			return err
		}

		job, err := service.getJobDetails(sid)
		if err != nil {
			return err
		}
		err = validateJob(sid, job)
		if err != nil {
			return err
		}

		// fetch results and disable the default result count limit (0 = disabled)
		serviceUrl := fmt.Sprintf("%v%v/%v/results?count=0&output_mode=json", service.Config.restURL, splunkEndpoint, sid)
		req, err := http.NewRequest("GET", serviceUrl, nil)
		req.SetBasicAuth(service.Config.user, service.Config.password)
		resp, err = service.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New(resp.Status)
		}
		return nil
	}

	err := retry.Do(httpCall, retry.RetryChecker(func(e error) bool { return e != nil }), retry.MaxTries(2), retry.Sleep(2*time.Second))
	service.updateHealth(err)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (service *splunkService) updateHealth(err error) {
	switch err.(type) {
	case *JobFailure:
		err = nil
	}
	if err != nil {
		service.lastHealth = healthStatus{message: "Splunk error", err: err, time: time.Now()}
	} else {
		service.lastHealth = healthStatus{message: "Splunk is ok", time: time.Now()}
	}
}

func validateJob(sid string, job *jobDetails) error {
	if len(job.Entry) > 0 {
		// mainly looking for warnings caused by index failures (type=WARN), but we treat any message as a bad omen for now
		// note that index failures still result in status=DONE
		if len(job.Entry[0].Content.Messages) > 0 {
			message := fmt.Sprintf("Splunk job %v has status %v with messages: %v", sid, job.Entry[0].Content.DispatchState, job.Entry[0].Content.Messages)
			log.Printf(message)
			return NewJobFailure(message)
		}

		if job.Entry[0].Content.DispatchState == "FAILED" {
			message := "Splunk job with sid %v is %v"
			log.Printf(message, sid, job.Entry[0].Content.DispatchState)
			return NewJobFailure(message)
		}
	}
	return nil
}

func (service *splunkService) newJob(query string) (string, error) {
	var resp *http.Response
	serviceUrl := fmt.Sprintf("%v%v", service.Config.restURL, splunkEndpoint)
	req, err := http.NewRequest("POST", serviceUrl, strings.NewReader(query))
	req.SetBasicAuth(service.Config.user, service.Config.password)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err = service.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", errors.New(resp.Status)
	}

	decoder := json.NewDecoder(bufio.NewReader(resp.Body))
	sidResp := sidResponse{}
	err = decoder.Decode(&sidResp)
	if err != nil {
		return "", err
	}
	return sidResp.Sid, nil
}

func (service *splunkService) getJobDetails(sid string) (*jobDetails, error) {
	var resp *http.Response
	serviceUrl := fmt.Sprintf("%v%v/%v?output_mode=json", service.Config.restURL, splunkEndpoint, sid)
	req, err := http.NewRequest("GET", serviceUrl, nil)
	req.SetBasicAuth(service.Config.user, service.Config.password)
	resp, err = service.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}

	decoder := json.NewDecoder(bufio.NewReader(resp.Body))
	job := jobDetails{}
	err = decoder.Decode(&job)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (service *splunkService) IsHealthy() healthStatus {
	if time.Now().Before(service.lastHealth.time.Add(healthCachePeriod)) {
		return service.lastHealth
	}
	v := url.Values{}
	v.Set("search", healthcheckQuery)
	v.Set("earliest_time", "-10s")

	service.doQuery(v.Encode())
	return service.lastHealth
}

func newSplunkService(config splunkAccessConfig) SplunkServiceI {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	return &splunkService{HTTPClient: client, Config: config}
}

package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/giantswarm/retry-go"
)

const (
	splunkEndpoint            = "/services/search/jobs/export?output_mode=json"
	defaultEarliestTime       = "-10m"
	transactionsQueryTemplate = `search index="heroku" source="http:upp" sourcetype="heroku:drain" monitoring_event=true (environment="%s" OR environment="pub-%s") (content_type="%s" OR content_type="") transaction_id!="SYNTHETIC*" transaction_id!="*carousel*"  | fields content_type, event, isValid, level, service_name, @time, transaction_id, uuid`
	latestEventQueryTemplate  = `search index="heroku" source="http:upp" sourcetype="heroku:drain" monitoring_event=true (environment="%s" OR environment="pub-%s") content_type="%s" event="PublishEnd" | fields content_type, event, isValid, level, service_name, @time, transaction_id, uuid | head 1`
	healthcheckQuery          = `search index=_audit | head 1`
	healthCachePeriod         = time.Minute
)

// ErrNoResults returned when the Splunk query yields no results
var ErrNoResults = errors.New("No results")
var regionRegex = regexp.MustCompile("-u[ks]]$")

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

type splunkRow struct {
	Preview bool             `json:"preview"`
	Offset  int              `json:"offset"`
	LastRow bool             `json:"lastrow"`
	Result  *json.RawMessage `json:"result"`
}

type splunkBaseEvent struct {
	Raw        string `json:"_raw"`
	Sourcetype string `json:"sourcetype"`
	_Time      string `json:"_time"`
	Host       string `json:"host"`
	Index      string `json:"index"`
	Linecount  string `json:"linecount"`
	Source     string `json:"source"`
}

type splunkPublishEvent struct {
	splunkBaseEvent
	publishEvent
}

func (service *splunkService) GetTransactions(query monitoringQuery) ([]transactionEvent, error) {
	queryString := fmt.Sprintf(transactionsQueryTemplate, service.Config.environment, regionRegex.ReplaceAllString(service.Config.environment, "*"), query.ContentType)

	if len(query.UUIDs) > 0 {
		queryString += " uuid IN ("
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
	for {
		row := splunkRow{}
		err = decoder.Decode(&row)
		if err == nil {
			if row.Result != nil && len(*row.Result) > 0 {
				splunkEvent := splunkPublishEvent{}

				err = json.Unmarshal(*row.Result, &splunkEvent)
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, err
				}

				event := splunkEvent.publishEvent

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
		}
		if err == io.EOF || row.LastRow {
			break
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
	queryString := fmt.Sprintf(latestEventQueryTemplate, service.Config.environment, regionRegex.ReplaceAllString(service.Config.environment, "*"), query.ContentType)

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
	for {
		row := splunkRow{}
		err = decoder.Decode(&row)
		if err == nil {
			splunkPublishEvent := splunkPublishEvent{}
			if row.Result != nil && len(*row.Result) > 0 {
				err = json.Unmarshal(*row.Result, &splunkPublishEvent)
				if err != nil {
					return nil, err
				}
				if err == nil {
					return &splunkPublishEvent.publishEvent, nil
				}
			}
		}
		if err == io.EOF || row.LastRow {
			break
		}
	}

	return nil, ErrNoResults
}

func (service *splunkService) doQuery(query string) (*http.Response, error) {
	var resp *http.Response
	httpCall := func() error {
		req, err := http.NewRequest("POST", service.Config.restURL+splunkEndpoint, strings.NewReader(query))
		req.SetBasicAuth(service.Config.user, service.Config.password)
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		resp, err = service.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New(resp.Status)
		}
		return nil
	}

	err := retry.Do(httpCall, retry.RetryChecker(func(e error) bool { return e != nil }), retry.MaxTries(3), retry.Sleep(time.Second))
	if err != nil {
		service.lastHealth = healthStatus{message: "Splunk error", err: err, time: time.Now()}
		return nil, err
	}
	service.lastHealth = healthStatus{message: "Splunk is ok", time: time.Now()}

	return resp, nil
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

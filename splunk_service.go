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
)

const (
	splunkEndpoint            = "/services/search/jobs/export?output_mode=json"
	defaultEarliestTime       = "-10m"
	transactionsQueryTemplate = `search index=heroku source="http:upp" sourcetype="heroku:drain" monitoring_event=true (environment="%s" OR environment="pub-%s") (content_type="%s" OR content_type="") | fields * | transaction transaction_id | search event!="PublishEnd"`
	latestEventQueryTemplate  = `search index=heroku source="http:upp" sourcetype="heroku:drain" monitoring_event=true (environment="%s" OR environment="pub-%s") (content_type="%s" OR content_type="") event="PublishEnd" | fields * | head 1`
	healthcheckQuery          = `search index=_audit | head 1`
)

var NoResultsError = errors.New("No results")

// SplunkServiceI Splunk based event reader service
type SplunkServiceI interface {
	GetTransactions(query monitoringQuery) ([]transactionEvent, error)
	GetLastEvent(query monitoringQuery) (*publishEvent, error)
	doQuery(queryString string) ([]splunkRow, error)
	IsHealthy() (string, error)
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
}

type monitoringQuery struct {
	ContentType   string
	EarliestTime  string
	UUIDs         []string
	TransactionID string
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

type splunkTransactionEvent struct {
	splunkBaseEvent
	transactionEvent
}

func (service *splunkService) GetTransactions(query monitoringQuery) ([]transactionEvent, error) {

	envRegex := regexp.MustCompile("-u[ks]]$")
	queryString := fmt.Sprintf(transactionsQueryTemplate, service.Config.environment, envRegex.ReplaceAllString(service.Config.environment, "*"), query.ContentType)

	if query.TransactionID != "" {
		queryString += fmt.Sprintf(" transaction_id = %s", query.TransactionID)
	}

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

	rows, err := service.doQuery(v.Encode())
	if err != nil {
		return nil, err
	}

	transactions := []transactionEvent{}
	for _, row := range rows {
		splunkTransaction := splunkTransactionEvent{}
		if row.Result != nil && len(*row.Result) > 0 {
			err = json.Unmarshal(*row.Result, &splunkTransaction)
			if err != nil {
				return nil, err
			}
			if err == nil {
				events := []publishEvent{}
				decoder := json.NewDecoder(strings.NewReader(splunkTransaction.Raw))
				for {
					event := publishEvent{}
					err = decoder.Decode(&event)
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, err
					}
					if err == nil {
						events = append(events, event)
					}
				}
				transaction := splunkTransaction.transactionEvent
				transaction.Events = events
				transactions = append(transactions, transaction)
			}
		}
	}
	return transactions, nil
}

func (service *splunkService) GetLastEvent(query monitoringQuery) (*publishEvent, error) {

	envRegex := regexp.MustCompile("-u[ks]]$")
	queryString := fmt.Sprintf(latestEventQueryTemplate, service.Config.environment, envRegex.ReplaceAllString(service.Config.environment, "*"), query.ContentType)

	v := url.Values{}
	v.Set("search", queryString)
	if query.EarliestTime != "" {
		v.Set("earliest_time", query.EarliestTime)
	}

	rows, err := service.doQuery(v.Encode())
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		row := rows[0]
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
	return nil, NoResultsError
}

func (service *splunkService) doQuery(query string) ([]splunkRow, error) {

	req, err := http.NewRequest("POST", service.Config.restURL+splunkEndpoint, strings.NewReader(query))
	req.SetBasicAuth(service.Config.user, service.Config.password)
	resp, err := service.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}

	var rows []splunkRow
	decoder := json.NewDecoder(bufio.NewReader(resp.Body))
	for {
		row := splunkRow{}
		err = decoder.Decode(&row)
		if err == nil {
			rows = append(rows, row)
		}
		if err == io.EOF || row.LastRow {
			break
		}
	}
	return rows, nil
}

func (service *splunkService) IsHealthy() (string, error) {
	v := url.Values{}
	v.Set("search", healthcheckQuery)
	v.Set("earliest_time", "-10s")

	rows, err := service.doQuery(v.Encode())
	if err != nil {
		return "Splunk error", err
	}
	if rows == nil || len(rows) == 0 {
		return "No results from Splunk", errors.New("no results from splunk")
	}

	return "Splunk is ok", nil
}

func newSplunkService(config splunkAccessConfig) SplunkServiceI {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	return &splunkService{HTTPClient: client, Config: config}
}

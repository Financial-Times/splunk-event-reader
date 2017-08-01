package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	splunkEndpoint            = "/services/search/jobs/export?output_mode=json"
	defaultEarliestTime       = "-5m"
	queryTemplate             = `search index=heroku source="http:coco_up" sourcetype="heroku:drain" monitoring_event=true (environment="%s" OR environment="pub-%s") (content_type="%s" OR content_type="") | fields *`
	transactionsQueryTemplate = `search index=heroku source="http:coco_up" sourcetype="heroku:drain" monitoring_event=true (environment="%s" OR environment="pub-%s") (content_type="%s" OR content_type="") | fields * | transaction transaction_id endswith="event=PublishEnd" keepevicted=true`
)

// SplunkServiceI Splunk based event reader service
type SplunkServiceI interface {
	GetEvents(query monitoringQuery) ([]publishEvent, error)
	GetTransactions(query monitoringQuery) ([]transactionEvent, error)
	doQuery(queryString string) ([]splunkRow, error)
}

type splunkAccessConfig struct {
	user     string
	password string
	restURL  string
}

type splunkService struct {
	sync.RWMutex
	HTTPClient *http.Client
	Config     splunkAccessConfig
}

type monitoringQuery struct {
	Environment  string
	ContentType  string
	EarliestTime string
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

func (service *splunkService) GetEvents(query monitoringQuery) ([]publishEvent, error) {

	queryString := fmt.Sprintf(queryTemplate, query.Environment, query.Environment, query.ContentType)

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

	results := []publishEvent{}
	for _, row := range rows {
		result := splunkPublishEvent{}
		err = json.Unmarshal(*row.Result, &result)
		if err != nil {
			fmt.Print(err)
		}
		if err == nil {
			results = append(results, result.publishEvent)
		}

	}
	return results, nil
}

func (service *splunkService) GetTransactions(query monitoringQuery) ([]transactionEvent, error) {

	queryString := fmt.Sprintf(transactionsQueryTemplate, query.Environment, query.Environment, query.ContentType)

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
		err = json.Unmarshal(*row.Result, &splunkTransaction)
		if err != nil {
			fmt.Println(err)
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
					fmt.Print(err)
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
	return transactions, nil
}

func (service *splunkService) doQuery(query string) ([]splunkRow, error) {

	req, err := http.NewRequest("POST", service.Config.restURL+splunkEndpoint, strings.NewReader(query))
	req.SetBasicAuth(service.Config.user, service.Config.password)
	resp, err := service.HTTPClient.Do(req)
	if err != nil {
		return nil, err
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

func newSplunkService(config splunkAccessConfig) SplunkServiceI {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	return &splunkService{HTTPClient: client, Config: config}
}

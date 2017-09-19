# splunk-event-reader
_Should be the same as the github repo name but it isn't always._

[![Circle CI](https://circleci.com/gh/Financial-Times/splunk-event-reader/tree/master.png?style=shield)](https://circleci.com/gh/Financial-Times/splunk-event-reader/tree/master)[![Go Report Card](https://goreportcard.com/badge/github.com/Financial-Times/splunk-event-reader)](https://goreportcard.com/report/github.com/Financial-Times/splunk-event-reader) [![Coverage Status](https://coveralls.io/repos/github/Financial-Times/splunk-event-reader/badge.svg)](https://coveralls.io/github/Financial-Times/splunk-event-reader)

## Introduction

_What is this service and what is it for? What other services does it depend on_

Reads Splunk events via the Splunk REST API

## Installation
      
_How can I install it_

Download the source code, dependencies and test dependencies:

        go get -u github.com/kardianos/govendor
        go get -u github.com/Financial-Times/splunk-event-reader
        cd $GOPATH/src/github.com/Financial-Times/splunk-event-reader
        govendor sync
        go build .

## Running locally
_How can I run it_

1. Run the tests and install the binary:

        govendor sync
        govendor test -v -race
        go install

2. Run the binary (using the `help` flag to see the available optional arguments):

        $GOPATH/bin/splunk-event-reader [--help]

Options:

      --app-system-code="splunk-event-reader"   System Code of the application ($APP_SYSTEM_CODE)
      --app-name="Splunk Event Reader"          Application name ($APP_NAME)
      --port="8080"                             Port to listen on ($APP_PORT)
      --environment=""                          Name of the cluster ($ENVIRONMENT)
      --splunk-user=""                          Splunk user name ($SPLUNK_USER)
      --splunk-password=""                      Splunk password ($SPLUNK_PASSWORD)
      --splunk-url=""                           Splunk URL ($SPLUNK_URL)
        
3. Test:

    Using curl:

            curl http://localhost:8080/transactions | json_pp

## Build and deployment
_How can I build and deploy it (lots of this will be links out as the steps will be common)_

* Built by Docker Hub on merge to master: [coco/splunk-event-reader](https://hub.docker.com/r/coco/splunk-event-reader/)
* CI provided by CircleCI: [splunk-event-reader](https://circleci.com/gh/Financial-Times/splunk-event-reader)

## Service endpoints
_What are the endpoints offered by the service_

e.g.
### GET

`/{contentType}/transactions?[interval={relativeTime}][&uuid={uuid}]`

Returns a set of unclosed transactions in a given interval
* contentType - type of content processed in the transactions to be returned. Currently only `annotations` are supported.
* relativeTime - earliest time to search from, in minutes or seconds. Default is `10m`
* uuid - filter transactions by uuid; supports multiple values

Response example:
```
[{
    transaction_id: "tid_h3pfihmzqd",
    uuid: "919b15c0-f5a9-4288-89c1-2c0420529a7a",
    closed_txn: "0",
    duration: "6",
    eventcount: "7",
    events: 
    [
        {
        content_type: "",
        environment: "pub-xp",
        event: "Ingest",
        level: "info",
        monitoring_event: "true",
        msg: "Successfully ingested",
        platform: "up-coco",
        service_name: "native-ingester-metadata",
        @time: "2017-09-12T11:56:50.765463097Z",
        transaction_id: "tid_h3pfihmzqd",
        uuid: "919b15c0-f5a9-4288-89c1-2c0420529a7a"
        },
        {...}
    ]
},
{...}]
```

`/{contentType}/events?lastEvent=true&[]interval={relativeTime}]`

Returns the last `PublishEnd` event within the interval

* contentType - as above
* relativeTime - earliest time to search from, in minutes or seconds. If not specified, search is performed on all time (this can be costly if there is no such event in he index)

Response example:
```
{
    content_type: "Annotations",
    environment: "xp",
    event: "PublishEnd",
    isValid: "true",
    level: "info",
    monitoring_event: "true",
    msg: "Transaction has finished%!(EXTRA []interface {}=[])",
    platform: "up-coco",
    service_name: "annotations-monitoring-service",
    @time: "2017-09-13T08:27:34.051915987Z",
    transaction_id: "tid_gkfnwqwybl",
    uuid: "468b9400-97ff-11e7-a652-cde3f882dd7b"
}
```

## Healthchecks
Admin endpoints are:

`/__gtg`

`/__health`

`/__build-info`


These are the checks performed:

* Splunk availability check. This is actually cached for 1 minute based on the last Splunk API call result

## Other information
_Anything else you want to add._

_e.g. (NB: this example may be something we want to extract as it's probably common to a lot of services)_

### Logging

* The application uses [logrus](https://github.com/Sirupsen/logrus); the log file is initialised in [main.go](main.go).
* Logging requires an `env` app parameter, for all environments other than `local` logs are written to file.
* When running locally, logs are written to console. If you want to log locally to file, you need to pass in an env parameter that is != `local`.
* NOTE: `/__build-info` and `/__gtg` endpoints are not logged as they are called every second from varnish/vulcand and this information is not needed in logs/splunk.

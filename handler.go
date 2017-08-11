package main

import (
	"encoding/json"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/satori/go.uuid"
	"net/http"
	"regexp"
)

const (
	uuidPathVar            = "uuid"
	transactionIDPathVar   = "transactionId"
	intervalPathVar        = "interval"
	lastEventPathVar       = "lastEvent"
	contentTypePathVar     = "contentType"
	contentTypeAnnotations = "annotations"
)

var (
	contentTypes  = []string{contentTypeAnnotations}
	tidRegex      = regexp.MustCompile(`^[-_a-zA-Z0-9]+$`)
	intervalRegex = regexp.MustCompile(`^\d+[msh]$`)
)

type requestHandler struct {
	splunkService SplunkServiceI
}

func (handler *requestHandler) getTransactions(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	uuids := request.URL.Query()[uuidPathVar]
	interval := request.URL.Query().Get(intervalPathVar)

	if !isValidContentType(contentType) {
		logrus.Errorf("Invalid content type %s", contentType)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, uuid := range uuids {
		if !isValidUUID(uuid) {
			logrus.Errorf("Invalid UUID %s", uuid)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	if interval != "" && !isValidInterval(interval) {
		logrus.Errorf("Invalid interval %s", interval)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	query := monitoringQuery{ContentType: contentType, UUIDs: uuids}
	if interval != "" {
		query.EarliestTime = "-" + interval
	}
	transactions, err := handler.splunkService.GetTransactions(query)

	switch err {
	case nil:
		writer.WriteHeader(http.StatusOK)
		msg, _ := json.Marshal(transactions)
		writer.Write([]byte(msg))
	default:
		logrus.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func (handler *requestHandler) getTransactionsByID(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	id := mux.Vars(request)[transactionIDPathVar]
	interval := request.URL.Query().Get(intervalPathVar)

	if !isValidContentType(contentType) {
		logrus.Errorf("Invalid content type %s", contentType)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if id != "" && !isValidTransactionID(id) {
		logrus.Errorf("Invalid transaction id %s", id)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if interval != "" && !isValidInterval(interval) {
		logrus.Errorf("Invalid interval %s", interval)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	query := monitoringQuery{ContentType: contentType, TransactionID: id}
	if interval != "" {
		query.EarliestTime = "-" + interval
	}
	transactions, err := handler.splunkService.GetTransactions(query)

	switch err {
	case nil:
		if len(transactions) > 0 {
			writer.WriteHeader(http.StatusOK)
			msg, _ := json.Marshal(transactions[0])
			writer.Write([]byte(msg))
		} else {
			writer.WriteHeader(http.StatusNotFound)
		}
	default:
		logrus.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func (handler *requestHandler) getLastEvent(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	interval := request.URL.Query().Get(intervalPathVar)
	lastEvent := request.URL.Query().Get(lastEventPathVar)

	if !isValidContentType(contentType) {
		logrus.Errorf("Invalid content type %s", contentType)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if !isValidLastEventFlag(lastEvent) {
		logrus.Errorf("lastEvent param must be true for the /events endpoint, value is %s", lastEvent)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if interval != "" && !isValidInterval(interval) {
		logrus.Errorf("Invalid interval %s", interval)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	query := monitoringQuery{ContentType: contentType}
	if interval != "" {
		query.EarliestTime = "-" + interval
	}
	publishEvent, err := handler.splunkService.GetLastEvent(query)

	switch err {
	case nil:
		writer.WriteHeader(http.StatusOK)
		msg, _ := json.Marshal(*publishEvent)
		writer.Write([]byte(msg))
	case NoResultsError:
		writer.WriteHeader(http.StatusNotFound)
	default:
		logrus.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func isValidLastEventFlag(lastEvent string) bool {
	return lastEvent == "true"
}

func isValidContentType(contentType string) bool {
	for _, ct := range contentTypes {
		if contentType == ct {
			return true
		}
	}
	return false
}

func isValidTransactionID(transactionID string) bool {
	return tidRegex.MatchString(transactionID)
}

func isValidInterval(interval string) bool {
	return intervalRegex.MatchString(interval)
}

func isValidUUID(id string) bool {
	_, err := uuid.FromString(id)
	return err == nil
}

package main

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/satori/go.uuid"
)

const (
	uuidPathVar            = "uuid"
	earliestTimePathVar    = "earliestTime"
	latestTimePathVar      = "latestTime"
	lastEventPathVar       = "lastEvent"
	contentTypePathVar     = "contentType"
	contentTypeAnnotations = "annotations"
)

var (
	contentTypes    = []string{contentTypeAnnotations}
	timePeriodRegex = regexp.MustCompile(`^-\d+[msh]$`)
)

type requestHandler struct {
	splunkService SplunkServiceI
}

func (handler *requestHandler) getTransactions(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	uuids := request.URL.Query()[uuidPathVar]
	earliestTime := request.URL.Query().Get(earliestTimePathVar)
	latestTime := request.URL.Query().Get(latestTimePathVar)

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

	if earliestTime != "" && !isValidTimePeriod(earliestTime) {
		logrus.Errorf("Invalid earliest time parameter %s", earliestTime)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if latestTime != "" && !isValidTimePeriod(latestTime) {
		logrus.Errorf("Invalid latest time parameter %s", latestTime)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	query := monitoringQuery{ContentType: contentType, UUIDs: uuids}
	if earliestTime != "" {
		query.EarliestTime = earliestTime
	}
	if latestTime != "" {
		query.LatestTime = latestTime
	}
	transactions, err := handler.splunkService.GetTransactions(query)

	switch err {
	case nil:
		writer.WriteHeader(http.StatusOK)
		msg, err := json.Marshal(transactions)
		if err != nil {
			logrus.Error(err)
			writer.WriteHeader(http.StatusInternalServerError)
		} else {
			writer.Write([]byte(msg))
		}
	default:
		logrus.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func (handler *requestHandler) getLastEvent(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	earliestTime := request.URL.Query().Get(earliestTimePathVar)
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

	if earliestTime != "" && !isValidTimePeriod(earliestTime) {
		logrus.Errorf("Invalid interval %s", earliestTime)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	query := monitoringQuery{ContentType: contentType}
	if earliestTime != "" {
		query.EarliestTime = earliestTime
	}
	publishEvent, err := handler.splunkService.GetLastEvent(query)

	switch err {
	case nil:
		writer.WriteHeader(http.StatusOK)
		msg, err := json.Marshal(*publishEvent)
		if err != nil {
			logrus.Error(err)
			writer.WriteHeader(http.StatusInternalServerError)
		} else {
			writer.Write([]byte(msg))
		}
	case ErrNoResults:
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

func isValidTimePeriod(interval string) bool {
	return timePeriodRegex.MatchString(interval)
}

func isValidUUID(id string) bool {
	_, err := uuid.FromString(id)
	return err == nil
}

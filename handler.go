package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/Financial-Times/go-logger/v2"
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
	log           *logger.UPPLogger
}

func (handler *requestHandler) getTransactions(writer http.ResponseWriter, request *http.Request) {

	log := handler.log

	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	uuids := request.URL.Query()[uuidPathVar]
	earliestTime := request.URL.Query().Get(earliestTimePathVar)
	latestTime := request.URL.Query().Get(latestTimePathVar)

	if !isValidContentType(contentType) {
		log.Errorf("Invalid content type %s", contentType)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, uuid := range uuids {
		if !isValidUUID(uuid) {
			log.Errorf("Invalid UUID %s", uuid)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	if earliestTime != "" && !isValidTimePeriod(earliestTime) {
		log.Errorf("Invalid earliest time parameter %s", earliestTime)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if latestTime != "" && !isValidTimePeriod(latestTime) {
		log.Errorf("Invalid latest time parameter %s", latestTime)
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

	if err != nil {
		log.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	msg, err := json.Marshal(transactions)
	if err != nil {
		log.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err = writer.Write([]byte(msg)); err != nil {
		log.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

}

func (handler *requestHandler) getLastEvent(writer http.ResponseWriter, request *http.Request) {

	log := handler.log

	defer request.Body.Close()

	contentType := mux.Vars(request)[contentTypePathVar]
	earliestTime := request.URL.Query().Get(earliestTimePathVar)
	lastEvent := request.URL.Query().Get(lastEventPathVar)

	if !isValidContentType(contentType) {
		log.Errorf("Invalid content type %s", contentType)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if !isValidLastEventFlag(lastEvent) {
		log.Errorf("lastEvent param must be true for the /events endpoint, value is %s", lastEvent)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	if earliestTime != "" && !isValidTimePeriod(earliestTime) {
		log.Errorf("Invalid interval %s", earliestTime)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	query := monitoringQuery{ContentType: contentType}
	if earliestTime != "" {
		query.EarliestTime = earliestTime
	}
	publishEvent, err := handler.splunkService.GetLastEvent(query)

	if err != nil {
		if errors.Is(err, ErrNoResults) {
			writer.WriteHeader(http.StatusNotFound)
		} else {
			log.Error(err)
			writer.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	msg, err := json.Marshal(*publishEvent)
	if err != nil {
		log.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err = writer.Write([]byte(msg)); err != nil {
		log.Error(err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
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
	_, err := uuid.Parse(id)
	return err == nil
}

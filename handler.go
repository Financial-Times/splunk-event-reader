package main

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"net/http"
)

type requestHandler struct {
	splunkService SplunkServiceI
}

func (handler *requestHandler) getTransactions(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)["contentType"]
	uuids := request.URL.Query()["uuid"]
	transactions, err := handler.splunkService.GetTransactions(monitoringQuery{EarliestTime: "-5m", ContentType: contentType, UUIDs: uuids})

	switch err {
	case nil:
		writer.WriteHeader(http.StatusOK)
		msg, _ := json.Marshal(transactions)
		writer.Write([]byte(msg))
	default:
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func (handler *requestHandler) getTransactionsByID(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	contentType := mux.Vars(request)["contentType"]
	id := mux.Vars(request)["transactionId"]
	transactions, err := handler.splunkService.GetTransactions(monitoringQuery{EarliestTime: "-5m", ContentType: contentType, TransactionID: id})

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
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

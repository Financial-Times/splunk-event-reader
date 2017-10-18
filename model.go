package main

type publishEvent struct {
	ContentType     string `json:"content_type"`
	Event           string `json:"event"`
	IsValid         string `json:"isValid,omitempty"`
	Level           string `json:"level"`
	MonitoringEvent string `json:"monitoring_event"`
	ServiceName     string `json:"service_name"`
	Time            string `json:"@time"`
	TransactionID   string `json:"transaction_id"`
	UUID            string `json:"uuid"`
}

type transactionEvent struct {
	TransactionID string         `json:"transaction_id"`
	UUID          string         `json:"uuid"`
	ClosedTxn     string         `json:"closed_txn"`
	EventCount    int            `json:"eventcount"`
	Events        []publishEvent `json:"events"`
	StartTime     string         `json:"start_time"`
}

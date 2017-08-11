package main

type publishEvent struct {
	ContentType     string `json:"content_type"`
	Environment     string `json:"environment"`
	Event           string `json:"event"`
	IsValid         string `json:"isValid,omitempty"`
	Level           string `json:"level"`
	MonitoringEvent string `json:"monitoring_event"`
	Msg             string `json:"msg"`
	Platform        string `json:"platform"`
	ServiceName     string `json:"service_name"`
	Time            string `json:"@time"`
	TransactionID   string `json:"transaction_id"`
	UUID            string `json:"uuid"`
}

type transactionEvent struct {
	TransactionID string         `json:"transaction_id"`
	UUID          string         `json:"uuid"`
	ClosedTxn     string         `json:"closed_txn"`
	Duration      string         `json:"duration"`
	EventCount    string         `json:"eventcount"`
	Events        []publishEvent `json:"events"`
}

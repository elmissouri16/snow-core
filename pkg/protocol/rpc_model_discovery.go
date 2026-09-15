package protocol

// RPCModelDiscovery is the bounded, on-demand cross-provider catalog returned
// by models_discover. Partial reports unavailable catalogs, including canceled
// discovery; Truncated reports omitted models, bounded metadata, or exhaustion
// of the 2 MiB encoded-result budget. Discovery
// never changes the selected provider or model and exposes no auth inventory.
type RPCModelDiscovery struct {
	Models    []Model `json:"models"`
	Partial   bool    `json:"partial"`
	Truncated bool    `json:"truncated"`
}

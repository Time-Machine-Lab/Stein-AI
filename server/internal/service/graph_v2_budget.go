package service

import "encoding/json"

// One MiB is reserved for stop acknowledgements, terminal transitions and recent
// events. Payload admission never makes recovery writes impossible.
const MaxGraphRunDocumentBytes = 16 << 20
const graphRunPayloadLimit = MaxGraphRunDocumentBytes - (1 << 20)

func graphPayloadFitsV2(run GraphRunRecord, additionalBytes int) bool {
	if run.SchemaVersion != 2 {
		return true
	}
	copy := run
	if len(copy.Events) > 500 {
		copy.Events = copy.Events[len(copy.Events)-500:]
	}
	raw, err := json.Marshal(copy)
	return err == nil && len(raw)+additionalBytes <= graphRunPayloadLimit
}

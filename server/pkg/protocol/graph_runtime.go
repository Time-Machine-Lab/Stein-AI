package protocol

// GraphRuntimeSnapshot pins runtime selection and custom command semantics,
// excluding a machine-specific executable path and all live credentials.
type GraphRuntimeSnapshot struct {
	RuntimeID   string   `json:"runtime_id"`
	Provider    string   `json:"provider"`
	ProfileID   string   `json:"profile_id,omitempty"`
	CommandName string   `json:"command_name,omitempty"`
	FixedArgs   []string `json:"fixed_args,omitempty"`
}

const DaemonCapabilityGraphExecutionV2 = "graph-execution-v2"
const DaemonCapabilityGraphFileDeliveryV1 = "graph-file-delivery-v1"

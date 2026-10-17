package config

// Configuration struct to hold all service connection parameters
type Config struct {
	LLMInterfaceHost     string
	LLMInterfacePort     int
	DataAcquisitionHost  string
	DataAcquisitionPort  int
	SafetyPolicyHost     string
	SafetyPolicyPort     int
	ScalingExecutorHost  string
	ScalingExecutorPort  int
	MultiAgentReasoningHost string
	MultiAgentReasoningPort int
}
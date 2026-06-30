package subscription

// CandidateConfig is a runnable sing-box configuration candidate produced
// from one subscription source and consumed by the runner and selector.
type CandidateConfig struct {
	Name   string
	Source string
	Raw    []byte
	Parsed map[string]any
}

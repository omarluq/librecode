package workflow

const pipelineNotScheduled = "pipeline stopped before item was scheduled"

// PipelineResult is one callback outcome. Results retain input order.
type PipelineResult struct {
	Value any
	Error string
	Index int
}

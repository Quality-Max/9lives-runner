package runner

import "context"

// Store is the persistence boundary used by the reusable core. A future MCP
// transport or durable worker can provide another implementation without
// changing scheduling or adapter behavior.
type Store interface {
	Initialize(Plan) error
	AppendEvent(ProgressEvent) error
	PersistAttempt(Receipt, []byte, []byte) (Receipt, error)
	Finalize(RunSummary) error
	CancellationRequested() bool
}

// ProcessExecutor is the controlled command-execution boundary. Ordinary test
// runs use the local implementation; contract tests and future remote workers
// can supply a deterministic implementation.
type ProcessExecutor interface {
	Run(context.Context, Job, int) ProcessOutput
}

type ProcessOutput struct {
	Executed        bool
	ExitCode        int
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
	Err             error
	Termination     *Termination
}

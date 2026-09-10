package runner

type Adapter interface {
	Name() string
	Supports(path string) bool
	Plan(path, input string, index int) (Job, error)
	Validate(stdout []byte) (Validation, error)
}

type Validation struct {
	FailureCount       int
	ExecutedTests      int
	VerifiedAssertions int
	AssertionCoverage  string
	Description        string
}

func adapterFor(adapters []Adapter, path string) Adapter {
	for _, candidate := range adapters {
		if candidate.Supports(path) {
			return candidate
		}
	}
	return nil
}

func adapterNamed(adapters []Adapter, name string) Adapter {
	for _, candidate := range adapters {
		if candidate.Name() == name {
			return candidate
		}
	}
	return nil
}

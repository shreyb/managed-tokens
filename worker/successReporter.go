package worker

type SuccessReporter interface {
	GetServiceName() string
	GetSuccess() bool
}

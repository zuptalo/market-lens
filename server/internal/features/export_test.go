package features

// OverrideComputeForTest replaces a named definition's compute function until the returned
// function is called. Tests use it to inject a failure into one instrument's computation.
func OverrideComputeForTest(name string, compute func(Definition, Input) Result) (restore func()) {
	previous := specs[name]
	replaced := previous
	replaced.compute = compute
	specs[name] = replaced
	return func() { specs[name] = previous }
}

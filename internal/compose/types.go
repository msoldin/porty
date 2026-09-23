package compose

type Request struct {
	StackDir    string
	ProjectName string
	Environment map[string]string
}

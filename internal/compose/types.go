package compose

type Request struct {
	ImageOverrides map[string]string
	ImagePlatforms map[string]string
	StackDir       string
	ProjectName    string
	Environment    map[string]string
}

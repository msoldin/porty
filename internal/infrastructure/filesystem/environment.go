package filesystem

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrInvalidEnvironment = errors.New("invalid environment value")
	environmentKey        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func SerializeEnvironment(values map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if !environmentKey.MatchString(key) || strings.ContainsRune(value, 0) {
			return nil, ErrInvalidEnvironment
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result strings.Builder
	for _, key := range keys {
		value := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(values[key])
		result.WriteString(key)
		result.WriteString("=\"")
		result.WriteString(value)
		result.WriteString("\"\n")
	}
	return []byte(result.String()), nil
}

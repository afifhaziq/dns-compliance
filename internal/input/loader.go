package input

import (
	"bufio"
	"os"
	"strings"
)

// Load reads URLs from an optional file and optional CLI args, returning a
// deduplicated slice in order (file entries first, then args).
// Lines starting with # and blank lines in the file are ignored.
func Load(filePath string, args []string) ([]string, error) {
	seen := make(map[string]struct{})
	var urls []string

	add := func(raw string) {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			return
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			urls = append(urls, s)
		}
	}

	if filePath != "" {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			add(sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}

	for _, a := range args {
		add(a)
	}

	return urls, nil
}

package projectmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Inspect object keys before decoding into maps can discard declarations.
func rejectDuplicateModelKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var problems []string
	var visit func(string) error
	visit = func(context string) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key := keyToken.(string)
				if seen[key] && (context == "root" && (key == "resources" || key == "projectId") || context == "root.resources" && key == "models" || context == "root.resources.models") {
					problems = append(problems, fmt.Sprintf("%s has duplicate key %q", context, key))
				}
				seen[key] = true
				if err := visit(context + "." + key); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := visit(context + "[]"); err != nil {
					return err
				}
			}
		}
		_, err = decoder.Token()
		return err
	}
	if err := visit("root"); err != nil {
		return &DecodeError{Err: err}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return &ValidationError{Problems: problems}
	}
	return nil
}

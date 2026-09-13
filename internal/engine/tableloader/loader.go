package tableloader

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"parametron/internal/engine/table"
)

// LoadFiles loads planner-visible tables from logical IDs to JSON file paths.
// Returned tables are keyed by the exact logical IDs supplied by the caller.
func LoadFiles(inputs map[string]string) (map[string]*table.Table, error) {
	if len(inputs) == 0 {
		return map[string]*table.Table{}, nil
	}

	ids := make([]string, 0, len(inputs))
	for id, path := range inputs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("table logical ID must not be empty")
		}
		if id != strings.TrimSpace(id) {
			return nil, fmt.Errorf("table logical ID %q must not have leading or trailing whitespace", id)
		}
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("table %q path must not be empty", id)
		}
		if path != strings.TrimSpace(path) {
			return nil, fmt.Errorf("table %q path must not have leading or trailing whitespace", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	loaded := make(map[string]*table.Table, len(inputs))
	for _, id := range ids {
		path := inputs[id]
		loadedTable, err := table.LoadFile(path)
		if err != nil {
			if errors.Is(err, table.ErrIO) {
				return nil, fmt.Errorf("load table %q from %q: %s: %w", id, path, table.ErrIO, err)
			}
			return nil, fmt.Errorf("load table %q from %q: %w", id, path, err)
		}
		loaded[id] = loadedTable
	}

	return loaded, nil
}

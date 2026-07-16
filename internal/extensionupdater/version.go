package extensionupdater

import (
	"fmt"
	"strconv"
	"strings"
)

type numericVersion [4]uint16

func parseVersion(value string) (numericVersion, error) {
	var parsed numericVersion
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) == 0 || len(parts) > len(parsed) {
		return parsed, fmt.Errorf("invalid version %q", value)
	}
	for index, part := range parts {
		if part == "" {
			return parsed, fmt.Errorf("invalid version %q", value)
		}
		number, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return parsed, fmt.Errorf("invalid version %q: %w", value, err)
		}
		parsed[index] = uint16(number)
	}
	return parsed, nil
}

func CompareVersions(left, right string) (int, error) {
	a, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseVersion(right)
	if err != nil {
		return 0, err
	}
	for index := range a {
		if a[index] < b[index] {
			return -1, nil
		}
		if a[index] > b[index] {
			return 1, nil
		}
	}
	return 0, nil
}

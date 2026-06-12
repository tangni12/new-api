package constant

import (
	"fmt"
	"strconv"
	"strings"
)

func SetErrorLogSkipStatusCodes(input string) error {
	codes, err := parseErrorLogSkipStatusCodes(input)
	if err != nil {
		return err
	}
	ErrorLogSkipStatusCodes = codes
	return nil
}

func ShouldSkipErrorLogStatusCode(code int) bool {
	if ErrorLogSkipStatusCodes == nil {
		return false
	}
	_, ok := ErrorLogSkipStatusCodes[code]
	return ok
}

func parseErrorLogSkipStatusCodes(input string) (map[int]struct{}, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	codes := make(map[int]struct{})
	var invalid []string
	for _, item := range strings.Split(input, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		code, err := strconv.Atoi(item)
		if err != nil || code < 100 || code > 599 {
			invalid = append(invalid, item)
			continue
		}
		codes[code] = struct{}{}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid error log skip status codes: %s", strings.Join(invalid, ", "))
	}
	if len(codes) == 0 {
		return nil, nil
	}
	return codes, nil
}

// Package download normalizes the supported HTTP byte-range contract.
package download

import (
	"errors"
	"strconv"
	"strings"
)

const maxValidatorBytes = 128

// ErrInvalidRange indicates a malformed, unsupported, or unsatisfiable Range.
var ErrInvalidRange = errors.New("invalid byte range")

// ByteRange is one inclusive interval within the current object.
type ByteRange struct {
	Start int64
	End   int64
}

// Selection is the full or partial interval to deliver.
type Selection struct {
	Offset  int64
	Length  int64
	Partial bool
	Range   *ByteRange
}

// Select applies Range and If-Range to the current size and strong ETag.
func Select(rangeValues, ifRangeValues []string, size int64, currentETag string) (Selection, error) {
	full := Selection{Offset: 0, Length: size}
	if size < 0 {
		return Selection{}, ErrInvalidRange
	}
	if len(rangeValues) == 0 {
		return full, nil
	}
	if len(rangeValues) != 1 {
		return Selection{}, ErrInvalidRange
	}
	if !ifRangePermits(ifRangeValues, currentETag) {
		return full, nil
	}

	interval, err := parseRange(rangeValues[0], size)
	if err != nil {
		return Selection{}, err
	}
	return Selection{
		Offset:  interval.Start,
		Length:  interval.End - interval.Start + 1,
		Partial: true,
		Range:   &interval,
	}, nil
}

func ifRangePermits(values []string, currentETag string) bool {
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 || len(values[0]) > maxValidatorBytes ||
		!isStrongETag(values[0]) {
		return false
	}
	return values[0] == currentETag
}

func isStrongETag(value string) bool {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' ||
		strings.HasPrefix(value, "W/") {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if character == '"' || character < 0x21 || character == 0x7f {
			return false
		}
	}
	return true
}

func parseRange(header string, size int64) (ByteRange, error) {
	if len(header) == 0 || len(header) > maxValidatorBytes || size <= 0 ||
		!strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return ByteRange{}, ErrInvalidRange
	}
	specification := strings.TrimPrefix(header, "bytes=")
	if strings.Count(specification, "-") != 1 {
		return ByteRange{}, ErrInvalidRange
	}
	parts := strings.SplitN(specification, "-", 2)
	if parts[0] == "" && parts[1] == "" {
		return ByteRange{}, ErrInvalidRange
	}
	if parts[0] == "" {
		suffix, err := parseDecimal(parts[1])
		if err != nil || suffix <= 0 {
			return ByteRange{}, ErrInvalidRange
		}
		start := size - suffix
		if start < 0 {
			start = 0
		}
		return ByteRange{Start: start, End: size - 1}, nil
	}

	start, err := parseDecimal(parts[0])
	if err != nil || start >= size {
		return ByteRange{}, ErrInvalidRange
	}
	end := size - 1
	if parts[1] != "" {
		end, err = parseDecimal(parts[1])
		if err != nil || end < start {
			return ByteRange{}, ErrInvalidRange
		}
		if end >= size {
			end = size - 1
		}
	}
	return ByteRange{Start: start, End: end}, nil
}

func parseDecimal(value string) (int64, error) {
	parsed, err := strconv.ParseUint(value, 10, 63)
	if err != nil {
		return 0, ErrInvalidRange
	}
	return int64(parsed), nil
}

package script

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

func (f Flag) Parse(text string) (any, error) {
	switch f.Type {
	case PairFlag:
		name, value, split := strings.Cut(text, "=")
		switch {
		case !split:
			return nil, errors.New("it holds no =: a pair is written as its name, an = and the value, as in Type=Task")
		case name == "":
			return nil, errors.New("it names nothing before the =")
		}
		return pair{name: name, value: value}, nil
	case DurationFlag:
		return durationMinutes(text)
	}
	if len(f.Choices) > 0 && !slices.Contains(f.Choices, text) {
		return nil, errors.New("it is none of " + strings.Join(f.Choices, ", "))
	}
	return text, nil
}

func (f Flag) Value(parsed []any) any {
	switch {
	case f.Type == PairFlag:
		pairs := make([]pair, 0, len(parsed))
		for _, value := range parsed {
			pairs = append(pairs, value.(pair))
		}
		return pairs
	case f.Multiple:
		texts := make([]string, 0, len(parsed))
		for _, value := range parsed {
			texts = append(texts, value.(string))
		}
		return texts
	}
	return parsed[len(parsed)-1]
}

type pair struct {
	name  string
	value string
}

// A work item is whole minutes, and time.Duration holds fewer of them than the int32 YouTrack keeps.
const longestWorkItem = math.MaxInt64 / int64(time.Minute)

func durationMinutes(text string) (int, error) {
	minutes, read := periodMinutes(text)
	switch {
	case !read:
		return 0, errors.New("it is no ISO 8601 period of hours and minutes, as in PT1H30M, PT90M or PT0M: a work " +
			"item is the minutes it comes to, and neither a day nor a week is a fixed count of them — YouTrack " +
			"reads P1D as the working day of the instance — while a second and a fraction are no part of it")
	case minutes > longestWorkItem:
		return 0, fmt.Errorf("it is longer than the %d minutes a work item may be written for", longestWorkItem)
	}
	return int(minutes), nil
}

func periodMinutes(text string) (int64, bool) {
	rest, isPeriod := strings.CutPrefix(text, "PT")
	if !isPeriod {
		return 0, false
	}
	hours, rest, hoursGiven, read := countBefore(rest, 'H')
	if !read {
		return 0, false
	}
	minutes, rest, minutesGiven, read := countBefore(rest, 'M')
	if !read || rest != "" || (!hoursGiven && !minutesGiven) {
		return 0, false
	}
	return min(hours*60, pastLongest) + minutes, true
}

const pastLongest = longestWorkItem + 1

func countBefore(text string, mark byte) (count int64, rest string, given, read bool) {
	before, after, marked := strings.Cut(text, string(mark))
	if !marked {
		return 0, text, false, true
	}
	if before == "" {
		return 0, "", false, false
	}
	for _, digit := range []byte(before) {
		if digit < '0' || digit > '9' {
			return 0, "", false, false
		}
		count = min(count*10+int64(digit-'0'), pastLongest)
	}
	return count, after, true, true
}

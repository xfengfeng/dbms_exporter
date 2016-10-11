package common

import (
	"fmt"
	"regexp"
)

// ColumnUsage is an enum type differentiating different column handling behaviours.
type ColumnUsage int

const (
	_                        = iota
	DISCARD      ColumnUsage = iota // Ignore this column
	LABEL        ColumnUsage = iota // Use this column as a label
	COUNTER      ColumnUsage = iota // Use this column as a counter
	GAUGE        ColumnUsage = iota // Use this column as a gauge
	MAPPEDMETRIC ColumnUsage = iota // Use this column with the supplied mapping of text values
	DURATION     ColumnUsage = iota // This column should be interpreted as a text duration (and converted to milliseconds)
	FIXED        ColumnUsage = iota // This is not a column but rather a constant label that should be added to the metrics
)

// ColumnMapping defines how to build metrics from a given DB column.  Recipes
// map column names in resultsets to a ColumnMapping which describes how to
// transform the values into metrics.
type ColumnMapping struct {
	Usage       ColumnUsage
	Description string
	Mapping     map[string]float64 // Optional column mapping for MAPPEDMETRIC
	Regexp      *regexp.Regexp
	Fixedval    string
}

// StringToColumnUsage converts a string to the corresponding ColumnUsage.
func StringToColumnUsage(s string) (u ColumnUsage, err error) {
	switch s {
	case "DISCARD":
		u = DISCARD
	case "LABEL":
		u = LABEL
	case "COUNTER":
		u = COUNTER
	case "GAUGE":
		u = GAUGE
	case "MAPPEDMETRIC":
		u = MAPPEDMETRIC
	case "DURATION":
		u = DURATION
	case "FIXED":
		u = FIXED
	default:
		err = fmt.Errorf("wrong ColumnUsage given : %s", s)
	}

	return
}

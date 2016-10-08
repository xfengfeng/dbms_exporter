package common

import (
	"fmt"
)

type ColumnUsage int

const (
	_                        = iota
	DISCARD      ColumnUsage = iota // Ignore this column
	LABEL        ColumnUsage = iota // Use this column as a label
	COUNTER      ColumnUsage = iota // Use this column as a counter
	GAUGE        ColumnUsage = iota // Use this column as a gauge
	MAPPEDMETRIC ColumnUsage = iota // Use this column with the supplied mapping of text values
	DURATION     ColumnUsage = iota // This column should be interpreted as a text duration (and converted to milliseconds)
)

// User-friendly representation of a prometheus descriptor map
type ColumnMapping struct {
	Usage       ColumnUsage
	Description string
	Mapping     map[string]float64 // Optional column mapping for MAPPEDMETRIC
}

// convert a string to the corresponding ColumnUsage
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

	default:
		err = fmt.Errorf("wrong ColumnUsage given : %s", s)
	}

	return
}

type MetricQueryRecipe struct {
	// Basename is typically the name of a table to query
	Basename string
	// SqlQuery is what should be executed
	SqlQuery string
	// ResultMap maps column names in the resultset to the ColumnMapping that should be used to build a metric
	ResultMap map[string]ColumnMapping
}

package recipes

import (
	"bytes"
	"fmt"
	"os"
	"text/template"

	"github.com/ncabatoff/dbms_exporter/common"
	"github.com/ncabatoff/dbms_exporter/db"
	"github.com/prometheus/common/log"
)

// ResultMap describes how to handle a single resultset, i.e. how each column
// should be interpreted in terms of metrics.
type ResultMap map[string]common.ColumnMapping

// NamedResultMap associates a name with a ResultMap based on configuration.
type NamedResultMap struct {
	ResultMap
	Name string
}

// ShouldSkip returns true if this resultset should be ignored.
func (nrm NamedResultMap) ShouldSkip() bool {
	return len(nrm.ResultMap) == 0
}

// MultiResultMap describes how to handle a list of resultsets, as can be
// returned by some queries in DBs like Sybase and SQL Server.  Resultset don't
// intrically have names, just positions, but we're imposing a name so that we
// can craft metrics based on it.
type MultiResultMap []NamedResultMap

// MetricQueryRecipe is a collection of rules that specify how to produce a set
// of related metrics given a DB connection.  In other words, it knows how to
// issue query(s) and interpret the results.
type MetricQueryRecipe interface {
	// Return the basename associated with this recipe; all metrics yielded
	// will be prefixed with this
	GetNamespace() string
	// Returns a map to be used in interpreting the result of Run(): each
	// column in the resultset should be looked up in this map to determine
	// how to handle it.
	GetResultMaps() MultiResultMap
	// Run executes one or more queries and returns one or more resultsets.
	// There need not be a one-to-one mapping.
	Run(db.Conn) ([]db.ScannedResultSet, error)
}

// MetricQueryRecipeBase is common to all recipes.
type MetricQueryRecipeBase struct {
	// Namespace is typically the name of a table to query, and it provides
	// the base name of derived metrics (after the driver prefix).
	Namespace string
	// ResultMaps maps column names in resultsets to the ColumnMapping
	// that should be used to build a metric.
	Resultmaps MultiResultMap
}

// GetNamespace implements MetricQueryRecipe.
func (mqrb *MetricQueryRecipeBase) GetNamespace() string {
	return mqrb.Namespace
}

// GetResultMaps implements MetricQueryRecipe.
func (mqrb *MetricQueryRecipeBase) GetResultMaps() MultiResultMap {
	return mqrb.Resultmaps
}

type MetricQueryRecipeSimple struct {
	*MetricQueryRecipeBase
	// sqlquery is what should be executed
	Queries []string
}

func (mqrs *MetricQueryRecipeSimple) Run(conn db.Conn) ([]db.ScannedResultSet, error) {
	srss, err := mqrs.runQueries(conn)
	if err != nil {
		return nil, err
	}
	if len(srss) != len(mqrs.Resultmaps) {
		return nil, fmt.Errorf("Query for %q yielded %d resultsets and I wanted %d", mqrs.Namespace, len(srss), len(mqrs.Resultmaps))
	}

	return srss, err
}

func (mqrs *MetricQueryRecipeSimple) runQueries(conn db.Conn) ([]db.ScannedResultSet, error) {
	var accsrs = make([]db.ScannedResultSet, 0, len(mqrs.Resultmaps))
	for _, sql := range mqrs.Queries {
		log.Debugln("running SQL: ", sql)
		srss, err := conn.Query(sql)
		if err != nil {
			return nil, fmt.Errorf("Error running query <%s> on database: %v", sql, err)
		}

		accsrs = append(accsrs, srss...)
	}
	return accsrs, nil
}

type MetricQueryRecipeTemplated struct {
	*MetricQueryRecipeBase
	// Rangequery returns a single-column resultset over which to iterate
	Rangequery string
	// All templates are executed in the context of a range over the rangequery results.
	// Each resulting string is executed as an SQL query, and the resulting resultsets
	// are returned by the ResultSet (after filtering out empty resultsets.)
	Queries []*template.Template
}

// getRange returns the list of strings to iterate over based on the results
// of the rangeover query.  The query should yield a one-column resultset,
// the rows of which are returned as a list of strings produced by db.ToString().
func (mqrt *MetricQueryRecipeTemplated) getRange(conn db.Conn) ([]string, error) {
	srss, err := conn.Query(mqrt.Rangequery)
	if err != nil {
		return nil, err
	}
	if len(srss) != 1 {
		return nil, fmt.Errorf("rangeover query yielded %d resultsets rather than 1", len(srss))
	}

	srs := srss[0]
	if len(srs.Colnames) != 1 {
		return nil, fmt.Errorf("rangeover query yielded resultset with other than exactly 1 columns: %v", srs.Colnames)
	}
	if len(srs.Rows) < 1 {
		return nil, fmt.Errorf("rangeover query yielded resultset with no rows")
	}

	itover := make([]string, len(srs.Rows))
	for i, row := range srs.Rows {
		sval, ok := db.ToString(row[0])
		if !ok {
			return nil, fmt.Errorf("rangeover query returned a value I don't know how to handle: %v", sval)
		}
		itover[i] = sval
	}
	return itover, nil
}

func (mqrt *MetricQueryRecipeTemplated) Run(conn db.Conn) ([]db.ScannedResultSet, error) {
	itover, err := mqrt.getRange(conn)
	if err != nil {
		return nil, err
	}
	return mqrt.runQueries(conn, itover)
}

func (mqrt *MetricQueryRecipeTemplated) runQueries(conn db.Conn, itover []string) ([]db.ScannedResultSet, error) {
	var buf bytes.Buffer
	var accsrs = make([]db.ScannedResultSet, len(mqrt.Resultmaps))
	for _, it := range itover {
		for _, querytmpl := range mqrt.Queries {
			err := querytmpl.Execute(&buf, it)
			if err != nil {
				return nil, err
			}
			sql := buf.String()
			log.Debugln("running SQL: ", sql)
			srss, err := conn.Query(sql)
			if err != nil {
				return nil, fmt.Errorf("Error running query <%s> on database: %v", sql, err)
			}

			for i, srs := range srss {
				accsrs[i].Colnames = append(srs.Colnames, it)
				for _, row := range srs.Rows {
					row = append(row, it)
					accsrs[i].Rows = append(accsrs[i].Rows, row)
				}
			}

			buf.Reset()
		}
	}
	return accsrs, nil
}

func DumpMaps(recipes []MetricQueryRecipe) {
	for _, recipe := range recipes {
		fmt.Println(recipe.GetNamespace())
		fmt.Println("  queries:")
		if r, ok := recipe.(*MetricQueryRecipeSimple); ok {
			for _, sql := range r.Queries {
				fmt.Printf("    %s\n", sql)
			}
		} else if r, ok := recipe.(*MetricQueryRecipeTemplated); ok {
			fmt.Printf("    %s\n", r.Rangequery)
			for _, tmpl := range r.Queries {
				fmt.Printf("    ")
				tmpl.Execute(os.Stdout, "{{.}}")
				fmt.Println()
			}
		}

		fmt.Printf("  resultsets:\n")
		for _, rm := range recipe.GetResultMaps() {
			fmt.Printf("    %s:\n", rm.Name)
			for column, details := range rm.ResultMap {
				fmt.Printf("      %-40s %v\n", column, details)
			}
		}
		fmt.Println()
	}
}

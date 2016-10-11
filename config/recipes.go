package config

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/ncabatoff/dbms_exporter/common"
	"github.com/ncabatoff/dbms_exporter/recipes"
	"gopkg.in/yaml.v2"
)

// ReadRecipesFile opens the named file and extracts recipes from it.  All
// resulting metrics will be prefixed by prefix_.
func ReadRecipesFile(queriesPath, prefix string) ([]recipes.MetricQueryRecipe, error) {
	content, err := ioutil.ReadFile(queriesPath)
	if err != nil {
		return nil, err
	}
	return GetRecipes(prefix, string(content))
}

// GetRecipes extracts recipes from content.  All resulting metrics will be
// prefixed by prefix_.
func GetRecipes(prefix, content string) ([]recipes.MetricQueryRecipe, error) {
	var yamldata map[string]interface{}

	err := yaml.Unmarshal([]byte(content), &yamldata)
	if err != nil {
		return nil, err
	}

	var recipes []recipes.MetricQueryRecipe
	for basename, specs := range yamldata {
		recipe, err := getRecipe(prefix, basename, specs)
		if err != nil {
			return nil, fmt.Errorf("unable to parse recipe %q: %s", basename, err)
		}
		recipes = append(recipes, recipe)
	}

	return recipes, nil
}

func getRecipe(prefix, namespace string, specs interface{}) (recipes.MetricQueryRecipe, error) {
	var ok bool
	yamlRecipe, ok := specs.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("bad recipe: not a map")
	}

	var query string
	var queries []string
	var rangeover string
	var resultmaps recipes.MultiResultMap
	var resultmap recipes.ResultMap

	for ikey, ivalue := range yamlRecipe {
		key, ok := ikey.(string)
		if !ok {
			return nil, fmt.Errorf("key %v is not a string", ikey)
		}

		switch key {
		case "rangeover":
			rangeover, ok = ivalue.(string)
			if !ok {
				return nil, fmt.Errorf("rangeover %v is not a string", ivalue)
			}

		case "query":
			query, ok = ivalue.(string)
			if !ok {
				return nil, fmt.Errorf("query %v is not a string", ivalue)
			}

		case "queries":
			iqueries, ok := ivalue.([]interface{})
			if !ok {
				return nil, fmt.Errorf("queries %v is not a list", ivalue)
			}
			for i, iquery := range iqueries {
				query, ok := iquery.(string)
				if !ok {
					return nil, fmt.Errorf("query %d (%v) is not a string", i+1, iquery)
				}
				queries = append(queries, query)
			}

		case "metrics":
			rm, err := getMetrics(ivalue)
			if err != nil {
				return nil, err
			}
			resultmap = rm

		case "resultsets":
			rms, err := getResultSets(ivalue)
			if err != nil {
				return nil, err
			}
			resultmaps = rms
		default:
			return nil, fmt.Errorf("unknown recipe key %v", key)

		}
	}

	if resultmaps == nil && resultmap == nil {
		return nil, fmt.Errorf("no resultsets/metrics specified")
	}

	if resultmaps != nil && resultmap != nil {
		return nil, fmt.Errorf("cannot specify both resultsets and metrics")
	}
	if query == "" {
		query = "select * from " + namespace
	}
	if queries == nil {
		queries = []string{query}
	}

	if resultmaps == nil {
		resultmaps = recipes.MultiResultMap{recipes.NamedResultMap{
			ResultMap: resultmap,
			Name:      "metrics",
		}}
	}

	if rangeover != "" {
		var tmplQueries []*template.Template
		for i, query := range queries {
			t, err := template.New(namespace + strconv.Itoa(i)).Parse(query)
			if err != nil {
				return nil, fmt.Errorf("error parsing template for query %d: %v", i, err)
			}
			tmplQueries = append(tmplQueries, t)
		}
		return &recipes.MetricQueryRecipeTemplated{
			MetricQueryRecipeBase: &recipes.MetricQueryRecipeBase{
				Namespace:  prefix + "_" + namespace,
				Resultmaps: resultmaps,
			},
			Rangequery: rangeover,
			Queries:    tmplQueries,
		}, nil
	}
	return &recipes.MetricQueryRecipeSimple{
		MetricQueryRecipeBase: &recipes.MetricQueryRecipeBase{
			Namespace:  prefix + "_" + namespace,
			Resultmaps: resultmaps,
		},
		Queries: queries,
	}, nil

}

func getMetrics(value interface{}) (recipes.ResultMap, error) {
	imetrics, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("metrics %v is not a list", value)
	}

	metric_map := make(recipes.ResultMap)
	for i, c := range imetrics {
		var err error
		metname, cmap, err := getMetric(c)
		if err != nil {
			return nil, fmt.Errorf("metric %d (%q) invalid: %v", i+1, metname, err)
		}
		metric_map[metname] = *cmap
	}
	return metric_map, nil
}

func getMetric(imetric interface{}) (string, *common.ColumnMapping, error) {
	column, ok := imetric.(map[interface{}]interface{})
	if !ok {
		return "", nil, fmt.Errorf("not a map")
	}

	if len(column) != 1 {
		return "", nil, fmt.Errorf("map does not have 1 member but %d", len(column))
	}

	var cmap common.ColumnMapping
	var name string

	for n, a := range column {
		name, ok = n.(string)
		if !ok {
			return "", nil, fmt.Errorf("non-string name %v", n)
		}
		name = strings.Replace(name, " ", "_", -1)

		attrs, ok := a.(map[interface{}]interface{})
		if !ok {
			return "", nil, fmt.Errorf("non-map value %v", a)
		}
		for iattr_key, iattr_val := range attrs {
			attr_key, ok := iattr_key.(string)
			if !ok {
				return "", nil, fmt.Errorf("non-string attribute key %v", iattr_key)
			}
			attr_val, ok := iattr_val.(string)
			if !ok {
				return "", nil, fmt.Errorf("non-string attribute value %v for key %q", iattr_val, attr_key)
			}

			switch attr_key {
			case "usage":
				usage, err := common.StringToColumnUsage(attr_val)
				if err != nil {
					return "", nil, err
				}
				cmap.Usage = usage
			case "description":
				cmap.Description = attr_val
			case "regexp":
				// TODO handle bad regular expressions without panic
				cmap.Regexp = regexp.MustCompile(attr_val)
			case "value":
				cmap.Fixedval = attr_val
			default:
				return "", nil, fmt.Errorf("unknown key %q", attr_key)
			}
		}
		if cmap.Usage == 0 {
			return "", nil, fmt.Errorf("no usage specified")
		}
		if cmap.Usage != common.DISCARD && cmap.Usage != common.LABEL && len(cmap.Description) == 0 {
			return "", nil, fmt.Errorf("no description specified for non-DISCARD/LABEL usage")
		}
		if cmap.Usage == common.FIXED && len(cmap.Fixedval) == 0 {
			return "", nil, fmt.Errorf("no value specified for FIXED usage")
		}

		// TODO add support for mappings
		cmap.Mapping = nil

	}
	return name, &cmap, nil
}

func getResultSets(ivalue interface{}) (recipes.MultiResultMap, error) {
	rss, ok := ivalue.([]interface{})
	if !ok {
		return nil, fmt.Errorf("resultsets %v is not a list", ivalue)
	}

	var resultmaps recipes.MultiResultMap
	for i, r := range rss {
		rs, ok := r.(map[interface{}]interface{})
		if !ok {
			return nil, fmt.Errorf("resultset %d (%v) is not a map", i, r)
		}
		if len(rs) != 1 {
			return nil, fmt.Errorf("resultset %d: only one element allowed per resultset", i)
		}
		for irname, irvalue := range rs {
			rname, ok := irname.(string)
			if !ok {
				return nil, fmt.Errorf("resultset %d: map key %v is not a string", i, irname)
			}
			if rname == "discard" {
				if irvalue != nil {
					return nil, fmt.Errorf("resultset %d: no metrics may be specified for the special resultset name 'discard'", i)
				}
				resultmaps = append(resultmaps, recipes.NamedResultMap{Name: "discard"})
			} else {
				rm, err := getMetrics(irvalue)
				if err != nil {
					return nil, fmt.Errorf("resultset %d (%s): %v: %v", i, rname, err, irvalue)
				}
				nrm := recipes.NamedResultMap{Name: rname, ResultMap: rm}
				resultmaps = append(resultmaps, nrm)
			}
		}
	}
	return resultmaps, nil
}

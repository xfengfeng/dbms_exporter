package config

import (
	"fmt"
	"io/ioutil"

	"github.com/ncabatoff/dbms_exporter/common"
	"gopkg.in/yaml.v2"
)

func DumpMaps(recipes []common.MetricQueryRecipe) {
	for _, recipe := range recipes {
		fmt.Println(recipe.Basename)
		fmt.Printf("  %s\n", recipe.SqlQuery)
		for column, details := range recipe.ResultMap {
			fmt.Printf("    %-40s %v\n", column, details)
		}
		fmt.Println()
	}
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
			default:
				return "", nil, fmt.Errorf("unknown key %q", attr_key)
			}
		}
		if cmap.Usage == 0 {
			return "", nil, fmt.Errorf("no usage specified")
		}
		if cmap.Usage != common.DISCARD && cmap.Usage != common.LABEL && len(cmap.Description) == 0 {
			return "", nil, fmt.Errorf("no description specifiedf for non-DISCARD/LABEL usage")
		}

		// TODO add support for mappings
		cmap.Mapping = nil

	}
	return name, &cmap, nil
}

func getRecipe(basename string, specs interface{}) (*common.MetricQueryRecipe, error) {
	var ok bool
	yamlRecipe, ok := specs.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("bad recipe: not a map")
	}

	query := "select * from " + basename
	metric_map := make(map[string]common.ColumnMapping)

	for ikey, ivalue := range yamlRecipe {
		key, ok := ikey.(string)
		if !ok {
			return nil, fmt.Errorf("key %v is not a string", ikey)
		}

		switch key {
		case "query":
			query, ok = ivalue.(string)
			if !ok {
				return nil, fmt.Errorf("query %v is not a string", ivalue)
			}

		case "metrics":
			imetrics, ok := ivalue.([]interface{})
			if !ok {
				return nil, fmt.Errorf("metrics %v is not a list", ivalue)
			}
			for i, c := range imetrics {
				var err error
				metname, cmap, err := getMetric(c)
				if err != nil {
					return nil, fmt.Errorf("metric %d (%q) invalid: %v", i+1, metname, err)
				}
				metric_map[metname] = *cmap
			}
		default:
			return nil, fmt.Errorf("unknown recipe key %v", key)

		}
	}
	if len(metric_map) == 0 {
		return nil, fmt.Errorf("no metrics defined")
	}

	return &common.MetricQueryRecipe{
		Basename:  basename,
		SqlQuery:  query,
		ResultMap: metric_map,
	}, nil
}

func ReadRecipesFile(queriesPath string) ([]common.MetricQueryRecipe, error) {
	content, err := ioutil.ReadFile(queriesPath)
	if err != nil {
		return nil, err
	}
	return GetRecipes(string(content))
}

func GetRecipes(content string) ([]common.MetricQueryRecipe, error) {
	var yamldata map[string]interface{}

	err := yaml.Unmarshal([]byte(content), &yamldata)
	if err != nil {
		return nil, err
	}

	var recipes []common.MetricQueryRecipe
	for basename, specs := range yamldata {
		recipe, err := getRecipe(basename, specs)
		if err != nil {
			return nil, fmt.Errorf("unable to parse recipe %q: %v", basename, err)
		}
		recipes = append(recipes, *recipe)
	}

	return recipes, nil
}

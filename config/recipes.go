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
		fmt.Printf("  %s\n", recipe.Sqlquery)
		for column, details := range recipe.Resultmap {
			fmt.Printf("    %-40s %v\n", column, details)
		}
		fmt.Println()
	}
}

func getRecipe(basename string, specs interface{}) (*common.MetricQueryRecipe, error) {
	query := "select * from " + basename
	metric_map := make(map[string]common.ColumnMapping)

	for key, value := range specs.(map[interface{}]interface{}) {
		switch key.(string) {
		case "query":
			query = value.(string)

		case "metrics":
			for _, c := range value.([]interface{}) {
				column := c.(map[interface{}]interface{})

				for n, a := range column {
					var cmap common.ColumnMapping

					name := n.(string)

					for attr_key, attr_val := range a.(map[interface{}]interface{}) {
						switch attr_key.(string) {
						case "usage":
							usage, err := common.StringToColumnUsage(attr_val.(string))
							if err != nil {
								return nil, err
							}
							cmap.Usage = usage
						case "description":
							cmap.Description = attr_val.(string)
						}
					}

					cmap.Mapping = nil

					metric_map[name] = cmap
				}
			}
		}
	}
	return &common.MetricQueryRecipe{
		Basename:  basename,
		Sqlquery:  query,
		Resultmap: metric_map,
	}, nil
}

func GetRecipes(queriesPath string) ([]common.MetricQueryRecipe, error) {
	var yamldata map[string]interface{}

	content, err := ioutil.ReadFile(queriesPath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(content, &yamldata)
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

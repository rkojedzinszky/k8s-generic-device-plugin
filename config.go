
package main

import (
	"fmt"
	"io/ioutil"
	yaml "gopkg.in/yaml.v2"
)

func readconfig(configFile string) (*Resource, error) {
	buf, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var resource Resource
	if err = yaml.UnmarshalStrict(buf, &resource); err != nil {
		return nil, err
	}

	idMap := make(map[string]bool)
	for _, set := range resource.Sets {
		if _, ok := idMap[set.ID]; ok {
			return nil, fmt.Errorf("%s: ID %s already defined", resource.Name, set.ID)
		}
		idMap[set.ID] = true
	}

	return &resource, nil
}


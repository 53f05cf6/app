package main

import (
	"fmt"
)

type Info struct {
	Name  string
	Value string
}

func (i Info) String() string {
	return fmt.Sprintf("%s:%s\n", i.Name, i.Value)
}

type InfoList []Info

func (il InfoList) String() (str string) {
	for i, info := range il {
		str += fmt.Sprintf("%d. %s", i+1, info.String())
	}

	return
}

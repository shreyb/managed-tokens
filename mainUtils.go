package main

import "github.com/shreyb/managed-tokens/worker"

func loadServiceConfigsIntoChannel(chanToLoad chan<- *worker.ServiceConfig, serviceConfigs []*worker.ServiceConfig) {
	defer close(chanToLoad)
	for _, sc := range serviceConfigs {
		chanToLoad <- sc
	}
}

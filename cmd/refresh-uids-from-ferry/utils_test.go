package main

import "github.com/spf13/viper"

func reset() {
	viper.Reset()
	devEnvironmentLabel = ""
}

package main

import (
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type ferryResponse struct {
	FerryStatus string   `json:"ferry_status"`
	FerryError  []string `json:"ferry_error"`
	FerryOutput struct {
		ExpirationDate string `json:"expirationdate"`
		FullName       string `json:"fullname"`
		GroupAccount   bool   `json:"groupaccount"`
		Status         bool   `json:"status"`
		Uid            int    `json:"uid"`
		VOPersonId     string `json:"vopersonid"`
	} `json:"ferry_output"`
}

type entryFromFerry struct {
	Username string
	Uid      int
}

func getAllAccountsFromConfig() []string {
	s := make([]string, 0)

	for experiment := range viper.GetStringMap("experiments") {
		roleConfigPath := "experiments." + experiment + ".roles"
		for role := range viper.GetStringMap(roleConfigPath) {
			accountConfigPath := roleConfigPath + "." + role + ".account"
			account := viper.GetString(accountConfigPath)
			log.WithField("account", account).Info("Found account")
			s = append(s, account)
		}
	}
	return s
}

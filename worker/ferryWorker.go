package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"text/template"

	"github.com/shreyb/managed-tokens/db"
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

const ferryRequestDefaultTimeoutStr string = "30s"

var ferryURLUIDTemplate = template.Must(template.New("ferry").Parse("{{.Hostname}}:{{.Port}}/{{.API}}?username={{.Username}}"))

// UIDEntryFromFerry blah blah.  Implements utils.FerryUIDDatum
type UIDEntryFromFerry struct {
	username string
	uid      int
}

func (u *UIDEntryFromFerry) String() string {
	return fmt.Sprintf("Username: %s, Uid: %d", u.username, u.uid)
}

func (u *UIDEntryFromFerry) Username() string {
	return u.username
}

func (u *UIDEntryFromFerry) Uid() int {
	return u.uid
}

type ferryUIDResponse struct {
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

func GetFERRYUIDData(ctx context.Context, username string, ferryHost string, ferryPort int,
	requestRunnerWithAuthMethodFunc func(ctx context.Context, url, verb string) (*http.Response, error),
	ferryDataChan chan<- db.FerryUIDDatum) (*UIDEntryFromFerry, error) {
	entry := UIDEntryFromFerry{}

	ferryRequestTimeout, err := utils.GetProperTimeoutFromContext(ctx, ferryRequestDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse ferryRequest timeout")
	}

	ferryAPIConfig := struct{ Hostname, Port, API, Username string }{
		Hostname: ferryHost,
		Port:     strconv.Itoa(ferryPort),
		API:      "getUserInfo",
		Username: username,
	}

	var b strings.Builder
	if err := ferryURLUIDTemplate.Execute(&b, ferryAPIConfig); err != nil {
		fatalErr := fmt.Errorf("could not execute ferryURLUID template: %w", err)
		log.Fatal(fatalErr)
	}

	ferryRequestCtx, ferryRequestCancel := context.WithTimeout(ctx, ferryRequestTimeout)
	defer ferryRequestCancel()
	resp, err := requestRunnerWithAuthMethodFunc(ferryRequestCtx, b.String(), "GET")
	if err != nil {
		log.WithField("account", username).Error("Attempt to get UID from FERRY failed")
		if err2 := ctx.Err(); err2 == context.DeadlineExceeded {
			log.WithField("account", username).Error("context deadline exceeded")
			return &entry, err2
		}
		log.WithField("account", username).Error(err)
		return &entry, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithField("account", username).Error("Could not read body from HTTP response")
		return &entry, err
	}

	parsedResponse := ferryUIDResponse{}
	if err := json.Unmarshal(body, &parsedResponse); err != nil {
		log.WithField("account", username).Errorf("Could not unmarshal FERRY response: %s", err)
		return &entry, err
	}

	if parsedResponse.FerryStatus == "failure" {
		log.WithField("account", username).Errorf("FERRY server error: %s", parsedResponse.FerryError)
		return &entry, errors.New("unspecified FERRY error.  Check logs")
	}

	entry.username = username
	entry.uid = parsedResponse.FerryOutput.Uid

	log.WithField("account", username).Info("Successfully got data from FERRY")
	return &entry, nil
}

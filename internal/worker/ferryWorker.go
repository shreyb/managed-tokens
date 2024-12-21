// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/metrics"
)

const ferryRequestDefaultTimeoutStr string = "30s"
const ferryUserUIDAPI string = "getUserInfo"

var ferryURLUIDTemplate = template.Must(template.New("ferry").Parse("{{.Hostname}}:{{.Port}}/{{.API}}?username={{.Username}}"))

var (
	ferryRequestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "managed_tokens",
		Name:      "ferry_request_duration_seconds",
		Help:      "The amount of time it took in seconds to make a request to FERRY and receive the response",
	})
	ferryRequestErrorCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "ferry_request_error_count",
		Help:      "The number of requests to FERRY that failed",
	})
)

func init() {
	metrics.MetricsRegistry.MustRegister(ferryRequestDuration)
	metrics.MetricsRegistry.MustRegister(ferryRequestErrorCount)
}

// UIDEntryFromFerry is an entry that represents data returned from the FERRY getUserInfo API.  It implements utils.FerryUIDDatum
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

// ferryUIDResponse holds the unmarshalled JSON data from a query to FERRY's getUserInfo API
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

// GetFERRYUIDData queries FERRY for user information.  This func abstracts away the actual details of formulating
// the HTTP request, including authentication, headers, etc.  All of these details must be provided in the requestRunnerWithAuthMethodFunc func
// that is passed in.  An example from a caller could look like this:
//
// // myauthfunc sends a request to a server without any authentication
// func myauthfunc(ctx context.Context, url, verb string) (*http.Response, error){
//
//			client := &http.Client{}
//			req, err := http.NewRequest(verb, url, nil)
//			if err != nil {}
//			resp, err := client.Do(req)
//			return resp, err
//	}
//
// ctx := context.Background()
// myentry, err := GetFERRYUIDData(ctx, "user1", "https://example.com", 0, myauthfunc)
func GetFERRYUIDData(ctx context.Context, username string, ferryHost string, ferryPort int,
	requestRunnerWithAuthMethodFunc func(ctx context.Context, url, verb string) (*http.Response, error),
	ferryDataChan chan<- db.FerryUIDDatum) (*UIDEntryFromFerry, error) {
	ctx, span := tracer.Start(ctx, "GetFERRYUIDData")
	span.SetAttributes(
		attribute.String("username", username),
		attribute.String("ferryHost", ferryHost),
		attribute.Int("ferryPort", ferryPort),
	)
	defer span.End()

	funcLogger := log.WithFields(log.Fields{
		"account":   username,
		"ferryHost": ferryHost,
		"ferryPort": ferryPort,
	})

	entry := UIDEntryFromFerry{}

	ferryRequestTimeout, defaultUsed, err := contextStore.GetProperTimeout(ctx, ferryRequestDefaultTimeoutStr)
	if err != nil {
		span.SetStatus(codes.Error, "Could not parse ferryRequest timeout")
		funcLogger.Fatal("Could not parse ferryRequest timeout")
	}
	if defaultUsed {
		funcLogger.Debug("Using default timeout for FERRY request")
	}

	ferryAPIConfig := struct{ Hostname, Port, API, Username string }{
		Hostname: ferryHost,
		Port:     strconv.Itoa(ferryPort),
		API:      ferryUserUIDAPI,
		Username: username,
	}

	var b strings.Builder
	if err := ferryURLUIDTemplate.Execute(&b, ferryAPIConfig); err != nil {
		fatalErr := fmt.Errorf("could not execute ferryURLUID template: %w", err)
		span.SetStatus(codes.Error, fatalErr.Error())
		funcLogger.Fatal(fatalErr)
	}

	startRequest := time.Now()
	ferryRequestCtx, ferryRequestCancel := context.WithTimeout(ctx, ferryRequestTimeout)
	defer ferryRequestCancel()
	resp, err := requestRunnerWithAuthMethodFunc(ferryRequestCtx, b.String(), "GET")
	if err != nil {
		msg := "Attempt to get UID from FERRY failed"
		ferryRequestErrorCount.Inc()
		if err2 := ctx.Err(); errors.Is(err2, context.DeadlineExceeded) {
			retErr := fmt.Errorf("%s: timeout error: %w", msg, ctx.Err())
			logErrorWithTracing(funcLogger, span, retErr)
			return &entry, retErr
		}
		err = fmt.Errorf("%s: %w", msg, err)
		logErrorWithTracing(funcLogger, span, err)
		return &entry, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ferryRequestErrorCount.Inc()
		err = fmt.Errorf("could not read body from HTTP response: %w", err)
		logErrorWithTracing(funcLogger, span, err)
		return &entry, err
	}

	parsedResponse := ferryUIDResponse{}
	if err := json.Unmarshal(body, &parsedResponse); err != nil {
		err = fmt.Errorf("could not unmarshal FERRY response: %w", err)
		logErrorWithTracing(funcLogger, span, err)
		return &entry, err
	}

	if parsedResponse.FerryStatus == "failure" {
		ferryRequestErrorCount.Inc()
		err = fmt.Errorf("FERRY server error: %s", parsedResponse.FerryError)
		logErrorWithTracing(funcLogger, span, err)
		return &entry, errors.New("unspecified FERRY error.  Check logs")
	}

	entry.username = username
	entry.uid = parsedResponse.FerryOutput.Uid

	logSuccessWithTracing(funcLogger, span, "Successfully got data from FERRY")
	dur := time.Since(startRequest).Seconds()
	ferryRequestDuration.Observe(dur)
	return &entry, nil
}

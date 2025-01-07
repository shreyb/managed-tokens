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

package db

import (
	"context"
	"fmt"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fermitools/managed-tokens/internal/contextStore"
	"github.com/fermitools/managed-tokens/internal/tracing"
)

// SQL Statements
var (
	insertIntoUIDTableStatement = `
	INSERT INTO uids(username, uid)
	VALUES
		(?, ?)
	ON CONFLICT(username) DO
		UPDATE SET uid = ?;
	`
	confirmUIDsInTableStatement = `
	SELECT username, uid FROM uids;
	`
	getUIDbyUsernameStatement = `
	SELECT uid
	FROM uids
	WHERE username = ? ;
	`
)

// FerryUIDDatum represents a piece of data from FERRY that encompasses username to UID mapping
type FerryUIDDatum interface {
	Username() string
	Uid() int
	String() string
}

// Implements both insertValues and FERRYUIDDatum interfaces
type ferryUidDatum struct {
	username string
	uid      int
}

func (f *ferryUidDatum) Username() string { return f.username }
func (f *ferryUidDatum) Uid() int         { return f.uid }
func (f *ferryUidDatum) String() string   { return fmt.Sprintf("%s, %d", f.username, f.uid) }

// f.uid is doubled here because of the ON CONFLICT...UPDATE clause
func (f *ferryUidDatum) insertValues() []any { return []any{f.username, f.uid, f.uid} }

func (f *ferryUidDatum) unpackDataRow(resultRow []any) (dataRowUnpacker, error) {
	// Make sure we have the right number of values
	if len(resultRow) != 2 {
		return nil, &errDatabaseDataWrongStructure{resultRow, "Expected 2 values"}
	}
	// Type check each element
	usernameVal, usernameOk := resultRow[0].(string)
	uidVal, uidOk := resultRow[1].(int64)
	if !(usernameOk && uidOk) {
		errVal := []any{resultRow[0], resultRow[1]}
		return nil, &errDatabaseDataWrongType{errVal, "expected (string, int64)"}
	}
	return &ferryUidDatum{usernameVal, int(uidVal)}, nil
}

// InsertUidsIntoTableFromFERRY takes a slice of FERRYUIDDatum and inserts the data it represents into the FERRYUIDDatabase.
// If the username in a FERRYUIDDatum object already exists in the database, this method will overwrite the database record
// with the information in the FERRYUIDDatum
func (m *ManagedTokensDatabase) InsertUidsIntoTableFromFERRY(ctx context.Context, ferryData []FerryUIDDatum) error {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.InsertUidsIntoTableFromFERRY")
	span.SetAttributes(attribute.String("dbLocation", m.filename))
	defer span.End()

	ferryUIDDatumSlice := ferryUIDDatumInterfaceSlicetoInsertValuesSlice(ferryData)

	if debugEnabled {
		debugLogger.Debug("Inserting uids into database")
	}
	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoUIDTableStatement, ferryUIDDatumSlice); err != nil {
		err = fmt.Errorf("could not update uids table in database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	tracing.LogSuccessWithTrace(span, "Updated uid table in database with FERRY data")
	return nil
}

// ConfirmUIDsInTable returns all the user to UID mapping information in the ManagedTokensDatabase in the form of
// a FERRYUIDDatum slice
func (m *ManagedTokensDatabase) ConfirmUIDsInTable(ctx context.Context) ([]FerryUIDDatum, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.ConfirmUIDsInTable")
	span.SetAttributes(attribute.String("dbLocation", m.filename))
	defer span.End()

	// dataConverted := make([]FerryUIDDatum, 0)
	if debugEnabled {
		debugLogger.Debug("Getting usernames and uids from database")
	}
	data, err := getValuesTransactionRunner(ctx, m.db, confirmUIDsInTableStatement)
	if err != nil {
		err = fmt.Errorf("could not get usernames and uids from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	if len(data) == 0 {
		span.AddEvent("No uids in database")
		return nil, nil
	}

	// Unpack data
	unpackedData, err := unpackData[*ferryUidDatum](data)
	if err != nil {
		err = fmt.Errorf("could not unpack UID Data row: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}
	convertedData := make([]FerryUIDDatum, 0, len(unpackedData))
	for _, elt := range unpackedData {
		convertedData = append(convertedData, elt)
	}
	tracing.LogSuccessWithTrace(span, "Got usernames and uids from database")
	return convertedData, nil
}

// GetUIDByUsername queries the ManagedTokensDatabase for a UID, given a username
func (m *ManagedTokensDatabase) GetUIDByUsername(ctx context.Context, username string) (int, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetUIDByUsername")
	span.SetAttributes(
		attribute.String("dbLocation", m.filename),
		attribute.String("username", username),
	)

	dbTimeout, _, err := contextStore.GetProperTimeout(ctx, dbDefaultTimeoutStr)
	if err != nil {
		err = fmt.Errorf("could not parse db timeout duration: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return 0, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	stmt, err := m.db.Prepare(getUIDbyUsernameStatement)
	if err != nil {
		err = fmt.Errorf("could not prepare query to get UID: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return 0, err
	}
	defer stmt.Close()

	var uid int
	if debugEnabled {
		debugLogger.Debug(fmt.Sprintf("Getting UID from database. Query %s, Username: %s", getUIDbyUsernameStatement, username))
	}
	err = stmt.QueryRowContext(dbContext, username).Scan(&uid)
	if err != nil {
		err = fmt.Errorf("could not execute query to get UID: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return 0, err
	}

	tracing.LogSuccessWithTrace(span, "Got UID from database",
		tracing.KeyValueForLog{Key: "username", Value: username},
		tracing.KeyValueForLog{Key: "uid", Value: strconv.Itoa(uid)},
	)
	return uid, nil
}

// Helper funcs

func ferryUIDDatumInterfaceSlicetoInsertValuesSlice(data []FerryUIDDatum) []insertValues {
	sl := make([]insertValues, 0, len(data))
	for _, ferryDatum := range data {
		sl = append(sl,
			&ferryUidDatum{ferryDatum.Username(), ferryDatum.Uid()},
		)
	}
	return sl
}

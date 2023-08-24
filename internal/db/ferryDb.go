package db

import (
	"context"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/utils"
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
		msg := "uid data has wrong structure"
		log.Errorf("%s: %v", msg, resultRow)
		return nil, errDatabaseDataWrongStructure
	}
	// Type check each element
	usernameVal, usernameOk := resultRow[0].(string)
	uidVal, uidOk := resultRow[1].(int64)
	if !(usernameOk && uidOk) {
		msg := "uid query result datum has wrong type.  Expected (string, int64)"
		log.Errorf("%s: got (%T, %T)", msg, resultRow[0], resultRow[1])
		return nil, errDatabaseDataWrongType
	}
	log.Debugf("Got UID row: %s, %d", usernameVal, uidVal)
	return &ferryUidDatum{usernameVal, int(uidVal)}, nil
}

// InsertUidsIntoTableFromFERRY takes a slice of FERRYUIDDatum and inserts the data it represents into the FERRYUIDDatabase.
// If the username in a FERRYUIDDatum object already exists in the database, this method will overwrite the database record
// with the information in the FERRYUIDDatum
func (m *ManagedTokensDatabase) InsertUidsIntoTableFromFERRY(ctx context.Context, ferryData []FerryUIDDatum) error {
	funcLogger := log.WithField("dbLocation", m.filename)

	ferryUIDDatumSlice := ferryUIDDatumInterfaceSlicetoInsertValuesSlice(ferryData)

	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoUIDTableStatement, ferryUIDDatumSlice); err != nil {
		funcLogger.Error("Could not update uids table in database")
		return err
	}
	funcLogger.Debug("Updated uid table in database with FERRY data")
	return nil
}

// ConfirmUIDsInTable returns all the user to UID mapping information in the ManagedTokensDatabase in the form of
// a FERRYUIDDatum slice
func (m *ManagedTokensDatabase) ConfirmUIDsInTable(ctx context.Context) ([]FerryUIDDatum, error) {
	funcLogger := log.WithField("dbLocation", m.filename)

	// dataConverted := make([]FerryUIDDatum, 0)
	data, err := getValuesTransactionRunner(ctx, m.db, confirmUIDsInTableStatement)
	if err != nil {
		funcLogger.Error("Could not get usernames and uids from database")
		return nil, err
	}

	if len(data) == 0 {
		funcLogger.Debug("No uids in database")
		return nil, nil
	}

	// Unpack data
	unpackedData, err := unpackData[*ferryUidDatum](data)
	if err != nil {
		funcLogger.Error("Error unpacking UID Data row")
		return nil, err
	}
	convertedData := make([]FerryUIDDatum, 0, len(unpackedData))
	for _, elt := range unpackedData {
		convertedData = append(convertedData, elt)
	}
	return convertedData, nil
}

// GetUIDByUsername queries the ManagedTokensDatabase for a UID, given a username
func (m *ManagedTokensDatabase) GetUIDByUsername(ctx context.Context, username string) (int, error) {
	funcLogger := log.WithField("dbLocation", m.filename)
	var uid int

	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return uid, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	stmt, err := m.db.Prepare(getUIDbyUsernameStatement)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return uid, dbContext.Err()
		}
		funcLogger.Errorf("Could not prepare query to get UID: %s", err)
		return uid, err
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(dbContext, username).Scan(&uid)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return uid, dbContext.Err()
		}
		funcLogger.Errorf("Could not execute query to get UID: %s", err)
		return uid, err
	}
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

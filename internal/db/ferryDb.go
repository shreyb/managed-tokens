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

func (f *ferryUidDatum) values() []any { return []any{f.username, f.uid, f.uid} }

func (f *ferryUidDatum) Username() string { return f.username }
func (f *ferryUidDatum) Uid() int         { return f.uid }
func (f *ferryUidDatum) String() string   { return fmt.Sprintf("%s, %d", f.username, f.uid) }

// InsertUidsIntoTableFromFERRY takes a slice of FERRYUIDDatum and inserts the data it represents into the FERRYUIDDatabase.
// If the username in a FERRYUIDDatum object already exists in the database, this method will overwrite the database record
// with the information in the FERRYUIDDatum
func (m *ManagedTokensDatabase) InsertUidsIntoTableFromFERRY(ctx context.Context, ferryData []FerryUIDDatum) error {
	ferryUIDDatumSlice := make([]insertValues, 0)
	for _, ferryDatum := range ferryData {
		ferryUIDDatumSlice = append(ferryUIDDatumSlice,
			&ferryUidDatum{ferryDatum.Username(), ferryDatum.Uid()},
		)
	}

	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoUIDTableStatement, ferryUIDDatumSlice); err != nil {
		log.Error("Could not update uids table in database")
		return err
	}
	log.Debug("Updated uid table in database with FERRY data")
	return nil
}

// ConfirmUIDsInTable returns all the user to UID mapping information in the ManagedTokensDatabase in the form of
// a FERRYUIDDatum slice
func (m *ManagedTokensDatabase) ConfirmUIDsInTable(ctx context.Context) ([]FerryUIDDatum, error) {
	dataConverted := make([]FerryUIDDatum, 0)
	data, err := getValuesTransactionRunner(ctx, m.db, confirmUIDsInTableStatement)
	if err != nil {
		log.Error("Could not get usernames and uids from database")
		return dataConverted, err
	}

	if len(data) == 0 {
		log.Debug("No uids in database")
		return dataConverted, nil
	}

	// Unpack data
	for _, resultRow := range data {
		// Make sure we have the right number of values
		if len(resultRow) != 2 {
			msg := "uid data has wrong structure"
			log.Errorf("%s: %v", msg, resultRow)
			return dataConverted, errDatabaseDataWrongStructure
		}
		// Type check each element
		usernameVal, usernameOk := resultRow[0].(string)
		uidVal, uidOk := resultRow[1].(int64)
		if !(usernameOk && uidOk) {
			msg := "uid query result datum has wrong type.  Expected (string, int)"
			log.Errorf("%s: got (%T, %T)", msg, usernameVal, uidVal)
			return dataConverted, errDatabaseDataWrongType
		}
		log.Debugf("Got UID row: %s, %d", usernameVal, uidVal)
		dataConverted = append(dataConverted, &ferryUidDatum{usernameVal, int(uidVal)})
	}
	return dataConverted, nil
}

// GetUIDByUsername queries the ManagedTokensDatabase for a UID, given a username
func (m *ManagedTokensDatabase) GetUIDByUsername(ctx context.Context, username string) (int, error) {
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
			log.Error("Context timeout")
			return uid, dbContext.Err()
		}
		log.Errorf("Could not prepare query to get UID: %s", err)
		return uid, err
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(dbContext, username).Scan(&uid)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return uid, dbContext.Err()
		}
		log.Errorf("Could not execute query to get UID: %s", err)
		return uid, err
	}
	return uid, nil
}

package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/utils"
)

// SQL Statements
var (
	createUIDTableStatement = `
	CREATE TABLE uids (
	username STRING NOT NULL PRIMARY KEY,
	uid INTEGER NOT NULL
	);
	`
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

// FERRYUIDDatabase is a database in which FERRY username to uid mappings are stored
type FERRYUIDDatabase struct {
	filename string
	db       *sql.DB
}

// OpenOrCreateFERRYUIDDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the database already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func OpenOrCreateFERRYUIDDatabase(filename string) (*FERRYUIDDatabase, error) {
	var err error
	f := FERRYUIDDatabase{filename: filename}
	err = openOrCreateDatabase(&f)
	if err != nil {
		log.WithField("filename", f.filename).Error("Could not create or open FERRYUIDDatabase")
	}
	return &f, err
}

// Filename returns the path to the file holding the database
func (f *FERRYUIDDatabase) Filename() string {
	return f.filename
}

// Database returns the underlying SQL database of the FERRYUIDDatabase
func (f *FERRYUIDDatabase) Database() *sql.DB {
	return f.db
}

// Open opens the underlying db for the FERRYUIDDatabase and assigns the resultant *sql.DB object to the FERRYUIDDatabase
func (f *FERRYUIDDatabase) Open() error {
	var err error
	f.db, err = sql.Open("sqlite3", f.filename)
	if err != nil {
		msg := "Could not open the database file"
		log.WithField("filename", f.filename).Errorf("%s: %s", msg, err)
		return &databaseOpenError{msg}
	}
	return nil
}

// Close closes the FERRYUIDDatabase
func (f *FERRYUIDDatabase) Close() error {
	return f.db.Close()
}

// initialize prepares a new FERRYUIDDatabase for use and returns a pointer to the underlying sql.DB
func (f *FERRYUIDDatabase) initialize() error {
	var err error
	if f.db, err = sql.Open("sqlite3", f.filename); err != nil {
		log.WithField("filename", f.filename).Error(err)
		return err
	}

	// Set our application ID
	if _, err := f.db.Exec(fmt.Sprintf("PRAGMA application_id=%d;", ApplicationId)); err != nil {
		log.WithField("filename", f.filename).Error(err)
		return err
	}

	// Create the UID table
	if err = f.createUidsTable(); err != nil {
		log.Error("Could not create the UID table in the FERRYUIDDatabase")
		return err
	}
	return nil
}

// FERRYUIDDatabse-specific functions

// createUidsTable creates a database table in the FERRYUIDDatabase that holds the username to UID mapping
func (f *FERRYUIDDatabase) createUidsTable() error {
	if _, err := f.db.Exec(createUIDTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created uid table in FERRYUIDDatabase")
	return nil
}

// InsertUidsIntoTableFromFERRY takes a slice of FERRYUIDDatum and inserts the data it represents into the FERRYUIDDatabase.
// If the username in a FERRYUIDDatum object already exists in the database, this method will overwrite the database record
// with the information in the FERRYUIDDatum
func (f *FERRYUIDDatabase) InsertUidsIntoTableFromFERRY(ctx context.Context, ferryData []FerryUIDDatum) error {
	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	tx, err := f.db.Begin()
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Errorf("Could not open transaction to database: %s", err)
		return err
	}

	insertStatement, err := tx.Prepare(insertIntoUIDTableStatement)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Errorf("Could not prepare INSERT statement to database: %s", err)
		return err
	}
	defer insertStatement.Close()

	for _, datum := range ferryData {
		_, err := insertStatement.ExecContext(dbContext, datum.Username(), datum.Uid(), datum.Uid())
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return dbContext.Err()
			}
			log.Errorf("Could not insert FERRY data into database: %s", err)
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Errorf("Could not commit transaction to database.  Rolling back.  Error: %s", err)
		return err
	}

	log.Info("Inserted FERRY data into database")
	return nil
}

// ConfirmUIDsInTable returns all the user to UID mapping information in the FERRYUIDDatabase in the form of
// a FERRYUIDDatum slice
func (f *FERRYUIDDatabase) ConfirmUIDsInTable(ctx context.Context) ([]FerryUIDDatum, error) {
	var username string
	var uid int
	data := make([]FerryUIDDatum, 0)
	log.Debug("Checking UIDs in DB table")

	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return data, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	rows, err := f.db.QueryContext(dbContext, confirmUIDsInTableStatement)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return data, dbContext.Err()
		}
		log.Errorf("Error running SELECT query against database: %s", err)
		return data, err
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&username, &uid)
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return data, dbContext.Err()
			}
			log.Errorf("Error retrieving results of SELECT query: %s", err)
			return data, err
		}
		data = append(data, &checkDatum{
			username: username,
			uid:      uid,
		})
		log.Debugf("Got row: %s, %d", username, uid)
	}
	err = rows.Err()
	if err != nil {
		log.Error(err)
		return data, err
	}
	return data, nil
}

// GetUIDByUsername queries the FERRYUIDDatabase for a UID, given a username
func (f *FERRYUIDDatabase) GetUIDByUsername(ctx context.Context, username string) (int, error) {
	var uid int

	dbTimeout, err := utils.GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Error("Could not parse db timeout duration")
		return uid, err
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	stmt, err := f.db.Prepare(getUIDbyUsernameStatement)
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

// checkDatum hold a username and uid. It implements the FERRYUIDDatum interface
type checkDatum struct {
	username string
	uid      int
}

func (c *checkDatum) Username() string { return c.username }
func (c *checkDatum) Uid() int         { return c.uid }
func (c *checkDatum) String() string   { return fmt.Sprintf("Username: %s, Uid: %d", c.username, c.uid) }

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/utils"
)

// Much thanks to K. Retzke - a lot of the boilerplate DB code is adapted from his fifemail application

const (
	// ApplicationId is used to uniquely identify a sqlite database as belonging to an application, rather than being a simple DB
	ApplicationId              = 0x5da82553
	dbDefaultTimeoutStr string = "10s"
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

type FerryUIDDatum interface {
	Username() string
	Uid() int
	String() string
}

type FERRYUIDDatabase struct {
	filename string
	db       *sql.DB
}

// OpenOrCreateDatabase opens a sqlite3 database for reading or writing, and returns a *FERRYUIDDatabase object.  If the databse already
// exists at the filename provided, it will open that database as long as the ApplicationId matches
func OpenOrCreateDatabase(filename string) (*FERRYUIDDatabase, error) {
	f := FERRYUIDDatabase{filename: filename}
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		err = f.initialize()
		if err != nil {
			msg := "Could not create new FERRYUIDDatabase"
			log.Error(msg)
			if err := os.Remove(filename); errors.Is(err, os.ErrNotExist) {
				log.Error("Could not remove corrupt database file.  Please do so manually")
				return &FERRYUIDDatabase{}, err
			}
			return &FERRYUIDDatabase{}, &ferryUIDDatabaseCreateError{msg}
		}
		log.WithField("filename", filename).Debug("Created new FERRYUIDDatabase")
	} else {
		f.db, err = sql.Open("sqlite3", filename)
		if err != nil {
			msg := "Could not open the UID database file"
			log.WithField("filename", filename).Errorf("%s: %s", msg, err)
		}
		log.WithField("filename", filename).Debug("FERRYUIDDatabase file already exists.  Will try to use it")
	}
	if err := f.check(); err != nil {
		msg := "FERRYUIDDatabase failed check"
		log.WithField("filename", filename).Error(msg)
		return &f, &ferryUIDDatabaseCheckError{msg}
	}
	log.WithField("filename", filename).Debug("FERRYUIDDatabase connection ready")
	return &f, nil
}

func (f *FERRYUIDDatabase) Close() error {
	return f.db.Close()
}

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

func (f *FERRYUIDDatabase) check() error {
	var dbApplicationId int
	err := f.db.QueryRow("PRAGMA application_id").Scan(&dbApplicationId)
	if err != nil {
		log.WithField("filename", f.filename).Error("Could not get application_id from FERRYUIDDatabase")
		return err
	}
	if dbApplicationId != ApplicationId {
		errMsg := fmt.Sprintf("Application IDs do not match.  Got %d, expected %d", dbApplicationId, ApplicationId)
		log.WithField("filename", f.filename).Errorf(errMsg)
		return errors.New(errMsg)
	}
	return nil
}

func (f *FERRYUIDDatabase) createUidsTable() error {
	if _, err := f.db.Exec(createUIDTableStatement); err != nil {
		log.Error(err)
		return err
	}
	log.Debug("Created uid table in FERRYUIDDatabase")
	return nil
}

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

type checkDatum struct {
	username string
	uid      int
}

func (c *checkDatum) Username() string { return c.username }
func (c *checkDatum) Uid() int         { return c.uid }
func (c *checkDatum) String() string   { return fmt.Sprintf("Username: %s, Uid: %d", c.username, c.uid) }

type ferryUIDDatabaseCreateError struct{ msg string }

func (f *ferryUIDDatabaseCreateError) Error() string { return f.msg }

type ferryUIDDatabaseCheckError struct{ msg string }

func (f *ferryUIDDatabaseCheckError) Error() string { return f.msg }

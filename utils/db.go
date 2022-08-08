package utils

import (
	"context"
	"database/sql"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

const dbDefaultTimeoutStr string = "10s"

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
}

func CreateUidsTableInDB(ctx context.Context, db *sql.DB) error {
	dbTimeout, err := GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse db timeout duration")
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	_, err = db.ExecContext(dbContext, createUIDTableStatement)
	if dbContext.Err() == context.DeadlineExceeded {
		log.Error("Context timeout")
		return dbContext.Err()
	}
	if err != nil {
		log.Error(err)
		return err
	}

	log.Info("Created new database and table")
	return nil
}

func InsertUidsIntoTableFromFERRY(ctx context.Context, db *sql.DB, ferryData []FerryUIDDatum) error {
	dbTimeout, err := GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse db timeout duration")
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	tx, err := db.Begin()
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Error(err)
		log.Error("Could not open transaction to database")
		return err
	}

	insertStatement, err := tx.Prepare(insertIntoUIDTableStatement)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Error(err)
		log.Error("Could not prepare INSERT statement to database")
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
			log.Error(err)
			log.Error("Could not insert FERRY data into database")
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return dbContext.Err()
		}
		log.Error(err)
		log.Error("Could not commit transaction to database.  Rolling back.")
		return err
	}

	log.Info("Inserted data into database")
	return nil
}

func ConfirmUIDsInTable(ctx context.Context, db *sql.DB) (int, error) {
	var username string
	var uid int
	var rowsCount int

	dbTimeout, err := GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse db timeout duration")
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	rowsOut := make([][]string, 0)
	rows, err := db.QueryContext(dbContext, confirmUIDsInTableStatement)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return rowsCount, dbContext.Err()
		}
		log.Error("Error running SELECT query against database")
		log.Error(err)
		return rowsCount, err
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&username, &uid)
		if err != nil {
			if dbContext.Err() == context.DeadlineExceeded {
				log.Error("Context timeout")
				return rowsCount, dbContext.Err()
			}
			log.Error("Error retrieving results of SELECT query")
			log.Error(err)
			return rowsCount, err
		}
		rowsOut = append(rowsOut, []string{
			username,
			strconv.Itoa(uid),
		})
		rowsCount += 1
	}
	err = rows.Err()
	if err != nil {
		log.Error(err)
		return rowsCount, err
	}
	log.Info("UID output: ", rowsOut)
	return rowsCount, nil
}

func GetUIDByUsername(ctx context.Context, db *sql.DB, username string) (int, error) {
	var uid int

	dbTimeout, err := GetProperTimeoutFromContext(ctx, dbDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse db timeout duration")
	}
	dbContext, dbCancel := context.WithTimeout(ctx, dbTimeout)
	defer dbCancel()

	stmt, err := db.Prepare(getUIDbyUsernameStatement)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return uid, dbContext.Err()
		}
		log.Error("Could not prepare query to get UID")
		log.Error(err)
		return uid, err
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(dbContext, username).Scan(&uid)
	if err != nil {
		if dbContext.Err() == context.DeadlineExceeded {
			log.Error("Context timeout")
			return uid, dbContext.Err()
		}
		log.Error("Could not execute query to get UID")
		log.Error(err)
		return uid, err
	}
	return uid, nil
}
